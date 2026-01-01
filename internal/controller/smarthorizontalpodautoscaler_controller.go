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
	"fmt"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	autoscalingv1alpha1 "github.com/sarabala1979/SmartHPA/api/v1alpha1"
)

const (
	// requeueIntervalShort is the interval for quick retries on transient errors.
	requeueIntervalShort = 5 * time.Second
	// requeueIntervalMedium is the interval for retries on recoverable errors.
	requeueIntervalMedium = 30 * time.Second
	// requeueIntervalLong is the interval for periodic reconciliation.
	requeueIntervalLong = 5 * time.Minute

	// finalizerName is used to ensure cleanup on deletion
	finalizerName = "autoscaling.sarabala.io/smarthpa-finalizer"

	// queueSendTimeout is the timeout for sending to the scheduler queue
	queueSendTimeout = 5 * time.Second
)

// Condition types for SmartHPA status
const (
	ConditionTypeReady     = "Ready"
	ConditionTypeError     = "Error"
	ConditionTypeHPAValid  = "HPAValid"
	ConditionTypeScheduled = "Scheduled"
)

// Condition reasons
const (
	ReasonReconciled       = "Reconciled"
	ReasonHPANotFound      = "HPANotFound"
	ReasonHPACreated       = "HPACreated"
	ReasonHPACreateFailed  = "HPACreateFailed"
	ReasonScheduleEnqueued = "ScheduleEnqueued"
	ReasonScheduleFailed   = "ScheduleFailed"
	ReasonFinalizerFailed  = "FinalizerFailed"
	ReasonCleanupComplete  = "CleanupComplete"
)

// SchedulerInterface defines the interface for scheduler operations needed by the controller
type SchedulerInterface interface {
	// RemoveContext removes the scheduler context for a SmartHPA (cleanup on delete)
	RemoveContext(key types.NamespacedName)
	// RefreshContext forces a refresh of the scheduler context (for spec updates)
	RefreshContext(key types.NamespacedName)
}

// SmartHorizontalPodAutoscalerReconciler reconciles a SmartHorizontalPodAutoscaler object
type SmartHorizontalPodAutoscalerReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Recorder  record.EventRecorder
	queue     chan types.NamespacedName
	scheduler SchedulerInterface
}

// +kubebuilder:rbac:groups=autoscaling.sarabala.io,resources=smarthorizontalpodautoscalers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=autoscaling.sarabala.io,resources=smarthorizontalpodautoscalers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=autoscaling.sarabala.io,resources=smarthorizontalpodautoscalers/finalizers,verbs=update
// +kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile moves the current state of the cluster closer to the desired state
// by managing HPA configurations based on SmartHorizontalPodAutoscaler specs.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *SmartHorizontalPodAutoscalerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the SmartHorizontalPodAutoscaler instance
	smartHPA := &autoscalingv1alpha1.SmartHorizontalPodAutoscaler{}
	if err := r.Get(ctx, req.NamespacedName, smartHPA); err != nil {
		if apierrors.IsNotFound(err) {
			// Resource was deleted, cleanup already handled by finalizer
			logger.Info("SmartHorizontalPodAutoscaler not found, likely deleted",
				"name", req.Name,
				"namespace", req.Namespace)
			return ctrl.Result{}, nil
		}
		logger.Error(err, "unable to fetch SmartHorizontalPodAutoscaler")
		return ctrl.Result{RequeueAfter: requeueIntervalMedium}, err
	}

	// Handle deletion with finalizer
	if !smartHPA.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, smartHPA)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(smartHPA, finalizerName) {
		logger.Info("Adding finalizer to SmartHPA", "name", smartHPA.Name)
		controllerutil.AddFinalizer(smartHPA, finalizerName)
		if err := r.Update(ctx, smartHPA); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		// Re-fetch after update to get the latest version
		if err := r.Get(ctx, req.NamespacedName, smartHPA); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Initialize status conditions if needed
	if smartHPA.Status.Conditions == nil {
		smartHPA.Status.Conditions = []metav1.Condition{}
	}

	// Create HPA from HPASpecTemplate if present
	if smartHPA.Spec.HPASpecTemplate != nil {
		if err := r.ensureHPAFromTemplate(ctx, smartHPA); err != nil {
			logger.Error(err, "Failed to ensure HPA from template")
			r.recordEvent(smartHPA, corev1.EventTypeWarning, ReasonHPACreateFailed,
				fmt.Sprintf("Failed to create HPA from template: %v", err))
			return ctrl.Result{RequeueAfter: requeueIntervalMedium}, err
		}
	}

	// Validate and fetch the referenced HPA
	hpaValid := true
	if err := r.validateHPAReference(ctx, smartHPA); err != nil {
		logger.Error(err, "HPA reference validation failed")
		hpaValid = false
		meta.SetStatusCondition(&smartHPA.Status.Conditions, metav1.Condition{
			Type:               ConditionTypeHPAValid,
			Status:             metav1.ConditionFalse,
			Reason:             ReasonHPANotFound,
			Message:            fmt.Sprintf("Referenced HPA not found: %v", err),
			LastTransitionTime: metav1.Now(),
		})
		if err := r.Status().Update(ctx, smartHPA); err != nil {
			logger.Error(err, "Failed to update status")
		}
		return ctrl.Result{RequeueAfter: requeueIntervalMedium}, err
	}

	// HPA is valid
	if hpaValid {
		meta.SetStatusCondition(&smartHPA.Status.Conditions, metav1.Condition{
			Type:               ConditionTypeHPAValid,
			Status:             metav1.ConditionTrue,
			Reason:             ReasonReconciled,
			Message:            "Referenced HPA exists and is valid",
			LastTransitionTime: metav1.Now(),
		})
	}

	// Enqueue for scheduling with timeout to avoid blocking
	if err := r.enqueueForScheduling(ctx, req.NamespacedName, smartHPA); err != nil {
		logger.Error(err, "Failed to enqueue for scheduling")
		meta.SetStatusCondition(&smartHPA.Status.Conditions, metav1.Condition{
			Type:               ConditionTypeScheduled,
			Status:             metav1.ConditionFalse,
			Reason:             ReasonScheduleFailed,
			Message:            fmt.Sprintf("Failed to enqueue for scheduling: %v", err),
			LastTransitionTime: metav1.Now(),
		})
	} else {
		meta.SetStatusCondition(&smartHPA.Status.Conditions, metav1.Condition{
			Type:               ConditionTypeScheduled,
			Status:             metav1.ConditionTrue,
			Reason:             ReasonScheduleEnqueued,
			Message:            "Successfully enqueued for scheduling",
			LastTransitionTime: metav1.Now(),
		})
	}

	// Update overall Ready condition
	meta.SetStatusCondition(&smartHPA.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeReady,
		Status:             metav1.ConditionTrue,
		Reason:             ReasonReconciled,
		Message:            "SmartHPA reconciled successfully",
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: smartHPA.Generation,
	})

	// Update status
	if err := r.Status().Update(ctx, smartHPA); err != nil {
		logger.Error(err, "unable to update SmartHPA status")
		return ctrl.Result{RequeueAfter: requeueIntervalShort}, err
	}

	r.recordEvent(smartHPA, corev1.EventTypeNormal, ReasonReconciled, "SmartHPA reconciled successfully")
	logger.V(1).Info("SmartHPA reconciled successfully", "generation", smartHPA.Generation)

	// Requeue for periodic reconciliation
	return ctrl.Result{RequeueAfter: requeueIntervalLong}, nil
}

// handleDeletion handles the deletion of a SmartHPA resource using finalizers
func (r *SmartHorizontalPodAutoscalerReconciler) handleDeletion(ctx context.Context, smartHPA *autoscalingv1alpha1.SmartHorizontalPodAutoscaler) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(smartHPA, finalizerName) {
		// Finalizer already removed, nothing to do
		return ctrl.Result{}, nil
	}

	logger.Info("Handling deletion of SmartHPA", "name", smartHPA.Name, "namespace", smartHPA.Namespace)

	// Cleanup: Remove scheduler context for this SmartHPA
	key := types.NamespacedName{Name: smartHPA.Name, Namespace: smartHPA.Namespace}
	if r.scheduler != nil {
		r.scheduler.RemoveContext(key)
		logger.Info("Removed scheduler context", "key", key)
	}

	// Record cleanup event
	r.recordEvent(smartHPA, corev1.EventTypeNormal, ReasonCleanupComplete, "Scheduler context cleaned up")

	// Remove the finalizer
	controllerutil.RemoveFinalizer(smartHPA, finalizerName)
	if err := r.Update(ctx, smartHPA); err != nil {
		logger.Error(err, "Failed to remove finalizer")
		r.recordEvent(smartHPA, corev1.EventTypeWarning, ReasonFinalizerFailed,
			fmt.Sprintf("Failed to remove finalizer: %v", err))
		return ctrl.Result{}, err
	}

	logger.Info("Successfully cleaned up SmartHPA", "name", smartHPA.Name)
	return ctrl.Result{}, nil
}

// ensureHPAFromTemplate creates an HPA from the template if it doesn't exist
func (r *SmartHorizontalPodAutoscalerReconciler) ensureHPAFromTemplate(ctx context.Context, smartHPA *autoscalingv1alpha1.SmartHorizontalPodAutoscaler) error {
	logger := log.FromContext(ctx)

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
	if err == nil {
		// HPA already exists
		logger.V(1).Info("HPA from template already exists", "hpa", hpaName)
		return nil
	}

	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to check HPA existence: %w", err)
	}

	// HPA does not exist, create it
	newHPA := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:        hpaName,
			Namespace:   hpaNamespace,
			Labels:      smartHPA.Spec.HPASpecTemplate.Metadata.Labels,
			Annotations: smartHPA.Spec.HPASpecTemplate.Metadata.Annotations,
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			// ScaleTargetRef must be set - this should be part of HPASpecTemplate
			// For now, we require HPAObjectRef to point to an existing HPA with scaleTargetRef
		},
	}

	// Copy spec fields from template
	if smartHPA.Spec.HPASpecTemplate.Spec != nil {
		if smartHPA.Spec.HPASpecTemplate.Spec.MinReplicas != nil {
			newHPA.Spec.MinReplicas = smartHPA.Spec.HPASpecTemplate.Spec.MinReplicas
		}
		if smartHPA.Spec.HPASpecTemplate.Spec.MaxReplicas != nil {
			newHPA.Spec.MaxReplicas = *smartHPA.Spec.HPASpecTemplate.Spec.MaxReplicas
		}
	}

	// Set owner reference for garbage collection
	if err := ctrl.SetControllerReference(smartHPA, newHPA, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference: %w", err)
	}

	if err := r.Create(ctx, newHPA); err != nil {
		return fmt.Errorf("failed to create HPA: %w", err)
	}

	logger.Info("Created HPA from HPASpecTemplate", "hpa", hpaName)
	r.recordEvent(smartHPA, corev1.EventTypeNormal, ReasonHPACreated,
		fmt.Sprintf("Created HPA %s/%s from template", hpaNamespace, hpaName))

	return nil
}

// validateHPAReference validates that the referenced HPA exists
func (r *SmartHorizontalPodAutoscalerReconciler) validateHPAReference(ctx context.Context, smartHPA *autoscalingv1alpha1.SmartHorizontalPodAutoscaler) error {
	hpaRef := smartHPA.Spec.HPAObjectRef
	if hpaRef == nil {
		return nil // No reference to validate
	}

	if hpaRef.Name == "" {
		return nil // Empty name, skip validation
	}

	namespace := hpaRef.Namespace
	if namespace == "" {
		namespace = smartHPA.Namespace
	}

	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	hpaKey := types.NamespacedName{
		Name:      hpaRef.Name,
		Namespace: namespace,
	}

	if err := r.Get(ctx, hpaKey, hpa); err != nil {
		return fmt.Errorf("referenced HPA %s/%s not found: %w", namespace, hpaRef.Name, err)
	}

	return nil
}

// enqueueForScheduling sends the SmartHPA to the scheduler queue with timeout
func (r *SmartHorizontalPodAutoscalerReconciler) enqueueForScheduling(ctx context.Context, key types.NamespacedName, smartHPA *autoscalingv1alpha1.SmartHorizontalPodAutoscaler) error {
	logger := log.FromContext(ctx)

	// Check if there are any triggers to schedule
	if len(smartHPA.Spec.Triggers) == 0 {
		logger.V(1).Info("No triggers defined, skipping scheduling")
		return nil
	}

	// Check if HPAObjectRef is valid
	if smartHPA.Spec.HPAObjectRef == nil || smartHPA.Spec.HPAObjectRef.Name == "" {
		logger.V(1).Info("No HPAObjectRef specified, skipping scheduling")
		return nil
	}

	// Send to queue with timeout to avoid blocking forever
	select {
	case r.queue <- key:
		logger.V(1).Info("Enqueued SmartHPA for scheduling", "namespacedName", key)
		return nil
	case <-time.After(queueSendTimeout):
		return fmt.Errorf("timeout sending to scheduler queue")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *SmartHorizontalPodAutoscalerReconciler) SetupWithManager(mgr ctrl.Manager, queue chan types.NamespacedName, scheduler SchedulerInterface) error {
	r.queue = queue
	r.scheduler = scheduler
	r.Recorder = mgr.GetEventRecorderFor("smarthpa-controller")

	return ctrl.NewControllerManagedBy(mgr).
		For(&autoscalingv1alpha1.SmartHorizontalPodAutoscaler{}).
		Owns(&autoscalingv2.HorizontalPodAutoscaler{}). // Watch owned HPAs (created from template)
		Watches(
			&autoscalingv2.HorizontalPodAutoscaler{},
			handler.EnqueueRequestsFromMapFunc(r.findSmartHPAsForHPA),
		). // Watch all HPAs to catch referenced HPA changes
		Complete(r)
}

// recordEvent records an event if the recorder is available
func (r *SmartHorizontalPodAutoscalerReconciler) recordEvent(obj runtime.Object, eventType, reason, message string) {
	if r.Recorder != nil {
		r.Recorder.Event(obj, eventType, reason, message)
	}
}

// findSmartHPAsForHPA returns reconcile requests for SmartHPAs that reference the given HPA
func (r *SmartHorizontalPodAutoscalerReconciler) findSmartHPAsForHPA(ctx context.Context, hpa client.Object) []reconcile.Request {
	logger := log.FromContext(ctx)

	// List all SmartHPAs
	smartHPAList := &autoscalingv1alpha1.SmartHorizontalPodAutoscalerList{}
	if err := r.List(ctx, smartHPAList); err != nil {
		logger.Error(err, "Failed to list SmartHPAs")
		return nil
	}

	var requests []reconcile.Request
	hpaNamespace := hpa.GetNamespace()
	hpaName := hpa.GetName()

	for _, smartHPA := range smartHPAList.Items {
		// Check if this SmartHPA references the changed HPA
		if smartHPA.Spec.HPAObjectRef != nil {
			refNamespace := smartHPA.Spec.HPAObjectRef.Namespace
			if refNamespace == "" {
				refNamespace = smartHPA.Namespace
			}
			if refNamespace == hpaNamespace && smartHPA.Spec.HPAObjectRef.Name == hpaName {
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      smartHPA.Name,
						Namespace: smartHPA.Namespace,
					},
				})
				logger.V(1).Info("HPA change triggers SmartHPA reconcile",
					"hpa", hpaName, "smartHPA", smartHPA.Name)
			}
		}
	}

	return requests
}
