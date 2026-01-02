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

package server

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	autoscalingv1alpha1 "github.com/sarabala1979/SmartHPA/api/v1alpha1"
)

// SmartHPAListResponse is the response for listing SmartHPAs
type SmartHPAListResponse struct {
	Items []SmartHPAResponse `json:"items"`
}

// SmartHPAResponse is the response format for a SmartHPA
type SmartHPAResponse struct {
	Name              string                                  `json:"name"`
	Namespace         string                                  `json:"namespace"`
	Labels            map[string]string                       `json:"labels,omitempty"`
	CreationTimestamp string                                  `json:"creationTimestamp"`
	Generation        int64                                   `json:"generation"`
	HPAObjectRef      *autoscalingv1alpha1.HPAObjectReference `json:"hpaObjectRef,omitempty"`
	Triggers          autoscalingv1alpha1.Triggers            `json:"triggers,omitempty"`
	Conditions        []metav1.Condition                      `json:"conditions,omitempty"`
}

// CreateSmartHPARequest is the request format for creating a SmartHPA
type CreateSmartHPARequest struct {
	Name         string                                  `json:"name"`
	Namespace    string                                  `json:"namespace"`
	Labels       map[string]string                       `json:"labels,omitempty"`
	HPAObjectRef *autoscalingv1alpha1.HPAObjectReference `json:"hpaObjectRef,omitempty"`
	Triggers     autoscalingv1alpha1.Triggers            `json:"triggers,omitempty"`
}

// UpdateSmartHPARequest is the request format for updating a SmartHPA
type UpdateSmartHPARequest struct {
	Labels       map[string]string                       `json:"labels,omitempty"`
	HPAObjectRef *autoscalingv1alpha1.HPAObjectReference `json:"hpaObjectRef,omitempty"`
	Triggers     autoscalingv1alpha1.Triggers            `json:"triggers,omitempty"`
}


