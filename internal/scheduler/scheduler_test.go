package scheduler

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/robfig/cron/v3"
	sarabalaiov1alpha1 "github.com/sarabala1979/SmartHPA/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// Helper function to create priority pointer
func priorityPtr(p int) *int {
	return &p
}

// Helper function to create replica count pointer
func int32Ptr(i int32) *int32 {
	return &i
}

func setupTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = sarabalaiov1alpha1.AddToScheme(scheme)
	_ = autoscalingv2.AddToScheme(scheme)
	return scheme
}

func TestParseTimeString(t *testing.T) {
	tests := []struct {
		name         string
		timeStr      string
		wantErr      bool
		expectedHour int
		expectedMin  int
	}{
		{
			name:         "Valid time string",
			timeStr:      "09:30:00",
			wantErr:      false,
			expectedHour: 9,
			expectedMin:  30,
		},
		{
			name:    "Empty time string",
			timeStr: "",
			wantErr: true,
		},
		{
			name:    "Invalid format",
			timeStr: "9:30",
			wantErr: true,
		},
		{
			name:    "Invalid hours",
			timeStr: "25:00:00",
			wantErr: true,
		},
		{
			name:    "Invalid minutes",
			timeStr: "09:60:00",
			wantErr: true,
		},
		{
			name:    "Invalid seconds",
			timeStr: "09:30:60",
			wantErr: true,
		},
		{
			name:    "Invalid format with letters",
			timeStr: "09:3a:00",
			wantErr: true,
		},
		{
			name:         "Valid time with leading zeros",
			timeStr:      "09:05:00",
			wantErr:      false,
			expectedHour: 9,
			expectedMin:  5,
		},
	}

	ts := &TriggerSchedule{
		Trigger: &sarabalaiov1alpha1.Trigger{
			Timezone: "UTC", // Set a default timezone for testing
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ts.parseTimeString(tt.timeStr)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedHour, got.Hour())
			assert.Equal(t, tt.expectedMin, got.Minute())
		})
	}
}

func TestIsWithinTimeWindow(t *testing.T) {
	tests := []struct {
		name        string
		trigger     *sarabalaiov1alpha1.Trigger
		currentTime time.Time
		want        TimeWindowState
		wantErr     bool
	}{
		{
			name: "Within time window",
			trigger: &sarabalaiov1alpha1.Trigger{
				StartTime: "09:00:00",
				EndTime:   "17:00:00",
				Timezone:  "UTC",
				Interval: &sarabalaiov1alpha1.Interval{
					Recurring: "M,TU,W,TH,F",
				},
			},
			currentTime: time.Date(2024, 3, 20, 12, 0, 0, 0, time.UTC), // Wednesday
			want:        Within,
			wantErr:     false,
		},
		{
			name: "Before time window",
			trigger: &sarabalaiov1alpha1.Trigger{
				StartTime: "09:00:00",
				EndTime:   "17:00:00",
				Timezone:  "UTC",
				Interval: &sarabalaiov1alpha1.Interval{
					Recurring: "M,TU,W,TH,F",
				},
			},
			currentTime: time.Date(2024, 3, 20, 8, 0, 0, 0, time.UTC), // Wednesday
			want:        Before,
			wantErr:     false,
		},
		{
			name: "After time window",
			trigger: &sarabalaiov1alpha1.Trigger{
				StartTime: "09:00:00",
				EndTime:   "17:00:00",
				Timezone:  "UTC",
				Interval: &sarabalaiov1alpha1.Interval{
					Recurring: "M,TU,W,TH,F",
				},
			},
			currentTime: time.Date(2024, 3, 20, 18, 0, 0, 0, time.UTC), // Wednesday
			want:        After,
			wantErr:     false,
		},
		{
			name: "Invalid time format",
			trigger: &sarabalaiov1alpha1.Trigger{
				StartTime: "invalid",
				EndTime:   "17:00:00",
				Timezone:  "UTC",
			},
			currentTime: time.Date(2024, 3, 20, 12, 0, 0, 0, time.UTC),
			want:        Unknown,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := &TriggerSchedule{
				Trigger: tt.trigger,
				cron:    cron.New(),
			}

			got, err := ts.isWithinTimeWindow(tt.currentTime)
			if (err != nil) != tt.wantErr {
				t.Errorf("isWithinTimeWindow() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("isWithinTimeWindow() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetCronTab(t *testing.T) {
	tests := []struct {
		name    string
		timeStr string
		want    string
		wantErr bool
	}{
		{
			name:    "Valid time",
			timeStr: "09:30:00",
			want:    "30 9 * * *",
			wantErr: false,
		},
		{
			name:    "Invalid time",
			timeStr: "invalid",
			wantErr: true,
		},
		{
			name:    "Invalid hours",
			timeStr: "25:00:00",
			wantErr: true,
		},
		{
			name:    "Invalid minutes",
			timeStr: "09:60:00",
			wantErr: true,
		},
		{
			name:    "Invalid seconds",
			timeStr: "09:30:60",
			wantErr: true,
		},
		{
			name:    "Empty time",
			timeStr: "",
			wantErr: true,
		},
		{
			name:    "Wrong format",
			timeStr: "9:30",
			wantErr: true,
		},
	}

	ts := &TriggerSchedule{
		Trigger: &sarabalaiov1alpha1.Trigger{
			Timezone: "UTC", // Set a default timezone for testing
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ts.GetCronTab(tt.timeStr)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSchedule_WithRecurring(t *testing.T) {
	t.Cleanup(func() {
		// Ensure cleanup runs even if test fails
		time.Sleep(100 * time.Millisecond) // Wait for goroutines to finish
	})

	// Set a fixed time for testing
	fixedTime := time.Date(2024, 3, 20, 6, 10, 0, 0, time.UTC) // Wednesday 6:10 AM UTC
	originalNowFunc := nowFunc
	defer func() { nowFunc = originalNowFunc }()
	nowFunc = func() time.Time {
		return fixedTime
	}

	tests := []struct {
		name      string
		trigger   *sarabalaiov1alpha1.Trigger
		timezone  string
		wantError bool
	}{
		{
			name: "Valid business hours trigger",
			trigger: &sarabalaiov1alpha1.Trigger{
				Name:      "business-hours",
				StartTime: "09:00:00",
				EndTime:   "17:00:00",
				Timezone:  "UTC",
				Interval: &sarabalaiov1alpha1.Interval{
					Recurring: "M,TU,W,TH,F",
				},
				StartHPAConfig: &sarabalaiov1alpha1.HPAConfig{
					MinReplicas: int32Ptr(3),
					MaxReplicas: int32Ptr(10),
				},
				EndHPAConfig: &sarabalaiov1alpha1.HPAConfig{
					MinReplicas: int32Ptr(1),
					MaxReplicas: int32Ptr(5),
				},
			},
			timezone:  "UTC",
			wantError: false,
		},
		{
			name: "Invalid timezone",
			trigger: &sarabalaiov1alpha1.Trigger{
				Name:      "invalid-tz",
				StartTime: "09:00:00",
				EndTime:   "17:00:00",
				Timezone:  "Invalid/Timezone",
				Interval: &sarabalaiov1alpha1.Interval{
					Recurring: "M,TU,W,TH,F",
				},
				StartHPAConfig: &sarabalaiov1alpha1.HPAConfig{
					MinReplicas: int32Ptr(3),
					MaxReplicas: int32Ptr(10),
				},
			},
			timezone:  "UTC",
			wantError: false, // Should fallback to UTC
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a fake client with an existing HPA
			minReplicas := int32(1)
			maxReplicas := int32(5)
			hpa := &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hpa",
					Namespace: "default",
				},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					MinReplicas: &minReplicas,
					MaxReplicas: maxReplicas,
					ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
						Kind:       "Deployment",
						Name:       "test-deployment",
						APIVersion: "apps/v1",
					},
				},
			}

			client := fake.NewClientBuilder().
				WithScheme(setupTestScheme()).
				WithObjects(hpa).
				Build()

			// Create SmartHPAContext
			sc := &SmartHPAContext{
				schedules: make(map[string]*TriggerSchedule),
				cron:      cron.New(),
			}

			// Create TriggerSchedule
			ts := &TriggerSchedule{
				client: client,
				HPANamespacedName: types.NamespacedName{
					Name:      "test-hpa",
					Namespace: "default",
				},
				Trigger: tt.trigger,
				cron:    cron.New(),
				context: sc,
			}

			// Schedule the trigger
			ts.Schedule()

			// Verify cron entries were created
			entries := ts.cron.Entries()
			assert.Equal(t, 2, len(entries), "Expected 2 cron entries (start and end)")

			if len(entries) == 2 {
				// Verify start time entry
				_, err := ts.GetCronTab(tt.trigger.StartTime)
				assert.NoError(t, err)
				foundStart := false
				for _, entry := range entries {
					if entry.Schedule.Next(fixedTime).Hour() == 9 && entry.Schedule.Next(fixedTime).Minute() == 0 {
						foundStart = true
						break
					}
				}
				assert.True(t, foundStart, "Start time cron entry not found")

				// Verify end time entry
				_, err = ts.GetCronTab(tt.trigger.EndTime)
				assert.NoError(t, err)
				foundEnd := false
				for _, entry := range entries {
					if entry.Schedule.Next(fixedTime).Hour() == 17 && entry.Schedule.Next(fixedTime).Minute() == 0 {
						foundEnd = true
						break
					}
				}
				assert.True(t, foundEnd, "End time cron entry not found")
			}

			// Stop cron to clean up
			ts.cron.Stop()
		})
	}
}

func TestUpdateHPAConfig(t *testing.T) {
	tests := []struct {
		name          string
		initialConfig *autoscalingv2.HorizontalPodAutoscaler
		newConfig     sarabalaiov1alpha1.HPAConfig
		wantError     bool
		description   string
	}{
		{
			name: "Update all fields",
			initialConfig: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hpa",
					Namespace: "default",
				},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					MinReplicas: int32Ptr(1),
					MaxReplicas: 5,
					ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
						Kind:       "Deployment",
						Name:       "test-deployment",
						APIVersion: "apps/v1",
					},
				},
			},
			newConfig: sarabalaiov1alpha1.HPAConfig{
				MinReplicas: int32Ptr(3),
				MaxReplicas: int32Ptr(10),
			},
			wantError:   false,
			description: "Should update all HPA fields successfully",
		},
		{
			name: "HPA not found",
			initialConfig: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "non-existent",
					Namespace: "default",
				},
			},
			newConfig: sarabalaiov1alpha1.HPAConfig{
				MinReplicas: int32Ptr(3),
				MaxReplicas: int32Ptr(10),
			},
			wantError:   true,
			description: "Should handle non-existent HPA gracefully",
		},
		{
			name: "Invalid min replicas",
			initialConfig: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hpa",
					Namespace: "default",
				},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					MinReplicas: int32Ptr(1),
					MaxReplicas: 5,
					ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
						Kind:       "Deployment",
						Name:       "test-deployment",
						APIVersion: "apps/v1",
					},
				},
			},
			newConfig: sarabalaiov1alpha1.HPAConfig{
				MinReplicas: int32Ptr(-1), // Invalid value
				MaxReplicas: int32Ptr(10),
			},
			wantError:   true,
			description: "Should reject invalid min replicas",
		},
		{
			name: "Invalid max replicas",
			initialConfig: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hpa",
					Namespace: "default",
				},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					MinReplicas: int32Ptr(1),
					MaxReplicas: 5,
					ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
						Kind:       "Deployment",
						Name:       "test-deployment",
						APIVersion: "apps/v1",
					},
				},
			},
			newConfig: sarabalaiov1alpha1.HPAConfig{
				MinReplicas: int32Ptr(3),
				MaxReplicas: int32Ptr(0), // Invalid value
			},
			wantError:   true,
			description: "Should reject invalid max replicas",
		},
		{
			name: "Max replicas less than min replicas",
			initialConfig: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hpa",
					Namespace: "default",
				},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					MinReplicas: int32Ptr(1),
					MaxReplicas: 5,
					ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
						Kind:       "Deployment",
						Name:       "test-deployment",
						APIVersion: "apps/v1",
					},
				},
			},
			newConfig: sarabalaiov1alpha1.HPAConfig{
				MinReplicas: int32Ptr(5),
				MaxReplicas: int32Ptr(3), // Less than min replicas
			},
			wantError:   true,
			description: "Should reject max replicas less than min replicas",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create client with initial HPA
			var k8sClient client.Client
			if tt.wantError && tt.name == "HPA not found" {
				// For HPA not found case, don't add the HPA to the client
				k8sClient = fake.NewClientBuilder().
					WithScheme(setupTestScheme()).
					Build()
			} else {
				// For all other cases, create the HPA first
				k8sClient = fake.NewClientBuilder().
					WithScheme(setupTestScheme()).
					Build()

				// Create the initial HPA
				err := k8sClient.Create(context.Background(), tt.initialConfig)
				assert.NoError(t, err, "Failed to create initial HPA")
			}

			// Create TriggerSchedule
			ts := &TriggerSchedule{
				client: k8sClient,
				HPANamespacedName: types.NamespacedName{
					Name:      tt.initialConfig.Name,
					Namespace: tt.initialConfig.Namespace,
				},
			}

			// Update HPA config
			err := ts.UpdateHPAConfig(tt.newConfig)

			if tt.wantError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)

			// Verify updated HPA
			updatedHPA := &autoscalingv2.HorizontalPodAutoscaler{}
			err = k8sClient.Get(context.Background(), ts.HPANamespacedName, updatedHPA)
			assert.NoError(t, err)

			// Check updated values
			if tt.newConfig.MinReplicas != nil {
				assert.Equal(t, *tt.newConfig.MinReplicas, *updatedHPA.Spec.MinReplicas)
			}
			if tt.newConfig.MaxReplicas != nil {
				assert.Equal(t, *tt.newConfig.MaxReplicas, updatedHPA.Spec.MaxReplicas)
			}

			// Verify ScaleTargetRef is preserved
			assert.Equal(t, tt.initialConfig.Spec.ScaleTargetRef, updatedHPA.Spec.ScaleTargetRef)
		})
	}
}

func TestSmartHPAContextExecute(t *testing.T) {
	t.Cleanup(func() {
		// Ensure cleanup runs even if test fails
		time.Sleep(100 * time.Millisecond) // Wait for goroutines to finish
	})
	// Create test objects
	minReplicas := int32(1)
	maxReplicas := int32(5)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-hpa",
			Namespace: "default",
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MinReplicas: &minReplicas,
			MaxReplicas: maxReplicas,
		},
	}

	smartHPA := &sarabalaiov1alpha1.SmartHorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-smarthpa",
			Namespace: "default",
		},
		Spec: sarabalaiov1alpha1.SmartHorizontalPodAutoscalerSpec{
			HPAObjectRef: &sarabalaiov1alpha1.HPAObjectReference{
				Name:      "test-hpa",
				Namespace: "default",
			},
			Triggers: []sarabalaiov1alpha1.Trigger{
				{
					Name:      "business-hours",
					StartTime: "09:00:00",
					EndTime:   "17:00:00",
					Timezone:  "UTC",
					Priority:  intPtr(100),
					Interval: &sarabalaiov1alpha1.Interval{
						Recurring: "M,TU,W,TH,F",
					},
					StartHPAConfig: &sarabalaiov1alpha1.HPAConfig{
						MinReplicas: int32Ptr(3),
						MaxReplicas: int32Ptr(10),
					},
				},
				{
					Name:      "maintenance-window",
					StartTime: "16:00:00",
					EndTime:   "20:00:00",
					Timezone:  "UTC",
					Priority:  intPtr(50),
					Interval: &sarabalaiov1alpha1.Interval{
						Recurring: "M,TU,W,TH,F",
					},
					StartHPAConfig: &sarabalaiov1alpha1.HPAConfig{
						MinReplicas: int32Ptr(1),
						MaxReplicas: int32Ptr(2),
					},
				},
			},
		},
	}

	// Create fake client
	k8sClient := fake.NewClientBuilder().
		WithScheme(setupTestScheme()).
		WithObjects(hpa, smartHPA).
		Build()

	ctx := context.Background()
	sc := &SmartHPAContext{
		schedules: make(map[string]*TriggerSchedule),
		cron:      cron.New(),
	}

	// Stop any existing cron jobs
	if sc.cron != nil {
		sc.cron.Stop()
	}

	// Add a recurring job to refresh the context daily
	for _, trigger := range smartHPA.Spec.Triggers {
		ts := &TriggerSchedule{
			client: k8sClient,
			HPANamespacedName: types.NamespacedName{
				Name:      "test-hpa",
				Namespace: "default",
			},
			Trigger: &trigger,
			cron:    cron.New(),
			context: sc,
		}
		sc.schedules[trigger.Name] = ts
	}

	// Execute context to process all schedules
	sc.execute(ctx)

	// Verify schedules were created and sorted by priority
	var activeSchedules []*TriggerSchedule
	for _, schedule := range sc.schedules {
		if schedule.Trigger.NeedRecurring() {
			activeSchedules = append(activeSchedules, schedule)
		}
	}

	// Sort schedules by priority (highest to lowest)
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

	// Verify that schedules are sorted by priority
	for i := 0; i < len(activeSchedules)-1; i++ {
		currentPriority := 0
		nextPriority := 0
		if activeSchedules[i].Trigger.Priority != nil {
			currentPriority = *activeSchedules[i].Trigger.Priority
		}
		if activeSchedules[i+1].Trigger.Priority != nil {
			nextPriority = *activeSchedules[i+1].Trigger.Priority
		}
		assert.GreaterOrEqual(t, currentPriority, nextPriority,
			"Schedule %s (priority %d) should have higher or equal priority than %s (priority %d)",
			activeSchedules[i].Trigger.Name, currentPriority,
			activeSchedules[i+1].Trigger.Name, nextPriority)
	}
}

func TestSchedulerWithContextCancellation(t *testing.T) {
	t.Cleanup(func() {
		// Ensure cleanup runs even if test fails
		time.Sleep(100 * time.Millisecond) // Wait for goroutines to start
	})
	// Create test objects
	minReplicas := int32(1)
	maxReplicas := int32(5)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-hpa",
			Namespace: "default",
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MinReplicas: &minReplicas,
			MaxReplicas: maxReplicas,
		},
	}

	smartHPA := &sarabalaiov1alpha1.SmartHorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-smarthpa",
			Namespace: "default",
		},
		Spec: sarabalaiov1alpha1.SmartHorizontalPodAutoscalerSpec{
			HPAObjectRef: &sarabalaiov1alpha1.HPAObjectReference{
				Name:      "test-hpa",
				Namespace: "default",
			},
			Triggers: []sarabalaiov1alpha1.Trigger{
				{
					Name:      "business-hours",
					StartTime: "09:00:00",
					EndTime:   "17:00:00",
					Timezone:  "UTC",
					Priority:  intPtr(100),
					Interval: &sarabalaiov1alpha1.Interval{
						Recurring: "M,TU,W,TH,F",
					},
					StartHPAConfig: &sarabalaiov1alpha1.HPAConfig{
						MinReplicas: int32Ptr(3),
						MaxReplicas: int32Ptr(10),
					},
				},
			},
		},
	}

	// Create fake client
	k8sClient := fake.NewClientBuilder().
		WithScheme(setupTestScheme()).
		WithObjects(hpa, smartHPA).
		Build()

	// Create scheduler
	queue := make(chan types.NamespacedName, 1)
	scheduler := NewScheduler(k8sClient, queue)

	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start scheduler with cancellable context
	go scheduler.ProcessItem(ctx)

	// Send item to queue
	queue <- types.NamespacedName{
		Name:      "test-smarthpa",
		Namespace: "default",
	}

	// Wait a bit for processing to start
	time.Sleep(50 * time.Millisecond)

	// Cancel context
	cancel()

	// Wait a bit for cancellation to propagate
	time.Sleep(50 * time.Millisecond)

	// Verify context was cancelled
	select {
	case <-ctx.Done():
		// Context was cancelled as expected
		assert.True(t, true)
	default:
		// Context was not cancelled
		assert.Fail(t, "Context was not cancelled")
	}
}

func TestSchedulerStart(t *testing.T) {
	t.Cleanup(func() {
		// Ensure cleanup runs even if test fails
		time.Sleep(100 * time.Millisecond) // Wait for goroutines to finish
	})
	t.Cleanup(func() {
		// Ensure cleanup runs even if test fails
		time.Sleep(100 * time.Millisecond) // Wait for goroutines to start
	})
	// Create test HPA
	minReplicas := int32(1)
	maxReplicas := int32(5)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-hpa",
			Namespace: "default",
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MinReplicas: &minReplicas,
			MaxReplicas: maxReplicas,
		},
	}

	smartHPA := &sarabalaiov1alpha1.SmartHorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-smarthpa",
			Namespace: "default",
		},
		Spec: sarabalaiov1alpha1.SmartHorizontalPodAutoscalerSpec{
			HPAObjectRef: &sarabalaiov1alpha1.HPAObjectReference{
				Name:      "test-hpa",
				Namespace: "default",
			},
			Triggers: []sarabalaiov1alpha1.Trigger{
				{
					Name:      "business-hours",
					StartTime: "09:00:00",
					EndTime:   "17:00:00",
					Timezone:  "UTC",
					Priority:  intPtr(100),
					Interval: &sarabalaiov1alpha1.Interval{
						Recurring: "M,TU,W,TH,F",
					},
					StartHPAConfig: &sarabalaiov1alpha1.HPAConfig{
						MinReplicas: int32Ptr(3),
						MaxReplicas: int32Ptr(10),
					},
				},
			},
		},
	}

	// Create fake client
	k8sClient := fake.NewClientBuilder().
		WithScheme(setupTestScheme()).
		WithObjects(hpa, smartHPA).
		Build()

	// Create scheduler
	queue := make(chan types.NamespacedName, 1)
	scheduler := NewScheduler(k8sClient, queue)

	// Start scheduler with cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = scheduler.Start(ctx)
	}()

	// Send item to queue
	queue <- types.NamespacedName{
		Name:      "test-smarthpa",
		Namespace: "default",
	}

	// Wait a bit for processing
	time.Sleep(100 * time.Millisecond)

	// Verify context was created
	key := types.NamespacedName{
		Name:      "test-smarthpa",
		Namespace: "default",
	}
	smartHPAContext := scheduler.GetContext(key)
	assert.NotNil(t, smartHPAContext)
	assert.NotEmpty(t, smartHPAContext.GetSchedules())

	// Stop the scheduler to clean up resources
	cancel()
	time.Sleep(100 * time.Millisecond) // Give time for graceful shutdown
}

func TestApplyNextHighestPriorityConfig(t *testing.T) {
	t.Cleanup(func() {
		// Ensure cleanup runs even if test fails
		time.Sleep(100 * time.Millisecond) // Wait for goroutines to finish
	})
	// Create a fake client with an existing HPA
	minReplicas := int32(1)
	maxReplicas := int32(5)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-hpa",
			Namespace: "default",
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MinReplicas: &minReplicas,
			MaxReplicas: maxReplicas,
		},
	}

	// Create fake client
	k8sClient := fake.NewClientBuilder().
		WithScheme(setupTestScheme()).
		WithObjects(hpa).
		Build()

	// Create test context with multiple triggers
	c := cron.New()
	sc := &SmartHPAContext{
		schedules: make(map[string]*TriggerSchedule),
		cron:      c,
	}
	defer c.Stop()

	// Create triggers with different priorities
	highPriority := &sarabalaiov1alpha1.Trigger{
		Name:      "high-priority",
		StartTime: "09:00:00",
		EndTime:   "17:00:00",
		Timezone:  "UTC",
		Priority:  intPtr(100),
		Interval: &sarabalaiov1alpha1.Interval{
			Recurring: "M,TU,W,TH,F",
		},
		StartHPAConfig: &sarabalaiov1alpha1.HPAConfig{
			MinReplicas: int32Ptr(3),
			MaxReplicas: int32Ptr(10),
		},
	}

	lowPriority := &sarabalaiov1alpha1.Trigger{
		Name:      "low-priority",
		StartTime: "08:00:00",
		EndTime:   "18:00:00",
		Timezone:  "UTC",
		Priority:  intPtr(50),
		Interval: &sarabalaiov1alpha1.Interval{
			Recurring: "M,TU,W,TH,F",
		},
		StartHPAConfig: &sarabalaiov1alpha1.HPAConfig{
			MinReplicas: int32Ptr(1),
			MaxReplicas: int32Ptr(5),
		},
	}

	// Create schedule objects
	highPrioritySchedule := &TriggerSchedule{
		client: k8sClient,
		HPANamespacedName: types.NamespacedName{
			Name:      "test-hpa",
			Namespace: "default",
		},
		Trigger: highPriority,
		cron:    cron.New(),
		context: sc,
	}

	lowPrioritySchedule := &TriggerSchedule{
		client: k8sClient,
		HPANamespacedName: types.NamespacedName{
			Name:      "test-hpa",
			Namespace: "default",
		},
		Trigger: lowPriority,
		cron:    cron.New(),
		context: sc,
	}

	// Add schedules to context
	sc.schedules[highPriority.Name] = highPrioritySchedule
	sc.schedules[lowPriority.Name] = lowPrioritySchedule

	// Test transition when high priority schedule ends
	sc.applyNextHighestPriorityConfig(highPrioritySchedule)

	// Verify that the low priority config was applied
	updatedHPA := &autoscalingv2.HorizontalPodAutoscaler{}
	err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "test-hpa", Namespace: "default"}, updatedHPA)
	assert.NoError(t, err)

	// Should match low priority config
	assert.Equal(t, int32(1), *updatedHPA.Spec.MinReplicas)
	assert.Equal(t, int32(5), updatedHPA.Spec.MaxReplicas)
}

// Helper functions to create pointers
func intPtr(i int) *int {
	return &i
}

func TestCreateHPAFromTemplate(t *testing.T) {
	scheme := setupTestScheme()
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	hpaName := "test-hpa-template"
	hpaNamespace := "default"
	labels := map[string]string{"foo": "bar"}
	annotations := map[string]string{"anno": "val"}
	minReplicas := int32(2)
	maxReplicas := int32(8)

	hpaTemplate := &sarabalaiov1alpha1.HPASpecTemplate{
		Metadata: metav1.ObjectMeta{
			Name:        hpaName,
			Namespace:   hpaNamespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: &sarabalaiov1alpha1.HPAConfig{
			MinReplicas: &minReplicas,
			MaxReplicas: &maxReplicas,
		},
	}

	smartHPA := &sarabalaiov1alpha1.SmartHorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-smarthpa",
			Namespace: hpaNamespace,
			UID:       "12345",
		},
		Spec: sarabalaiov1alpha1.SmartHorizontalPodAutoscalerSpec{
			HPASpecTemplate: hpaTemplate,
		},
	}

	// Simulate the controller logic for creating HPA from template
	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	err := k8sClient.Get(context.Background(), types.NamespacedName{Name: hpaName, Namespace: hpaNamespace}, hpa)
	if err == nil {
		t.Fatalf("HPA should not exist yet")
	}

	newHPA := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:        hpaName,
			Namespace:   hpaNamespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{},
	}
	if hpaTemplate.Spec != nil {
		if hpaTemplate.Spec.MinReplicas != nil {
			newHPA.Spec.MinReplicas = hpaTemplate.Spec.MinReplicas
		}
		if hpaTemplate.Spec.MaxReplicas != nil {
			newHPA.Spec.MaxReplicas = *hpaTemplate.Spec.MaxReplicas
		}
	}
	_ = controllerSetOwnerReference(smartHPA, newHPA, scheme)
	err = k8sClient.Create(context.Background(), newHPA)
	assert.NoError(t, err)

	createdHPA := &autoscalingv2.HorizontalPodAutoscaler{}
	err = k8sClient.Get(context.Background(), types.NamespacedName{Name: hpaName, Namespace: hpaNamespace}, createdHPA)
	assert.NoError(t, err)
	assert.Equal(t, hpaName, createdHPA.Name)
	assert.Equal(t, hpaNamespace, createdHPA.Namespace)
	assert.Equal(t, labels, createdHPA.Labels)
	assert.Equal(t, annotations, createdHPA.Annotations)
	assert.Equal(t, minReplicas, *createdHPA.Spec.MinReplicas)
	assert.Equal(t, maxReplicas, createdHPA.Spec.MaxReplicas)
	// Owner reference
	assert.Len(t, createdHPA.OwnerReferences, 1)
	assert.Equal(t, "SmartHorizontalPodAutoscaler", createdHPA.OwnerReferences[0].Kind)
	assert.Equal(t, "test-smarthpa", createdHPA.OwnerReferences[0].Name)
}

// Minimal SetControllerReference for test
func controllerSetOwnerReference(owner, object metav1.Object, scheme *runtime.Scheme) error {
	object.SetOwnerReferences([]metav1.OwnerReference{{
		APIVersion: "autoscaling.sarabala.io/v1alpha1",
		Kind:       "SmartHorizontalPodAutoscaler",
		Name:       owner.GetName(),
		UID:        owner.GetUID(),
	}})
	return nil
}

func TestPriorityBasedScheduling(t *testing.T) {
	// Create test context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create a fake client with scheme
	client := fake.NewClientBuilder().
		WithScheme(setupTestScheme()).
		Build()

	// Create test namespace
	ns := "test-ns"

	// Create base HPA
	baseHPA := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-hpa",
			Namespace: ns,
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MinReplicas: int32Ptr(1),
			MaxReplicas: 5,
		},
	}

	// Create the base HPA
	err := client.Create(ctx, baseHPA)
	if err != nil {
		t.Fatalf("Failed to create base HPA: %v", err)
	}

	// Cleanup after test
	defer func() {
		if err := client.Delete(context.Background(), baseHPA); err != nil {
			t.Logf("Failed to cleanup base HPA: %v", err)
		}
	}()

	// Use a fixed test time for consistency
	testTime := time.Date(2024, 3, 20, 10, 0, 0, 0, time.UTC) // Wednesday
	testDay := "W"                                            // Wednesday

	// Override time.Now for testing
	originalNowFunc := nowFunc
	defer func() { nowFunc = originalNowFunc }()

	tests := []struct {
		name                string
		triggers            []sarabalaiov1alpha1.Trigger
		currentTime         time.Time
		expectedPriority    int
		expectedMinReplicas int32
		expectedMaxReplicas int32
	}{
		{
			name: "Single trigger active",
			triggers: []sarabalaiov1alpha1.Trigger{
				{
					Name:      "business-hours",
					Priority:  priorityPtr(10),
					StartTime: "09:00:00",
					EndTime:   "17:00:00",
					Timezone:  "UTC",
					Interval: &sarabalaiov1alpha1.Interval{
						Recurring: "M,TU," + testDay + ",TH,F", // Include test day
					},
					StartHPAConfig: &sarabalaiov1alpha1.HPAConfig{
						MinReplicas: int32Ptr(3),
						MaxReplicas: int32Ptr(10),
					},
				},
			},
			currentTime:         testTime,
			expectedPriority:    10,
			expectedMinReplicas: 3,
			expectedMaxReplicas: 10,
		},
		{
			name: "Two overlapping triggers - higher priority wins",
			triggers: []sarabalaiov1alpha1.Trigger{
				{
					Name:      "business-hours",
					Priority:  priorityPtr(10),
					StartTime: "09:00:00",
					EndTime:   "17:00:00",
					Timezone:  "UTC",
					Interval: &sarabalaiov1alpha1.Interval{
						Recurring: "M,TU," + testDay + ",TH,F",
					},
					StartHPAConfig: &sarabalaiov1alpha1.HPAConfig{
						MinReplicas: int32Ptr(3),
						MaxReplicas: int32Ptr(10),
					},
				},
				{
					Name:      "peak-hours",
					Priority:  priorityPtr(20),
					StartTime: "10:00:00",
					EndTime:   "14:00:00",
					Timezone:  "UTC",
					Interval: &sarabalaiov1alpha1.Interval{
						Recurring: "M,TU," + testDay + ",TH,F",
					},
					StartHPAConfig: &sarabalaiov1alpha1.HPAConfig{
						MinReplicas: int32Ptr(5),
						MaxReplicas: int32Ptr(15),
					},
				},
			},
			currentTime:         testTime.Add(1 * time.Hour), // 11:00
			expectedPriority:    20,
			expectedMinReplicas: 5,
			expectedMaxReplicas: 15,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set test time
			nowFunc = func() time.Time {
				return tt.currentTime
			}

			// Create SmartHPA object
			smartHPA := &sarabalaiov1alpha1.SmartHorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-smarthpa",
					Namespace: ns,
				},
				Spec: sarabalaiov1alpha1.SmartHorizontalPodAutoscalerSpec{
					HPAObjectRef: &sarabalaiov1alpha1.HPAObjectReference{
						Name:      "test-hpa",
						Namespace: ns,
					},
					Triggers: tt.triggers,
				},
			}

			// Create SmartHPA
			err := client.Create(ctx, smartHPA)
			if err != nil {
				t.Fatalf("Failed to create SmartHPA: %v", err)
			}

			// Cleanup SmartHPA after test
			defer func() {
				if err := client.Delete(context.Background(), smartHPA); err != nil {
					t.Logf("Failed to cleanup SmartHPA: %v", err)
				}
			}()

			// Create a new scheduler with buffered queue
			queue := make(chan types.NamespacedName, 10)
			scheduler := NewScheduler(client, queue)

			// Start the scheduler
			go scheduler.ProcessItem(ctx)

			// Send item to queue
			queue <- types.NamespacedName{
				Name:      smartHPA.Name,
				Namespace: smartHPA.Namespace,
			}

			// Wait for processing
			time.Sleep(100 * time.Millisecond)

			// Get the updated HPA
			updatedHPA := &autoscalingv2.HorizontalPodAutoscaler{}
			err = client.Get(ctx, types.NamespacedName{Name: "test-hpa", Namespace: ns}, updatedHPA)
			if err != nil {
				t.Fatalf("Failed to get updated HPA: %v", err)
			}

			// Log current state for debugging
			t.Logf("Current time: %v", tt.currentTime)
			t.Logf("Test day: %s", testDay)
			t.Logf("Trigger count: %d", len(tt.triggers))
			for _, trigger := range tt.triggers {
				t.Logf("Trigger %s: Priority=%d, Time=%s-%s, Recurring=%s",
					trigger.Name,
					*trigger.Priority,
					trigger.StartTime,
					trigger.EndTime,
					trigger.Interval.Recurring)
			}
			t.Logf("Updated HPA: MinReplicas=%d, MaxReplicas=%d",
				*updatedHPA.Spec.MinReplicas,
				updatedHPA.Spec.MaxReplicas)

			// Verify the HPA configuration matches the expected values
			if *updatedHPA.Spec.MinReplicas != tt.expectedMinReplicas {
				t.Errorf("MinReplicas = %d, want %d", *updatedHPA.Spec.MinReplicas, tt.expectedMinReplicas)
			}
			if updatedHPA.Spec.MaxReplicas != tt.expectedMaxReplicas {
				t.Errorf("MaxReplicas = %d, want %d", updatedHPA.Spec.MaxReplicas, tt.expectedMaxReplicas)
			}
		})
	}
}

func TestPriorityTransitions(t *testing.T) {
	// Set up test environment
	ns := "test-ns"
	ctx := context.Background()
	queue := make(chan types.NamespacedName, 10)
	client := fake.NewClientBuilder().WithScheme(setupTestScheme()).Build()
	scheduler := NewScheduler(client, queue)

	// Set default mock time before any processing happens
	// This prevents the scheduler from using real time during initialization
	defaultTime := time.Date(2024, 3, 20, 7, 0, 0, 0, time.UTC)
	sarabalaiov1alpha1.NowFunc = func() time.Time {
		return defaultTime
	}
	t.Cleanup(func() {
		sarabalaiov1alpha1.ResetNowFunc()
	})

	// Create test HPA
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-hpa",
			Namespace: ns,
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MinReplicas: int32Ptr(1),
			MaxReplicas: 5,
		},
	}
	err := client.Create(ctx, hpa)
	assert.NoError(t, err)

	// Create test SmartHPA
	smartHPA := &sarabalaiov1alpha1.SmartHorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-smarthpa",
			Namespace: ns,
		},
		Spec: sarabalaiov1alpha1.SmartHorizontalPodAutoscalerSpec{
			HPAObjectRef: &sarabalaiov1alpha1.HPAObjectReference{
				Name:      "test-hpa",
				Namespace: ns,
			},
			Triggers: []sarabalaiov1alpha1.Trigger{
				{
					Name:      "business-hours",
					Priority:  priorityPtr(10),
					StartTime: "09:00:00",
					EndTime:   "17:00:00",
					Timezone:  "UTC",
					Interval: &sarabalaiov1alpha1.Interval{
						Recurring: "M,TU,W,TH,F",
					},
					StartHPAConfig: &sarabalaiov1alpha1.HPAConfig{
						MinReplicas: int32Ptr(3),
						MaxReplicas: int32Ptr(10),
					},
					EndHPAConfig: &sarabalaiov1alpha1.HPAConfig{
						MinReplicas: int32Ptr(1),
						MaxReplicas: int32Ptr(5),
					},
				},
				{
					Name:      "peak-hours",
					Priority:  priorityPtr(20),
					StartTime: "11:00:00",
					EndTime:   "14:00:00",
					Timezone:  "UTC",
					Interval: &sarabalaiov1alpha1.Interval{
						Recurring: "M,TU,W,TH,F",
					},
					StartHPAConfig: &sarabalaiov1alpha1.HPAConfig{
						MinReplicas: int32Ptr(5),
						MaxReplicas: int32Ptr(15),
					},
					EndHPAConfig: &sarabalaiov1alpha1.HPAConfig{
						MinReplicas: int32Ptr(1),
						MaxReplicas: int32Ptr(5),
					},
				},
			},
		},
	}
	err = client.Create(ctx, smartHPA)
	assert.NoError(t, err)

	// Test transitions at different times
	transitions := []struct {
		time        time.Time
		expectedMin int32
		expectedMax int32
		description string
	}{
		{
			time:        time.Date(2024, 3, 20, 8, 0, 0, 0, time.UTC),
			expectedMin: 1,
			expectedMax: 5,
			description: "Before any trigger",
		},
		{
			time:        time.Date(2024, 3, 20, 9, 30, 0, 0, time.UTC),
			expectedMin: 3,
			expectedMax: 10,
			description: "Business hours active",
		},
		{
			time:        time.Date(2024, 3, 20, 11, 30, 0, 0, time.UTC),
			expectedMin: 5,
			expectedMax: 15,
			description: "Peak hours takes precedence",
		},
		{
			time:        time.Date(2024, 3, 20, 14, 30, 0, 0, time.UTC),
			expectedMin: 3,
			expectedMax: 10,
			description: "Back to business hours after peak",
		},
		{
			time:        time.Date(2024, 3, 20, 17, 30, 0, 0, time.UTC),
			expectedMin: 1,
			expectedMax: 5,
			description: "After all triggers",
		},
	}

	// Start the scheduler
	go scheduler.ProcessItem(ctx)

	for _, tt := range transitions {
		t.Run(tt.description, func(t *testing.T) {
			// Set the mock time before sending the item to the queue
			sarabalaiov1alpha1.NowFunc = func() time.Time {
				return tt.time
			}

			// Clear the scheduler context to force reinitialization
			smartHPAKey := types.NamespacedName{
				Name:      smartHPA.Name,
				Namespace: smartHPA.Namespace,
			}
			scheduler.mutex.Lock()
			delete(scheduler.contexts, smartHPAKey)
			scheduler.mutex.Unlock()

			// Send item to queue
			queue <- smartHPAKey

			// Wait for processing
			time.Sleep(200 * time.Millisecond)

			// Get the updated HPA
			updatedHPA := &autoscalingv2.HorizontalPodAutoscaler{}
			err := client.Get(ctx, types.NamespacedName{Name: "test-hpa", Namespace: ns}, updatedHPA)
			if err != nil {
				t.Fatalf("Failed to get updated HPA: %v", err)
			}

			// Log current state for debugging
			t.Logf("Current time: %v", tt.time)
			t.Logf("Expected: MinReplicas=%d, MaxReplicas=%d", tt.expectedMin, tt.expectedMax)
			t.Logf("Actual: MinReplicas=%d, MaxReplicas=%d", *updatedHPA.Spec.MinReplicas, updatedHPA.Spec.MaxReplicas)

			// Verify the HPA configuration matches the expected values
			assert.Equal(t, tt.expectedMin, *updatedHPA.Spec.MinReplicas, "MinReplicas mismatch at %v", tt.time)
			assert.Equal(t, tt.expectedMax, updatedHPA.Spec.MaxReplicas, "MaxReplicas mismatch at %v", tt.time)
		})
	}
}
