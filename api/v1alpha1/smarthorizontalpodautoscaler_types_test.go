package v1alpha1

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSmartHorizontalPodAutoscaler_DeepCopy(t *testing.T) {
	minReplicas := int32(1)
	maxReplicas := int32(5)

	shpa := &SmartHorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shpa",
			Namespace: "default",
		},
		Spec: SmartHorizontalPodAutoscalerSpec{
			HPAObjectRef: &HPAObjectReference{
				Name:      "test-hpa",
				Namespace: "default",
			},
			Triggers: []Trigger{
				{
					Name:      "business-hours",
					StartTime: "09:00:00",
					EndTime:   "17:00:00",
					Timezone:  "America/Los_Angeles",
					Interval: &Interval{
						Recurring: "M,TU,W,TH,F",
					},
					StartHPAConfig: &HPAConfig{
						MinReplicas: &minReplicas,
						MaxReplicas: &maxReplicas,
					},
					EndHPAConfig: &HPAConfig{
						MinReplicas: &minReplicas,
						MaxReplicas: &maxReplicas,
					},
				},
			},
		},
		Status: SmartHorizontalPodAutoscalerStatus{
			Conditions: []metav1.Condition{
				{
					Type:    "Ready",
					Status:  metav1.ConditionTrue,
					Reason:  "ScheduleCreated",
					Message: "Schedule created successfully",
				},
			},
		},
	}

	copy := shpa.DeepCopy()
	shpa.Name = "new-shpa"
	shpa.Spec.HPAObjectRef.Name = "new-hpa"
	shpa.Status.Conditions[0].Type = "Error"

	assert.Equal(t, "test-shpa", copy.Name)
	assert.Equal(t, "test-hpa", copy.Spec.HPAObjectRef.Name)
	assert.Equal(t, "Ready", copy.Status.Conditions[0].Type)
}

func TestTrigger_NeedRecurring(t *testing.T) {
	tests := []struct {
		name    string
		trigger *Trigger
		want    bool
	}{
		{
			name: "With recurring pattern",
			trigger: &Trigger{
				Interval: &Interval{
					Recurring: "M,TU,W,TH,F",
				},
			},
			want: true,
		},
		{
			name: "Without recurring pattern",
			trigger: &Trigger{
				Interval: &Interval{},
			},
			want: false,
		},
		{
			name: "With dates",
			trigger: &Trigger{
				Interval: &Interval{
					StartDate: "2025-04-23T09:00:00-07:00",
					EndDate:   "2025-04-24T09:00:00-07:00",
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.trigger.NeedRecurring()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHPAConfig_DeepCopy(t *testing.T) {
	minReplicas := int32(1)
	maxReplicas := int32(5)
	desiredReplicas := int32(3)

	config := &HPAConfig{
		MinReplicas:     &minReplicas,
		MaxReplicas:     &maxReplicas,
		DesiredReplicas: &desiredReplicas,
	}

	copy := config.DeepCopy()
	*config.MinReplicas = 2
	*config.MaxReplicas = 6
	*config.DesiredReplicas = 4

	assert.Equal(t, int32(1), *copy.MinReplicas)
	assert.Equal(t, int32(5), *copy.MaxReplicas)
	assert.Equal(t, int32(3), *copy.DesiredReplicas)
}

func TestInterval_DeepCopy(t *testing.T) {
	interval := &Interval{
		Recurring: "M,TU,W,TH,F",
	}

	copy := interval.DeepCopy()

	interval.Recurring = "SA,SU"

	assert.Equal(t, "M,TU,W,TH,F", copy.Recurring)
}

func TestTrigger_DeepCopy(t *testing.T) {
	minReplicas := int32(1)
	maxReplicas := int32(5)

	trigger := &Trigger{
		Name:      "business-hours",
		StartTime: "09:00:00",
		EndTime:   "17:00:00",
		Timezone:  "America/Los_Angeles",
		Interval: &Interval{
			Recurring: "M,TU,W,TH,F",
		},
		StartHPAConfig: &HPAConfig{
			MinReplicas: &minReplicas,
			MaxReplicas: &maxReplicas,
		},
		EndHPAConfig: &HPAConfig{
			MinReplicas: &minReplicas,
			MaxReplicas: &maxReplicas,
		},
	}

	copy := trigger.DeepCopy()
	trigger.Name = "night-shift"
	trigger.StartTime = "18:00:00"
	trigger.EndTime = "06:00:00"
	trigger.Timezone = "UTC"
	*trigger.StartHPAConfig.MinReplicas = 2

	assert.Equal(t, "business-hours", copy.Name)
	assert.Equal(t, "09:00:00", copy.StartTime)
	assert.Equal(t, "17:00:00", copy.EndTime)
	assert.Equal(t, "America/Los_Angeles", copy.Timezone)
	assert.Equal(t, int32(1), *copy.StartHPAConfig.MinReplicas)
}

func TestSmartHorizontalPodAutoscaler_Validation(t *testing.T) {
	minReplicas := int32(1)
	maxReplicas := int32(5)
	desiredReplicas := int32(3)

	tests := []struct {
		name    string
		shpa    *SmartHorizontalPodAutoscaler
		wantErr bool
	}{
		{
			name: "Valid configuration",
			shpa: &SmartHorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shpa",
					Namespace: "default",
				},
				Spec: SmartHorizontalPodAutoscalerSpec{
					HPAObjectRef: &HPAObjectReference{
						Name:      "test-hpa",
						Namespace: "default",
					},
					Triggers: []Trigger{
						{
							Name:      "business-hours",
							StartTime: "09:00:00",
							EndTime:   "17:00:00",
							Timezone:  "America/Los_Angeles",
							Interval: &Interval{
								Recurring: "M,TU,W,TH,F",
							},
							StartHPAConfig: &HPAConfig{
								MinReplicas:     &minReplicas,
								MaxReplicas:     &maxReplicas,
								DesiredReplicas: &desiredReplicas,
							},
							EndHPAConfig: &HPAConfig{
								MinReplicas:     &minReplicas,
								MaxReplicas:     &maxReplicas,
								DesiredReplicas: &desiredReplicas,
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Invalid time format",
			shpa: &SmartHorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shpa",
					Namespace: "default",
				},
				Spec: SmartHorizontalPodAutoscalerSpec{
					HPAObjectRef: &HPAObjectReference{
						Name:      "test-hpa",
						Namespace: "default",
					},
					Triggers: []Trigger{
						{
							Name:      "business-hours",
							StartTime: "9:00", // Invalid format
							EndTime:   "17:00",
							Timezone:  "America/Los_Angeles",
							Interval: &Interval{
								Recurring: "M,TU,W,TH,F",
							},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Missing HPA reference",
			shpa: &SmartHorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shpa",
					Namespace: "default",
				},
				Spec: SmartHorizontalPodAutoscalerSpec{
					Triggers: []Trigger{
						{
							Name:      "business-hours",
							StartTime: "09:00:00",
							EndTime:   "17:00:00",
						},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse the time strings to validate them
			for _, trigger := range tt.shpa.Spec.Triggers {
				_, err := time.Parse("15:04:05", trigger.StartTime)
				if err != nil && !tt.wantErr {
					t.Errorf("Invalid start time format: %v", err)
				}
				_, err = time.Parse("15:04:05", trigger.EndTime)
				if err != nil && !tt.wantErr {
					t.Errorf("Invalid end time format: %v", err)
				}
			}

			// Validate HPA reference
			if tt.shpa.Spec.HPAObjectRef == nil {
				if !tt.wantErr {
					t.Error("Missing HPA reference")
				}
			} else if tt.shpa.Spec.HPAObjectRef.Name == "" && !tt.wantErr {
				t.Error("Empty HPA reference name")
			}
		})
	}
}
