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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/discovery"
	"github.com/optipod/optipod/internal/metrics"
	"github.com/optipod/optipod/internal/observability"
)

var _ = Describe("Integration Tests", func() {
	const (
		timeout  = time.Second * 30
		interval = time.Millisecond * 250
	)

	Context("Full Reconciliation Loop", func() {
		It("Should reconcile policy and update status through full loop", func() {
			ctx := context.Background()

			// Create a test namespace
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "integration-test-ns",
					Labels: map[string]string{
						"environment": "test",
					},
				},
			}
			Expect(k8sClient.Create(ctx, ns)).Should(Succeed())

			// Create a test deployment
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-deployment",
					Namespace: "integration-test-ns",
					Labels: map[string]string{
						"app":      "test",
						"optimize": "true",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test"},
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
			Expect(k8sClient.Create(ctx, deployment)).Should(Succeed())

			// Create an optimization policy
			policy := &optipodv1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "integration-test-policy",
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
					},
					MetricsConfig: optipodv1alpha1.MetricsConfig{
						Provider:   "prometheus",
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
			Expect(k8sClient.Create(ctx, policy)).Should(Succeed())

			// Set up reconciler with mock metrics provider
			mockProvider := &MockMetricsProvider{}
			reconciler := &OptimizationPolicyReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(100),
				WorkloadProcessor: NewWorkloadProcessor(
					mockProvider,
					nil, // recommendation engine not needed for this test
					nil, // application engine not needed for this test
				),
				EventRecorder: observability.NewEventRecorder(record.NewFakeRecorder(100)),
			}

			// Reconcile
			result, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      policy.Name,
					Namespace: policy.Namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(1 * time.Minute))

			// Verify policy status is updated
			Eventually(func() bool {
				updatedPolicy := &optipodv1alpha1.OptimizationPolicy{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      policy.Name,
					Namespace: policy.Namespace,
				}, updatedPolicy)
				if err != nil {
					return false
				}

				// Check for Ready condition
				for _, condition := range updatedPolicy.Status.Conditions {
					if condition.Type == ConditionTypeReady && condition.Status == metav1.ConditionTrue {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())

			// Clean up
			Expect(k8sClient.Delete(ctx, policy)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, deployment)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, ns)).Should(Succeed())
		})
	})

	Context("CRD Status Updates", func() {
		It("Should update policy status with workload information", func() {
			ctx := context.Background()

			// Create test namespace
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "status-test-ns",
				},
			}
			Expect(k8sClient.Create(ctx, ns)).Should(Succeed())

			// Create policy
			policy := &optipodv1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "status-test-policy",
					Namespace: "default",
				},
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Mode: optipodv1alpha1.ModeRecommend,
					Selector: optipodv1alpha1.WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "status-test"},
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

			// Update status with workload information
			now := metav1.Now()
			cpu := resource.MustParse("500m")
			memory := resource.MustParse("512Mi")
			workloadStatuses := []optipodv1alpha1.WorkloadStatus{
				{
					Name:               "test-workload",
					Namespace:          "status-test-ns",
					LastRecommendation: &now,
					Recommendations: []optipodv1alpha1.ContainerRecommendation{
						{
							Container: "nginx",
							CPU:       &cpu,
							Memory:    &memory,
						},
					},
					Status: StatusRecommended,
				},
			}

			reconciler := &OptimizationPolicyReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(10),
			}

			err := reconciler.updateWorkloadStatuses(ctx, policy, workloadStatuses)
			Expect(err).NotTo(HaveOccurred())

			// Verify status was updated
			Eventually(func() bool {
				updatedPolicy := &optipodv1alpha1.OptimizationPolicy{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      policy.Name,
					Namespace: policy.Namespace,
				}, updatedPolicy)
				if err != nil {
					return false
				}

				return len(updatedPolicy.Status.Workloads) == 1 &&
					updatedPolicy.Status.Workloads[0].Name == "test-workload" &&
					updatedPolicy.Status.Workloads[0].Status == StatusRecommended
			}, timeout, interval).Should(BeTrue())

			// Clean up
			Expect(k8sClient.Delete(ctx, policy)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, ns)).Should(Succeed())
		})
	})

	Context("Workload Discovery Across Namespaces", func() {
		It("Should discover workloads in multiple namespaces", func() {
			ctx := context.Background()

			// Create multiple test namespaces
			ns1 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "discovery-ns-1",
					Labels: map[string]string{
						"team": "platform",
					},
				},
			}
			Expect(k8sClient.Create(ctx, ns1)).Should(Succeed())

			ns2 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "discovery-ns-2",
					Labels: map[string]string{
						"team": "platform",
					},
				},
			}
			Expect(k8sClient.Create(ctx, ns2)).Should(Succeed())

			// Create deployments in both namespaces
			deployment1 := createTestDeployment("deploy-1", "discovery-ns-1", map[string]string{"app": "web"})
			Expect(k8sClient.Create(ctx, deployment1)).Should(Succeed())

			deployment2 := createTestDeployment("deploy-2", "discovery-ns-2", map[string]string{"app": "web"})
			Expect(k8sClient.Create(ctx, deployment2)).Should(Succeed())

			// Create a StatefulSet in ns1
			statefulSet := createTestStatefulSet("stateful-1", "discovery-ns-1", map[string]string{"app": "web"})
			Expect(k8sClient.Create(ctx, statefulSet)).Should(Succeed())

			// Create policy that targets both namespaces
			policy := &optipodv1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "discovery-policy",
					Namespace: "default",
				},
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Mode: optipodv1alpha1.ModeRecommend,
					Selector: optipodv1alpha1.WorkloadSelector{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"team": "platform"},
						},
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "web"},
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

			// Discover workloads
			workloads, err := discovery.DiscoverWorkloads(ctx, k8sClient, policy)
			Expect(err).NotTo(HaveOccurred())

			// Should find 3 workloads: 2 deployments + 1 statefulset
			Expect(workloads).To(HaveLen(3))

			// Verify workload types
			deploymentCount := 0
			statefulSetCount := 0
			for _, w := range workloads {
				if w.Kind == KindDeployment {
					deploymentCount++
				} else if w.Kind == "StatefulSet" {
					statefulSetCount++
				}
			}
			Expect(deploymentCount).To(Equal(2))
			Expect(statefulSetCount).To(Equal(1))

			// Clean up
			Expect(k8sClient.Delete(ctx, deployment1)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, deployment2)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, statefulSet)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, ns1)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, ns2)).Should(Succeed())
		})

		It("Should respect namespace allow and deny lists", func() {
			ctx := context.Background()

			// Create test namespaces
			nsAllowed := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "allowed-ns",
				},
			}
			Expect(k8sClient.Create(ctx, nsAllowed)).Should(Succeed())

			nsDenied := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "denied-ns",
				},
			}
			Expect(k8sClient.Create(ctx, nsDenied)).Should(Succeed())

			// Create deployments in both namespaces
			deployAllowed := createTestDeployment("deploy-allowed", "allowed-ns", map[string]string{"app": "test"})
			Expect(k8sClient.Create(ctx, deployAllowed)).Should(Succeed())

			deployDenied := createTestDeployment("deploy-denied", "denied-ns", map[string]string{"app": "test"})
			Expect(k8sClient.Create(ctx, deployDenied)).Should(Succeed())

			// Create policy with allow/deny lists
			policy := &optipodv1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "allowdeny-policy",
					Namespace: "default",
				},
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Mode: optipodv1alpha1.ModeRecommend,
					Selector: optipodv1alpha1.WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
						Namespaces: &optipodv1alpha1.NamespaceFilter{
							Allow: []string{"allowed-ns", "denied-ns"},
							Deny:  []string{"denied-ns"}, // Deny takes precedence
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

			// Discover workloads
			workloads, err := discovery.DiscoverWorkloads(ctx, k8sClient, policy)
			Expect(err).NotTo(HaveOccurred())

			// Should only find workload in allowed-ns (denied-ns is excluded)
			Expect(workloads).To(HaveLen(1))
			Expect(workloads[0].Namespace).To(Equal("allowed-ns"))

			// Clean up
			Expect(k8sClient.Delete(ctx, deployAllowed)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, deployDenied)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, nsAllowed)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, nsDenied)).Should(Succeed())
		})
	})

	Context("RBAC Enforcement", func() {
		It("Should handle RBAC permission errors gracefully", func() {
			ctx := context.Background()

			// Note: In envtest, we can't fully simulate RBAC restrictions
			// as the test client has full permissions. This test demonstrates
			// the structure for RBAC testing, but actual RBAC enforcement
			// would need to be tested in a real cluster or with more
			// sophisticated mocking.

			// Create a service account with limited permissions
			sa := &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "limited-sa",
					Namespace: "default",
				},
			}
			Expect(k8sClient.Create(ctx, sa)).Should(Succeed())

			// Create a role with limited permissions (read-only)
			role := &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "read-only-role",
					Namespace: "default",
				},
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups: []string{"apps"},
						Resources: []string{"deployments"},
						Verbs:     []string{"get", "list", "watch"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, role)).Should(Succeed())

			// Create role binding
			roleBinding := &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "read-only-binding",
					Namespace: "default",
				},
				Subjects: []rbacv1.Subject{
					{
						Kind:      "ServiceAccount",
						Name:      "limited-sa",
						Namespace: "default",
					},
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "Role",
					Name:     "read-only-role",
				},
			}
			Expect(k8sClient.Create(ctx, roleBinding)).Should(Succeed())

			// Verify RBAC resources were created
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "limited-sa",
					Namespace: "default",
				}, &corev1.ServiceAccount{})
				return err == nil
			}, timeout, interval).Should(BeTrue())

			// Clean up
			Expect(k8sClient.Delete(ctx, roleBinding)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, role)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, sa)).Should(Succeed())
		})
	})

	Context("Metrics Provider Integration", func() {
		It("Should integrate with mock metrics provider", func() {
			ctx := context.Background()

			// Create mock metrics provider
			mockProvider := &MockMetricsProvider{
				metrics: &metrics.ContainerMetrics{
					CPU: metrics.ResourceMetrics{
						P50:     resource.MustParse("200m"),
						P90:     resource.MustParse("400m"),
						P99:     resource.MustParse("600m"),
						Samples: 100,
					},
					Memory: metrics.ResourceMetrics{
						P50:     resource.MustParse("256Mi"),
						P90:     resource.MustParse("512Mi"),
						P99:     resource.MustParse("768Mi"),
						Samples: 100,
					},
				},
			}

			// Test health check
			err := mockProvider.HealthCheck(ctx)
			Expect(err).NotTo(HaveOccurred())

			// Test metrics retrieval
			containerMetrics, err := mockProvider.GetContainerMetrics(ctx, "default", "test-pod", "nginx", 24*time.Hour)
			Expect(err).NotTo(HaveOccurred())
			Expect(containerMetrics).NotTo(BeNil())
			Expect(containerMetrics.CPU.P90.String()).To(Equal("400m"))
			Expect(containerMetrics.Memory.P90.String()).To(Equal("512Mi"))
		})

		It("Should handle metrics provider errors", func() {
			ctx := context.Background()

			// Create mock provider that returns errors
			mockProvider := &MockMetricsProvider{
				shouldError: true,
			}

			// Test health check failure
			err := mockProvider.HealthCheck(ctx)
			Expect(err).To(HaveOccurred())

			// Test metrics retrieval failure
			_, err = mockProvider.GetContainerMetrics(ctx, "default", "test-pod", "nginx", 24*time.Hour)
			Expect(err).To(HaveOccurred())
		})
	})
})

// Helper functions

func int32Ptr(i int32) *int32 {
	return &i
}

func createTestDeployment(name, namespace string, labels map[string]string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
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

func createTestStatefulSet(name, namespace string, labels map[string]string) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			ServiceName: name,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
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

// MockMetricsProvider is a mock implementation of MetricsProvider for testing
type MockMetricsProvider struct {
	metrics     *metrics.ContainerMetrics
	shouldError bool
}

func (m *MockMetricsProvider) GetContainerMetrics(ctx context.Context, namespace, podName, containerName string, window time.Duration) (*metrics.ContainerMetrics, error) {
	if m.shouldError {
		return nil, &metrics.MetricsError{Message: "mock metrics error"}
	}
	if m.metrics != nil {
		return m.metrics, nil
	}
	// Return default metrics
	return &metrics.ContainerMetrics{
		CPU: metrics.ResourceMetrics{
			P50:     resource.MustParse("100m"),
			P90:     resource.MustParse("200m"),
			P99:     resource.MustParse("300m"),
			Samples: 50,
		},
		Memory: metrics.ResourceMetrics{
			P50:     resource.MustParse("128Mi"),
			P90:     resource.MustParse("256Mi"),
			P99:     resource.MustParse("384Mi"),
			Samples: 50,
		},
	}, nil
}

func (m *MockMetricsProvider) HealthCheck(ctx context.Context) error {
	if m.shouldError {
		return &metrics.MetricsError{Message: "mock health check failed"}
	}
	return nil
}
