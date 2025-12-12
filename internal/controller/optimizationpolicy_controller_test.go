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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
)

var _ = Describe("OptimizationPolicy Controller", func() {
	const (
		timeout  = time.Second * 10
		interval = time.Millisecond * 250
	)

	Context("When reconciling a valid OptimizationPolicy", func() {
		It("Should update status to Ready", func() {
			ctx := context.Background()

			policy := &optipodv1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy-valid",
					Namespace: "default",
				},
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Mode: optipodv1alpha1.ModeAuto,
					Selector: optipodv1alpha1.WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
					},
					MetricsConfig: optipodv1alpha1.MetricsConfig{
						Provider:   "prometheus",
						Percentile: "P90",
					},
					ResourceBounds: optipodv1alpha1.ResourceBounds{
						CPU: optipodv1alpha1.ResourceBound{
							Min: resource.MustParse("100m"),
							Max: resource.MustParse("4000m"),
						},
						Memory: optipodv1alpha1.ResourceBound{
							Min: resource.MustParse("128Mi"),
							Max: resource.MustParse("8Gi"),
						},
					},
					UpdateStrategy: optipodv1alpha1.UpdateStrategy{
						AllowInPlaceResize: true,
						UpdateRequestsOnly: true,
					},
				},
			}

			Expect(k8sClient.Create(ctx, policy)).Should(Succeed())

			// Set up reconciler with event recorder
			reconciler := &OptimizationPolicyReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(10),
			}

			// Reconcile
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      policy.Name,
					Namespace: policy.Namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Check status
			Eventually(func() bool {
				updatedPolicy := &optipodv1alpha1.OptimizationPolicy{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      policy.Name,
					Namespace: policy.Namespace,
				}, updatedPolicy)
				if err != nil {
					return false
				}

				for _, condition := range updatedPolicy.Status.Conditions {
					if condition.Type == "Ready" && condition.Status == metav1.ConditionTrue {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())

			// Clean up
			Expect(k8sClient.Delete(ctx, policy)).Should(Succeed())
		})
	})

	Context("When reconciling an invalid OptimizationPolicy", func() {
		It("Should update status to ValidationFailed", func() {
			ctx := context.Background()

			policy := &optipodv1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy-invalid",
					Namespace: "default",
				},
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Mode: optipodv1alpha1.ModeAuto,
					Selector: optipodv1alpha1.WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
					},
					MetricsConfig: optipodv1alpha1.MetricsConfig{
						Provider:   "prometheus",
						Percentile: "P90",
					},
					ResourceBounds: optipodv1alpha1.ResourceBounds{
						CPU: optipodv1alpha1.ResourceBound{
							Min: resource.MustParse("4000m"), // Invalid: min > max
							Max: resource.MustParse("100m"),
						},
						Memory: optipodv1alpha1.ResourceBound{
							Min: resource.MustParse("128Mi"),
							Max: resource.MustParse("8Gi"),
						},
					},
					UpdateStrategy: optipodv1alpha1.UpdateStrategy{
						AllowInPlaceResize: true,
						UpdateRequestsOnly: true,
					},
				},
			}

			Expect(k8sClient.Create(ctx, policy)).Should(Succeed())

			// Set up reconciler with event recorder
			fakeRecorder := record.NewFakeRecorder(10)
			reconciler := &OptimizationPolicyReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: fakeRecorder,
			}

			// Reconcile
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      policy.Name,
					Namespace: policy.Namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Check status
			Eventually(func() bool {
				updatedPolicy := &optipodv1alpha1.OptimizationPolicy{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      policy.Name,
					Namespace: policy.Namespace,
				}, updatedPolicy)
				if err != nil {
					return false
				}

				for _, condition := range updatedPolicy.Status.Conditions {
					if condition.Type == "Ready" && condition.Status == metav1.ConditionFalse &&
						condition.Reason == "ValidationFailed" {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())

			// Check that an event was emitted
			Eventually(func() bool {
				select {
				case event := <-fakeRecorder.Events:
					return len(event) > 0
				default:
					return false
				}
			}, timeout, interval).Should(BeTrue())

			// Clean up
			Expect(k8sClient.Delete(ctx, policy)).Should(Succeed())
		})
	})

	Context("When reconciling with reconciliation interval", func() {
		It("Should requeue after the specified interval", func() {
			ctx := context.Background()

			policy := &optipodv1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy-requeue",
					Namespace: "default",
				},
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Mode: optipodv1alpha1.ModeRecommend,
					Selector: optipodv1alpha1.WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
					},
					MetricsConfig: optipodv1alpha1.MetricsConfig{
						Provider:   "prometheus",
						Percentile: "P90",
					},
					ResourceBounds: optipodv1alpha1.ResourceBounds{
						CPU: optipodv1alpha1.ResourceBound{
							Min: resource.MustParse("100m"),
							Max: resource.MustParse("4000m"),
						},
						Memory: optipodv1alpha1.ResourceBound{
							Min: resource.MustParse("128Mi"),
							Max: resource.MustParse("8Gi"),
						},
					},
					UpdateStrategy: optipodv1alpha1.UpdateStrategy{
						AllowInPlaceResize: true,
						UpdateRequestsOnly: true,
					},
					ReconciliationInterval: metav1.Duration{Duration: 2 * time.Minute},
				},
			}

			Expect(k8sClient.Create(ctx, policy)).Should(Succeed())

			// Set up reconciler
			reconciler := &OptimizationPolicyReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(10),
			}

			// Reconcile
			result, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      policy.Name,
					Namespace: policy.Namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify that requeue is set with the correct interval
			// In Recommend mode, the requeue interval is doubled
			Expect(result.RequeueAfter).To(Equal(4 * time.Minute))

			// Clean up
			Expect(k8sClient.Delete(ctx, policy)).Should(Succeed())
		})
	})
})
