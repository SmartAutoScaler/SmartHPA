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

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	autoscalingv1alpha1 "github.com/sarabala1979/SmartHPA/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const smartHPAFinalizer = "autoscaling.sarabala.io/finalizer"

// SetStatusCondition sets the newCondition in the existingConditions slice.
// If a condition of the same type already exists, it updates it. Otherwise, it appends the newCondition.
func SetStatusCondition(existingConditions *[]metav1.Condition, newCondition metav1.Condition) {
	if existingConditions == nil {
		existingConditions = &[]metav1.Condition{}
	}

	existingCondition := meta.FindStatusCondition(*existingConditions, newCondition.Type)
	if existingCondition == nil {
		newCondition.LastTransitionTime = metav1.Now()
		*existingConditions = append(*existingConditions, newCondition)
		return
	}

	if existingCondition.Status != newCondition.Status || existingCondition.Reason != newCondition.Reason || existingCondition.Message != newCondition.Message {
		if existingCondition.Status != newCondition.Status {
			existingCondition.LastTransitionTime = metav1.Now()
		}
		existingCondition.Status = newCondition.Status
		existingCondition.Reason = newCondition.Reason
		existingCondition.Message = newCondition.Message
	}
}

// SmartHorizontalPodAutoscalerReconciler reconciles a SmartHorizontalPodAutoscaler object
type SmartHorizontalPodAutoscalerReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	queue         chan types.NamespacedName
	DeletionQueue chan types.NamespacedName
}

// +kubebuilder:rbac:groups=autoscaling.sarabala.io,resources=smarthorizontalpodautoscalers,verbs=get;list;watch;create;update;patch;delete;removefinalizers
// +kubebuilder:rbac:groups=autoscaling.sarabala.io,resources=smarthorizontalpodautoscalers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=autoscaling.sarabala.io,resources=smarthorizontalpodautoscalers/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Enhance Reconcile logic for SmartHPA.
// Currently, this controller primarily fetches the SmartHorizontalPodAutoscaler and its
// referenced HorizontalPodAutoscaler, updates status conditions, and enqueues the
// SmartHPA for processing by the scheduler component (which handles applying scheduled
// changes to the HPA).
//
// The "smart" aspect of SmartHPA, where its own spec (e.g., policies, overrides,
// or advanced trigger interpretations beyond simple time-based scheduling) would directly
// influence the referenced HPA's configuration (e.g., dynamically adjusting min/max
// replicas, metrics, or behavior of the HPA itself based on SmartHPA's defined strategies),
// is not yet implemented in this Reconcile loop.
//
// Future enhancements could involve this controller taking a more active role:
// 1. Interpreting SmartHPA policies/triggers that are not purely time-based.
// 2. Calculating necessary modifications to the target HPA's spec based on these policies
//    and the current time (as determined by the scheduler or an internal ticker).
// 3. Applying these modifications directly to the HPA object, making the controller
//    the central point of authority for HPA adjustments driven by SmartHPA.
// This would make the SmartHPA controller more aligned with typical operator patterns
// where the controller actively drives the state of managed resources.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *SmartHorizontalPodAutoscalerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the SmartHorizontalPodAutoscaler instance
	smartHPA := &autoscalingv1alpha1.SmartHorizontalPodAutoscaler{}
	if err := r.Get(ctx, req.NamespacedName, smartHPA); err != nil {
		logger.Error(err, "unable to fetch SmartHorizontalPodAutoscaler")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Handle finalizer logic
	if smartHPA.ObjectMeta.DeletionTimestamp.IsZero() {
		// The object is not being deleted, so if it does not have our finalizer,
		// then lets add the finalizer and update the object. This is equivalent
		// to registering our finalizer.
		if !controllerutil.ContainsFinalizer(smartHPA, smartHPAFinalizer) {
			controllerutil.AddFinalizer(smartHPA, smartHPAFinalizer)
			if err := r.Update(ctx, smartHPA); err != nil {
				logger.Error(err, "unable to add finalizer to SmartHPA")
				return ctrl.Result{}, err
			}
			logger.Info("Added finalizer to SmartHPA")
			return ctrl.Result{Requeue: true}, nil
		}
	} else {
		// The object is being deleted
		if controllerutil.ContainsFinalizer(smartHPA, smartHPAFinalizer) {
			// Our finalizer is present, so lets handle any external dependency
			logger.Info("SmartHPA is being deleted, signaling scheduler for cleanup", "SmartHPA", req.NamespacedName)
			r.DeletionQueue <- req.NamespacedName // Signal scheduler
			logger.Info("Signaled scheduler to clean up resources for deleted SmartHPA", "SmartHPA", req.NamespacedName)

			// Remove our finalizer from the list and update it.
			controllerutil.RemoveFinalizer(smartHPA, smartHPAFinalizer)
			if err := r.Update(ctx, smartHPA); err != nil {
				logger.Error(err, "unable to remove finalizer from SmartHPA")
				return ctrl.Result{}, err
			}
			logger.Info("Removed finalizer from SmartHPA")
		}
		// Stop reconciliation as the item is being deleted
		return ctrl.Result{}, nil
	}

	// ---- Existing reconciliation logic ----
	// Only proceed if the object is not being deleted (which is implicitly handled by the finalizer logic above)

	// Fetch the referenced HPA
	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	hpaName := types.NamespacedName{
		Name:      smartHPA.Spec.HPAObjectRef.Name,
		Namespace: smartHPA.Spec.HPAObjectRef.Namespace,
	}
	if err := r.Get(ctx, hpaName, hpa); err != nil {
		condition := metav1.Condition{
			Type:    "Error",
			Status:  metav1.ConditionTrue,
			Reason:  "HPANotFound",
			Message: "Referenced HPA not found",
		}
		SetStatusCondition(&smartHPA.Status.Conditions, condition)
		if errStatus := r.Status().Update(ctx, smartHPA); errStatus != nil {
			logger.Error(errStatus, "unable to update SmartHPA status for HPANotFound")
		}
		// Return error to requeue, as this might be a transient issue.
		return ctrl.Result{}, err
	}

	// Update status to reflect successful reconciliation
	condition := metav1.Condition{
		Type:    "Ready",
		Status:  metav1.ConditionTrue,
		Reason:  "Reconciled",
		Message: "SmartHPA reconciled successfully",
	}
	SetStatusCondition(&smartHPA.Status.Conditions, condition)
	if err := r.Status().Update(ctx, smartHPA); err != nil {
		logger.Error(err, "unable to update SmartHPA status for Reconciled")
		return ctrl.Result{}, err
	}

	// Enqueue for scheduling
	logger.Info("Enqueueing SmartHPA for scheduling", "SmartHPA", req.NamespacedName)
	r.queue <- req.NamespacedName
	klog.Infof("Enqueued SmartHPA %s", req.NamespacedName)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SmartHorizontalPodAutoscalerReconciler) SetupWithManager(mgr ctrl.Manager, queue chan types.NamespacedName, deletionQueue chan types.NamespacedName) error {
	r.queue = queue
	r.DeletionQueue = deletionQueue
	return ctrl.NewControllerManagedBy(mgr).
		For(&autoscalingv1alpha1.SmartHorizontalPodAutoscaler{}).
		Complete(r)
}
