# AI Scheduler Design

**Date:** 2026-03-23
**Status:** Approved
**Scope:** v1 rule-based AI scheduler embedded in the SmartHPA controller

---

## Overview

The AI scheduler automatically generates and applies time-window scaling triggers for SmartHPA resources. It uses historical Prometheus metrics and Kubernetes HPA data to detect recurring traffic patterns and writes updated triggers directly to the SmartHPA CR — no human intervention required.

---

## Architecture

The scheduler lives inside the existing controller binary as a new `Scheduler` component in `internal/scheduler/`. It runs two loops per SmartHPA resource with `aiScheduler.enabled: true`:

1. **Periodic loop** — runs on a configurable interval (default 1h). Pulls historical Prometheus + Kubernetes HPA data, runs pattern analysis, and patches the SmartHPA CR's `spec.triggers`.

2. **Deviation loop** — runs on a shorter interval (default 5m). Compares current replica count and CPU/memory metrics against the active trigger's expected range. If deviation exceeds the configured threshold, triggers an immediate re-analysis.

Both loops share a single `Analyzer` that produces trigger recommendations.

### File layout

```
internal/
  scheduler/
    scheduler.go        # orchestrates periodic and deviation loops
    analyzer.go         # rule-based pattern analysis
    metrics_client.go   # Prometheus + Kubernetes metrics fetching
    trigger_writer.go   # patches SmartHPA CR triggers
```

The `Scheduler` is initialized in `cmd/main.go` alongside the existing controller manager, sharing the same Kubernetes client and Prometheus URL from existing env config.

---

## Rule-Based Analysis

The `Analyzer` processes historical data in three steps:

### 1. Data collection

- **Prometheus:** queries `kube_horizontalpodautoscaler_status_current_replicas` over the past N days (configurable, default 14)
- **Kubernetes:** reads HPA status history from the controller's existing cache

### 2. Pattern detection

Heuristics applied in order:

- **Time-of-day bucketing:** groups replica counts by hour-of-day across all collected days; computes p50 and p95 per bucket
- **Day-of-week detection:** if weekday vs. weekend p95 differ by more than 20%, produces separate trigger sets for each
- **Peak window identification:** finds contiguous hour buckets where p95 exceeds the overall p50 by a configurable multiplier (default 1.5x); these define the `minReplicas`/`maxReplicas` boundaries for each trigger

### 3. Trigger generation

Outputs a `[]TriggerSpec` where each trigger covers a detected time window. Priority is assigned by peak magnitude (highest replica demand = highest priority). Triggers overlapping an existing manually-defined trigger are skipped — manual triggers always win.

---

## Configuration

A new optional `aiScheduler` field on the SmartHPA spec controls behavior per-resource:

```yaml
spec:
  aiScheduler:
    enabled: true
    analysisIntervalHours: 1         # periodic loop interval (default: 1)
    deviationCheckIntervalMinutes: 5  # deviation loop interval (default: 5)
    deviationThresholdPercent: 20     # deviation % that triggers re-analysis (default: 20)
    lookbackDays: 14                  # days of history to analyze (default: 14)
    peakMultiplier: 1.5               # peak sensitivity multiplier (default: 1.5)
```

---

## Controller Integration

- The `Scheduler` is started from `cmd/main.go` with `mgr.GetClient()` and the existing Prometheus URL env var
- It watches SmartHPA resources and manages a per-resource goroutine pair (periodic + deviation loops)
- AI-generated triggers are annotated with `smarthpa.io/source: ai-scheduler`
- The main reconciler does not overwrite triggers with this annotation if a manually-defined trigger covers the same window

---

## Observability

The scheduler emits Kubernetes events on the SmartHPA resource:

| Reason | When |
|--------|------|
| `AISchedulerUpdated` | Triggers successfully updated after analysis |
| `DeviationDetected` | Current metrics deviate beyond threshold |
| `PrometheusUnavailable` | Prometheus unreachable during analysis cycle |
| `InsufficientData` | Not enough history to generate triggers |

---

## Error Handling

- **Prometheus unreachable:** logs warning, emits `PrometheusUnavailable` event, skips update cycle — existing triggers are never removed on error
- **Zero triggers generated:** skips update, emits `InsufficientData` event — no changes applied
- **Goroutine panics:** recovered and restarted with exponential backoff

---

## Testing

| File | Type | Coverage |
|------|------|----------|
| `analyzer_test.go` | Unit | Flat traffic, clear peaks, weekday/weekend splits, insufficient data |
| `scheduler_test.go` | Integration (envtest) | Triggers applied after cycle; manual triggers not overwritten |
| `metrics_client_test.go` | Unit | Mock Prometheus HTTP server |
