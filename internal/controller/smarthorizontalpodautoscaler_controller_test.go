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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	autoscalingv1alpha1 "github.com/sarabala1979/SmartHPA/api/v1alpha1"
)

// mockScheduler implements SchedulerInterface for testing
type mockScheduler struct{}

func (m *mockScheduler) RemoveContext(key types.NamespacedName)  {}
func (m *mockScheduler) RefreshContext(key types.NamespacedName) {}

var _ = Describe("SmartHorizontalPodAutoscaler Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"
		const hpaName = "test-hpa"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		var (
			minReplicas     = int32(1)
			maxReplicas     = int32(5)
			desiredReplicas = int32(3)
		)

		BeforeEach(func() {
			By("Creating the HPA resource")
			hpa := &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      hpaName,
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
			Expect(k8sClient.Create(ctx, hpa)).To(Succeed())

			By("Creating the SmartHPA resource")
			smartHPA := &autoscalingv1alpha1.SmartHorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: autoscalingv1alpha1.SmartHorizontalPodAutoscalerSpec{
					HPAObjectRef: &autoscalingv1alpha1.HPAObjectReference{
						Name:      hpaName,
						Namespace: "default",
					},
					Triggers: []autoscalingv1alpha1.Trigger{
						{
							Name:      "business-hours",
							StartTime: "09:00:00",
							EndTime:   "17:00:00",
							Timezone:  "America/Los_Angeles",
							Interval: &autoscalingv1alpha1.Interval{
								Recurring: "M,TU,W,TH,F",
							},
							StartHPAConfig: &autoscalingv1alpha1.HPAConfig{
								MinReplicas:     &minReplicas,
								MaxReplicas:     &maxReplicas,
								DesiredReplicas: &desiredReplicas,
							},
							EndHPAConfig: &autoscalingv1alpha1.HPAConfig{
								MinReplicas:     &minReplicas,
								MaxReplicas:     &maxReplicas,
								DesiredReplicas: &desiredReplicas,
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, smartHPA)).To(Succeed())
		})

		AfterEach(func() {
			By("Cleaning up resources")
			// Clean up SmartHPA - need to remove finalizer first if present
			smartHPA := &autoscalingv1alpha1.SmartHorizontalPodAutoscaler{}
			if err := k8sClient.Get(ctx, typeNamespacedName, smartHPA); err == nil {
				// Remove finalizer if present to allow deletion
				if len(smartHPA.Finalizers) > 0 {
					smartHPA.Finalizers = nil
					_ = k8sClient.Update(ctx, smartHPA)
				}
				_ = k8sClient.Delete(ctx, smartHPA)
				// Wait for deletion
				Eventually(func() bool {
					err := k8sClient.Get(ctx, typeNamespacedName, smartHPA)
					return err != nil
				}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())
			}

			hpa := &autoscalingv2.HorizontalPodAutoscaler{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: hpaName, Namespace: "default"}, hpa); err == nil {
				_ = k8sClient.Delete(ctx, hpa)
				// Wait for deletion
				Eventually(func() bool {
					err := k8sClient.Get(ctx, types.NamespacedName{Name: hpaName, Namespace: "default"}, hpa)
					return err != nil
				}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())
			}
		})

		Context("Basic reconciliation", func() {
			It("should successfully reconcile the resource", func() {
				By("Reconciling the created resource")
				queue := make(chan types.NamespacedName, 100)
				fakeRecorder := record.NewFakeRecorder(10)
				controllerReconciler := &SmartHorizontalPodAutoscalerReconciler{
					Client:    k8sClient,
					Scheme:    k8sClient.Scheme(),
					Recorder:  fakeRecorder,
					queue:     queue,
					scheduler: &mockScheduler{},
				}

				// Start a goroutine to consume from the queue
				queueCtx, queueCancel := context.WithCancel(ctx)
				defer queueCancel()
				go func() {
					for {
						select {
						case <-queue:
							// Just consume the items
						case <-queueCtx.Done():
							return
						}
					}
				}()

				By("Setting test timeouts")
				SetDefaultEventuallyTimeout(10 * time.Second)
				SetDefaultEventuallyPollingInterval(100 * time.Millisecond)

				// First reconcile adds finalizer
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: typeNamespacedName,
				})
				Expect(err).NotTo(HaveOccurred())

				// Second reconcile processes normally after finalizer is added
				_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: typeNamespacedName,
				})
				Expect(err).NotTo(HaveOccurred())

				By("Verifying SmartHPA status")
				updatedSmartHPA := &autoscalingv1alpha1.SmartHorizontalPodAutoscaler{}
				err = k8sClient.Get(ctx, typeNamespacedName, updatedSmartHPA)
				Expect(err).NotTo(HaveOccurred())
				Expect(updatedSmartHPA.Status.Conditions).NotTo(BeEmpty())

				By("Verifying finalizer was added")
				Expect(updatedSmartHPA.Finalizers).To(ContainElement(finalizerName))
			})
		})

		Context("Error handling", func() {
			It("should handle missing HPA gracefully", func() {
				By("Deleting the HPA")
				hpa := &autoscalingv2.HorizontalPodAutoscaler{
					ObjectMeta: metav1.ObjectMeta{
						Name:      hpaName,
						Namespace: "default",
					},
				}
				Expect(k8sClient.Delete(ctx, hpa)).To(Succeed())

				// Wait for HPA to be deleted
				Eventually(func() bool {
					err := k8sClient.Get(ctx, types.NamespacedName{Name: hpaName, Namespace: "default"}, hpa)
					return err != nil
				}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())

				By("Reconciling the resource")
				queue := make(chan types.NamespacedName, 100)
				fakeRecorder := record.NewFakeRecorder(10)
				controllerReconciler := &SmartHorizontalPodAutoscalerReconciler{
					Client:    k8sClient,
					Scheme:    k8sClient.Scheme(),
					Recorder:  fakeRecorder,
					queue:     queue,
					scheduler: &mockScheduler{},
				}

				// Start a goroutine to consume from the queue
				queueCtx, queueCancel := context.WithCancel(ctx)
				defer queueCancel()
				go func() {
					for {
						select {
						case <-queue:
							// Just consume the items
						case <-queueCtx.Done():
							return
						}
					}
				}()

				// First reconcile adds finalizer
				_, _ = controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: typeNamespacedName,
				})

				// Second reconcile should fail with HPA not found
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: typeNamespacedName,
				})
				Expect(err).To(HaveOccurred())

				By("Verifying error condition in status")
				updatedSmartHPA := &autoscalingv1alpha1.SmartHorizontalPodAutoscaler{}
				err = k8sClient.Get(ctx, typeNamespacedName, updatedSmartHPA)
				Expect(err).NotTo(HaveOccurred())
				Expect(updatedSmartHPA.Status.Conditions).To(ContainElement(gstruct.MatchFields(gstruct.IgnoreExtras,
					gstruct.Fields{
						"Type":   Equal(ConditionTypeHPAValid),
						"Status": Equal(metav1.ConditionFalse),
					},
				)))
			})
		})

		Context("Deletion handling", func() {
			It("should clean up scheduler context on deletion", func() {
				By("Adding finalizer via reconciliation")
				queue := make(chan types.NamespacedName, 100)
				fakeRecorder := record.NewFakeRecorder(10)
				mockSched := &mockScheduler{}
				controllerReconciler := &SmartHorizontalPodAutoscalerReconciler{
					Client:    k8sClient,
					Scheme:    k8sClient.Scheme(),
					Recorder:  fakeRecorder,
					queue:     queue,
					scheduler: mockSched,
				}

				// Start a goroutine to consume from the queue
				queueCtx, queueCancel := context.WithCancel(ctx)
				defer queueCancel()
				go func() {
					for {
						select {
						case <-queue:
							// Just consume the items
						case <-queueCtx.Done():
							return
						}
					}
				}()

				// Reconcile to add finalizer
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: typeNamespacedName,
				})
				Expect(err).NotTo(HaveOccurred())

				By("Deleting the SmartHPA")
				smartHPA := &autoscalingv1alpha1.SmartHorizontalPodAutoscaler{}
				Expect(k8sClient.Get(ctx, typeNamespacedName, smartHPA)).To(Succeed())
				Expect(k8sClient.Delete(ctx, smartHPA)).To(Succeed())

				By("Reconciling the deletion")
				_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: typeNamespacedName,
				})
				Expect(err).NotTo(HaveOccurred())

				By("Verifying the SmartHPA is deleted")
				Eventually(func() bool {
					err := k8sClient.Get(ctx, typeNamespacedName, smartHPA)
					return err != nil
				}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())
			})
		})
	})
})
