package scheduler

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/robfig/cron/v3"
	autoscalingv1alpha1 "github.com/sarabala1979/SmartHPA/api/v1alpha1"
)

// Package-level logger for scheduler operations.
var log = ctrl.Log.WithName("scheduler")

const (
	// defaultWorkerCount is the number of concurrent workers processing items.
	defaultWorkerCount = 10
	// stopGracePeriod is the time to wait for goroutines during shutdown.
	stopGracePeriod = 50 * time.Millisecond
	// DefaultQueueSize is the default buffer size for the scheduler queue
	DefaultQueueSize = 100
)

// TriggerSchedule represents a scheduled trigger with its HPA configuration.
type TriggerSchedule struct {
	client            client.Client
	HPANamespacedName types.NamespacedName
	Trigger           *autoscalingv1alpha1.Trigger
	cron              *cron.Cron
	context           *SmartHPAContext // Reference to parent context for priority handling
	mutex             sync.RWMutex     // Protects cron and trigger
}

// TimeWindowState represents the state of a time window
type TimeWindowState int

const (
	Unknown TimeWindowState = iota
	Before
	Within
	After
)

// SmartHPAContext holds the schedules for a single SmartHPA resource.
type SmartHPAContext struct {
	schedules          map[string]*TriggerSchedule
	cron               *cron.Cron
	mutex              sync.RWMutex // Protects schedules map
	generation         int64        // Track the generation to detect spec changes
	cancelFunc         context.CancelFunc
	smartHPAKey        types.NamespacedName
	activeTriggerName  string       // Name of the currently active (highest priority) trigger
	activeTriggerMutex sync.RWMutex // Protects activeTriggerName
}

// Scheduler manages the scheduling of HPA configurations
type Scheduler struct {
	client   client.Client
	contexts map[types.NamespacedName]*SmartHPAContext // key: namespace/name
	queue    chan types.NamespacedName
	done     chan struct{}
	mutex    sync.RWMutex // Protects contexts map
}

// NewScheduler creates a new scheduler instance
func NewScheduler(client client.Client, queue chan types.NamespacedName) *Scheduler {
	return &Scheduler{
		contexts: make(map[types.NamespacedName]*SmartHPAContext),
		client:   client,
		queue:    queue,
		done:     make(chan struct{}),
	}
}

// NewSchedulerQueue creates a buffered channel for the scheduler queue
func NewSchedulerQueue() chan types.NamespacedName {
	return make(chan types.NamespacedName, DefaultQueueSize)
}

// ProcessItem processes SmartHPA items from the queue and sets up their schedules.
func (s *Scheduler) ProcessItem(ctx context.Context) {
	logger := log.WithName("worker")
	defer logger.V(1).Info("worker goroutine exiting")

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.done:
			return
		case item := <-s.queue:
			s.processSmartHPA(ctx, item)
		}
	}
}

// processSmartHPA handles a single SmartHPA item from the queue.
func (s *Scheduler) processSmartHPA(ctx context.Context, item types.NamespacedName) {
	logger := log.WithValues("smartHPA", item)
	logger.V(1).Info("processing SmartHPA")

	var obj autoscalingv1alpha1.SmartHorizontalPodAutoscaler
	if err := s.client.Get(ctx, item, &obj); err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Object was deleted, remove context if exists
			logger.V(1).Info("SmartHPA not found, removing context if exists")
			s.RemoveContext(item)
			return
		}
		logger.Error(err, "failed to get SmartHPA")
		return
	}

	// Check if HPAObjectRef is valid
	if obj.Spec.HPAObjectRef == nil {
		logger.V(1).Info("no HPAObjectRef specified, skipping scheduling")
		return
	}

	s.mutex.RLock()
	existingContext := s.contexts[item]
	s.mutex.RUnlock()

	// Check if we need to rebuild the context (new or spec changed)
	needsRebuild := existingContext == nil || existingContext.generation != obj.Generation

	if needsRebuild {
		// Stop and remove old context if exists
		if existingContext != nil {
			logger.Info("SmartHPA spec changed, rebuilding schedules",
				"oldGeneration", existingContext.generation,
				"newGeneration", obj.Generation)
			existingContext.stop()
		}

		// Create new context
		hpaContext := s.initializeContext(ctx, &obj)
		s.mutex.Lock()
		s.contexts[item] = hpaContext
		s.mutex.Unlock()

		hpaContext.cron.Start()
		go hpaContext.execute(ctx)

		logger.Info("Created/rebuilt scheduler context", "generation", obj.Generation)
	} else {
		logger.V(1).Info("SmartHPA unchanged, skipping rebuild", "generation", obj.Generation)
	}
}

// initializeContext creates a new SmartHPAContext with schedules for all triggers.
func (s *Scheduler) initializeContext(ctx context.Context, obj *autoscalingv1alpha1.SmartHorizontalPodAutoscaler) *SmartHPAContext {
	_, cancel := context.WithCancel(ctx)

	hpaContext := &SmartHPAContext{
		schedules:   make(map[string]*TriggerSchedule),
		cron:        cron.New(),
		generation:  obj.Generation,
		cancelFunc:  cancel,
		smartHPAKey: types.NamespacedName{Name: obj.Name, Namespace: obj.Namespace},
	}

	hpaNamespace := obj.Spec.HPAObjectRef.Namespace
	if hpaNamespace == "" {
		hpaNamespace = obj.Namespace
	}

	for i := range obj.Spec.Triggers {
		trigger := &obj.Spec.Triggers[i]

		// Skip suspended triggers
		if trigger.Suspend {
			log.V(1).Info("Skipping suspended trigger", "trigger", trigger.Name)
			continue
		}

		loc := loadTimezone(trigger.Timezone)

		schedule := &TriggerSchedule{
			client:  s.client,
			Trigger: trigger,
			cron:    cron.New(cron.WithLocation(loc)),
			HPANamespacedName: types.NamespacedName{
				Namespace: hpaNamespace,
				Name:      obj.Spec.HPAObjectRef.Name,
			},
			context: hpaContext,
		}

		hpaContext.mutex.Lock()
		hpaContext.schedules[trigger.Name] = schedule
		hpaContext.mutex.Unlock()
	}

	return hpaContext
}

// loadTimezone loads a timezone location, falling back to UTC on error.
func loadTimezone(timezone string) *time.Location {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		log.V(1).Info("invalid timezone, using UTC", "timezone", timezone, "error", err)
		return time.UTC
	}
	return loc
}

// Start implements manager.Runnable interface for lifecycle management.
// It starts the scheduler workers and blocks until the context is cancelled.
func (s *Scheduler) Start(ctx context.Context) error {
	log.Info("starting scheduler", "workers", defaultWorkerCount)

	for i := 0; i < defaultWorkerCount; i++ {
		go s.ProcessItem(ctx)
	}

	// Block until context is cancelled
	<-ctx.Done()
	log.Info("scheduler context cancelled, initiating shutdown")
	s.Stop()
	return nil
}

// GetContext safely retrieves a context by key
func (s *Scheduler) GetContext(key types.NamespacedName) *SmartHPAContext {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.contexts[key]
}

// RemoveContext removes and stops the scheduler context for a SmartHPA
// This is called when a SmartHPA is deleted
func (s *Scheduler) RemoveContext(key types.NamespacedName) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	hpaCtx, exists := s.contexts[key]
	if !exists {
		log.V(1).Info("No context to remove", "key", key)
		return
	}

	log.Info("Removing scheduler context", "key", key)
	hpaCtx.stop()
	delete(s.contexts, key)
}

// RefreshContext forces a refresh of the scheduler context for a SmartHPA
// This is called when a SmartHPA spec is updated
func (s *Scheduler) RefreshContext(key types.NamespacedName) {
	// Simply enqueue the item for reprocessing
	// The processSmartHPA function will detect the generation change and rebuild
	select {
	case s.queue <- key:
		log.V(1).Info("Enqueued context refresh", "key", key)
	default:
		log.Info("Queue full, context refresh may be delayed", "key", key)
	}
}

// GetSchedules safely retrieves the schedules map
func (c *SmartHPAContext) GetSchedules() map[string]*TriggerSchedule {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	// Create a copy of the map to avoid race conditions
	schedulesCopy := make(map[string]*TriggerSchedule, len(c.schedules))
	for k, v := range c.schedules {
		schedulesCopy[k] = v
	}
	return schedulesCopy
}

// GetActiveTriggerName returns the name of the currently active (highest priority) trigger
func (c *SmartHPAContext) GetActiveTriggerName() string {
	c.activeTriggerMutex.RLock()
	defer c.activeTriggerMutex.RUnlock()
	return c.activeTriggerName
}

// setActiveTrigger updates the currently active trigger
func (c *SmartHPAContext) setActiveTrigger(name string, priority int) {
	c.activeTriggerMutex.Lock()
	defer c.activeTriggerMutex.Unlock()
	c.activeTriggerName = name
	log.Info("Active trigger changed", "smartHPA", c.smartHPAKey, "trigger", name, "priority", priority)
}

// GetHighestPriorityActiveTrigger returns the current highest priority active trigger
func (c *SmartHPAContext) GetHighestPriorityActiveTrigger() *TriggerSchedule {
	activeSchedules := c.getActiveSchedulesSortedByPriority("")
	if len(activeSchedules) > 0 {
		return activeSchedules[0]
	}
	return nil
}

// Stop gracefully stops the scheduler and cleans up all resources.
func (s *Scheduler) Stop() {
	log.Info("stopping scheduler")

	// Signal all goroutines to stop (safe to call multiple times via select)
	select {
	case <-s.done:
		// Already closed
	default:
		close(s.done)
	}

	// Wait for goroutines to finish
	time.Sleep(stopGracePeriod)

	// Stop all cron jobs and clean up contexts
	s.mutex.Lock()
	defer s.mutex.Unlock()

	for key, hpaCtx := range s.contexts {
		log.V(1).Info("Stopping context", "key", key)
		hpaCtx.stop()
	}

	// Clear the contexts map
	s.contexts = make(map[types.NamespacedName]*SmartHPAContext)
	log.Info("scheduler stopped")
}

// stop stops all cron jobs in the context.
func (c *SmartHPAContext) stop() {
	// Cancel context-specific goroutines
	if c.cancelFunc != nil {
		c.cancelFunc()
	}

	if c.cron != nil {
		c.cron.Stop()
	}

	c.mutex.RLock()
	schedules := make([]*TriggerSchedule, 0, len(c.schedules))
	for _, schedule := range c.schedules {
		schedules = append(schedules, schedule)
	}
	c.mutex.RUnlock()

	for _, schedule := range schedules {
		schedule.mutex.Lock()
		if schedule.cron != nil {
			schedule.cron.Stop()
		}
		schedule.mutex.Unlock()
	}
}

// For testing purposes - use a function that calls NowFunc() so it picks up test mocks
var nowFunc = func() time.Time { return autoscalingv1alpha1.NowFunc() }

// getPriority returns the priority value from a trigger, defaulting to 0 if nil.
func getPriority(trigger *autoscalingv1alpha1.Trigger) int {
	if trigger == nil || trigger.Priority == nil {
		return 0
	}
	return *trigger.Priority
}

// parseTimeString parses a time string in "15:04:05" format and returns a time.Time for today.
func (ts *TriggerSchedule) parseTimeString(timeStr string) (time.Time, error) {
	if timeStr == "" {
		return time.Time{}, fmt.Errorf("empty time string")
	}

	loc := loadTimezone(ts.Trigger.Timezone)

	// Parse the time string in format "15:04:05" (hour:minute:second)
	t, err := time.Parse("15:04:05", timeStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid time format: %w", err)
	}

	// Use the current date from nowFunc() with the parsed time in the correct timezone
	now := nowFunc().In(loc)
	return time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), t.Second(), 0, loc), nil
}

func (ts *TriggerSchedule) GetCronTab(timeStr string) (string, error) {
	t, err := ts.parseTimeString(timeStr)
	if err != nil {
		return "", err
	}
	// Format: minute hour day month weekday
	// Use the trigger's recurring weekdays for the weekday field
	weekdays := ts.Trigger.GetCronWeekdays()
	return fmt.Sprintf("%d %d * * %s", t.Minute(), t.Hour(), weekdays), nil
}

// isWithinTimeWindow checks if the given time falls within the trigger's time window.
func (ts *TriggerSchedule) isWithinTimeWindow(now time.Time) (TimeWindowState, error) {
	startTime, err := ts.parseTimeString(ts.Trigger.StartTime)
	if err != nil {
		return Unknown, fmt.Errorf("failed to parse start time: %w", err)
	}

	endTime, err := ts.parseTimeString(ts.Trigger.EndTime)
	if err != nil {
		return Unknown, fmt.Errorf("failed to parse end time: %w", err)
	}

	loc := loadTimezone(ts.Trigger.Timezone)
	currentTime := now.In(loc)

	// Convert to minutes since midnight for comparison
	current := toMinutesSinceMidnight(currentTime)
	start := toMinutesSinceMidnight(startTime)
	end := toMinutesSinceMidnight(endTime)

	logger := log.WithValues("trigger", ts.Trigger.Name)
	logger.V(2).Info("time window check",
		"current", formatTime(currentTime),
		"start", formatTime(startTime),
		"end", formatTime(endTime))

	if !ts.Trigger.NeedRecurring() {
		return Unknown, nil
	}

	// Check if within time window
	if isTimeWithinWindow(current, start, end) {
		return Within, nil
	}

	if current >= end {
		return After, nil
	}
	return Before, nil
}

// toMinutesSinceMidnight converts a time to minutes since midnight.
func toMinutesSinceMidnight(t time.Time) int {
	return t.Hour()*60 + t.Minute()
}

// formatTime formats a time for logging.
func formatTime(t time.Time) string {
	return fmt.Sprintf("%02d:%02d", t.Hour(), t.Minute())
}

// isTimeWithinWindow checks if current time is within the start-end window.
func isTimeWithinWindow(current, start, end int) bool {
	if start <= end {
		// Normal time window (e.g., 09:00-17:00)
		return current >= start && current < end
	}
	// Overnight time window (e.g., 22:00-06:00)
	return current >= start || current < end
}

// Schedule sets up cron jobs for the trigger's start and end times.
func (ts *TriggerSchedule) Schedule() {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()

	logger := log.WithValues("trigger", ts.Trigger.Name)
	logger.V(1).Info("scheduling trigger",
		"startTime", ts.Trigger.StartTime,
		"endTime", ts.Trigger.EndTime)

	// Check if current time is within the time window
	state, err := ts.isWithinTimeWindow(nowFunc())
	if err != nil {
		logger.Error(err, "failed to check time window")
		return
	}

	// Apply initial configuration based on current time and priority
	ts.applyInitialConfig(state)

	// Schedule start and end times
	if err := ts.scheduleCronJobs(logger); err != nil {
		logger.Error(err, "failed to schedule cron jobs")
		return
	}

	ts.cron.Start()
}

// applyInitialConfig applies the initial HPA configuration based on the current time window state.
func (ts *TriggerSchedule) applyInitialConfig(state TimeWindowState) {
	thisPriority := getPriority(ts.Trigger)
	logger := log.WithValues("trigger", ts.Trigger.Name, "priority", thisPriority, "state", state)

	switch state {
	case Within:
		if ts.hasHigherPriorityActive() {
			logger.V(1).Info("higher priority schedule active, skipping config application")
			return
		}
		// This is the highest priority active trigger at startup
		if ts.context != nil {
			ts.context.setActiveTrigger(ts.Trigger.Name, thisPriority)
		}
		if ts.Trigger.StartHPAConfig != nil {
			logger.Info("applying initial start config (highest active priority)")
			if err := ts.UpdateHPAConfig(*ts.Trigger.StartHPAConfig); err != nil {
				logger.Error(err, "failed to apply start config")
			}
		}
	case After:
		// Only apply end config if no other triggers are active
		if ts.context != nil {
			activeSchedules := ts.context.getActiveSchedulesSortedByPriority(ts.Trigger.Name)
			if len(activeSchedules) > 0 {
				logger.V(1).Info("other schedules are active, not applying end config")
				return
			}
		}
		if ts.Trigger.EndHPAConfig != nil {
			logger.V(1).Info("applying initial end config (no active schedules)")
			if err := ts.UpdateHPAConfig(*ts.Trigger.EndHPAConfig); err != nil {
				logger.Error(err, "failed to apply end config")
			}
		}
	}
}

// hasHigherPriorityActive checks if any other active schedule has higher priority.
func (ts *TriggerSchedule) hasHigherPriorityActive() bool {
	thisPriority := getPriority(ts.Trigger)

	for _, schedule := range ts.context.schedules {
		if schedule.Trigger.Name == ts.Trigger.Name {
			continue
		}

		scheduleState, err := schedule.isWithinTimeWindow(nowFunc())
		if err != nil {
			log.Error(err, "failed to check time window", "schedule", schedule.Trigger.Name)
			continue
		}

		if scheduleState == Within && getPriority(schedule.Trigger) > thisPriority {
			return true
		}
	}
	return false
}

// scheduleCronJobs adds the start and end cron jobs for the trigger.
func (ts *TriggerSchedule) scheduleCronJobs(logger interface{ Info(string, ...interface{}) }) error {
	startCron, err := ts.GetCronTab(ts.Trigger.StartTime)
	if err != nil {
		return fmt.Errorf("failed to get start cron: %w", err)
	}

	endCron, err := ts.GetCronTab(ts.Trigger.EndTime)
	if err != nil {
		return fmt.Errorf("failed to get end cron: %w", err)
	}

	log.Info("scheduling cron jobs", "trigger", ts.Trigger.Name, "startCron", startCron, "endCron", endCron)

	// Add start cron job
	if _, err := ts.cron.AddFunc(startCron, ts.onStart); err != nil {
		return fmt.Errorf("failed to add start cron: %w", err)
	}

	// Add end cron job
	if _, err := ts.cron.AddFunc(endCron, ts.onEnd); err != nil {
		return fmt.Errorf("failed to add end cron: %w", err)
	}

	return nil
}

// onStart is called when the trigger's start time is reached.
func (ts *TriggerSchedule) onStart() {
	thisPriority := getPriority(ts.Trigger)
	logger := log.WithValues("trigger", ts.Trigger.Name, "priority", thisPriority)
	logger.Info("trigger started")

	// Check if a higher priority schedule is already active
	// If so, don't apply this trigger's config - let the higher priority one remain in effect
	if ts.hasHigherPriorityActive() {
		logger.Info("higher priority schedule is active, not applying this trigger's config")
		return
	}

	// This is now the highest priority active trigger
	if ts.context != nil {
		ts.context.setActiveTrigger(ts.Trigger.Name, thisPriority)
	}

	if ts.Trigger.StartHPAConfig != nil {
		logger.Info("applying start config (highest active priority)")
		if err := ts.UpdateHPAConfig(*ts.Trigger.StartHPAConfig); err != nil {
			logger.Error(err, "failed to apply start config")
		}
	}
}

// onEnd is called when the trigger's end time is reached.
func (ts *TriggerSchedule) onEnd() {
	thisPriority := getPriority(ts.Trigger)
	logger := log.WithValues("trigger", ts.Trigger.Name, "priority", thisPriority)
	logger.Info("trigger ended")

	// Find the next highest priority active schedule
	// This determines what config should be applied now
	if ts.context != nil {
		activeSchedules := ts.context.getActiveSchedulesSortedByPriority(ts.Trigger.Name)

		if len(activeSchedules) > 0 {
			topSchedule := activeSchedules[0]
			topPriority := getPriority(topSchedule.Trigger)

			if topPriority > thisPriority {
				// A higher priority schedule is still active - it should remain in control
				// Don't apply our end config, don't change anything
				logger.Info("higher priority schedule still active, not applying end config",
					"activeSchedule", topSchedule.Trigger.Name,
					"activePriority", topPriority)
				return
			}

			// Apply the next highest priority schedule's config and track it
			logger.Info("applying next highest priority config", "schedule", topSchedule.Trigger.Name)
			ts.context.setActiveTrigger(topSchedule.Trigger.Name, topPriority)
			if topSchedule.Trigger.StartHPAConfig != nil {
				if err := topSchedule.UpdateHPAConfig(*topSchedule.Trigger.StartHPAConfig); err != nil {
					logger.Error(err, "failed to apply config", "schedule", topSchedule.Trigger.Name)
				}
			}
			return
		}

		// No other active schedules - clear active trigger
		ts.context.setActiveTrigger("", 0)
	}

	// No other active schedules, apply this trigger's end config as default
	logger.Info("no other active schedules, applying end config as default")
	if ts.Trigger.EndHPAConfig != nil {
		if err := ts.UpdateHPAConfig(*ts.Trigger.EndHPAConfig); err != nil {
			logger.Error(err, "failed to apply end config")
		}
	}
}

// UpdateHPAConfig updates the HPA with the given configuration.
func (ts *TriggerSchedule) UpdateHPAConfig(config autoscalingv1alpha1.HPAConfig) error {
	return ts.UpdateHPAConfigWithContext(context.Background(), config)
}

// UpdateHPAConfigWithContext updates the HPA with the given configuration using the provided context.
func (ts *TriggerSchedule) UpdateHPAConfigWithContext(ctx context.Context, config autoscalingv1alpha1.HPAConfig) error {
	logger := log.WithValues("hpa", ts.HPANamespacedName)
	logger.V(1).Info("updating HPA config", "config", config)

	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	if err := ts.client.Get(ctx, ts.HPANamespacedName, hpa); err != nil {
		return fmt.Errorf("failed to get HPA: %w", err)
	}

	hpaCopy := hpa.DeepCopy()

	// Validate and apply configuration
	if err := applyHPAConfig(hpaCopy, config); err != nil {
		return err
	}

	logger.V(1).Info("validated config", "minReplicas", hpaCopy.Spec.MinReplicas, "maxReplicas", hpaCopy.Spec.MaxReplicas)

	if err := ts.client.Update(ctx, hpaCopy); err != nil {
		return fmt.Errorf("failed to update HPA: %w", err)
	}

	logger.Info("successfully updated HPA",
		"minReplicas", hpaCopy.Spec.MinReplicas,
		"maxReplicas", hpaCopy.Spec.MaxReplicas)

	return nil
}

// applyHPAConfig validates and applies the config to the HPA spec.
func applyHPAConfig(hpa *autoscalingv2.HorizontalPodAutoscaler, config autoscalingv1alpha1.HPAConfig) error {
	if config.MinReplicas != nil {
		if *config.MinReplicas < 0 {
			return fmt.Errorf("minReplicas cannot be negative")
		}
		hpa.Spec.MinReplicas = config.MinReplicas
	}

	if config.MaxReplicas != nil {
		if *config.MaxReplicas <= 0 {
			return fmt.Errorf("maxReplicas must be greater than 0")
		}
		hpa.Spec.MaxReplicas = *config.MaxReplicas
	}

	// Validate min <= max
	if hpa.Spec.MinReplicas != nil && *hpa.Spec.MinReplicas > hpa.Spec.MaxReplicas {
		return fmt.Errorf("minReplicas (%d) cannot be greater than maxReplicas (%d)",
			*hpa.Spec.MinReplicas, hpa.Spec.MaxReplicas)
	}

	return nil
}

// applyNextHighestPriorityConfig finds and applies the next highest priority active schedule.
func (c *SmartHPAContext) applyNextHighestPriorityConfig(currentSchedule *TriggerSchedule) {
	activeSchedules := c.getActiveSchedulesSortedByPriority(currentSchedule.Trigger.Name)

	if len(activeSchedules) > 0 {
		topSchedule := activeSchedules[0]
		topPriority := getPriority(topSchedule.Trigger)
		log.Info("applying next highest priority config", "schedule", topSchedule.Trigger.Name, "priority", topPriority)

		c.setActiveTrigger(topSchedule.Trigger.Name, topPriority)
		if topSchedule.Trigger.StartHPAConfig != nil {
			if err := topSchedule.UpdateHPAConfig(*topSchedule.Trigger.StartHPAConfig); err != nil {
				log.Error(err, "failed to apply config", "schedule", topSchedule.Trigger.Name)
			}
		}
		return
	}

	// No active schedules, clear active trigger and apply the default config
	log.V(1).Info("no active schedules found, applying default config")
	c.setActiveTrigger("", 0)
	if currentSchedule.Trigger.EndHPAConfig != nil {
		if err := currentSchedule.UpdateHPAConfig(*currentSchedule.Trigger.EndHPAConfig); err != nil {
			log.Error(err, "failed to apply default config")
		}
	}
}

// getActiveSchedulesSortedByPriority returns active schedules sorted by priority (highest first).
func (c *SmartHPAContext) getActiveSchedulesSortedByPriority(excludeName string) []*TriggerSchedule {
	var activeSchedules []*TriggerSchedule

	for _, schedule := range c.schedules {
		if schedule.Trigger.Name == excludeName {
			continue
		}

		state, err := schedule.isWithinTimeWindow(nowFunc())
		if err != nil {
			log.Error(err, "failed to check time window", "schedule", schedule.Trigger.Name)
			continue
		}

		if state == Within && schedule.Trigger.NeedRecurring() {
			activeSchedules = append(activeSchedules, schedule)
		}
	}

	// Sort by priority (highest first)
	sort.Slice(activeSchedules, func(i, j int) bool {
		return getPriority(activeSchedules[i].Trigger) > getPriority(activeSchedules[j].Trigger)
	})

	return activeSchedules
}

// execute processes all schedules in priority order.
func (sc *SmartHPAContext) execute(ctx context.Context) {
	schedulesCopy := sc.GetSchedules()
	log.Info("executing SmartHPAContext", "scheduleCount", len(schedulesCopy), "smartHPA", sc.smartHPAKey)

	// Build list of all schedules with valid intervals (not just today's matches)
	var activeSchedules []*TriggerSchedule
	for _, schedule := range schedulesCopy {
		schedule.mutex.Lock()
		schedule.context = sc
		schedule.mutex.Unlock()

		// Schedule all triggers with valid intervals - cron will handle day filtering
		if schedule.Trigger != nil && schedule.Trigger.HasValidInterval() {
			activeSchedules = append(activeSchedules, schedule)
		}
	}

	// Sort by priority (highest first)
	sort.Slice(activeSchedules, func(i, j int) bool {
		return getPriority(activeSchedules[i].Trigger) > getPriority(activeSchedules[j].Trigger)
	})

	// Process schedules in priority order (deduplicated)
	sc.mutex.Lock()
	defer sc.mutex.Unlock()

	processed := make(map[string]bool)
	for _, schedule := range activeSchedules {
		if processed[schedule.Trigger.Name] {
			continue
		}

		log.V(1).Info("executing schedule",
			"trigger", schedule.Trigger.Name,
			"priority", getPriority(schedule.Trigger))

		processed[schedule.Trigger.Name] = true
		schedule.Schedule()
	}

	// Add daily refresh job
	if _, err := sc.cron.AddFunc("0 0 * * *", func() {
		log.V(1).Info("daily refresh triggered", "scheduleCount", len(sc.schedules))
		sc.execute(ctx)
	}); err != nil {
		log.Error(err, "failed to add daily refresh cron")
	}
}
