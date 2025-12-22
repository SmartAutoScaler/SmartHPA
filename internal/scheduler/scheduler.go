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
	schedules map[string]*TriggerSchedule
	cron      *cron.Cron
	mutex     sync.RWMutex // Protects schedules map
}

// Scheduler manages the scheduling of HPA configurations
type Scheduler struct {
	client   client.Client
	contexts map[types.NamespacedName]*SmartHPAContext // key: namespace/nam
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
		logger.Error(err, "failed to get SmartHPA")
		return
	}

	// Check if HPAObjectRef is valid
	if obj.Spec.HPAObjectRef == nil {
		logger.V(1).Info("no HPAObjectRef specified, skipping scheduling")
		return
	}

	s.mutex.RLock()
	hpaContext := s.contexts[item]
	s.mutex.RUnlock()

	if hpaContext == nil {
		hpaContext = s.initializeContext(ctx, &obj)
		s.mutex.Lock()
		s.contexts[item] = hpaContext
		s.mutex.Unlock()
	}

	hpaContext.cron.Start()
	go hpaContext.execute(ctx)
}

// initializeContext creates a new SmartHPAContext with schedules for all triggers.
func (s *Scheduler) initializeContext(ctx context.Context, obj *autoscalingv1alpha1.SmartHorizontalPodAutoscaler) *SmartHPAContext {
	hpaContext := &SmartHPAContext{
		schedules: make(map[string]*TriggerSchedule),
		cron:      cron.New(),
	}

	for i := range obj.Spec.Triggers {
		trigger := &obj.Spec.Triggers[i]
		loc := loadTimezone(trigger.Timezone)

		schedule := &TriggerSchedule{
			client:  s.client,
			Trigger: trigger,
			cron:    cron.New(cron.WithLocation(loc)),
			HPANamespacedName: types.NamespacedName{
				Namespace: obj.Spec.HPAObjectRef.Namespace,
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

// Stop stops the scheduler and cleans up resources
// GetContext safely retrieves a context by key
func (s *Scheduler) GetContext(key types.NamespacedName) *SmartHPAContext {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.contexts[key]
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

	for _, hpaCtx := range s.contexts {
		hpaCtx.stop()
	}

	// Clear the contexts map
	s.contexts = make(map[types.NamespacedName]*SmartHPAContext)
	log.Info("scheduler stopped")
}

// stop stops all cron jobs in the context.
func (c *SmartHPAContext) stop() {
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
	// Return in the format that matches the test expectation
	// Format: minute hour day month weekday
	return fmt.Sprintf("%d %d * * *", t.Minute(), t.Hour()), nil
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
	logger := log.WithValues("trigger", ts.Trigger.Name)

	switch state {
	case Within:
		if ts.hasHigherPriorityActive() {
			logger.V(1).Info("higher priority schedule active, skipping config application")
			return
		}
		if ts.Trigger.StartHPAConfig != nil {
			if err := ts.UpdateHPAConfig(*ts.Trigger.StartHPAConfig); err != nil {
				logger.Error(err, "failed to apply start config")
			}
		}
	case After:
		if ts.Trigger.EndHPAConfig != nil {
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
	logger := log.WithValues("trigger", ts.Trigger.Name)
	logger.Info("trigger started")

	if ts.Trigger.StartHPAConfig != nil {
		if err := ts.UpdateHPAConfig(*ts.Trigger.StartHPAConfig); err != nil {
			logger.Error(err, "failed to apply start config")
		}
	}
}

// onEnd is called when the trigger's end time is reached.
func (ts *TriggerSchedule) onEnd() {
	logger := log.WithValues("trigger", ts.Trigger.Name)
	logger.Info("trigger ended")

	if ts.Trigger.EndHPAConfig != nil {
		if err := ts.UpdateHPAConfig(*ts.Trigger.EndHPAConfig); err != nil {
			logger.Error(err, "failed to apply end config")
		}
	}

	// Find and apply next highest priority config
	if ts.context != nil {
		ts.context.applyNextHighestPriorityConfig(ts)
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
		log.Info("applying next highest priority config", "schedule", topSchedule.Trigger.Name)

		if topSchedule.Trigger.StartHPAConfig != nil {
			if err := topSchedule.UpdateHPAConfig(*topSchedule.Trigger.StartHPAConfig); err != nil {
				log.Error(err, "failed to apply config", "schedule", topSchedule.Trigger.Name)
			}
		}
		return
	}

	// No active schedules, apply the default config
	log.V(1).Info("no active schedules found, applying default config")
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
	log.Info("executing SmartHPAContext", "scheduleCount", len(schedulesCopy))

	// Build list of active schedules
	var activeSchedules []*TriggerSchedule
	for _, schedule := range schedulesCopy {
		schedule.mutex.Lock()
		schedule.context = sc
		schedule.mutex.Unlock()

		if schedule.Trigger != nil && schedule.Trigger.NeedRecurring() {
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
