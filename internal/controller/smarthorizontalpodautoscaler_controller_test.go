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
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	autoscalingv1alpha1 "github.com/sarabala1979/SmartHPA/api/v1alpha1"
)

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
							Interval: &autoscalingv1alpha1.Interval{
								Recurring: "M,TU,W,TH,F",
								Timezone:  "America/Los_Angeles",
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
			smartHPA := &autoscalingv1alpha1.SmartHorizontalPodAutoscaler{}
			if err := k8sClient.Get(ctx, typeNamespacedName, smartHPA); err == nil {
				Expect(k8sClient.Delete(ctx, smartHPA)).To(Succeed())
			}

			hpa := &autoscalingv2.HorizontalPodAutoscaler{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: hpaName, Namespace: "default"}, hpa); err == nil {
				Expect(k8sClient.Delete(ctx, hpa)).To(Succeed())
			}
		})

		Context("Basic reconciliation", func() {
			It("should successfully reconcile the resource", func() {
				By("Reconciling the created resource")
				controllerReconciler := &SmartHorizontalPodAutoscalerReconciler{
					Client: k8sClient,
					Scheme: k8sClient.Scheme(),
				}

				By("Setting test timeouts")
				SetDefaultEventuallyTimeout(10 * time.Second)
				SetDefaultEventuallyPollingInterval(100 * time.Millisecond)

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: typeNamespacedName,
				})
				Expect(err).NotTo(HaveOccurred())

				By("Verifying SmartHPA status")
				updatedSmartHPA := &autoscalingv1alpha1.SmartHorizontalPodAutoscaler{}
				err = k8sClient.Get(ctx, typeNamespacedName, updatedSmartHPA)
				Expect(err).NotTo(HaveOccurred())
				Expect(updatedSmartHPA.Status.Conditions).NotTo(BeEmpty())
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

				By("Reconciling the resource")
				controllerReconciler := &SmartHorizontalPodAutoscalerReconciler{
					Client: k8sClient,
					Scheme: k8sClient.Scheme(),
				}

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
						"Type":   Equal("Error"),
						"Status": Equal(metav1.ConditionTrue),
					},
				)))
			})
		})
	})
})
