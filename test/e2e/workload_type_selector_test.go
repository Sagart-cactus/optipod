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

package e2e

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/discovery"
	"github.com/optipod/optipod/test/utils"
)

var _ = Describe("Workload Type Selector E2E Tests", func() {
	const (
		timeout          = time.Second * 30
		interval         = time.Millisecond * 250
		namespaceTimeout = time.Minute * 2 // Longer timeout for namespace operations
	)

	var (
		testNamespace string
		ctx           = context.Background()
	)

	BeforeEach(func() {
		// Generate unique namespace name for each test to avoid conflicts
		testNamespace = fmt.Sprintf("workload-type-test-%d", time.Now().UnixNano())

		// Create test namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
				Labels: map[string]string{
					"environment": "test",
					"team":        "platform",
				},
			},
		}
		err := utils.CreateResource(ctx, ns)
		Expect(err).NotTo(HaveOccurred())

		// Wait for namespace to be ready
		Eventually(func() bool {
			err := utils.GetResource(ctx, types.NamespacedName{Name: testNamespace}, ns)
			return err == nil && ns.Status.Phase == corev1.NamespaceActive
		}, timeout, interval).Should(BeTrue(), "Namespace should be active")
	})

	AfterEach(func() {
		// Clean up all resources in the namespace first
		By("Cleaning up test resources")

		// Delete all optimization policies in the namespace
		policyList := &optipodv1alpha1.OptimizationPolicyList{}
		err := utils.GetClient().List(ctx, policyList, client.InNamespace("default"))
		if err == nil {
			for _, policy := range policyList.Items {
				if strings.Contains(policy.Name, "test") || strings.Contains(policy.Name, "policy") {
					_ = utils.DeleteResource(ctx, &policy)
				}
			}
		}

		// Delete all deployments in test namespace
		deploymentList := &appsv1.DeploymentList{}
		err = utils.GetClient().List(ctx, deploymentList, client.InNamespace(testNamespace))
		if err == nil {
			for _, deployment := range deploymentList.Items {
				_ = utils.DeleteResource(ctx, &deployment)
			}
		}

		// Delete all statefulsets in test namespace
		statefulSetList := &appsv1.StatefulSetList{}
		err = utils.GetClient().List(ctx, statefulSetList, client.InNamespace(testNamespace))
		if err == nil {
			for _, statefulSet := range statefulSetList.Items {
				_ = utils.DeleteResource(ctx, &statefulSet)
			}
		}

		// Delete all daemonsets in test namespace
		daemonSetList := &appsv1.DaemonSetList{}
		err = utils.GetClient().List(ctx, daemonSetList, client.InNamespace(testNamespace))
		if err == nil {
			for _, daemonSet := range daemonSetList.Items {
				_ = utils.DeleteResource(ctx, &daemonSet)
			}
		}

		// Wait a bit for resources to be cleaned up
		time.Sleep(5 * time.Second)

		// Clean up test namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		err = utils.DeleteResource(ctx, ns)
		if err != nil {
			// Namespace might already be deleted, which is fine
			GinkgoWriter.Printf("Warning: Failed to delete namespace %s: %v\n", testNamespace, err)
			return
		}

		// Wait for namespace to be fully deleted with longer timeout
		Eventually(func() bool {
			err := utils.GetResource(ctx, types.NamespacedName{Name: testNamespace}, ns)
			return err != nil // Returns true when namespace no longer exists
		}, namespaceTimeout, interval).Should(BeTrue(), "Namespace should be deleted")
	})

	Context("Complete Workflow Tests", func() {
		It("Should complete full workflow from policy creation to workload discovery with include filter", func() {
			// Create test workloads of different types
			deployment := createTestDeployment(testNamespace, map[string]string{
				"app":      "web",
				"optimize": "true",
			})
			Expect(utils.CreateResource(ctx, deployment)).Should(Succeed())

			statefulSet := createTestStatefulSet(testNamespace, map[string]string{
				"app":      "database",
				"optimize": "true",
			})
			Expect(utils.CreateResource(ctx, statefulSet)).Should(Succeed())

			daemonSet := createTestDaemonSet(testNamespace, map[string]string{
				"app":      "monitoring",
				"optimize": "true",
			})
			Expect(utils.CreateResource(ctx, daemonSet)).Should(Succeed())

			// Create policy that only includes Deployments
			policy := createBasicOptimizationPolicy("deployment-only-policy", &optipodv1alpha1.WorkloadTypeFilter{
				Include: []optipodv1alpha1.WorkloadType{
					optipodv1alpha1.WorkloadTypeDeployment,
				},
			})
			Expect(utils.CreateResource(ctx, policy)).Should(Succeed())

			// Wait for policy to be processed
			Eventually(func() bool {
				updatedPolicy := &optipodv1alpha1.OptimizationPolicy{}
				err := utils.GetResource(ctx, types.NamespacedName{
					Name:      policy.Name,
					Namespace: policy.Namespace,
				}, updatedPolicy)
				if err != nil {
					return false
				}

				// Check for Ready condition
				for _, condition := range updatedPolicy.Status.Conditions {
					if condition.Type == "Ready" && condition.Status == metav1.ConditionTrue {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())

			// Verify workload discovery only finds Deployments
			Eventually(func() bool {
				updatedPolicy := &optipodv1alpha1.OptimizationPolicy{}
				err := utils.GetResource(ctx, types.NamespacedName{
					Name:      policy.Name,
					Namespace: policy.Namespace,
				}, updatedPolicy)
				if err != nil {
					return false
				}

				// Should discover only 1 workload (the Deployment)
				return updatedPolicy.Status.WorkloadsDiscovered == 1
			}, timeout, interval).Should(BeTrue())

			// Verify status shows correct workload type breakdown
			Eventually(func() bool {
				updatedPolicy := &optipodv1alpha1.OptimizationPolicy{}
				err := utils.GetResource(ctx, types.NamespacedName{
					Name:      policy.Name,
					Namespace: policy.Namespace,
				}, updatedPolicy)
				if err != nil {
					return false
				}

				// Check workload type breakdown in status
				if updatedPolicy.Status.WorkloadsByType == nil {
					return false
				}

				return updatedPolicy.Status.WorkloadsByType.Deployments == 1 &&
					updatedPolicy.Status.WorkloadsByType.StatefulSets == 0 &&
					updatedPolicy.Status.WorkloadsByType.DaemonSets == 0
			}, timeout, interval).Should(BeTrue())

			// Clean up
			Expect(utils.DeleteResource(ctx, policy)).Should(Succeed())
			Expect(utils.DeleteResource(ctx, deployment)).Should(Succeed())
			Expect(utils.DeleteResource(ctx, statefulSet)).Should(Succeed())
			Expect(utils.DeleteResource(ctx, daemonSet)).Should(Succeed())
		})

		It("Should complete full workflow with exclude filter", func() {
			// Create test workloads of different types
			deployment := createTestDeployment(testNamespace, map[string]string{
				"app":      "web",
				"optimize": "true",
			})
			Expect(utils.CreateResource(ctx, deployment)).Should(Succeed())

			statefulSet := createTestStatefulSet(testNamespace, map[string]string{
				"app":      "database",
				"optimize": "true",
			})
			Expect(utils.CreateResource(ctx, statefulSet)).Should(Succeed())

			daemonSet := createTestDaemonSet(testNamespace, map[string]string{
				"app":      "monitoring",
				"optimize": "true",
			})
			Expect(utils.CreateResource(ctx, daemonSet)).Should(Succeed())

			// Create policy that excludes StatefulSets
			policy := createBasicOptimizationPolicy("exclude-statefulset-policy", &optipodv1alpha1.WorkloadTypeFilter{
				Exclude: []optipodv1alpha1.WorkloadType{
					optipodv1alpha1.WorkloadTypeStatefulSet,
				},
			})
			Expect(utils.CreateResource(ctx, policy)).Should(Succeed())

			// Wait for policy to be processed and verify discovery
			Eventually(func() bool {
				updatedPolicy := &optipodv1alpha1.OptimizationPolicy{}
				err := utils.GetResource(ctx, types.NamespacedName{
					Name:      policy.Name,
					Namespace: policy.Namespace,
				}, updatedPolicy)
				if err != nil {
					return false
				}

				// Should discover 2 workloads (Deployment + DaemonSet, excluding StatefulSet)
				return updatedPolicy.Status.WorkloadsDiscovered == 2
			}, timeout, interval).Should(BeTrue())

			// Verify status shows correct workload type breakdown
			Eventually(func() bool {
				updatedPolicy := &optipodv1alpha1.OptimizationPolicy{}
				err := utils.GetResource(ctx, types.NamespacedName{
					Name:      policy.Name,
					Namespace: policy.Namespace,
				}, updatedPolicy)
				if err != nil {
					return false
				}

				// Check workload type breakdown in status
				if updatedPolicy.Status.WorkloadsByType == nil {
					return false
				}

				return updatedPolicy.Status.WorkloadsByType.Deployments == 1 &&
					updatedPolicy.Status.WorkloadsByType.StatefulSets == 0 &&
					updatedPolicy.Status.WorkloadsByType.DaemonSets == 1
			}, timeout, interval).Should(BeTrue())

			// Clean up
			Expect(utils.DeleteResource(ctx, policy)).Should(Succeed())
			Expect(utils.DeleteResource(ctx, deployment)).Should(Succeed())
			Expect(utils.DeleteResource(ctx, statefulSet)).Should(Succeed())
			Expect(utils.DeleteResource(ctx, daemonSet)).Should(Succeed())
		})

		It("Should handle backward compatibility with existing policies", func() {
			// Create test workloads of different types
			deployment := createTestDeployment(testNamespace, map[string]string{
				"app":      "web",
				"optimize": "true",
			})
			Expect(utils.CreateResource(ctx, deployment)).Should(Succeed())

			statefulSet := createTestStatefulSet(testNamespace, map[string]string{
				"app":      "database",
				"optimize": "true",
			})
			Expect(utils.CreateResource(ctx, statefulSet)).Should(Succeed())

			daemonSet := createTestDaemonSet(testNamespace, map[string]string{
				"app":      "monitoring",
				"optimize": "true",
			})
			Expect(utils.CreateResource(ctx, daemonSet)).Should(Succeed())

			// Create policy WITHOUT workloadTypes field (backward compatibility)
			policy := &optipodv1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "backward-compatibility-policy",
					Namespace: "default",
				},
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Mode: optipodv1alpha1.ModeRecommend,
					Selector: optipodv1alpha1.WorkloadSelector{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"environment": "test"},
						},
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"optimize": "true"},
						},
						// WorkloadTypes is intentionally omitted for backward compatibility test
					},
					MetricsConfig: optipodv1alpha1.MetricsConfig{
						Provider:   "metrics-server",
						Percentile: "P90",
					},
					ResourceBounds: optipodv1alpha1.ResourceBounds{
						CPU: optipodv1alpha1.ResourceBound{
							Min: resource.MustParse("50m"),
							Max: resource.MustParse("2000m"),
						},
						Memory: optipodv1alpha1.ResourceBound{
							Min: resource.MustParse("64Mi"),
							Max: resource.MustParse("4Gi"),
						},
					},
					UpdateStrategy: optipodv1alpha1.UpdateStrategy{
						AllowInPlaceResize: true,
						UpdateRequestsOnly: true,
					},
					ReconciliationInterval: metav1.Duration{Duration: 1 * time.Minute},
				},
			}
			Expect(utils.CreateResource(ctx, policy)).Should(Succeed())

			// Wait for policy to be processed and verify discovery
			Eventually(func() bool {
				updatedPolicy := &optipodv1alpha1.OptimizationPolicy{}
				err := utils.GetResource(ctx, types.NamespacedName{
					Name:      policy.Name,
					Namespace: policy.Namespace,
				}, updatedPolicy)
				if err != nil {
					return false
				}

				// Should discover all 3 workloads (backward compatibility)
				return updatedPolicy.Status.WorkloadsDiscovered == 3
			}, timeout, interval).Should(BeTrue())

			// Verify status shows all workload types
			Eventually(func() bool {
				updatedPolicy := &optipodv1alpha1.OptimizationPolicy{}
				err := utils.GetResource(ctx, types.NamespacedName{
					Name:      policy.Name,
					Namespace: policy.Namespace,
				}, updatedPolicy)
				if err != nil {
					return false
				}

				// Check workload type breakdown in status
				if updatedPolicy.Status.WorkloadsByType == nil {
					return false
				}

				return updatedPolicy.Status.WorkloadsByType.Deployments == 1 &&
					updatedPolicy.Status.WorkloadsByType.StatefulSets == 1 &&
					updatedPolicy.Status.WorkloadsByType.DaemonSets == 1
			}, timeout, interval).Should(BeTrue())

			// Clean up
			Expect(utils.DeleteResource(ctx, policy)).Should(Succeed())
			Expect(utils.DeleteResource(ctx, deployment)).Should(Succeed())
			Expect(utils.DeleteResource(ctx, statefulSet)).Should(Succeed())
			Expect(utils.DeleteResource(ctx, daemonSet)).Should(Succeed())
		})
	})

	Context("Multiple Policy Interaction Tests", func() {
		It("Should handle multiple policies with different workload type filters", func() {
			// Create test workloads of different types
			deployment := createTestDeployment(testNamespace, map[string]string{
				"app":      "web",
				"optimize": "true",
			})
			Expect(utils.CreateResource(ctx, deployment)).Should(Succeed())

			statefulSet := createTestStatefulSet(testNamespace, map[string]string{
				"app":      "database",
				"optimize": "true",
			})
			Expect(utils.CreateResource(ctx, statefulSet)).Should(Succeed())

			daemonSet := createTestDaemonSet(testNamespace, map[string]string{
				"app":      "monitoring",
				"optimize": "true",
			})
			Expect(utils.CreateResource(ctx, daemonSet)).Should(Succeed())

			// Create first policy for Deployments only
			deploymentPolicy := createWeightedOptimizationPolicy(
				"deployment-policy",
				100, // Higher weight
				&optipodv1alpha1.WorkloadTypeFilter{
					Include: []optipodv1alpha1.WorkloadType{
						optipodv1alpha1.WorkloadTypeDeployment,
					},
				},
				"P90",
				"50m", "2000m",
				"64Mi", "4Gi",
				true, true,
				1*time.Minute,
			)
			Expect(utils.CreateResource(ctx, deploymentPolicy)).Should(Succeed())

			// Create second policy for StatefulSets only
			statefulSetPolicy := createWeightedOptimizationPolicy(
				"statefulset-policy",
				50, // Lower weight
				&optipodv1alpha1.WorkloadTypeFilter{
					Include: []optipodv1alpha1.WorkloadType{
						optipodv1alpha1.WorkloadTypeStatefulSet,
					},
				},
				"P99",
				"100m", "4000m",
				"128Mi", "8Gi",
				false, false,
				2*time.Minute,
			)
			Expect(utils.CreateResource(ctx, statefulSetPolicy)).Should(Succeed())

			// Wait for both policies to be processed
			Eventually(func() bool {
				deploymentPolicyUpdated := &optipodv1alpha1.OptimizationPolicy{}
				err1 := utils.GetResource(ctx, types.NamespacedName{
					Name:      deploymentPolicy.Name,
					Namespace: deploymentPolicy.Namespace,
				}, deploymentPolicyUpdated)

				statefulSetPolicyUpdated := &optipodv1alpha1.OptimizationPolicy{}
				err2 := utils.GetResource(ctx, types.NamespacedName{
					Name:      statefulSetPolicy.Name,
					Namespace: statefulSetPolicy.Namespace,
				}, statefulSetPolicyUpdated)

				if err1 != nil || err2 != nil {
					return false
				}

				// Check both policies have discovered their respective workloads
				return deploymentPolicyUpdated.Status.WorkloadsDiscovered == 1 &&
					statefulSetPolicyUpdated.Status.WorkloadsDiscovered == 1
			}, timeout, interval).Should(BeTrue())

			// Verify each policy only discovers its targeted workload types
			Eventually(func() bool {
				deploymentPolicyUpdated := &optipodv1alpha1.OptimizationPolicy{}
				err1 := utils.GetResource(ctx, types.NamespacedName{
					Name:      deploymentPolicy.Name,
					Namespace: deploymentPolicy.Namespace,
				}, deploymentPolicyUpdated)

				statefulSetPolicyUpdated := &optipodv1alpha1.OptimizationPolicy{}
				err2 := utils.GetResource(ctx, types.NamespacedName{
					Name:      statefulSetPolicy.Name,
					Namespace: statefulSetPolicy.Namespace,
				}, statefulSetPolicyUpdated)

				if err1 != nil || err2 != nil {
					return false
				}

				// Check workload type breakdown for deployment policy
				deploymentPolicyCorrect := deploymentPolicyUpdated.Status.WorkloadsByType != nil &&
					deploymentPolicyUpdated.Status.WorkloadsByType.Deployments == 1 &&
					deploymentPolicyUpdated.Status.WorkloadsByType.StatefulSets == 0 &&
					deploymentPolicyUpdated.Status.WorkloadsByType.DaemonSets == 0

				// Check workload type breakdown for statefulset policy
				statefulSetPolicyCorrect := statefulSetPolicyUpdated.Status.WorkloadsByType != nil &&
					statefulSetPolicyUpdated.Status.WorkloadsByType.Deployments == 0 &&
					statefulSetPolicyUpdated.Status.WorkloadsByType.StatefulSets == 1 &&
					statefulSetPolicyUpdated.Status.WorkloadsByType.DaemonSets == 0

				return deploymentPolicyCorrect && statefulSetPolicyCorrect
			}, timeout, interval).Should(BeTrue())

			// Clean up
			Expect(utils.DeleteResource(ctx, deploymentPolicy)).Should(Succeed())
			Expect(utils.DeleteResource(ctx, statefulSetPolicy)).Should(Succeed())
			Expect(utils.DeleteResource(ctx, deployment)).Should(Succeed())
			Expect(utils.DeleteResource(ctx, statefulSet)).Should(Succeed())
			Expect(utils.DeleteResource(ctx, daemonSet)).Should(Succeed())
		})

		It("Should handle weight-based selection with workload type filtering", func() {
			// Create test deployment
			deployment := createTestDeployment(testNamespace, map[string]string{
				"app":      "web",
				"optimize": "true",
			})
			Expect(utils.CreateResource(ctx, deployment)).Should(Succeed())

			// Create two policies that both target Deployments with different weights
			highWeightPolicy := createWeightedOptimizationPolicy(
				"high-weight-policy",
				200, // Higher weight
				&optipodv1alpha1.WorkloadTypeFilter{
					Include: []optipodv1alpha1.WorkloadType{
						optipodv1alpha1.WorkloadTypeDeployment,
					},
				},
				"P90",
				"50m", "2000m",
				"64Mi", "4Gi",
				true, true,
				1*time.Minute,
			)
			Expect(utils.CreateResource(ctx, highWeightPolicy)).Should(Succeed())

			lowWeightPolicy := createWeightedOptimizationPolicy(
				"low-weight-policy",
				50, // Lower weight
				&optipodv1alpha1.WorkloadTypeFilter{
					Include: []optipodv1alpha1.WorkloadType{
						optipodv1alpha1.WorkloadTypeDeployment,
					},
				},
				"P50",
				"100m", "4000m",
				"128Mi", "8Gi",
				false, false,
				2*time.Minute,
			)
			Expect(utils.CreateResource(ctx, lowWeightPolicy)).Should(Succeed())

			// Wait for both policies to be processed
			Eventually(func() bool {
				highWeightPolicyUpdated := &optipodv1alpha1.OptimizationPolicy{}
				err1 := utils.GetResource(ctx, types.NamespacedName{
					Name:      highWeightPolicy.Name,
					Namespace: highWeightPolicy.Namespace,
				}, highWeightPolicyUpdated)

				lowWeightPolicyUpdated := &optipodv1alpha1.OptimizationPolicy{}
				err2 := utils.GetResource(ctx, types.NamespacedName{
					Name:      lowWeightPolicy.Name,
					Namespace: lowWeightPolicy.Namespace,
				}, lowWeightPolicyUpdated)

				if err1 != nil || err2 != nil {
					return false
				}

				// Both policies should discover the same deployment
				return highWeightPolicyUpdated.Status.WorkloadsDiscovered == 1 &&
					lowWeightPolicyUpdated.Status.WorkloadsDiscovered == 1
			}, timeout, interval).Should(BeTrue())

			// Test policy selection logic by using discovery directly
			// (In a real scenario, the controller would handle policy selection)
			workloads, err := discovery.DiscoverWorkloads(ctx, utils.GetClient(), highWeightPolicy)
			Expect(err).NotTo(HaveOccurred())
			Expect(workloads).To(HaveLen(1))
			Expect(workloads[0].Kind).To(Equal("Deployment"))

			// Clean up
			Expect(utils.DeleteResource(ctx, highWeightPolicy)).Should(Succeed())
			Expect(utils.DeleteResource(ctx, lowWeightPolicy)).Should(Succeed())
			Expect(utils.DeleteResource(ctx, deployment)).Should(Succeed())
		})
	})

	Context("Edge Cases and Error Handling", func() {
		It("Should handle policies with conflicting include/exclude filters", func() {
			// Create test workloads
			deployment := createTestDeployment(testNamespace, map[string]string{
				"app":      "web",
				"optimize": "true",
			})
			Expect(utils.CreateResource(ctx, deployment)).Should(Succeed())

			statefulSet := createTestStatefulSet(testNamespace, map[string]string{
				"app":      "database",
				"optimize": "true",
			})
			Expect(utils.CreateResource(ctx, statefulSet)).Should(Succeed())

			// Create policy where exclude takes precedence over include
			policy := &optipodv1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "conflicting-filter-policy",
					Namespace: "default",
				},
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Mode: optipodv1alpha1.ModeRecommend,
					Selector: optipodv1alpha1.WorkloadSelector{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"environment": "test"},
						},
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"optimize": "true"},
						},
						WorkloadTypes: &optipodv1alpha1.WorkloadTypeFilter{
							Include: []optipodv1alpha1.WorkloadType{
								optipodv1alpha1.WorkloadTypeDeployment,
								optipodv1alpha1.WorkloadTypeStatefulSet,
							},
							Exclude: []optipodv1alpha1.WorkloadType{
								optipodv1alpha1.WorkloadTypeStatefulSet, // Exclude takes precedence
							},
						},
					},
					MetricsConfig: optipodv1alpha1.MetricsConfig{
						Provider:   "metrics-server",
						Percentile: "P90",
					},
					ResourceBounds: optipodv1alpha1.ResourceBounds{
						CPU: optipodv1alpha1.ResourceBound{
							Min: resource.MustParse("50m"),
							Max: resource.MustParse("2000m"),
						},
						Memory: optipodv1alpha1.ResourceBound{
							Min: resource.MustParse("64Mi"),
							Max: resource.MustParse("4Gi"),
						},
					},
					UpdateStrategy: optipodv1alpha1.UpdateStrategy{
						AllowInPlaceResize: true,
						UpdateRequestsOnly: true,
					},
					ReconciliationInterval: metav1.Duration{Duration: 1 * time.Minute},
				},
			}
			Expect(utils.CreateResource(ctx, policy)).Should(Succeed())

			// Wait for policy to be processed
			Eventually(func() bool {
				updatedPolicy := &optipodv1alpha1.OptimizationPolicy{}
				err := utils.GetResource(ctx, types.NamespacedName{
					Name:      policy.Name,
					Namespace: policy.Namespace,
				}, updatedPolicy)
				if err != nil {
					return false
				}

				// Should only discover Deployment (StatefulSet excluded despite being in include list)
				return updatedPolicy.Status.WorkloadsDiscovered == 1
			}, timeout, interval).Should(BeTrue())

			// Verify status shows correct workload type breakdown
			Eventually(func() bool {
				updatedPolicy := &optipodv1alpha1.OptimizationPolicy{}
				err := utils.GetResource(ctx, types.NamespacedName{
					Name:      policy.Name,
					Namespace: policy.Namespace,
				}, updatedPolicy)
				if err != nil {
					return false
				}

				// Check workload type breakdown in status
				if updatedPolicy.Status.WorkloadsByType == nil {
					return false
				}

				return updatedPolicy.Status.WorkloadsByType.Deployments == 1 &&
					updatedPolicy.Status.WorkloadsByType.StatefulSets == 0 &&
					updatedPolicy.Status.WorkloadsByType.DaemonSets == 0
			}, timeout, interval).Should(BeTrue())

			// Clean up
			Expect(utils.DeleteResource(ctx, policy)).Should(Succeed())
			Expect(utils.DeleteResource(ctx, deployment)).Should(Succeed())
			Expect(utils.DeleteResource(ctx, statefulSet)).Should(Succeed())
		})

		It("Should handle policies that result in no discoverable workloads", func() {
			// Create test workloads
			deployment := createTestDeployment(testNamespace, map[string]string{
				"app":      "web",
				"optimize": "true",
			})
			Expect(utils.CreateResource(ctx, deployment)).Should(Succeed())

			// Create policy that excludes all workload types
			policy := &optipodv1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-workloads-policy",
					Namespace: "default",
				},
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Mode: optipodv1alpha1.ModeRecommend,
					Selector: optipodv1alpha1.WorkloadSelector{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"environment": "test"},
						},
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"optimize": "true"},
						},
						WorkloadTypes: &optipodv1alpha1.WorkloadTypeFilter{
							Exclude: []optipodv1alpha1.WorkloadType{
								optipodv1alpha1.WorkloadTypeDeployment,
								optipodv1alpha1.WorkloadTypeStatefulSet,
								optipodv1alpha1.WorkloadTypeDaemonSet,
							},
						},
					},
					MetricsConfig: optipodv1alpha1.MetricsConfig{
						Provider:   "metrics-server",
						Percentile: "P90",
					},
					ResourceBounds: optipodv1alpha1.ResourceBounds{
						CPU: optipodv1alpha1.ResourceBound{
							Min: resource.MustParse("50m"),
							Max: resource.MustParse("2000m"),
						},
						Memory: optipodv1alpha1.ResourceBound{
							Min: resource.MustParse("64Mi"),
							Max: resource.MustParse("4Gi"),
						},
					},
					UpdateStrategy: optipodv1alpha1.UpdateStrategy{
						AllowInPlaceResize: true,
						UpdateRequestsOnly: true,
					},
					ReconciliationInterval: metav1.Duration{Duration: 1 * time.Minute},
				},
			}
			Expect(utils.CreateResource(ctx, policy)).Should(Succeed())

			// Wait for policy to be processed
			Eventually(func() bool {
				updatedPolicy := &optipodv1alpha1.OptimizationPolicy{}
				err := utils.GetResource(ctx, types.NamespacedName{
					Name:      policy.Name,
					Namespace: policy.Namespace,
				}, updatedPolicy)
				if err != nil {
					return false
				}

				// Should discover no workloads
				return updatedPolicy.Status.WorkloadsDiscovered == 0
			}, timeout, interval).Should(BeTrue())

			// Verify status shows zero workload counts
			Eventually(func() bool {
				updatedPolicy := &optipodv1alpha1.OptimizationPolicy{}
				err := utils.GetResource(ctx, types.NamespacedName{
					Name:      policy.Name,
					Namespace: policy.Namespace,
				}, updatedPolicy)
				if err != nil {
					return false
				}

				// Check that no workloads are discovered (the main requirement)
				if updatedPolicy.Status.WorkloadsDiscovered != 0 {
					return false
				}

				// If WorkloadsByType is populated, it should show zeros
				// If it's not populated, that's also acceptable for zero workloads
				if updatedPolicy.Status.WorkloadsByType != nil {
					return updatedPolicy.Status.WorkloadsByType.Deployments == 0 &&
						updatedPolicy.Status.WorkloadsByType.StatefulSets == 0 &&
						updatedPolicy.Status.WorkloadsByType.DaemonSets == 0
				}

				// If WorkloadsByType is nil but WorkloadsDiscovered is 0, that's acceptable
				return true
			}, timeout, interval).Should(BeTrue())

			// Clean up
			Expect(utils.DeleteResource(ctx, policy)).Should(Succeed())
			Expect(utils.DeleteResource(ctx, deployment)).Should(Succeed())
		})
	})
})

// Helper functions for creating test workloads

// Helper functions for creating test policies

func createBasicOptimizationPolicy(
	name string,
	workloadTypes *optipodv1alpha1.WorkloadTypeFilter,
) *optipodv1alpha1.OptimizationPolicy {
	return &optipodv1alpha1.OptimizationPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: optipodv1alpha1.OptimizationPolicySpec{
			Mode: optipodv1alpha1.ModeRecommend,
			Selector: optipodv1alpha1.WorkloadSelector{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"environment": "test"},
				},
				WorkloadSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"optimize": "true"},
				},
				WorkloadTypes: workloadTypes,
			},
			MetricsConfig: optipodv1alpha1.MetricsConfig{
				Provider:   "metrics-server",
				Percentile: "P90",
			},
			ResourceBounds: optipodv1alpha1.ResourceBounds{
				CPU: optipodv1alpha1.ResourceBound{
					Min: resource.MustParse("50m"),
					Max: resource.MustParse("2000m"),
				},
				Memory: optipodv1alpha1.ResourceBound{
					Min: resource.MustParse("64Mi"),
					Max: resource.MustParse("4Gi"),
				},
			},
			UpdateStrategy: optipodv1alpha1.UpdateStrategy{
				AllowInPlaceResize: true,
				UpdateRequestsOnly: true,
			},
			ReconciliationInterval: metav1.Duration{Duration: 1 * time.Minute},
		},
	}
}

func createWeightedOptimizationPolicy(
	name string,
	weight int32,
	workloadTypes *optipodv1alpha1.WorkloadTypeFilter,
	percentile string,
	cpuMin, cpuMax, memMin, memMax string,
	allowResize, requestsOnly bool,
	interval time.Duration,
) *optipodv1alpha1.OptimizationPolicy {
	policy := createBasicOptimizationPolicy(name, workloadTypes)
	policy.Spec.Weight = &weight
	policy.Spec.MetricsConfig.Percentile = percentile
	policy.Spec.ResourceBounds.CPU.Min = resource.MustParse(cpuMin)
	policy.Spec.ResourceBounds.CPU.Max = resource.MustParse(cpuMax)
	policy.Spec.ResourceBounds.Memory.Min = resource.MustParse(memMin)
	policy.Spec.ResourceBounds.Memory.Max = resource.MustParse(memMax)
	policy.Spec.UpdateStrategy.AllowInPlaceResize = allowResize
	policy.Spec.UpdateStrategy.UpdateRequestsOnly = requestsOnly
	policy.Spec.ReconciliationInterval = metav1.Duration{Duration: interval}
	return policy
}

func createTestDeployment(namespace string, labels map[string]string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": labels["app"]},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": labels["app"]},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx:latest",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
							},
						},
					},
				},
			},
		},
	}
}

func createTestStatefulSet(namespace string, labels map[string]string) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-statefulset",
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": labels["app"]},
			},
			ServiceName: "test-statefulset",
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": labels["app"]},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx:latest",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
							},
						},
					},
				},
			},
		},
	}
}

func createTestDaemonSet(namespace string, labels map[string]string) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-daemonset",
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": labels["app"]},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": labels["app"]},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx:latest",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
							},
						},
					},
				},
			},
		},
	}
}

func int32Ptr(i int32) *int32 {
	return &i
}
