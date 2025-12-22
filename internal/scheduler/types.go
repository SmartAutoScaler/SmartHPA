package scheduler

import (
	autoscalingv1alpha1 "github.com/sarabala1979/SmartHPA/api/v1alpha1"
)

// SchedulerInterface defines the interface for scheduler implementations.
type SchedulerInterface interface {
	// Start starts the scheduler.
	Start()

	// Stop stops the scheduler.
	Stop()

	// AddOrUpdateSchedules processes triggers from a SmartHPA object and updates the schedule map.
	AddOrUpdateSchedules(shpa *autoscalingv1alpha1.SmartHorizontalPodAutoscaler) error

	// GetActiveSchedules returns all currently active schedules.
	GetActiveSchedules() []*TriggerSchedule
}
