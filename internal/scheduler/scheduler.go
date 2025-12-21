package scheduler

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/robfig/cron/v3"
	sarabalaiov1alpha1 "github.com/sarabala1979/SmartHPA/api/v1alpha1"
)

// TriggerSchedule represents a scheduled trigger with its HPA configuration
type TriggerSchedule struct {
	client            client.Client
	HPANamespacedName types.NamespacedName
	Trigger           *sarabalaiov1alpha1.Trigger
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

type SmartHPAContext struct {
	client    client.Client
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

func (s *Scheduler) ProcessItem(ctx context.Context) {
	defer func() {
		klog.Info("ProcessItem goroutine exiting")
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.done:
			return
		case item := <-s.queue:
			klog.Infof("Processing SmartHPA %s", item)
			var obj sarabalaiov1alpha1.SmartHorizontalPodAutoscaler
			err := s.client.Get(ctx, item, &obj)
			if err != nil {
				klog.Errorf("Error getting SmartHPA %s: %v", item, err)
				continue
			}
			s.mutex.RLock()
			hpaContext := s.contexts[item]
			s.mutex.RUnlock()
			if hpaContext == nil {
				hpaContext = &SmartHPAContext{
					schedules: make(map[string]*TriggerSchedule),
					cron:      cron.New(),
					mutex:     sync.RWMutex{},
				}
				// Initialize schedules with mutexes
				for _, trigger := range obj.Spec.Triggers {
					// Load timezone
					loc, err := time.LoadLocation(trigger.Timezone)
					if err != nil {
						klog.Errorf("invalid timezone %s: %v", trigger.Timezone, err)
						loc = time.UTC
					}
					hpaContext.mutex.Lock()
					schedule := &TriggerSchedule{
						client:  s.client,
						Trigger: &trigger,
						//create cron with timezone
						cron:  cron.New(cron.WithLocation(loc)),
						mutex: sync.RWMutex{},
						HPANamespacedName: types.NamespacedName{
							Namespace: obj.Spec.HPAObjectRef.Namespace,
							Name:      obj.Spec.HPAObjectRef.Name,
						},
						context: hpaContext,
					}
					hpaContext.schedules[trigger.Name] = schedule
					hpaContext.mutex.Unlock()
				}
				s.mutex.Lock()
				s.contexts[item] = hpaContext
				s.mutex.Unlock()
			}
			hpaContext.cron.Start()
			go hpaContext.execute(ctx)
		}
	}
}

// Start starts the scheduler
func (s *Scheduler) Start() {
	for i := 0; i < 10; i++ {
		go s.ProcessItem(context.Background())
	}
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

func (s *Scheduler) Stop() {
	// Signal all goroutines to stop
	close(s.done)

	// Wait a bit for goroutines to finish
	time.Sleep(50 * time.Millisecond)

	// Stop all cron jobs and clean up contexts
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Copy contexts to avoid holding lock during cron operations
	contextsToStop := make([]*SmartHPAContext, 0, len(s.contexts))
	for _, ctx := range s.contexts {
		contextsToStop = append(contextsToStop, ctx)
	}

	// Stop all cron jobs
	for _, ctx := range contextsToStop {
		if ctx.cron != nil {
			ctx.cron.Stop()
		}
		// Stop all trigger schedules
		ctx.mutex.RLock()
		schedules := make([]*TriggerSchedule, 0, len(ctx.schedules))
		for _, schedule := range ctx.schedules {
			schedules = append(schedules, schedule)
		}
		ctx.mutex.RUnlock()

		for _, schedule := range schedules {
			schedule.mutex.Lock()
			if schedule.cron != nil {
				schedule.cron.Stop()
			}
			schedule.mutex.Unlock()
		}
	}

	// Clear the contexts map
	s.contexts = make(map[types.NamespacedName]*SmartHPAContext)
}

// For testing purposes
var nowFunc = sarabalaiov1alpha1.NowFunc

func (ts *TriggerSchedule) parseTimeString(timeStr string) (time.Time, error) {
	// Handle empty string
	if timeStr == "" {
		return time.Time{}, fmt.Errorf("empty time string")
	}

	// Load timezone
	loc, err := time.LoadLocation(ts.Trigger.Timezone)
	if err != nil {
		klog.Warningf("Failed to load timezone %s, using UTC: %v", ts.Trigger.Timezone, err)
		loc = time.UTC
	}

	// Parse the time string in format "15:04:05" (hour:minute:second)
	t, err := time.Parse("15:04:05", timeStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid time format: %v", err)
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

func (ts *TriggerSchedule) isWithinTimeWindow(now time.Time) (TimeWindowState, error) {
	// Parse start and end times
	startTime, err := ts.parseTimeString(ts.Trigger.StartTime)
	if err != nil {
		return Unknown, fmt.Errorf("failed to parse start time: %v", err)
	}

	endTime, err := ts.parseTimeString(ts.Trigger.EndTime)
	if err != nil {
		return Unknown, fmt.Errorf("failed to parse end time: %v", err)
	}

	// Load timezone, default to UTC if not specified or invalid
	loc, err := time.LoadLocation(ts.Trigger.Timezone)
	if err != nil {
		klog.Warningf("Failed to load timezone %s for trigger %s, using UTC: %v", ts.Trigger.Timezone, ts.Trigger.Name, err)
		loc = time.UTC
	}

	// Get current time in the specified timezone
	currentTime := now.In(loc)

	// Extract hours and minutes for comparison
	currentHour := currentTime.Hour()
	currentMinute := currentTime.Minute()
	startHour := startTime.Hour()
	startMinute := startTime.Minute()
	endHour := endTime.Hour()
	endMinute := endTime.Minute()

	klog.Infof("Time comparison for trigger %s:\n  Current: %02d:%02d\n  Start: %02d:%02d\n  End: %02d:%02d",
		ts.Trigger.Name, currentHour, currentMinute, startHour, startMinute, endHour, endMinute)

	// Check if current time is within the time window
	if ts.Trigger.NeedRecurring() {
		// Convert times to minutes since midnight for easier comparison
		current := currentHour*60 + currentMinute
		start := startHour*60 + startMinute
		end := endHour*60 + endMinute

		if start <= end {
			// Normal time window (e.g., 09:00-17:00)
			if current >= start && current < end {
				return Within, nil
			}
		} else {
			// Overnight time window (e.g., 22:00-06:00)
			if current >= start || current < end {
				return Within, nil
			}
		}

		if current >= end {
			return After, nil
		}
		return Before, nil
	}

	return Unknown, nil
}

func (ts *TriggerSchedule) applyConfigBasedOnPriority() {
	if ts.Trigger.Priority != nil {
		klog.Infof("Applying config for trigger %s with priority %d", ts.Trigger.Name, *ts.Trigger.Priority)
		if err := ts.UpdateHPAConfig(*ts.Trigger.StartHPAConfig); err != nil {
			klog.Errorf("Failed to apply config for trigger %s: %v", ts.Trigger.Name, err)
		}
	} else {
		klog.Infof("No priority specified for trigger %s, applying default config", ts.Trigger.Name)
		if err := ts.UpdateHPAConfig(*ts.Trigger.StartHPAConfig); err != nil {
			klog.Errorf("Failed to apply default config for trigger %s: %v", ts.Trigger.Name, err)
		}
	}
}

func (ts *TriggerSchedule) Schedule() {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()
	klog.Infof("Scheduling trigger %s", ts.Trigger.Name)
	klog.Infof("Trigger %s start time: %s", ts.Trigger.Name, ts.Trigger.StartTime)
	klog.Infof("Trigger %s end time: %s", ts.Trigger.Name, ts.Trigger.EndTime)

	// Check if current time is within the time window
	state, err := ts.isWithinTimeWindow(nowFunc())
	if err != nil {
		klog.Errorf("Failed to check time window for trigger %s: %v", ts.Trigger.Name, err)
		return
	}

	// Apply initial configuration based on current time and priority
	if state == Within {
		// Check if there's a higher priority schedule active
		higherPriorityActive := false
		thisPriority := 0
		if ts.Trigger.Priority != nil {
			thisPriority = *ts.Trigger.Priority
		}

		// Get all active schedules
		for _, schedule := range ts.context.schedules {
			if schedule.Trigger.Name == ts.Trigger.Name {
				continue
			}

			// Check if this schedule is active and has higher priority
			scheduleState, err := schedule.isWithinTimeWindow(nowFunc())
			if err != nil {
				klog.Errorf("Failed to check time window for schedule %s: %v", schedule.Trigger.Name, err)
				continue
			}

			if scheduleState == Within {
				schedulePriority := 0
				if schedule.Trigger.Priority != nil {
					schedulePriority = *schedule.Trigger.Priority
				}

				if schedulePriority > thisPriority {
					higherPriorityActive = true
					break
				}
			}
		}

		// Only apply configuration if no higher priority schedule is active
		if !higherPriorityActive {
			if err := ts.UpdateHPAConfig(*ts.Trigger.StartHPAConfig); err != nil {
				klog.Errorf("Failed to update HPA config for trigger %s: %v", ts.Trigger.Name, err)
			}
		}
	} else if state == After {
		// If we're after the time window, apply the end config
		if ts.Trigger.EndHPAConfig != nil {
			if err := ts.UpdateHPAConfig(*ts.Trigger.EndHPAConfig); err != nil {
				klog.Errorf("Failed to apply end config for trigger %s: %v", ts.Trigger.Name, err)
			}
		}
	}

	// Schedule start and end times
	startCron, err := ts.GetCronTab(ts.Trigger.StartTime)
	if err != nil {
		klog.Errorf("Failed to get start cron tab for trigger %s: %v", ts.Trigger.Name, err)
		return
	}

	endCron, err := ts.GetCronTab(ts.Trigger.EndTime)
	if err != nil {
		klog.Errorf("Failed to get end cron tab for trigger %s: %v", ts.Trigger.Name, err)
		return
	}

	klog.Infof("Scheduling trigger %s from %s to %s", ts.Trigger.Name, startCron, endCron)

	// Add start cron job
	_, err = ts.cron.AddFunc(startCron, func() {
		klog.Infof("Starting trigger %s", ts.Trigger.Name)
		if err := ts.UpdateHPAConfig(*ts.Trigger.StartHPAConfig); err != nil {
			klog.Errorf("Failed to apply start config for trigger %s: %v", ts.Trigger.Name, err)
		}
	})
	if err != nil {
		klog.Errorf("Failed to add start cron for trigger %s: %v", ts.Trigger.Name, err)
		return
	}

	// Add end cron job
	_, err = ts.cron.AddFunc(endCron, func() {
		klog.Infof("Ending trigger %s", ts.Trigger.Name)
		if ts.Trigger.EndHPAConfig != nil {
			if err := ts.UpdateHPAConfig(*ts.Trigger.EndHPAConfig); err != nil {
				klog.Errorf("Failed to apply end config for trigger %s: %v", ts.Trigger.Name, err)
			}
		}
		// Find and apply next highest priority config
		if ts.context != nil {
			ts.context.applyNextHighestPriorityConfig(ts)
		}
	})
	if err != nil {
		klog.Errorf("Failed to add end cron for trigger %s: %v", ts.Trigger.Name, err)
		return
	}

	// Start the cron scheduler
	ts.cron.Start()
}

func (ts *TriggerSchedule) UpdateHPAConfig(config sarabalaiov1alpha1.HPAConfig) error {
	klog.Infof("Updating HPA %s with config %v", ts.HPANamespacedName.String(), config)
	// Get the HPA object
	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	err := ts.client.Get(context.Background(), ts.HPANamespacedName, hpa)
	klog.Infof("Got HPA %s", hpa.String())
	if err != nil {
		klog.Errorf("Failed to get HPA %s: %v", ts.HPANamespacedName.String(), err)
		return err
	}

	// Create a copy of the HPA for updates
	hpaCopy := hpa.DeepCopy()

	// Update the HPA spec with new values
	if config.MinReplicas != nil {
		if *config.MinReplicas < 0 {
			return fmt.Errorf("minReplicas cannot be negative")
		}
		hpaCopy.Spec.MinReplicas = config.MinReplicas
	}
	if config.MaxReplicas != nil {
		if *config.MaxReplicas <= 0 {
			return fmt.Errorf("maxReplicas must be greater than 0")
		}
		hpaCopy.Spec.MaxReplicas = *config.MaxReplicas
	}

	// Validate min <= max
	if hpaCopy.Spec.MinReplicas != nil && *hpaCopy.Spec.MinReplicas > hpaCopy.Spec.MaxReplicas {
		return fmt.Errorf("minReplicas (%d) cannot be greater than maxReplicas (%d)", *hpaCopy.Spec.MinReplicas, hpaCopy.Spec.MaxReplicas)
	}
	//add log
	klog.Infof("Validated min=%v, max=%v", hpaCopy.Spec.MinReplicas, hpaCopy.Spec.MaxReplicas)
	// Update the HPA object
	err = ts.client.Update(context.Background(), hpaCopy)
	if err != nil {
		klog.Errorf("Failed to update HPA %s: %v", ts.HPANamespacedName.String(), err)
		return err
	}

	klog.Infof("Successfully updated HPA %s with min=%v, max=%v",
		ts.HPANamespacedName.String(),
		hpaCopy.Spec.MinReplicas,
		hpaCopy.Spec.MaxReplicas)

	return nil
}

// applyNextHighestPriorityConfig finds and applies the next highest priority active schedule
func (c *SmartHPAContext) applyNextHighestPriorityConfig(currentSchedule *TriggerSchedule) {
	// Get all active schedules sorted by priority
	var activeSchedules []*TriggerSchedule
	for _, schedule := range c.schedules {
		if schedule.Trigger.Name == currentSchedule.Trigger.Name {
			continue
		}

		// Check if schedule is currently active
		state, err := schedule.isWithinTimeWindow(nowFunc())
		if err != nil {
			klog.Errorf("Failed to check time window for schedule %s: %v", schedule.Trigger.Name, err)
			continue
		}

		if state == Within && schedule.Trigger.NeedRecurring() {
			activeSchedules = append(activeSchedules, schedule)
		}
	}

	// Sort schedules by priority (highest first)
	sort.Slice(activeSchedules, func(i, j int) bool {
		iPriority := 0
		if activeSchedules[i].Trigger.Priority != nil {
			iPriority = *activeSchedules[i].Trigger.Priority
		}

		jPriority := 0
		if activeSchedules[j].Trigger.Priority != nil {
			jPriority = *activeSchedules[j].Trigger.Priority
		}

		return iPriority > jPriority
	})

	// Apply the highest priority active schedule's config
	if len(activeSchedules) > 0 {
		klog.Infof("Applying next highest priority config from schedule %s", activeSchedules[0].Trigger.Name)
		if err := activeSchedules[0].UpdateHPAConfig(*activeSchedules[0].Trigger.StartHPAConfig); err != nil {
			klog.Errorf("Failed to apply config from schedule %s: %v", activeSchedules[0].Trigger.Name, err)
		}
	} else {
		// If no active schedules, apply the default config
		klog.Infof("No active schedules found, applying default config")
		if currentSchedule.Trigger.EndHPAConfig != nil {
			if err := currentSchedule.UpdateHPAConfig(*currentSchedule.Trigger.EndHPAConfig); err != nil {
				klog.Errorf("Failed to apply default config: %v", err)
			}
		}
	}
}

func (sc *SmartHPAContext) execute(ctx context.Context) {
	// Get a safe copy of schedules
	sc.mutex.RLock()
	schedulesCopy := make(map[string]*TriggerSchedule, len(sc.schedules))
	for k, v := range sc.schedules {
		schedulesCopy[k] = v
	}
	sc.mutex.RUnlock()

	klog.Infof("Executing SmartHPAContext with %d schedules", len(schedulesCopy))

	// Create a slice of schedules for sorting by priority
	var activeSchedules []*TriggerSchedule
	for _, schedule := range schedulesCopy {
		// Set the context reference for priority handling
		schedule.mutex.Lock()
		schedule.context = sc
		schedule.mutex.Unlock()
		klog.Infof("schedule.Trigger.NeedRecurring()=%v", schedule.Trigger.NeedRecurring())
		if schedule.Trigger != nil && schedule.Trigger.NeedRecurring() {
			activeSchedules = append(activeSchedules, schedule)
		}
	}

	// Sort schedules by priority (highest to lowest)
	sort.Slice(activeSchedules, func(i, j int) bool {
		// Handle nil priority values
		iPriority := 0
		if activeSchedules[i].Trigger.Priority != nil {
			iPriority = *activeSchedules[i].Trigger.Priority
		}
		jPriority := 0
		if activeSchedules[j].Trigger.Priority != nil {
			jPriority = *activeSchedules[j].Trigger.Priority
		}
		return iPriority > jPriority
	})

	// Process schedules in priority order
	sc.mutex.Lock()
	defer sc.mutex.Unlock()
	klog.Infof("iterating activeschedules, %v", activeSchedules)
	schedulemap := make(map[string]bool)
	for _, schedule := range activeSchedules {
		//log
		klog.Infof("trigger name=%s", schedule.Trigger.Name)
		if schedulemap[schedule.Trigger.Name] {
			continue
		}
		priority := 0
		if schedule.Trigger.Priority != nil {
			priority = *schedule.Trigger.Priority
		}
		klog.Infof("Executing schedule %s with priority %d", schedule.Trigger.Name, priority)
		schedulemap[schedule.Trigger.Name] = true
		schedule.Schedule()
	}

	// Add a recurring job to refresh the context daily
	if _, err := sc.cron.AddFunc("0 0 * * *", func() {
		klog.Infof("Daily refresh: Executing SmartHPAContext with %d schedules", len(sc.schedules))
		sc.execute(ctx)
	}); err != nil {
		klog.Errorf("Failed to add daily refresh cron: %v", err)
	}
}
