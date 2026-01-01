/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

// WeekdayShort contains short forms of weekdays indexed by time.Weekday()
// Sunday=0, Monday=1, Tuesday=2, Wednesday=3, Thursday=4, Friday=5, Saturday=6
var WeekdayShort = []string{"SUN", "M", "TU", "W", "TH", "F", "SAT"}

// WeekdayToCron maps short weekday names to cron weekday numbers (0=Sunday, 6=Saturday)
var WeekdayToCron = map[string]string{
	"SUN": "0",
	"M":   "1",
	"TU":  "2",
	"W":   "3",
	"TH":  "4",
	"F":   "5",
	"SAT": "6",
}

// For testing purposes
var NowFunc = time.Now

func SetNowFunc(f func() time.Time) {
	NowFunc = f
}

func ResetNowFunc() {
	NowFunc = time.Now
}

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type HPAObjectReference struct {
	Namespace string `json:"namespace,omitempty" protobuf:"bytes,1,opt,name=namespace"`
	Name      string `json:"name,omitempty" protobuf:"bytes,2,opt,name=name"`
}

type HPASpecTemplate struct {
	Metadata metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Spec     *HPAConfig        `json:"spec,omitempty" protobuf:"bytes,1,opt,name=spec"`
}

type Triggers []Trigger

type HPAConfig struct {
	MinReplicas     *int32 `json:"minReplicas,omitempty" protobuf:"varint,1,opt, name=minReplicas"`
	MaxReplicas     *int32 `json:"maxReplicas,omitempty" protobuf:"varint,2,opt, name=maxReplicas"`
	DesiredReplicas *int32 `json:"desiredReplicas,omitempty" protobuf:"varint,3,opt, name=desiredReplicas"`
}

type Interval struct {
	Recurring string `json:"recurring,omitempty" protobuf:"bytes,1,opt,name=recurring"`
	StartDate string `json:"startDate,omitempty" protobuf:"bytes,2,opt,name=startDate"`
	EndDate   string `json:"endDate,omitempty" protobuf:"bytes,3,opt,name=endDate"`
}

func (i *Trigger) NeedRecurring() bool {
	// If no interval is specified, do not recur
	if i.Interval == nil {
		klog.Infof("NeedRecurring: No interval specified for trigger %s", i.Name)
		return false
	}

	// Load timezone, default to UTC if not specified or invalid
	loc, err := time.LoadLocation(i.Timezone)
	if err != nil {
		klog.Warningf("NeedRecurring: Failed to load timezone %s for trigger %s, using UTC: %v", i.Timezone, i.Name, err)
		loc = time.UTC
	}

	// Get current time in the specified timezone
	now := NowFunc().In(loc)

	// If recurring is specified, check if today matches
	if i.Interval.Recurring != "" {
		// Convert current weekday to short form
		currentDay := WeekdayShort[int(now.Weekday())]
		klog.Infof("NeedRecurring: Checking trigger %s - Current day: %s, Recurring: %s", i.Name, currentDay, i.Interval.Recurring)
		// Split recurring days and check for exact match
		days := strings.Split(i.Interval.Recurring, ",")
		for _, day := range days {
			trimmedDay := strings.TrimSpace(day)
			klog.Infof("NeedRecurring: Comparing %s with %s", currentDay, trimmedDay)
			if trimmedDay == currentDay {
				klog.Infof("NeedRecurring: Found match for trigger %s", i.Name)
				return true
			}
		}
		klog.Infof("NeedRecurring: No day match found for trigger %s", i.Name)
		return false
	}

	// If date range is specified, check if we're within range
	if i.Interval.StartDate != "" && i.Interval.EndDate != "" {
		// Parse start and end dates
		start, err := time.Parse(time.DateOnly, i.Interval.StartDate)
		if err != nil {
			klog.Errorf("NeedRecurring: Failed to parse start date for trigger %s: %v", i.Name, err)
			return false
		}
		end, err := time.Parse(time.DateOnly, i.Interval.EndDate)
		if err != nil {
			klog.Errorf("NeedRecurring: Failed to parse end date for trigger %s: %v", i.Name, err)
			return false
		}

		// Convert to the specified timezone
		start = start.In(loc)
		end = end.In(loc)

		return now.After(start) && now.Before(end)
	}

	klog.Infof("NeedRecurring: Neither recurring nor date range specified for trigger %s", i.Name)
	// If neither recurring nor date range is specified, do not recur
	return false
}

type Trigger struct {
	Name           string     `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`
	Priority       *int       `json:"priority,omitempty" protobuf:"varint,2,opt,name=priority"`
	Timezone       string     `json:"timezone,omitempty" protobuf:"bytes,3,opt,name=timezone"`
	Interval       *Interval  `json:"interval,omitempty" protobuf:"bytes,4,opt,name=interval"`
	StartTime      string     `json:"startTime,omitempty" protobuf:"bytes,5,opt,name=startTime"`
	EndTime        string     `json:"endTime,omitempty" protobuf:"bytes,6,opt,name=endTime"`
	StartHPAConfig *HPAConfig `json:"startHPAConfig,omitempty" protobuf:"bytes,7,opt,name=startHPAConfig"`
	EndHPAConfig   *HPAConfig `json:"endHPAConfig,omitempty" protobuf:"bytes,8,opt,name=endHPAConfig"`
	Suspend        bool       `json:"suspend,omitempty" protobuf:"bytes,8,opt,name=suspend"`
	//+kubebuilder:validation:Pattern=^([0-1][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9]$
}

// HasValidInterval returns true if the trigger has a valid recurring or date range interval.
// Unlike NeedRecurring, this doesn't check if today matches - it just checks if the interval is defined.
func (i *Trigger) HasValidInterval() bool {
	if i.Interval == nil {
		return false
	}
	// Has recurring pattern or date range
	return i.Interval.Recurring != "" || (i.Interval.StartDate != "" && i.Interval.EndDate != "")
}

// GetCronWeekdays converts the recurring pattern to cron weekday numbers.
// Returns "*" if no recurring pattern is set, or comma-separated weekday numbers (0=Sun, 6=Sat).
func (i *Trigger) GetCronWeekdays() string {
	if i.Interval == nil || i.Interval.Recurring == "" {
		return "*"
	}

	days := strings.Split(i.Interval.Recurring, ",")
	var cronDays []string
	for _, day := range days {
		trimmedDay := strings.TrimSpace(day)
		if cronNum, ok := WeekdayToCron[trimmedDay]; ok {
			cronDays = append(cronDays, cronNum)
		}
	}

	if len(cronDays) == 0 {
		return "*"
	}
	return strings.Join(cronDays, ",")
}

type Conditions []metav1.Condition

type CurrentTriggers []CurrentTrigger

type CurrentTrigger struct {
	Name      string     `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`
	HPAConfig *HPAConfig `json:"HPAConfig,omitempty" protobuf:"bytes,2,opt,name=HPAConfig"`
}
type SmartRecommendation struct {
	Enabled   bool `json:"enabled,omitempty" protobuf:"bytes,1,opt,name=enabled"`
	Frequency *int `json:"frequency,omitempty" protobuf:"bytes,2,opt,name=frequency"`
}

// SmartHorizontalPodAutoscalerSpec defines the desired state of SmartHorizontalPodAutoscaler
type SmartHorizontalPodAutoscalerSpec struct {
	HPASpecTemplate     *HPASpecTemplate     `json:"HPASpecTemplate,omitempty" protobuf:"bytes,1,opt,name=HPASpecTemplate"` // If set, a new HPA will be created from this template.
	SmartRecommendation *SmartRecommendation `json:"smartRecommendation,omitempty" protobuf:"bytes,1,opt,name=smartRecommendation"`
	HPAObjectRef        *HPAObjectReference  `json:"HPAObjectRef,omitempty" protobuf:"bytes,2,opt,name=HPAObjectRef"`
	Triggers            Triggers             `json:"triggers,omitempty" protobuf:"bytes,3,opt,name=triggers"`
}

// SmartHorizontalPodAutoscalerStatus defines the observed state of SmartHorizontalPodAutoscaler
type SmartHorizontalPodAutoscalerStatus struct {
	// Conditions represents the latest available observations of SmartHPA's current state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// HPASpecTemplate *HPASpecTemplate    `json:"HPASpecTemplate,omitempty" protobuf:"bytes,1,opt,name=HPASpecTemplate"`
	HPAObjectRef *HPAObjectReference `json:"HPAObjectRef,omitempty" protobuf:"bytes,2,opt,name=HPAObjectRef"`
	Triggers     Triggers            `json:"triggers,omitempty" protobuf:"bytes,3,opt,name=triggers"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// SmartHorizontalPodAutoscaler is the Schema for the smarthorizontalpodautoscalers API
type SmartHorizontalPodAutoscaler struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SmartHorizontalPodAutoscalerSpec   `json:"spec,omitempty"`
	Status SmartHorizontalPodAutoscalerStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// SmartHorizontalPodAutoscalerList contains a list of SmartHorizontalPodAutoscaler
type SmartHorizontalPodAutoscalerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SmartHorizontalPodAutoscaler `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SmartHorizontalPodAutoscaler{}, &SmartHorizontalPodAutoscalerList{})
}
