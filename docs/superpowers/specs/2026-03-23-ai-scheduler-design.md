# AI Scheduler Design

**Date:** 2026-03-23
**Status:** Approved
**Scope:** v1 rule-based AI scheduler embedded in the SmartHPA controller

---

## Overview

The AI scheduler automatically generates and applies time-window scaling triggers for SmartHPA resources. It uses historical Prometheus metrics to detect recurring traffic patterns and writes updated triggers directly to the SmartHPA CR — no human intervention required.

---

## Architecture

The scheduler is implemented as an extension of the existing `Scheduler` struct in `internal/scheduler/`. It adds two loops per SmartHPA resource with `aiScheduler.enabled: true`:

1. **Periodic loop** — runs on a configurable interval (default 1h). Pulls historical Prometheus data, runs pattern analysis, and patches the SmartHPA CR's `spec.triggers` using server-side apply with field manager `smarthpa-ai-scheduler`.

2. **Deviation loop** — runs on a shorter interval (default 5m). Compares the current replica count (fetched from the Kubernetes HPA object via the controller cache, not Prometheus) against the active trigger's `StartHPAConfig.minReplicas`/`maxReplicas`. If deviation exceeds the threshold, queues an immediate re-analysis. Re-analysis is suppressed when the active trigger has no `ai-` name prefix (i.e., is manually defined).

Both loops share a single `Analyzer` that produces `[]Trigger` recommendations (the existing `api/v1alpha1.Trigger` type).

### Goroutine lifecycle

`SmartHPAContext` in `internal/scheduler/scheduler.go` is extended with one new field:

```go
aiCancelFunc context.CancelFunc // cancels AI scheduler goroutines for this resource
```

The existing `cancelFunc` continues to manage the existing cron-based goroutine only. When the AI scheduler starts loops for a resource, it sets `aiCancelFunc`. When the resource is deleted or `aiScheduler.enabled` is set to false, `aiCancelFunc()` is called to stop only the AI loops. `stop()` is updated to also call `aiCancelFunc()` if set.

### Prometheus URL wiring

`NewScheduler` in `internal/scheduler/scheduler.go` is extended to accept a `prometheusURL string` parameter. In `cmd/main.go`, the existing `mlPrometheusURL` flag value is passed to `NewScheduler` (currently it is only passed to `runMLServer`). No new flag is introduced.

### File layout

New files added to the existing `internal/scheduler/` package:

```
internal/scheduler/
  scheduler.go           # existing — extended: aiCancelFunc field, NewScheduler signature, stop()
  ai_scheduler.go        # new — periodic and deviation loop orchestration
  analyzer.go            # new — rule-based pattern analysis
  metrics_client.go      # new — Prometheus metrics fetching
  trigger_writer.go      # new — server-side apply patch for SmartHPA CR triggers
  scheduler_test.go      # existing — extended with AI scheduler integration tests
  analyzer_test.go       # new — unit tests
  metrics_client_test.go # new — unit tests
```

---

## Types

### AISchedulerConfig

Added to `SmartHorizontalPodAutoscalerSpec` (requires regenerating `zz_generated.deepcopy.go` via `make generate`):

```go
// AIScheduler holds configuration for the AI-driven scheduling loop.
// +optional
AIScheduler *AISchedulerConfig `json:"aiScheduler,omitempty"`
```

New struct:

```go
type AISchedulerConfig struct {
    // Enabled activates the AI scheduler for this resource.
    Enabled bool `json:"enabled"`

    // AnalysisIntervalHours is how often the periodic analysis runs.
    // +kubebuilder:default=1
    // +kubebuilder:validation:Minimum=1
    // +kubebuilder:validation:Maximum=168
    AnalysisIntervalHours int `json:"analysisIntervalHours,omitempty"`

    // DeviationCheckIntervalMinutes is how often the deviation check runs.
    // +kubebuilder:default=5
    // +kubebuilder:validation:Minimum=1
    // +kubebuilder:validation:Maximum=60
    DeviationCheckIntervalMinutes int `json:"deviationCheckIntervalMinutes,omitempty"`

    // DeviationThresholdPercent is the percentage deviation from expected replicas
    // that triggers an immediate re-analysis.
    // +kubebuilder:default=20
    // +kubebuilder:validation:Minimum=5
    // +kubebuilder:validation:Maximum=100
    DeviationThresholdPercent int `json:"deviationThresholdPercent,omitempty"`

    // LookbackDays is how many days of Prometheus history to analyze.
    // +kubebuilder:default=14
    // +kubebuilder:validation:Minimum=3
    // +kubebuilder:validation:Maximum=90
    LookbackDays int `json:"lookbackDays,omitempty"`

    // PeakMultiplierPercent is the percentage above overall p50 that defines a peak window.
    // 150 means p95 must exceed p50 * 1.5 to be considered a peak.
    // +kubebuilder:default=150
    // +kubebuilder:validation:Minimum=110
    // +kubebuilder:validation:Maximum=500
    PeakMultiplierPercent int `json:"peakMultiplierPercent,omitempty"`

    // WeekdayWeekendSplitThresholdPercent is the minimum percentage difference
    // between weekday and weekend p95 that causes separate trigger sets to be generated.
    // +kubebuilder:default=20
    // +kubebuilder:validation:Minimum=5
    // +kubebuilder:validation:Maximum=100
    WeekdayWeekendSplitThresholdPercent int `json:"weekdayWeekendSplitThresholdPercent,omitempty"`

    // MinDataPointsRequired is the minimum number of hourly data points needed
    // before analysis proceeds. Below this, InsufficientData is emitted and no
    // triggers are written.
    // +kubebuilder:default=72
    // +kubebuilder:validation:Minimum=24
    MinDataPointsRequired int `json:"minDataPointsRequired,omitempty"`
}
```

---

## Rule-Based Analysis

The `Analyzer` processes Prometheus data in three steps:

### 1. Data collection

- **Prometheus only** — queries `kube_horizontalpodautoscaler_status_current_replicas` with step=1h over the past `lookbackDays` days. This metric reflects HPA decisions and directly represents the scaling behavior the system should reproduce.
- Kubernetes HPA history is **not** used — the Kubernetes API does not expose HPA time-series. All historical data comes from Prometheus.
- If fewer than `minDataPointsRequired` data points are returned, analysis aborts and emits `InsufficientData`.

### 2. Pattern detection

Heuristics applied in order:

- **Time-of-day bucketing:** groups replica counts by hour-of-day (0–23) across all collected days; computes p50 and p95 per bucket
- **Day-of-week detection:** if weekday p95 and weekend p95 differ by more than `weekdayWeekendSplitThresholdPercent`, produces separate trigger sets for weekdays and weekends
- **Peak window identification:** finds contiguous hour buckets where p95 exceeds the overall p50 by `peakMultiplierPercent / 100`; these become `minReplicas`/`maxReplicas` boundaries for each trigger

### 3. Trigger generation

- Outputs `[]Trigger` (existing `api/v1alpha1.Trigger` type)
- Priority assigned by peak magnitude (highest replica demand = highest priority)
- AI-generated triggers are distinguished by a name prefix `ai-` (e.g., `ai-peak-weekday-09-17`). No new field is added to `Trigger`.
- A trigger is treated as **manually defined** if its `Name` does not start with `ai-`. Manual triggers are never overwritten.
- A generated trigger is skipped if an existing manually-defined trigger's time window intersects it. "Intersects" means the hour ranges overlap by at least one hour.
- A user can "claim" an AI trigger as manual by renaming it (removing the `ai-` prefix), preventing the AI scheduler from ever overwriting it again.

---

## Write Protocol

`trigger_writer.go` uses **server-side apply** with field manager `smarthpa-ai-scheduler`. This means:

- Only `spec.triggers` entries owned by `smarthpa-ai-scheduler` (those with `ai-` names) are managed by the AI scheduler
- The main reconciler uses its own field manager (`smarthpa-controller`) for all other spec fields
- Concurrent writes from the periodic loop and the main reconciler do not conflict because they own disjoint fields

---

## Configuration (example)

```yaml
spec:
  aiScheduler:
    enabled: true
    analysisIntervalHours: 1
    deviationCheckIntervalMinutes: 5
    deviationThresholdPercent: 20
    lookbackDays: 14
    peakMultiplierPercent: 150
    weekdayWeekendSplitThresholdPercent: 20
    minDataPointsRequired: 72
```

---

## Observability

The scheduler emits Kubernetes events on the SmartHPA resource:

| Reason | When |
|--------|------|
| `AISchedulerUpdated` | Triggers successfully updated after analysis |
| `DeviationDetected` | Current replicas deviate beyond threshold; re-analysis queued |
| `PrometheusUnavailable` | Prometheus unreachable during periodic analysis cycle |
| `InsufficientData` | Fewer than `minDataPointsRequired` data points returned |

---

## Error Handling

- **Prometheus unreachable (periodic loop):** logs warning, emits `PrometheusUnavailable` event, skips update cycle — existing triggers are never removed
- **Prometheus unreachable (deviation loop):** the deviation loop reads current replicas from the Kubernetes HPA object via the controller cache (not Prometheus), so Prometheus unavailability does not affect it
- **Zero triggers / insufficient data:** skips update, emits `InsufficientData` event — no changes applied
- **Goroutine panics:** recovered and restarted with exponential backoff (initial 30s, max 10m)
- **Excessive deviation-triggered writes:** re-analyses triggered by the deviation loop are rate-limited to at most one per `deviationCheckIntervalMinutes` period per resource

---

## Testing

| File | Action | Type | Coverage |
|------|--------|------|----------|
| `analyzer_test.go` | New | Unit | Flat traffic; clear peaks; weekday/weekend splits; insufficient data (<72 points) |
| `scheduler_test.go` | Extend existing | Integration (envtest) | AI triggers applied after periodic cycle; manual triggers not overwritten; deviation triggers re-analysis; deviation suppressed during manual trigger window; Prometheus unavailable during periodic analysis |
| `metrics_client_test.go` | New | Unit | Mock Prometheus HTTP server; empty response; partial data |
