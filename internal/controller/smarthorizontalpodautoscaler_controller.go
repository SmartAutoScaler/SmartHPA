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
		logger.Error(err, "unable to fetch SmartHorizontalPodAutoscaler")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Fetch the referenced HPA
	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	hpaName := types.NamespacedName{
		Name:      smartHPA.Spec.HPAObjectRef.Name,
		Namespace: smartHPA.Spec.HPAObjectRef.Namespace,
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
		return ctrl.Result{}, err
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
		return ctrl.Result{}, err
	}

	// Enqueue for scheduling
	r.queue <- req.NamespacedName
	klog.Infof("Enqueued SmartHPA %s", req.NamespacedName)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SmartHorizontalPodAutoscalerReconciler) SetupWithManager(mgr ctrl.Manager, queue chan types.NamespacedName) error {
	r.queue = queue
	return ctrl.NewControllerManagedBy(mgr).
		For(&autoscalingv1alpha1.SmartHorizontalPodAutoscaler{}).
		Complete(r)
}
