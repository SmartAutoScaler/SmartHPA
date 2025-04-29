package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/robfig/cron/v3"
	sarabalaiov1alpha1 "github.com/sarabala1979/SmartHPA/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

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
	}

	ts := &TriggerSchedule{}
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

func TestSchedule_WithRecurring(t *testing.T) {
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
				Name:       "test-app",
				APIVersion: "apps/v1",
			},
		},
	}

	client := fake.NewClientBuilder().WithObjects(hpa).Build()

	trigger := &sarabalaiov1alpha1.Trigger{
		Name:      "business-hours",
		StartTime: "09:00:00",
		EndTime:   "17:00:00",
		Timezone:  "UTC",
		Interval: &sarabalaiov1alpha1.Interval{
			Recurring: "M,TU,W,TH,F",
		},
		StartHPAConfig: &sarabalaiov1alpha1.HPAConfig{
			MinReplicas: &minReplicas,
			MaxReplicas: &maxReplicas,
		},
		EndHPAConfig: &sarabalaiov1alpha1.HPAConfig{
			MinReplicas: &minReplicas,
			MaxReplicas: &maxReplicas,
		},
	}

	ts := &TriggerSchedule{
		client: client,
		HPANamespacedName: types.NamespacedName{
			Name:      "test-hpa",
			Namespace: "default",
		},
		Trigger: trigger,
		cron:    cron.New(),
	}

	ts.Schedule()

	// Verify cron jobs were set up correctly
	assert.NotNil(t, ts.cron)
	assert.Equal(t, 2, len(ts.cron.Entries()))
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
			want:    "0 30 9 * * *",
			wantErr: false,
		},
		{
			name:    "Invalid time",
			timeStr: "invalid",
			wantErr: true,
		},
	}

	ts := &TriggerSchedule{}
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

func TestCreateSmartHPAContext(t *testing.T) {
	client := fake.NewClientBuilder().Build()
	ctx := &SmartHPAContext{
		client:    client,
		schedules: make(map[string]*TriggerSchedule),
		cron:      cron.New(),
	}

	assert.NotNil(t, ctx)
	assert.Equal(t, client, ctx.client)
	assert.NotNil(t, ctx.schedules)
	assert.NotNil(t, ctx.cron)
}

func TestIsWithinTimeWindow(t *testing.T) {
	tests := []struct {
		name        string
		startTime   string
		endTime     string
		currentTime string
		want        int
		wantErr     bool
	}{
		{
			name:        "Within time window",
			startTime:   "09:00:00",
			endTime:     "17:00:00",
			currentTime: "13:00:00",
			want:        Within,
			wantErr:     false,
		},
		{
			name:        "Before time window",
			startTime:   "09:00:00",
			endTime:     "17:00:00",
			currentTime: "08:00:00",
			want:        NotStarted,
			wantErr:     false,
		},
		{
			name:        "After time window",
			startTime:   "09:00:00",
			endTime:     "17:00:00",
			currentTime: "18:00:00",
			want:        After,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse the current time
			currentTime, _ := time.Parse("15:04:05", tt.currentTime)

			// Create a fixed date for testing
			baseTime := time.Date(2025, 4, 23, 0, 0, 0, 0, time.UTC)
			currentTime = time.Date(baseTime.Year(), baseTime.Month(), baseTime.Day(),
				currentTime.Hour(), currentTime.Minute(), currentTime.Second(), 0, baseTime.Location())

			ts := &TriggerSchedule{
				Trigger: &sarabalaiov1alpha1.Trigger{
					StartTime: tt.startTime,
					EndTime:   tt.endTime,
					Timezone:  "UTC",
					Interval:  &sarabalaiov1alpha1.Interval{},
				},
			}
			got, err := ts.isWithinTimeWindow(currentTime)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestUpdateHPAConfig(t *testing.T) {
	// Create a fake client with an existing HPA
	minReplicas := int32(1)
	maxReplicas := int32(5)
	desiredReplicas := int32(3)
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
				Name:       "test-app",
				APIVersion: "apps/v1",
			},
		},
	}

	client := fake.NewClientBuilder().WithObjects(hpa).Build()

	tests := []struct {
		name        string
		config      sarabalaiov1alpha1.HPAConfig
		wantErr     bool
		checkValues func(*testing.T, *autoscalingv2.HorizontalPodAutoscaler)
	}{
		{
			name: "Update all values",
			config: sarabalaiov1alpha1.HPAConfig{
				MinReplicas:     &minReplicas,
				MaxReplicas:     &maxReplicas,
				DesiredReplicas: &desiredReplicas,
			},
			wantErr: false,
			checkValues: func(t *testing.T, hpa *autoscalingv2.HorizontalPodAutoscaler) {
				assert.Equal(t, minReplicas, *hpa.Spec.MinReplicas)
				assert.Equal(t, maxReplicas, hpa.Spec.MaxReplicas)
				assert.Equal(t, desiredReplicas, hpa.Status.DesiredReplicas)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := &TriggerSchedule{
				client: client,
				HPANamespacedName: types.NamespacedName{
					Name:      "test-hpa",
					Namespace: "default",
				},
			}
			err := ts.UpdateHPAConfig(tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)

			// Verify HPA was updated correctly
			updatedHPA := &autoscalingv2.HorizontalPodAutoscaler{}
			err = client.Get(context.Background(), ts.HPANamespacedName, updatedHPA)
			assert.NoError(t, err)
			tt.checkValues(t, updatedHPA)
		})
	}
}

func TestSchedule(t *testing.T) {
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
				Name:       "test-app",
				APIVersion: "apps/v1",
			},
		},
	}

	client := fake.NewClientBuilder().WithObjects(hpa).Build()

	tests := []struct {
		name      string
		trigger   *sarabalaiov1alpha1.Trigger
		mockTime  string
		wantState int
	}{
		{
			name: "Schedule during business hours",
			trigger: &sarabalaiov1alpha1.Trigger{
				Name:      "business-hours",
				StartTime: "09:00:00",
				EndTime:   "17:00:00",
				StartHPAConfig: &sarabalaiov1alpha1.HPAConfig{
					MinReplicas: &minReplicas,
					MaxReplicas: &maxReplicas,
				},
				EndHPAConfig: &sarabalaiov1alpha1.HPAConfig{
					MinReplicas: &minReplicas,
					MaxReplicas: &maxReplicas,
				},
			},
			mockTime:  "13:00:00",
			wantState: Within,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := &TriggerSchedule{
				client: client,
				HPANamespacedName: types.NamespacedName{
					Name:      "test-hpa",
					Namespace: "default",
				},
				Trigger: tt.trigger,
				cron:    cron.New(),
			}

			// Use a fixed date to ensure consistent comparison
			baseTime := time.Date(2025, 4, 23, 0, 0, 0, 0, time.UTC)
			current, _ := time.Parse("15:04:05", tt.mockTime)
			mockTime := time.Date(baseTime.Year(), baseTime.Month(), baseTime.Day(), current.Hour(), current.Minute(), current.Second(), 0, baseTime.Location())

			state, err := ts.isWithinTimeWindow(mockTime)
			assert.NoError(t, err)
			assert.Equal(t, tt.wantState, state)

			ts.Schedule()
			// Verify cron jobs were set up correctly
			assert.NotNil(t, ts.cron)
		})
	}
}

func TestSmartHPAContextExecute(t *testing.T) {
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
				Name:       "test-app",
				APIVersion: "apps/v1",
			},
		},
	}

	client := fake.NewClientBuilder().WithObjects(hpa).Build()

	tests := []struct {
		name    string
		trigger *sarabalaiov1alpha1.Trigger
		wantErr bool
	}{
		{
			name: "Execute with valid trigger",
			trigger: &sarabalaiov1alpha1.Trigger{
				Name:      "test-trigger",
				StartTime: "09:00:00",
				EndTime:   "17:00:00",
				Timezone:  "America/Los_Angeles",
				Interval: &sarabalaiov1alpha1.Interval{
					Recurring: "M,TU,W,TH,F",
				},
				StartHPAConfig: &sarabalaiov1alpha1.HPAConfig{
					MinReplicas: &minReplicas,
					MaxReplicas: &maxReplicas,
				},
				EndHPAConfig: &sarabalaiov1alpha1.HPAConfig{
					MinReplicas: &minReplicas,
					MaxReplicas: &maxReplicas,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			sc := &SmartHPAContext{
				client:    client,
				schedules: make(map[string]*TriggerSchedule),
				cron:      cron.New(),
			}

			ts := &TriggerSchedule{
				client: client,
				HPANamespacedName: types.NamespacedName{
					Name:      "test-hpa",
					Namespace: "default",
				},
				Trigger: tt.trigger,
				cron:    cron.New(),
			}

			sc.schedules[tt.trigger.Name] = ts
			sc.execute(ctx)

			// Verify schedule was created
			assert.Contains(t, sc.schedules, tt.trigger.Name)
			assert.NotNil(t, sc.schedules[tt.trigger.Name].cron)
		})
	}
}
