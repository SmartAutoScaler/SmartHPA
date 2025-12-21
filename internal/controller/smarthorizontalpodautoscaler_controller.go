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

package controller

import (
	"context"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	autoscalingv1alpha1 "github.com/sarabala1979/SmartHPA/api/v1alpha1"
)

// SmartHorizontalPodAutoscalerReconciler reconciles a SmartHorizontalPodAutoscaler object
type SmartHorizontalPodAutoscalerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	queue  chan types.NamespacedName
}

// +kubebuilder:rbac:groups=autoscaling.sarabala.io,resources=smarthorizontalpodautoscalers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=autoscaling.sarabala.io,resources=smarthorizontalpodautoscalers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=autoscaling.sarabala.io,resources=smarthorizontalpodautoscalers/finalizers,verbs=update
// +kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the SmartHorizontalPodAutoscaler object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *SmartHorizontalPodAutoscalerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the SmartHorizontalPodAutoscaler instance
	smartHPA := &autoscalingv1alpha1.SmartHorizontalPodAutoscaler{}
	if err := r.Get(ctx, req.NamespacedName, smartHPA); err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Resource was deleted, log it and return without error
			logger.Info("SmartHorizontalPodAutoscaler was deleted, cleaning up",
				"name", req.Name,
				"namespace", req.Namespace)
			return ctrl.Result{}, nil
		}
		logger.Error(err, "unable to fetch SmartHorizontalPodAutoscaler")
		return ctrl.Result{RequeueAfter: time.Second * 30}, client.IgnoreNotFound(err)
	}

	// Check if the resource is being deleted
	if !smartHPA.DeletionTimestamp.IsZero() {
		logger.Info("SmartHorizontalPodAutoscaler is being deleted, skipping reconciliation",
			"name", req.Name,
			"namespace", req.Namespace)
		return ctrl.Result{}, nil
	}

	// --- New logic: Create HPA from HPASpecTemplate if present ---
	if smartHPA.Spec.HPASpecTemplate != nil {
		hpaName := smartHPA.Spec.HPASpecTemplate.Metadata.Name
		hpaNamespace := smartHPA.Spec.HPASpecTemplate.Metadata.Namespace
		if hpaNamespace == "" {
			hpaNamespace = smartHPA.Namespace
		}
		if hpaName == "" {
			hpaName = smartHPA.Name + "-generated"
		}
		// Check if HPA already exists
		hpa := &autoscalingv2.HorizontalPodAutoscaler{}
		err := r.Get(ctx, types.NamespacedName{Name: hpaName, Namespace: hpaNamespace}, hpa)
		if err != nil {
			// HPA does not exist, create it
			newHPA := &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:        hpaName,
					Namespace:   hpaNamespace,
					Labels:      smartHPA.Spec.HPASpecTemplate.Metadata.Labels,
					Annotations: smartHPA.Spec.HPASpecTemplate.Metadata.Annotations,
				},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{},
			}
			// Copy spec fields from template
			if smartHPA.Spec.HPASpecTemplate.Spec != nil {
				if smartHPA.Spec.HPASpecTemplate.Spec.MinReplicas != nil {
					newHPA.Spec.MinReplicas = smartHPA.Spec.HPASpecTemplate.Spec.MinReplicas
				}
				if smartHPA.Spec.HPASpecTemplate.Spec.MaxReplicas != nil {
					newHPA.Spec.MaxReplicas = *smartHPA.Spec.HPASpecTemplate.Spec.MaxReplicas
				}
				// NOTE: Add more field mappings as needed for your use case
			}
			// Set owner reference
			if err := ctrl.SetControllerReference(smartHPA, newHPA, r.Scheme); err != nil {
				logger.Error(err, "unable to set owner reference on HPA")
				return ctrl.Result{}, err
			}
			if err := r.Create(ctx, newHPA); err != nil {
				logger.Error(err, "unable to create HPA from template")
				return ctrl.Result{}, err
			}
			logger.Info("Created HPA from HPASpecTemplate", "hpa", hpaName)
		} else {
			logger.Info("HPA from template already exists", "hpa", hpaName)
		}
	}

	// Fetch the referenced HPA
	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	hpaRef := smartHPA.Spec.HPAObjectRef
	if hpaRef != nil {
		hpaName := types.NamespacedName{
			Name:      hpaRef.Name,
			Namespace: hpaRef.Namespace,
		}
		if err := r.Get(ctx, hpaName, hpa); err != nil {
			// Update status condition to reflect error
			condition := metav1.Condition{
				Type:               "Error",
				Status:             metav1.ConditionTrue,
				Reason:             "HPANotFound",
				Message:            "Referenced HPA not found",
				LastTransitionTime: metav1.Now(),
			}
			smartHPA.Status.Conditions = []metav1.Condition{condition}
			if err := r.Status().Update(ctx, smartHPA); err != nil {
				logger.Error(err, "unable to update SmartHPA status")
			}
			return ctrl.Result{RequeueAfter: time.Second * 30}, err // Requeue after 30 seconds
		}
	}

	// Update status to reflect successful reconciliation
	condition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "Reconciled",
		Message:            "SmartHPA reconciled successfully",
		LastTransitionTime: metav1.Now(),
	}
	smartHPA.Status.Conditions = []metav1.Condition{condition}
	if err := r.Status().Update(ctx, smartHPA); err != nil {
		logger.Error(err, "unable to update SmartHPA status")
		return ctrl.Result{RequeueAfter: time.Second * 5}, err // Requeue after 5 seconds
	}

	// Enqueue for scheduling
	r.queue <- req.NamespacedName
	klog.Infof("Enqueued SmartHPA %s", req.NamespacedName)

	// Requeue for periodic reconciliation
	return ctrl.Result{RequeueAfter: time.Minute * 5}, nil // Requeue every 5 minutes
}

// SetupWithManager sets up the controller with the Manager.
func (r *SmartHorizontalPodAutoscalerReconciler) SetupWithManager(mgr ctrl.Manager, queue chan types.NamespacedName) error {
	r.queue = queue
	return ctrl.NewControllerManagedBy(mgr).
		For(&autoscalingv1alpha1.SmartHorizontalPodAutoscaler{}).
		Owns(&autoscalingv2.HorizontalPodAutoscaler{}). // Watch owned HPAs
		Complete(r)
}
