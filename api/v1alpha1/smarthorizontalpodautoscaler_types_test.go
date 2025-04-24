package v1alpha1

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestInterval_NeedRecurring(t *testing.T) {
	tests := []struct {
		name     string
		interval *Interval
		want     bool
	}{
		{
			name: "With recurring pattern",
			interval: &Interval{
				Recurring: "M,TU,W,TH,F",
				Timezone:  "America/Los_Angeles",
			},
			want: true,
		},
		{
			name: "Without recurring pattern",
			interval: &Interval{
				Timezone: "America/Los_Angeles",
			},
			want: false,
		},
		{
			name: "With dates",
			interval: &Interval{
				StartDate: "2025-04-23T09:00:00-07:00",
				EndDate:   "2025-04-24T09:00:00-07:00",
				Timezone:  "America/Los_Angeles",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.interval.NeedRecurring()
			assert.Equal(t, tt.want, got)
		})
	}
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
							Interval: &Interval{
								Recurring: "M,TU,W,TH,F",
								Timezone:  "America/Los_Angeles",
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
							Interval: &Interval{
								Recurring: "M,TU,W,TH,F",
								Timezone:  "America/Los_Angeles",
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
