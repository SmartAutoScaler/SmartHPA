package scheduler

import (
	"context"
	"fmt"
	"strings"
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
}

const (
	NotStarted = -1
	Within     = 0
	After      = 1
)

type SmartHPAContext struct {
	client    client.Client
	schedules map[string]*TriggerSchedule
	cron      *cron.Cron
}

// Scheduler manages the scheduling of HPA configurations
type Scheduler struct {
	client        client.Client
	contexts      map[types.NamespacedName]*SmartHPAContext // key: namespace/nam
	queue         chan types.NamespacedName
	DeletionQueue chan types.NamespacedName
}

// NewScheduler creates a new scheduler instance
func NewScheduler(client client.Client, queue chan types.NamespacedName, deletionQueue chan types.NamespacedName) *Scheduler {
	return &Scheduler{
		contexts:      make(map[types.NamespacedName]*SmartHPAContext),
		client:        client,
		queue:         queue,
		DeletionQueue: deletionQueue,
	}
}

func (s *Scheduler) processDeletions(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			klog.Info("Deletion processing loop stopped.")
			return
		case item := <-s.DeletionQueue:
			klog.Infof("Processing deletion for SmartHPA %s", item)
			if hpaContext, exists := s.contexts[item]; exists {
				klog.Infof("Stopping cron schedulers for SmartHPA %s", item)
				// Stop individual trigger cron jobs
				for name, schedule := range hpaContext.schedules {
					klog.Infof("Stopping cron for trigger %s in SmartHPA %s", name, item)
					schedule.cron.Stop() // Stop the cron scheduler for the specific trigger
				}
				// Stop the main cron for the SmartHPAContext (which handles daily refresh)
				klog.Infof("Stopping main cron for SmartHPAContext %s", item)
				hpaContext.cron.Stop()

				delete(s.contexts, item) // Remove from map
				klog.Infof("Cleaned up context for SmartHPA %s", item)
			} else {
				klog.Warningf("Context for SmartHPA %s not found for deletion", item)
			}
		}
	}
}

func (s *Scheduler) ProcessItem(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
		case item := <-s.queue:
			klog.Infof("Processing SmartHPA %s", item)
			var obj sarabalaiov1alpha1.SmartHorizontalPodAutoscaler
			err := s.client.Get(ctx, item, &obj)
			if err != nil {
				klog.Errorf("Error getting SmartHPA %s: %v", item, err)
				continue
			}
			hpaContext := s.contexts[item]
			if hpaContext == nil {
				hpaContext = &SmartHPAContext{
					schedules: make(map[string]*TriggerSchedule),
					cron:      cron.New(cron.WithSeconds()),
				}
				s.contexts[item] = hpaContext
			}
			for _, trigger := range obj.Spec.Triggers {
				// Load timezone
				loc, err := time.LoadLocation(trigger.Timezone)
				if err != nil {
					klog.Errorf("invalid timezone %s: %v", trigger.Timezone, err)
					loc = time.UTC
				}
				hpaContext.schedules[trigger.Name] = &TriggerSchedule{
					client:  s.client,
					Trigger: &trigger,
					//create cron with timezone
					cron: cron.New(cron.WithSeconds(), cron.WithLocation(loc)),
					HPANamespacedName: types.NamespacedName{
						Namespace: obj.Spec.HPAObjectRef.Namespace,
						Name:      obj.Spec.HPAObjectRef.Name,
					},
				}
			}
			hpaContext.cron.Start()
			go hpaContext.execute(ctx)
		}
	}
}

// Start starts the scheduler
func (s *Scheduler) Start(ctx context.Context) {
	for i := 0; i < 10; i++ { // Number of workers for processing items from r.queue
		go s.ProcessItem(ctx)
	}
	// Start a single worker for processing items from DeletionQueue
	go s.processDeletions(ctx)
	klog.Info("Scheduler started with item processing and deletion processing workers")
}

func (ts *TriggerSchedule) parseTimeString(timeStr string) (time.Time, error) {
	// Handle empty string
	if timeStr == "" {
		return time.Time{}, fmt.Errorf("empty time string")
	}
	// Parse the time string in format "15:04:05" (hour:minute:second)
	t, err := time.Parse("15:04:05", timeStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid time format: %v", err)
	}
	// Use current date with the parsed time
	nowInTriggerTimezone := time.Now().In(ts.cron.Location())
	return time.Date(
		nowInTriggerTimezone.Year(),
		nowInTriggerTimezone.Month(),
		nowInTriggerTimezone.Day(),
		t.Hour(),
		t.Minute(),
		t.Second(),
		0,
		ts.cron.Location(), // Use the location from the cron instance
	), nil
}

func (ts *TriggerSchedule) GetCronTab(timeStr string) (string, error) {
	t, err := ts.parseTimeString(timeStr)
	if err != nil {
		return "", err
	}
	// Return in the format that matches the test expectation
	return fmt.Sprintf("0 %d %d * * *", t.Minute(), t.Hour()), nil
}

func (ts *TriggerSchedule) isWithinTimeWindow(currentTime time.Time) (int, error) {
	startTime, err := ts.parseTimeString(ts.Trigger.StartTime)
	if err != nil {
		return 0, err
	}

	endTime, err := ts.parseTimeString(ts.Trigger.EndTime)
	if err != nil {
		return 0, err
	}

	// Convert currentTime to the trigger's timezone
	currentTimeInTriggerTimezone := currentTime.In(ts.cron.Location())

	// Get current hour and minute in trigger's timezone
	currentHourMin := time.Date(currentTimeInTriggerTimezone.Year(), currentTimeInTriggerTimezone.Month(), currentTimeInTriggerTimezone.Day(),
		currentTimeInTriggerTimezone.Hour(), currentTimeInTriggerTimezone.Minute(), 0, 0, ts.cron.Location())

	// Get start and end hour and minute using date from currentTimeInTriggerTimezone and time from parsed start/end times
	// startTime and endTime from parseTimeString should already be in ts.cron.Location()
	startHourMin := time.Date(currentTimeInTriggerTimezone.Year(), currentTimeInTriggerTimezone.Month(), currentTimeInTriggerTimezone.Day(),
		startTime.Hour(), startTime.Minute(), 0, 0, ts.cron.Location())
	endHourMin := time.Date(currentTimeInTriggerTimezone.Year(), currentTimeInTriggerTimezone.Month(), currentTimeInTriggerTimezone.Day(),
		endTime.Hour(), endTime.Minute(), 0, 0, ts.cron.Location())

	klog.Infof("Time comparison for trigger %s (TZ: %s):\n  Current: %02d:%02d\n  Start: %02d:%02d\n  End: %02d:%02d",
		ts.Trigger.Name, ts.cron.Location().String(),
		currentTimeInTriggerTimezone.Hour(), currentTimeInTriggerTimezone.Minute(),
		startTime.Hour(), startTime.Minute(),
		endTime.Hour(), endTime.Minute())

	// Compare times
	if currentHourMin.Equal(startHourMin) || (currentHourMin.After(startHourMin) && currentHourMin.Before(endHourMin)) {
		return Within, nil
	} else if currentHourMin.Before(startHourMin) {
		return NotStarted, nil
	} else {
		return After, nil
	}
}

func (ts *TriggerSchedule) Schedule(ctx context.Context) {
	klog.Infof("Scheduling trigger %s", ts.Trigger.Name)
	klog.Infof("Trigger %s start time: %s", ts.Trigger.Name, ts.Trigger.StartTime)
	klog.Infof("Trigger %s end time: %s", ts.Trigger.Name, ts.Trigger.EndTime)

	// Check if current time is within window
	state, err := ts.isWithinTimeWindow(time.Now())
	if err != nil {
		klog.Errorf("Failed to check time window for trigger %s: %v", ts.Trigger.Name, err)
		return
	}

	// Apply initial configuration based on current time
	if state == Within {
		klog.Infof("Current time is within window, applying start config for trigger %s", ts.Trigger.Name)
		if err := ts.UpdateHPAConfig(ctx, *ts.Trigger.StartHPAConfig); err != nil {
			klog.Errorf("Failed to apply start config for trigger %s: %v", ts.Trigger.Name, err)
		}
	}

	// Set up start cron schedule
	start, err := ts.GetCronTab(ts.Trigger.StartTime)
	if err != nil {
		klog.Errorf("Failed to get start cron for trigger %s: %v", ts.Trigger.Name, err)
		return
	}

	// Set up end cron schedule
	end, err := ts.GetCronTab(ts.Trigger.EndTime)
	if err != nil {
		klog.Errorf("Failed to get end cron for trigger %s: %v", ts.Trigger.Name, err)
		return
	}

	// Format for logs - cron expressions include seconds for display
	klog.Infof("Scheduling trigger %s from %s to %s", ts.Trigger.Name, start, end)

	// For robco/cron library, convert from 6-field to 5-field format by removing the seconds field
	startCron := strings.Join(strings.Split(start, " ")[1:], " ")
	endCron := strings.Join(strings.Split(end, " ")[1:], " ")

	// Add start schedule
	_, err = ts.cron.AddFunc(startCron, func() {
		klog.Infof("Executing trigger %s start schedule", ts.Trigger.Name)
		if err := ts.UpdateHPAConfig(ctx, *ts.Trigger.StartHPAConfig); err != nil {
			klog.Errorf("Failed to update HPA config for trigger %s start: %v", ts.Trigger.Name, err)
		}
	})
	if err != nil {
		klog.Errorf("Failed to add start cron for trigger %s: %v", ts.Trigger.Name, err)
		return
	}

	// Add end schedule
	_, err = ts.cron.AddFunc(endCron, func() {
		klog.Infof("Executing trigger %s end schedule", ts.Trigger.Name)
		if err := ts.UpdateHPAConfig(ctx, *ts.Trigger.EndHPAConfig); err != nil {
			klog.Errorf("Failed to update HPA config for trigger %s end: %v", ts.Trigger.Name, err)
		}
	})
	if err != nil {
		klog.Errorf("Failed to add end cron for trigger %s: %v", ts.Trigger.Name, err)
		return
	}

	ts.cron.Start()
}

func (ts *TriggerSchedule) UpdateHPAConfig(ctx context.Context, config sarabalaiov1alpha1.HPAConfig) error {
	klog.Infof("Updating HPA %s with config %v", ts.HPANamespacedName.String(), config)
	// Get the HPA object
	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	err := ts.client.Get(ctx, ts.HPANamespacedName, hpa)
	klog.Infof("Got HPA %s", hpa.String())
	if err != nil {
		klog.Errorf("Failed to get HPA %s: %v", ts.HPANamespacedName.String(), err)
		return err
	}

	// Update the HPA spec with new values
	if config.MinReplicas != nil {
		hpa.Spec.MinReplicas = config.MinReplicas
	}
	if config.MaxReplicas != nil {
		hpa.Spec.MaxReplicas = *config.MaxReplicas
	}

	// Update the HPA object
	err = ts.client.Update(ctx, hpa)
	if err != nil {
		klog.Errorf("Failed to update HPA %s: %v", ts.HPANamespacedName.String(), err)
		return err
	}

	klog.Infof("Successfully updated HPA %s spec with min=%v, max=%v",
		ts.HPANamespacedName.String(),
		hpa.Spec.MinReplicas,
		hpa.Spec.MaxReplicas)

	return nil
}

func (sc *SmartHPAContext) execute(ctx context.Context) {
	klog.Infof("Executing SmartHPAContext with %d schedules", len(sc.schedules))
	for _, schedule := range sc.schedules {
		klog.Infof("Executing schedule %s", schedule.Trigger.Name)
		if schedule.Trigger != nil {
			if schedule.Trigger.NeedRecurring() {
				klog.Infof("Scheduling trigger %s", schedule.Trigger.Name)
				schedule.Schedule(ctx)
			}
		}
		// Add a recurring job to refresh the context
		sc.cron.AddFunc("0 0 * * *", func() {
			klog.Infof("Executing SmartHPAContext with %d schedules", len(sc.schedules))
			sc.execute(ctx)
		})
	}
}
