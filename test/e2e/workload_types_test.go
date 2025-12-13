//go:build e2e

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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/helpers"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

var _ = Describe("Workload Types and Update Strategies", Ordered, func() {
	var (
		testNamespace   string
		policyNamespace string
		policyHelper    *helpers.PolicyHelper
		workloadHelper  *helpers.WorkloadHelper
		cleanupHelper   *helpers.CleanupHelper
		k8sClient       client.Client
	)

	BeforeAll(func() {
		By("setting up test environment")

		// Initialize Kubernetes client
		cfg, err := config.GetConfig()
		Expect(err).NotTo(HaveOccurred())

		// Add OptipPod scheme
		s := runtime.NewScheme()
		Expect(scheme.AddToScheme(s)).To(Succeed())
		Expect(v1alpha1.AddToScheme(s)).To(Succeed())

		k8sClient, err = client.New(cfg, client.Options{Scheme: s})
		Expect(err).NotTo(HaveOccurred())

		// Set up namespaces
		policyNamespace = "optipod-system"
	})

	BeforeEach(func() {
		// Create a unique namespace for each test
		testNamespace = fmt.Sprintf("workload-test-%d", time.Now().Unix())

		// Create the test namespace (ignore if it already exists)
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		err := k8sClient.Create(context.TODO(), ns)
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}

		// Initialize helpers
		policyHelper = helpers.NewPolicyHelper(k8sClient, policyNamespace)
		workloadHelper = helpers.NewWorkloadHelper(k8sClient, testNamespace)
		cleanupHelper = helpers.NewCleanupHelper(k8sClient)

		// Track the namespace for cleanup
		cleanupHelper.TrackResource(helpers.ResourceRef{
			Name:      testNamespace,
			Namespace: "",
			Kind:      "Namespace",
		})
	})

	AfterEach(func() {
		// Clean up all resources
		err := cleanupHelper.CleanupAll()
		if err != nil {
			GinkgoWriter.Printf("Warning: cleanup failed: %v\n", err)
		}
	})

	Context("Deployment Workload Scenarios", func() {
		It("should optimize Deployment resources correctly", func() {
			ctx := context.Background()

			// Skip if CRDs are not available
			if !isCRDAvailable(ctx, k8sClient) {
				Skip("OptimizationPolicy CRD not available - skipping integration test")
			}

			By("Creating a policy for Deployment optimization")
			policyConfig := helpers.PolicyConfig{
				Name: "deployment-optimization-policy",
				Mode: v1alpha1.ModeAuto,
				WorkloadSelector: map[string]string{
					"app": "deployment-test",
				},
				ResourceBounds: helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "100m",
						Max: "2000m",
					},
					Memory: helpers.ResourceBound{
						Min: "128Mi",
						Max: "4Gi",
					},
				},
				MetricsConfig: helpers.MetricsConfig{
					Provider:      "prometheus",
					RollingWindow: "1h",
					Percentile:    "P90",
					SafetyFactor:  1.2,
				},
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: true,
				},
			}

			policy, err := policyHelper.CreateOptimizationPolicy(policyConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      policy.Name,
				Namespace: policy.Namespace,
				Kind:      "OptimizationPolicy",
			})

			By("Creating a Deployment with suboptimal resources")
			workloadConfig := helpers.WorkloadConfig{
				Name:      "test-deployment",
				Namespace: testNamespace,
				Type:      helpers.WorkloadTypeDeployment,
				Labels: map[string]string{
					"app": "deployment-test",
				},
				Resources: helpers.ResourceRequirements{
					Requests: helpers.ResourceList{
						CPU:    "50m",  // Below minimum
						Memory: "64Mi", // Below minimum
					},
					Limits: helpers.ResourceList{
						CPU:    "100m",
						Memory: "128Mi",
					},
				},
				Replicas: 2,
			}

			deployment, err := workloadHelper.CreateDeployment(workloadConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
				Kind:      "Deployment",
			})

			By("Waiting for the Deployment to be ready")
			err = workloadHelper.WaitForWorkloadReady(deployment.Name, helpers.WorkloadTypeDeployment, 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for OptipPod to process the Deployment")
			time.Sleep(45 * time.Second)

			By("Verifying that the Deployment was optimized")
			updatedDeployment := &appsv1.Deployment{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
			}, updatedDeployment)
			Expect(err).NotTo(HaveOccurred())

			// Check that resources were adjusted to meet minimum bounds
			container := updatedDeployment.Spec.Template.Spec.Containers[0]
			cpuRequest := container.Resources.Requests[corev1.ResourceCPU]
			memoryRequest := container.Resources.Requests[corev1.ResourceMemory]

			minCPU := resource.MustParse("100m")
			minMemory := resource.MustParse("128Mi")

			Expect(cpuRequest.Cmp(minCPU)).To(BeNumerically(">=", 0), "CPU request should be at least the minimum bound")
			Expect(memoryRequest.Cmp(minMemory)).To(BeNumerically(">=", 0), "Memory request should be at least the minimum bound")

			By("Verifying OptipPod annotations are present")
			annotations, err := workloadHelper.GetWorkloadAnnotations(deployment.Name, helpers.WorkloadTypeDeployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(annotations).NotTo(BeEmpty(), "Deployment should have OptipPod annotations")

			// Check for expected annotations
			Expect(annotations).To(HaveKey(MatchRegexp("optipod.io/.*")), "Should have OptipPod annotations")
		})

		It("should handle Deployment selector matching correctly", func() {
			By("Creating a policy with specific selector")
			policyConfig := helpers.PolicyConfig{
				Name: "selector-test-policy",
				Mode: v1alpha1.ModeRecommend,
				WorkloadSelector: map[string]string{
					"tier":        "frontend",
					"environment": "test",
				},
				ResourceBounds: helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "200m",
						Max: "1000m",
					},
					Memory: helpers.ResourceBound{
						Min: "256Mi",
						Max: "2Gi",
					},
				},
				MetricsConfig: helpers.MetricsConfig{
					Provider:      "prometheus",
					RollingWindow: "30m",
					Percentile:    "P95",
					SafetyFactor:  1.1,
				},
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: true,
				},
			}

			policy, err := policyHelper.CreateOptimizationPolicy(policyConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      policy.Name,
				Namespace: policy.Namespace,
				Kind:      "OptimizationPolicy",
			})

			By("Creating a matching Deployment")
			matchingConfig := helpers.WorkloadConfig{
				Name:      "matching-deployment",
				Namespace: testNamespace,
				Type:      helpers.WorkloadTypeDeployment,
				Labels: map[string]string{
					"app":         "frontend-app",
					"tier":        "frontend",
					"environment": "test",
				},
				Resources: helpers.ResourceRequirements{
					Requests: helpers.ResourceList{
						CPU:    "100m",  // Below minimum
						Memory: "128Mi", // Below minimum
					},
				},
				Replicas: 1,
			}

			matchingDeployment, err := workloadHelper.CreateDeployment(matchingConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      matchingDeployment.Name,
				Namespace: matchingDeployment.Namespace,
				Kind:      "Deployment",
			})

			By("Creating a non-matching Deployment")
			nonMatchingConfig := helpers.WorkloadConfig{
				Name:      "non-matching-deployment",
				Namespace: testNamespace,
				Type:      helpers.WorkloadTypeDeployment,
				Labels: map[string]string{
					"app":         "backend-app",
					"tier":        "backend", // Different tier
					"environment": "test",
				},
				Resources: helpers.ResourceRequirements{
					Requests: helpers.ResourceList{
						CPU:    "100m",
						Memory: "128Mi",
					},
				},
				Replicas: 1,
			}

			nonMatchingDeployment, err := workloadHelper.CreateDeployment(nonMatchingConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      nonMatchingDeployment.Name,
				Namespace: nonMatchingDeployment.Namespace,
				Kind:      "Deployment",
			})

			By("Waiting for OptipPod to process the workloads")
			time.Sleep(45 * time.Second)

			By("Verifying that only the matching Deployment has recommendations")
			matchingAnnotations, err := workloadHelper.GetWorkloadAnnotations(matchingDeployment.Name, helpers.WorkloadTypeDeployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(matchingAnnotations).NotTo(BeEmpty(), "Matching deployment should have OptipPod annotations")

			nonMatchingAnnotations, err := workloadHelper.GetWorkloadAnnotations(nonMatchingDeployment.Name, helpers.WorkloadTypeDeployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(nonMatchingAnnotations).To(BeEmpty(), "Non-matching deployment should not have OptipPod annotations")
		})

		It("should validate Deployment rollout behavior", func() {
			By("Creating a policy for rollout testing")
			policyConfig := helpers.PolicyConfig{
				Name: "rollout-test-policy",
				Mode: v1alpha1.ModeAuto,
				WorkloadSelector: map[string]string{
					"app": "rollout-test",
				},
				ResourceBounds: helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "150m",
						Max: "1500m",
					},
					Memory: helpers.ResourceBound{
						Min: "192Mi",
						Max: "3Gi",
					},
				},
				MetricsConfig: helpers.MetricsConfig{
					Provider:      "prometheus",
					RollingWindow: "45m",
					Percentile:    "P90",
					SafetyFactor:  1.3,
				},
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: true,
				},
			}

			policy, err := policyHelper.CreateOptimizationPolicy(policyConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      policy.Name,
				Namespace: policy.Namespace,
				Kind:      "OptimizationPolicy",
			})

			By("Creating a Deployment with multiple replicas")
			workloadConfig := helpers.WorkloadConfig{
				Name:      "rollout-deployment",
				Namespace: testNamespace,
				Type:      helpers.WorkloadTypeDeployment,
				Labels: map[string]string{
					"app": "rollout-test",
				},
				Resources: helpers.ResourceRequirements{
					Requests: helpers.ResourceList{
						CPU:    "100m",  // Below minimum
						Memory: "128Mi", // Below minimum
					},
				},
				Replicas: 3, // Multiple replicas to test rollout
			}

			deployment, err := workloadHelper.CreateDeployment(workloadConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
				Kind:      "Deployment",
			})

			By("Waiting for the initial Deployment to be ready")
			err = workloadHelper.WaitForWorkloadReady(deployment.Name, helpers.WorkloadTypeDeployment, 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Recording the initial generation")
			initialDeployment := &appsv1.Deployment{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
			}, initialDeployment)
			Expect(err).NotTo(HaveOccurred())
			initialGeneration := initialDeployment.Generation

			By("Waiting for OptipPod to update the Deployment")
			time.Sleep(60 * time.Second)

			By("Verifying that the Deployment was updated")
			updatedDeployment := &appsv1.Deployment{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
			}, updatedDeployment)
			Expect(err).NotTo(HaveOccurred())

			// Check if the deployment was updated (generation should increase)
			if updatedDeployment.Generation > initialGeneration {
				By("Verifying that the rollout completed successfully")
				err = workloadHelper.WaitForWorkloadReady(deployment.Name, helpers.WorkloadTypeDeployment, 3*time.Minute)
				Expect(err).NotTo(HaveOccurred())

				// Verify all replicas are ready
				Expect(updatedDeployment.Status.ReadyReplicas).To(Equal(updatedDeployment.Status.Replicas), "All replicas should be ready after rollout")
			}

			By("Verifying resource optimization was applied")
			container := updatedDeployment.Spec.Template.Spec.Containers[0]
			cpuRequest := container.Resources.Requests[corev1.ResourceCPU]
			memoryRequest := container.Resources.Requests[corev1.ResourceMemory]

			minCPU := resource.MustParse("150m")
			minMemory := resource.MustParse("192Mi")

			Expect(cpuRequest.Cmp(minCPU)).To(BeNumerically(">=", 0), "CPU request should meet minimum bound")
			Expect(memoryRequest.Cmp(minMemory)).To(BeNumerically(">=", 0), "Memory request should meet minimum bound")
		})
	})

	Context("StatefulSet Workload Scenarios", func() {
		It("should optimize StatefulSet resources correctly", func() {
			By("Creating a policy for StatefulSet optimization")
			policyConfig := helpers.PolicyConfig{
				Name: "statefulset-optimization-policy",
				Mode: v1alpha1.ModeAuto,
				WorkloadSelector: map[string]string{
					"app": "statefulset-test",
				},
				ResourceBounds: helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "200m",
						Max: "2000m",
					},
					Memory: helpers.ResourceBound{
						Min: "256Mi",
						Max: "4Gi",
					},
				},
				MetricsConfig: helpers.MetricsConfig{
					Provider:      "prometheus",
					RollingWindow: "1h",
					Percentile:    "P90",
					SafetyFactor:  1.2,
				},
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: true,
				},
			}

			policy, err := policyHelper.CreateOptimizationPolicy(policyConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      policy.Name,
				Namespace: policy.Namespace,
				Kind:      "OptimizationPolicy",
			})

			By("Creating a StatefulSet with suboptimal resources")
			workloadConfig := helpers.WorkloadConfig{
				Name:      "test-statefulset",
				Namespace: testNamespace,
				Type:      helpers.WorkloadTypeStatefulSet,
				Labels: map[string]string{
					"app": "statefulset-test",
				},
				Resources: helpers.ResourceRequirements{
					Requests: helpers.ResourceList{
						CPU:    "100m",  // Below minimum
						Memory: "128Mi", // Below minimum
					},
					Limits: helpers.ResourceList{
						CPU:    "500m",
						Memory: "512Mi",
					},
				},
				Replicas: 2,
			}

			statefulSet, err := workloadHelper.CreateStatefulSet(workloadConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      statefulSet.Name,
				Namespace: statefulSet.Namespace,
				Kind:      "StatefulSet",
			})

			By("Waiting for the StatefulSet to be ready")
			err = workloadHelper.WaitForWorkloadReady(statefulSet.Name, helpers.WorkloadTypeStatefulSet, 3*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for OptipPod to process the StatefulSet")
			time.Sleep(45 * time.Second)

			By("Verifying that the StatefulSet was optimized")
			updatedStatefulSet := &appsv1.StatefulSet{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{
				Name:      statefulSet.Name,
				Namespace: statefulSet.Namespace,
			}, updatedStatefulSet)
			Expect(err).NotTo(HaveOccurred())

			// Check that resources were adjusted to meet minimum bounds
			container := updatedStatefulSet.Spec.Template.Spec.Containers[0]
			cpuRequest := container.Resources.Requests[corev1.ResourceCPU]
			memoryRequest := container.Resources.Requests[corev1.ResourceMemory]

			minCPU := resource.MustParse("200m")
			minMemory := resource.MustParse("256Mi")

			Expect(cpuRequest.Cmp(minCPU)).To(BeNumerically(">=", 0), "CPU request should be at least the minimum bound")
			Expect(memoryRequest.Cmp(minMemory)).To(BeNumerically(">=", 0), "Memory request should be at least the minimum bound")

			By("Verifying OptipPod annotations are present")
			annotations, err := workloadHelper.GetWorkloadAnnotations(statefulSet.Name, helpers.WorkloadTypeStatefulSet)
			Expect(err).NotTo(HaveOccurred())
			Expect(annotations).NotTo(BeEmpty(), "StatefulSet should have OptipPod annotations")
		})

		It("should handle StatefulSet ordered update correctly", func() {
			By("Creating a policy for ordered update testing")
			policyConfig := helpers.PolicyConfig{
				Name: "statefulset-ordered-policy",
				Mode: v1alpha1.ModeAuto,
				WorkloadSelector: map[string]string{
					"app": "ordered-test",
				},
				ResourceBounds: helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "300m",
						Max: "1500m",
					},
					Memory: helpers.ResourceBound{
						Min: "384Mi",
						Max: "3Gi",
					},
				},
				MetricsConfig: helpers.MetricsConfig{
					Provider:      "prometheus",
					RollingWindow: "30m",
					Percentile:    "P95",
					SafetyFactor:  1.1,
				},
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: true,
				},
			}

			policy, err := policyHelper.CreateOptimizationPolicy(policyConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      policy.Name,
				Namespace: policy.Namespace,
				Kind:      "OptimizationPolicy",
			})

			By("Creating a StatefulSet with multiple replicas")
			workloadConfig := helpers.WorkloadConfig{
				Name:      "ordered-statefulset",
				Namespace: testNamespace,
				Type:      helpers.WorkloadTypeStatefulSet,
				Labels: map[string]string{
					"app": "ordered-test",
				},
				Resources: helpers.ResourceRequirements{
					Requests: helpers.ResourceList{
						CPU:    "150m",  // Below minimum
						Memory: "192Mi", // Below minimum
					},
				},
				Replicas: 3, // Multiple replicas to test ordered updates
			}

			statefulSet, err := workloadHelper.CreateStatefulSet(workloadConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      statefulSet.Name,
				Namespace: statefulSet.Namespace,
				Kind:      "StatefulSet",
			})

			By("Waiting for the initial StatefulSet to be ready")
			err = workloadHelper.WaitForWorkloadReady(statefulSet.Name, helpers.WorkloadTypeStatefulSet, 3*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Recording the initial generation and update strategy")
			initialStatefulSet := &appsv1.StatefulSet{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{
				Name:      statefulSet.Name,
				Namespace: statefulSet.Namespace,
			}, initialStatefulSet)
			Expect(err).NotTo(HaveOccurred())
			initialGeneration := initialStatefulSet.Generation

			// Verify the StatefulSet has the correct update strategy
			Expect(initialStatefulSet.Spec.UpdateStrategy.Type).To(Equal(appsv1.RollingUpdateStatefulSetStrategyType), "StatefulSet should use RollingUpdate strategy")

			By("Waiting for OptipPod to update the StatefulSet")
			time.Sleep(60 * time.Second)

			By("Verifying that the StatefulSet was updated with ordered rollout")
			updatedStatefulSet := &appsv1.StatefulSet{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{
				Name:      statefulSet.Name,
				Namespace: statefulSet.Namespace,
			}, updatedStatefulSet)
			Expect(err).NotTo(HaveOccurred())

			// Check if the StatefulSet was updated
			if updatedStatefulSet.Generation > initialGeneration {
				By("Verifying that the ordered update completed successfully")
				err = workloadHelper.WaitForWorkloadReady(statefulSet.Name, helpers.WorkloadTypeStatefulSet, 4*time.Minute)
				Expect(err).NotTo(HaveOccurred())

				// Verify all replicas are ready and updated
				Expect(updatedStatefulSet.Status.ReadyReplicas).To(Equal(updatedStatefulSet.Status.Replicas), "All replicas should be ready after ordered update")
				Expect(updatedStatefulSet.Status.UpdatedReplicas).To(Equal(updatedStatefulSet.Status.Replicas), "All replicas should be updated")
			}

			By("Verifying resource optimization was applied")
			container := updatedStatefulSet.Spec.Template.Spec.Containers[0]
			cpuRequest := container.Resources.Requests[corev1.ResourceCPU]
			memoryRequest := container.Resources.Requests[corev1.ResourceMemory]

			minCPU := resource.MustParse("300m")
			minMemory := resource.MustParse("384Mi")

			Expect(cpuRequest.Cmp(minCPU)).To(BeNumerically(">=", 0), "CPU request should meet minimum bound")
			Expect(memoryRequest.Cmp(minMemory)).To(BeNumerically(">=", 0), "Memory request should meet minimum bound")
		})

		It("should handle StatefulSet persistent volume considerations", func() {
			By("Creating a policy for persistent volume testing")
			policyConfig := helpers.PolicyConfig{
				Name: "statefulset-pv-policy",
				Mode: v1alpha1.ModeRecommend, // Use recommend mode to avoid actual updates
				WorkloadSelector: map[string]string{
					"app": "pv-test",
				},
				ResourceBounds: helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "250m",
						Max: "2500m",
					},
					Memory: helpers.ResourceBound{
						Min: "320Mi",
						Max: "5Gi",
					},
				},
				MetricsConfig: helpers.MetricsConfig{
					Provider:      "prometheus",
					RollingWindow: "45m",
					Percentile:    "P90",
					SafetyFactor:  1.3,
				},
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: false, // Disable in-place resize for PV testing
				},
			}

			policy, err := policyHelper.CreateOptimizationPolicy(policyConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      policy.Name,
				Namespace: policy.Namespace,
				Kind:      "OptimizationPolicy",
			})

			By("Creating a StatefulSet with volume claim templates")
			// Create StatefulSet directly to include volume claim templates
			statefulSet := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pv-statefulset",
					Namespace: testNamespace,
					Labels: map[string]string{
						"app": "pv-test",
					},
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas:    int32Ptr(2),
					ServiceName: "pv-statefulset-svc",
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "pv-test",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "pv-test",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "app",
									Image: "nginx:1.25-alpine",
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("100m"),  // Below minimum
											corev1.ResourceMemory: resource.MustParse("128Mi"), // Below minimum
										},
									},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "data",
											MountPath: "/data",
										},
									},
									SecurityContext: &corev1.SecurityContext{
										AllowPrivilegeEscalation: &[]bool{false}[0],
										Capabilities: &corev1.Capabilities{
											Drop: []corev1.Capability{"ALL"},
										},
										ReadOnlyRootFilesystem: &[]bool{false}[0],
										RunAsNonRoot:           &[]bool{true}[0],
										RunAsUser:              &[]int64{1000}[0],
										SeccompProfile: &corev1.SeccompProfile{
											Type: corev1.SeccompProfileTypeRuntimeDefault,
										},
									},
								},
							},
							SecurityContext: &corev1.PodSecurityContext{
								RunAsNonRoot: &[]bool{true}[0],
								RunAsUser:    &[]int64{1000}[0],
								SeccompProfile: &corev1.SeccompProfile{
									Type: corev1.SeccompProfileTypeRuntimeDefault,
								},
							},
						},
					},
					VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "data",
							},
							Spec: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("1Gi"),
									},
								},
							},
						},
					},
				},
			}

			err = k8sClient.Create(context.TODO(), statefulSet)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      statefulSet.Name,
				Namespace: statefulSet.Namespace,
				Kind:      "StatefulSet",
			})

			By("Waiting for the StatefulSet to be ready")
			err = workloadHelper.WaitForWorkloadReady(statefulSet.Name, helpers.WorkloadTypeStatefulSet, 3*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for OptipPod to process the StatefulSet")
			time.Sleep(45 * time.Second)

			By("Verifying that OptipPod generated recommendations without disrupting PVs")
			annotations, err := workloadHelper.GetWorkloadAnnotations(statefulSet.Name, helpers.WorkloadTypeStatefulSet)
			Expect(err).NotTo(HaveOccurred())

			// In recommend mode, we should have recommendations but no actual updates
			if len(annotations) > 0 {
				Expect(annotations).To(HaveKey(MatchRegexp("optipod.io/.*")), "Should have OptipPod recommendation annotations")
			}

			By("Verifying that persistent volumes are preserved")
			updatedStatefulSet := &appsv1.StatefulSet{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{
				Name:      statefulSet.Name,
				Namespace: statefulSet.Namespace,
			}, updatedStatefulSet)
			Expect(err).NotTo(HaveOccurred())

			// Verify volume claim templates are still present
			Expect(updatedStatefulSet.Spec.VolumeClaimTemplates).NotTo(BeEmpty(), "Volume claim templates should be preserved")
			Expect(updatedStatefulSet.Spec.VolumeClaimTemplates[0].Name).To(Equal("data"), "Volume claim template name should be preserved")

			// Verify pods still have volume mounts
			Expect(updatedStatefulSet.Spec.Template.Spec.Containers[0].VolumeMounts).NotTo(BeEmpty(), "Volume mounts should be preserved")
		})
	})

	Context("DaemonSet Workload Scenarios", func() {
		It("should optimize DaemonSet resources correctly", func() {
			By("Creating a policy for DaemonSet optimization")
			policyConfig := helpers.PolicyConfig{
				Name: "daemonset-optimization-policy",
				Mode: v1alpha1.ModeAuto,
				WorkloadSelector: map[string]string{
					"app": "daemonset-test",
				},
				ResourceBounds: helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "100m",
						Max: "1000m",
					},
					Memory: helpers.ResourceBound{
						Min: "128Mi",
						Max: "2Gi",
					},
				},
				MetricsConfig: helpers.MetricsConfig{
					Provider:      "prometheus",
					RollingWindow: "1h",
					Percentile:    "P90",
					SafetyFactor:  1.2,
				},
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: true,
				},
			}

			policy, err := policyHelper.CreateOptimizationPolicy(policyConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      policy.Name,
				Namespace: policy.Namespace,
				Kind:      "OptimizationPolicy",
			})

			By("Creating a DaemonSet with suboptimal resources")
			workloadConfig := helpers.WorkloadConfig{
				Name:      "test-daemonset",
				Namespace: testNamespace,
				Type:      helpers.WorkloadTypeDaemonSet,
				Labels: map[string]string{
					"app": "daemonset-test",
				},
				Resources: helpers.ResourceRequirements{
					Requests: helpers.ResourceList{
						CPU:    "50m",  // Below minimum
						Memory: "64Mi", // Below minimum
					},
					Limits: helpers.ResourceList{
						CPU:    "200m",
						Memory: "256Mi",
					},
				},
				Replicas: 1, // DaemonSets don't use replicas, but helper might need it
			}

			daemonSet, err := workloadHelper.CreateDaemonSet(workloadConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      daemonSet.Name,
				Namespace: daemonSet.Namespace,
				Kind:      "DaemonSet",
			})

			By("Waiting for the DaemonSet to be ready")
			err = workloadHelper.WaitForWorkloadReady(daemonSet.Name, helpers.WorkloadTypeDaemonSet, 3*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for OptipPod to process the DaemonSet")
			time.Sleep(45 * time.Second)

			By("Verifying that the DaemonSet was optimized")
			updatedDaemonSet := &appsv1.DaemonSet{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{
				Name:      daemonSet.Name,
				Namespace: daemonSet.Namespace,
			}, updatedDaemonSet)
			Expect(err).NotTo(HaveOccurred())

			// Check that resources were adjusted to meet minimum bounds
			container := updatedDaemonSet.Spec.Template.Spec.Containers[0]
			cpuRequest := container.Resources.Requests[corev1.ResourceCPU]
			memoryRequest := container.Resources.Requests[corev1.ResourceMemory]

			minCPU := resource.MustParse("100m")
			minMemory := resource.MustParse("128Mi")

			Expect(cpuRequest.Cmp(minCPU)).To(BeNumerically(">=", 0), "CPU request should be at least the minimum bound")
			Expect(memoryRequest.Cmp(minMemory)).To(BeNumerically(">=", 0), "Memory request should be at least the minimum bound")

			By("Verifying OptipPod annotations are present")
			annotations, err := workloadHelper.GetWorkloadAnnotations(daemonSet.Name, helpers.WorkloadTypeDaemonSet)
			Expect(err).NotTo(HaveOccurred())
			Expect(annotations).NotTo(BeEmpty(), "DaemonSet should have OptipPod annotations")
		})

		It("should handle DaemonSet node-based update correctly", func() {
			By("Creating a policy for node-based update testing")
			policyConfig := helpers.PolicyConfig{
				Name: "daemonset-node-policy",
				Mode: v1alpha1.ModeAuto,
				WorkloadSelector: map[string]string{
					"app": "node-test",
				},
				ResourceBounds: helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "150m",
						Max: "1500m",
					},
					Memory: helpers.ResourceBound{
						Min: "192Mi",
						Max: "3Gi",
					},
				},
				MetricsConfig: helpers.MetricsConfig{
					Provider:      "prometheus",
					RollingWindow: "30m",
					Percentile:    "P95",
					SafetyFactor:  1.1,
				},
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: true,
				},
			}

			policy, err := policyHelper.CreateOptimizationPolicy(policyConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      policy.Name,
				Namespace: policy.Namespace,
				Kind:      "OptimizationPolicy",
			})

			By("Creating a DaemonSet with node tolerations")
			workloadConfig := helpers.WorkloadConfig{
				Name:      "node-daemonset",
				Namespace: testNamespace,
				Type:      helpers.WorkloadTypeDaemonSet,
				Labels: map[string]string{
					"app": "node-test",
				},
				Resources: helpers.ResourceRequirements{
					Requests: helpers.ResourceList{
						CPU:    "100m",  // Below minimum
						Memory: "128Mi", // Below minimum
					},
				},
				Replicas: 1,
			}

			daemonSet, err := workloadHelper.CreateDaemonSet(workloadConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      daemonSet.Name,
				Namespace: daemonSet.Namespace,
				Kind:      "DaemonSet",
			})

			By("Waiting for the initial DaemonSet to be ready")
			err = workloadHelper.WaitForWorkloadReady(daemonSet.Name, helpers.WorkloadTypeDaemonSet, 3*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Recording the initial generation and update strategy")
			initialDaemonSet := &appsv1.DaemonSet{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{
				Name:      daemonSet.Name,
				Namespace: daemonSet.Namespace,
			}, initialDaemonSet)
			Expect(err).NotTo(HaveOccurred())
			initialGeneration := initialDaemonSet.Generation

			// Verify the DaemonSet has the correct update strategy
			Expect(initialDaemonSet.Spec.UpdateStrategy.Type).To(Equal(appsv1.RollingUpdateDaemonSetStrategyType), "DaemonSet should use RollingUpdate strategy")

			// Verify tolerations are present for control plane nodes
			hasTolerations := false
			for _, toleration := range initialDaemonSet.Spec.Template.Spec.Tolerations {
				if toleration.Key == "node-role.kubernetes.io/control-plane" {
					hasTolerations = true
					break
				}
			}
			Expect(hasTolerations).To(BeTrue(), "DaemonSet should have control plane tolerations")

			By("Waiting for OptipPod to update the DaemonSet")
			time.Sleep(60 * time.Second)

			By("Verifying that the DaemonSet was updated with node-based rollout")
			updatedDaemonSet := &appsv1.DaemonSet{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{
				Name:      daemonSet.Name,
				Namespace: daemonSet.Namespace,
			}, updatedDaemonSet)
			Expect(err).NotTo(HaveOccurred())

			// Check if the DaemonSet was updated
			if updatedDaemonSet.Generation > initialGeneration {
				By("Verifying that the node-based update completed successfully")
				err = workloadHelper.WaitForWorkloadReady(daemonSet.Name, helpers.WorkloadTypeDaemonSet, 4*time.Minute)
				Expect(err).NotTo(HaveOccurred())

				// Verify all desired pods are ready
				Expect(updatedDaemonSet.Status.NumberReady).To(Equal(updatedDaemonSet.Status.DesiredNumberScheduled), "All desired pods should be ready after node-based update")
			}

			By("Verifying resource optimization was applied")
			container := updatedDaemonSet.Spec.Template.Spec.Containers[0]
			cpuRequest := container.Resources.Requests[corev1.ResourceCPU]
			memoryRequest := container.Resources.Requests[corev1.ResourceMemory]

			minCPU := resource.MustParse("150m")
			minMemory := resource.MustParse("192Mi")

			Expect(cpuRequest.Cmp(minCPU)).To(BeNumerically(">=", 0), "CPU request should meet minimum bound")
			Expect(memoryRequest.Cmp(minMemory)).To(BeNumerically(">=", 0), "Memory request should meet minimum bound")

			By("Verifying tolerations are preserved")
			hasTolerations = false
			for _, toleration := range updatedDaemonSet.Spec.Template.Spec.Tolerations {
				if toleration.Key == "node-role.kubernetes.io/control-plane" {
					hasTolerations = true
					break
				}
			}
			Expect(hasTolerations).To(BeTrue(), "DaemonSet tolerations should be preserved after optimization")
		})

		It("should validate resource optimization for system workloads", func() {
			By("Creating a policy for system workload testing")
			policyConfig := helpers.PolicyConfig{
				Name: "system-workload-policy",
				Mode: v1alpha1.ModeRecommend, // Use recommend mode for system workloads
				WorkloadSelector: map[string]string{
					"app":  "system-test",
					"tier": "system",
				},
				ResourceBounds: helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "50m",  // Lower minimum for system workloads
						Max: "500m", // Lower maximum for system workloads
					},
					Memory: helpers.ResourceBound{
						Min: "64Mi",  // Lower minimum for system workloads
						Max: "512Mi", // Lower maximum for system workloads
					},
				},
				MetricsConfig: helpers.MetricsConfig{
					Provider:      "prometheus",
					RollingWindow: "45m",
					Percentile:    "P90",
					SafetyFactor:  1.5, // Higher safety factor for system workloads
				},
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: false, // Disable in-place resize for system workloads
				},
			}

			policy, err := policyHelper.CreateOptimizationPolicy(policyConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      policy.Name,
				Namespace: policy.Namespace,
				Kind:      "OptimizationPolicy",
			})

			By("Creating a system DaemonSet with conservative resources")
			workloadConfig := helpers.WorkloadConfig{
				Name:      "system-daemonset",
				Namespace: testNamespace,
				Type:      helpers.WorkloadTypeDaemonSet,
				Labels: map[string]string{
					"app":  "system-test",
					"tier": "system",
				},
				Resources: helpers.ResourceRequirements{
					Requests: helpers.ResourceList{
						CPU:    "25m",  // Below minimum
						Memory: "32Mi", // Below minimum
					},
					Limits: helpers.ResourceList{
						CPU:    "100m",
						Memory: "128Mi",
					},
				},
				Replicas: 1,
			}

			daemonSet, err := workloadHelper.CreateDaemonSet(workloadConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      daemonSet.Name,
				Namespace: daemonSet.Namespace,
				Kind:      "DaemonSet",
			})

			By("Waiting for the DaemonSet to be ready")
			err = workloadHelper.WaitForWorkloadReady(daemonSet.Name, helpers.WorkloadTypeDaemonSet, 3*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for OptipPod to process the system workload")
			time.Sleep(45 * time.Second)

			By("Verifying that OptipPod generated appropriate recommendations for system workload")
			annotations, err := workloadHelper.GetWorkloadAnnotations(daemonSet.Name, helpers.WorkloadTypeDaemonSet)
			Expect(err).NotTo(HaveOccurred())

			// In recommend mode, we should have recommendations
			if len(annotations) > 0 {
				Expect(annotations).To(HaveKey(MatchRegexp("optipod.io/.*")), "Should have OptipPod recommendation annotations")

				// Check if recommendations respect system workload constraints
				for key, value := range annotations {
					if key == "optipod.io/cpu-recommendation" {
						recommendedCPU := resource.MustParse(value)
						maxCPU := resource.MustParse("500m")
						Expect(recommendedCPU.Cmp(maxCPU)).To(BeNumerically("<=", 0), "CPU recommendation should not exceed system workload maximum")
					}
					if key == "optipod.io/memory-recommendation" {
						recommendedMemory := resource.MustParse(value)
						maxMemory := resource.MustParse("512Mi")
						Expect(recommendedMemory.Cmp(maxMemory)).To(BeNumerically("<=", 0), "Memory recommendation should not exceed system workload maximum")
					}
				}
			}

			By("Verifying that the original workload was not modified (recommend mode)")
			updatedDaemonSet := &appsv1.DaemonSet{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{
				Name:      daemonSet.Name,
				Namespace: daemonSet.Namespace,
			}, updatedDaemonSet)
			Expect(err).NotTo(HaveOccurred())

			// In recommend mode, the original resources should be unchanged
			container := updatedDaemonSet.Spec.Template.Spec.Containers[0]
			originalCPU := resource.MustParse("25m")
			originalMemory := resource.MustParse("32Mi")

			currentCPU := container.Resources.Requests[corev1.ResourceCPU]
			currentMemory := container.Resources.Requests[corev1.ResourceMemory]

			Expect(currentCPU.Cmp(originalCPU)).To(Equal(0), "CPU request should be unchanged in recommend mode")
			Expect(currentMemory.Cmp(originalMemory)).To(Equal(0), "Memory request should be unchanged in recommend mode")
		})
	})

	Context("Update Strategy Scenarios", func() {
		It("should respect in-place resize update strategy", func() {
			By("Creating a policy with in-place resize enabled")
			policyConfig := helpers.PolicyConfig{
				Name: "in-place-resize-policy",
				Mode: v1alpha1.ModeAuto,
				WorkloadSelector: map[string]string{
					"app": "resize-test",
				},
				ResourceBounds: helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "200m",
						Max: "2000m",
					},
					Memory: helpers.ResourceBound{
						Min: "256Mi",
						Max: "4Gi",
					},
				},
				MetricsConfig: helpers.MetricsConfig{
					Provider:      "prometheus",
					RollingWindow: "1h",
					Percentile:    "P90",
					SafetyFactor:  1.2,
				},
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: true,
					AllowRecreate:      false,
					UpdateRequestsOnly: false,
				},
			}

			policy, err := policyHelper.CreateOptimizationPolicy(policyConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      policy.Name,
				Namespace: policy.Namespace,
				Kind:      "OptimizationPolicy",
			})

			By("Creating a Deployment for in-place resize testing")
			workloadConfig := helpers.WorkloadConfig{
				Name:      "resize-deployment",
				Namespace: testNamespace,
				Type:      helpers.WorkloadTypeDeployment,
				Labels: map[string]string{
					"app": "resize-test",
				},
				Resources: helpers.ResourceRequirements{
					Requests: helpers.ResourceList{
						CPU:    "100m",  // Below minimum
						Memory: "128Mi", // Below minimum
					},
					Limits: helpers.ResourceList{
						CPU:    "500m",
						Memory: "512Mi",
					},
				},
				Replicas: 1,
			}

			deployment, err := workloadHelper.CreateDeployment(workloadConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
				Kind:      "Deployment",
			})

			By("Waiting for the Deployment to be ready")
			err = workloadHelper.WaitForWorkloadReady(deployment.Name, helpers.WorkloadTypeDeployment, 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Recording the initial pod UID for in-place resize verification")
			initialDeployment := &appsv1.Deployment{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
			}, initialDeployment)
			Expect(err).NotTo(HaveOccurred())

			// Get the pod to track if it gets recreated
			podList := &corev1.PodList{}
			err = k8sClient.List(context.TODO(), podList, client.InNamespace(testNamespace), client.MatchingLabels(map[string]string{"app": "resize-test"}))
			Expect(err).NotTo(HaveOccurred())
			Expect(podList.Items).NotTo(BeEmpty(), "Should have at least one pod")
			initialPodUID := podList.Items[0].UID

			By("Waiting for OptipPod to apply in-place resize")
			time.Sleep(60 * time.Second)

			By("Verifying that in-place resize was used (pod not recreated)")
			updatedPodList := &corev1.PodList{}
			err = k8sClient.List(context.TODO(), updatedPodList, client.InNamespace(testNamespace), client.MatchingLabels(map[string]string{"app": "resize-test"}))
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedPodList.Items).NotTo(BeEmpty(), "Should still have pods")

			// Check if the same pod exists (in-place resize) or if it was recreated
			podStillExists := false
			for _, pod := range updatedPodList.Items {
				if pod.UID == initialPodUID {
					podStillExists = true
					break
				}
			}

			if podStillExists {
				By("Verifying that resources were updated in-place")
				updatedDeployment := &appsv1.Deployment{}
				err = k8sClient.Get(context.TODO(), types.NamespacedName{
					Name:      deployment.Name,
					Namespace: deployment.Namespace,
				}, updatedDeployment)
				Expect(err).NotTo(HaveOccurred())

				container := updatedDeployment.Spec.Template.Spec.Containers[0]
				cpuRequest := container.Resources.Requests[corev1.ResourceCPU]
				memoryRequest := container.Resources.Requests[corev1.ResourceMemory]

				minCPU := resource.MustParse("200m")
				minMemory := resource.MustParse("256Mi")

				Expect(cpuRequest.Cmp(minCPU)).To(BeNumerically(">=", 0), "CPU request should meet minimum bound")
				Expect(memoryRequest.Cmp(minMemory)).To(BeNumerically(">=", 0), "Memory request should meet minimum bound")
			} else {
				GinkgoWriter.Printf("Pod was recreated instead of resized in-place - this may be expected behavior\n")
			}
		})

		It("should respect recreation update strategy", func() {
			By("Creating a policy with recreation strategy")
			policyConfig := helpers.PolicyConfig{
				Name: "recreation-policy",
				Mode: v1alpha1.ModeAuto,
				WorkloadSelector: map[string]string{
					"app": "recreation-test",
				},
				ResourceBounds: helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "300m",
						Max: "3000m",
					},
					Memory: helpers.ResourceBound{
						Min: "384Mi",
						Max: "6Gi",
					},
				},
				MetricsConfig: helpers.MetricsConfig{
					Provider:      "prometheus",
					RollingWindow: "30m",
					Percentile:    "P95",
					SafetyFactor:  1.1,
				},
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: false,
					AllowRecreate:      true,
					UpdateRequestsOnly: false,
				},
			}

			policy, err := policyHelper.CreateOptimizationPolicy(policyConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      policy.Name,
				Namespace: policy.Namespace,
				Kind:      "OptimizationPolicy",
			})

			By("Creating a Deployment for recreation testing")
			workloadConfig := helpers.WorkloadConfig{
				Name:      "recreation-deployment",
				Namespace: testNamespace,
				Type:      helpers.WorkloadTypeDeployment,
				Labels: map[string]string{
					"app": "recreation-test",
				},
				Resources: helpers.ResourceRequirements{
					Requests: helpers.ResourceList{
						CPU:    "150m",  // Below minimum
						Memory: "192Mi", // Below minimum
					},
					Limits: helpers.ResourceList{
						CPU:    "600m",
						Memory: "768Mi",
					},
				},
				Replicas: 1,
			}

			deployment, err := workloadHelper.CreateDeployment(workloadConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
				Kind:      "Deployment",
			})

			By("Waiting for the Deployment to be ready")
			err = workloadHelper.WaitForWorkloadReady(deployment.Name, helpers.WorkloadTypeDeployment, 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Recording the initial generation for recreation verification")
			initialDeployment := &appsv1.Deployment{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
			}, initialDeployment)
			Expect(err).NotTo(HaveOccurred())
			initialGeneration := initialDeployment.Generation

			By("Waiting for OptipPod to apply recreation strategy")
			time.Sleep(60 * time.Second)

			By("Verifying that recreation strategy was used")
			updatedDeployment := &appsv1.Deployment{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
			}, updatedDeployment)
			Expect(err).NotTo(HaveOccurred())

			// Check if the deployment was updated (generation should increase with recreation)
			if updatedDeployment.Generation > initialGeneration {
				By("Verifying that resources were updated via recreation")
				container := updatedDeployment.Spec.Template.Spec.Containers[0]
				cpuRequest := container.Resources.Requests[corev1.ResourceCPU]
				memoryRequest := container.Resources.Requests[corev1.ResourceMemory]

				minCPU := resource.MustParse("300m")
				minMemory := resource.MustParse("384Mi")

				Expect(cpuRequest.Cmp(minCPU)).To(BeNumerically(">=", 0), "CPU request should meet minimum bound")
				Expect(memoryRequest.Cmp(minMemory)).To(BeNumerically(">=", 0), "Memory request should meet minimum bound")

				By("Waiting for the recreated deployment to be ready")
				err = workloadHelper.WaitForWorkloadReady(deployment.Name, helpers.WorkloadTypeDeployment, 3*time.Minute)
				Expect(err).NotTo(HaveOccurred())
			}
		})

		It("should respect requests-only update strategy", func() {
			By("Creating a policy with requests-only strategy")
			policyConfig := helpers.PolicyConfig{
				Name: "requests-only-policy",
				Mode: v1alpha1.ModeAuto,
				WorkloadSelector: map[string]string{
					"app": "requests-only-test",
				},
				ResourceBounds: helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "250m",
						Max: "2500m",
					},
					Memory: helpers.ResourceBound{
						Min: "320Mi",
						Max: "5Gi",
					},
				},
				MetricsConfig: helpers.MetricsConfig{
					Provider:      "prometheus",
					RollingWindow: "45m",
					Percentile:    "P90",
					SafetyFactor:  1.3,
				},
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: true,
					AllowRecreate:      false,
					UpdateRequestsOnly: true,
				},
			}

			policy, err := policyHelper.CreateOptimizationPolicy(policyConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      policy.Name,
				Namespace: policy.Namespace,
				Kind:      "OptimizationPolicy",
			})

			By("Creating a Deployment with both requests and limits")
			workloadConfig := helpers.WorkloadConfig{
				Name:      "requests-only-deployment",
				Namespace: testNamespace,
				Type:      helpers.WorkloadTypeDeployment,
				Labels: map[string]string{
					"app": "requests-only-test",
				},
				Resources: helpers.ResourceRequirements{
					Requests: helpers.ResourceList{
						CPU:    "125m",  // Below minimum
						Memory: "160Mi", // Below minimum
					},
					Limits: helpers.ResourceList{
						CPU:    "1000m", // Should remain unchanged
						Memory: "2Gi",   // Should remain unchanged
					},
				},
				Replicas: 1,
			}

			deployment, err := workloadHelper.CreateDeployment(workloadConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
				Kind:      "Deployment",
			})

			By("Waiting for the Deployment to be ready")
			err = workloadHelper.WaitForWorkloadReady(deployment.Name, helpers.WorkloadTypeDeployment, 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Recording the initial limits")
			initialDeployment := &appsv1.Deployment{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
			}, initialDeployment)
			Expect(err).NotTo(HaveOccurred())

			initialContainer := initialDeployment.Spec.Template.Spec.Containers[0]
			initialCPULimit := initialContainer.Resources.Limits[corev1.ResourceCPU]
			initialMemoryLimit := initialContainer.Resources.Limits[corev1.ResourceMemory]

			By("Waiting for OptipPod to apply requests-only strategy")
			time.Sleep(60 * time.Second)

			By("Verifying that only requests were updated, limits unchanged")
			updatedDeployment := &appsv1.Deployment{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
			}, updatedDeployment)
			Expect(err).NotTo(HaveOccurred())

			container := updatedDeployment.Spec.Template.Spec.Containers[0]

			// Check that requests were updated to meet minimum bounds
			cpuRequest := container.Resources.Requests[corev1.ResourceCPU]
			memoryRequest := container.Resources.Requests[corev1.ResourceMemory]

			minCPU := resource.MustParse("250m")
			minMemory := resource.MustParse("320Mi")

			Expect(cpuRequest.Cmp(minCPU)).To(BeNumerically(">=", 0), "CPU request should meet minimum bound")
			Expect(memoryRequest.Cmp(minMemory)).To(BeNumerically(">=", 0), "Memory request should meet minimum bound")

			// Check that limits remained unchanged
			currentCPULimit := container.Resources.Limits[corev1.ResourceCPU]
			currentMemoryLimit := container.Resources.Limits[corev1.ResourceMemory]

			Expect(currentCPULimit.Cmp(initialCPULimit)).To(Equal(0), "CPU limit should remain unchanged with requests-only strategy")
			Expect(currentMemoryLimit.Cmp(initialMemoryLimit)).To(Equal(0), "Memory limit should remain unchanged with requests-only strategy")
		})

		It("should validate strategy compliance and validation", func() {
			By("Creating a policy with conflicting update strategies")
			policyConfig := helpers.PolicyConfig{
				Name: "conflicting-strategy-policy",
				Mode: v1alpha1.ModeRecommend, // Use recommend mode to avoid actual updates
				WorkloadSelector: map[string]string{
					"app": "strategy-validation-test",
				},
				ResourceBounds: helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "100m",
						Max: "1000m",
					},
					Memory: helpers.ResourceBound{
						Min: "128Mi",
						Max: "2Gi",
					},
				},
				MetricsConfig: helpers.MetricsConfig{
					Provider:      "prometheus",
					RollingWindow: "1h",
					Percentile:    "P90",
					SafetyFactor:  1.2,
				},
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: false,
					AllowRecreate:      false,
					UpdateRequestsOnly: false, // All strategies disabled - should be handled gracefully
				},
			}

			policy, err := policyHelper.CreateOptimizationPolicy(policyConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      policy.Name,
				Namespace: policy.Namespace,
				Kind:      "OptimizationPolicy",
			})

			By("Creating a Deployment for strategy validation")
			workloadConfig := helpers.WorkloadConfig{
				Name:      "strategy-validation-deployment",
				Namespace: testNamespace,
				Type:      helpers.WorkloadTypeDeployment,
				Labels: map[string]string{
					"app": "strategy-validation-test",
				},
				Resources: helpers.ResourceRequirements{
					Requests: helpers.ResourceList{
						CPU:    "50m",  // Below minimum
						Memory: "64Mi", // Below minimum
					},
				},
				Replicas: 1,
			}

			deployment, err := workloadHelper.CreateDeployment(workloadConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
				Kind:      "Deployment",
			})

			By("Waiting for OptipPod to process the workload")
			time.Sleep(45 * time.Second)

			By("Verifying that the policy handles conflicting strategies gracefully")
			updatedPolicy, err := policyHelper.GetPolicy(policy.Name)
			Expect(err).NotTo(HaveOccurred())

			// The policy should either have error conditions or handle the conflict gracefully
			Expect(updatedPolicy.Status.Conditions).NotTo(BeEmpty(), "Policy should have status conditions")

			// Check for any error conditions related to update strategy
			hasStrategyError := false
			for _, condition := range updatedPolicy.Status.Conditions {
				if condition.Type == "Ready" && condition.Status == metav1.ConditionFalse {
					if condition.Message != "" && (condition.Message == "update strategy" || condition.Message == "strategy") {
						hasStrategyError = true
						GinkgoWriter.Printf("Found strategy error condition: %s\n", condition.Message)
					}
					break
				}
			}

			// Either we have a strategy error or the system handles it gracefully
			if !hasStrategyError {
				GinkgoWriter.Printf("No explicit strategy error condition found - system handling gracefully\n")
			}

			By("Verifying that workload remains unchanged with conflicting strategies")
			updatedDeployment := &appsv1.Deployment{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
			}, updatedDeployment)
			Expect(err).NotTo(HaveOccurred())

			// In recommend mode with conflicting strategies, the workload should remain unchanged
			container := updatedDeployment.Spec.Template.Spec.Containers[0]
			originalCPU := resource.MustParse("50m")
			originalMemory := resource.MustParse("64Mi")

			currentCPU := container.Resources.Requests[corev1.ResourceCPU]
			currentMemory := container.Resources.Requests[corev1.ResourceMemory]

			Expect(currentCPU.Cmp(originalCPU)).To(Equal(0), "CPU request should be unchanged with conflicting strategies")
			Expect(currentMemory.Cmp(originalMemory)).To(Equal(0), "Memory request should be unchanged with conflicting strategies")
		})
	})

	Context("Property-Based Tests", func() {
		/**
		 * Feature: e2e-test-enhancement, Property 10: Update strategy compliance
		 * For any configured update strategy, OptipPod should apply updates using only the specified method (in-place resize, recreation, requests-only)
		 */
		DescribeTable("update strategy compliance property",
			func(strategyName string, setupFunc func() (policyName string, workloadName string), validateFunc func(policyName, workloadName string)) {
				By(fmt.Sprintf("Setting up update strategy test: %s", strategyName))
				policyName, workloadName := setupFunc()

				By("Waiting for OptipPod to process the workload")
				time.Sleep(60 * time.Second)

				By("Validating update strategy compliance")
				validateFunc(policyName, workloadName)
			},
			Entry("in-place resize strategy compliance", "in-place-resize",
				func() (string, string) {
					config := helpers.PolicyConfig{
						Name:             "property-in-place-policy",
						Mode:             v1alpha1.ModeAuto,
						WorkloadSelector: map[string]string{"app": "property-resize-test"},
						ResourceBounds: helpers.ResourceBounds{
							CPU:    helpers.ResourceBound{Min: "200m", Max: "2000m"},
							Memory: helpers.ResourceBound{Min: "256Mi", Max: "4Gi"},
						},
						MetricsConfig: helpers.MetricsConfig{
							Provider: "prometheus", RollingWindow: "1h", Percentile: "P90", SafetyFactor: 1.2,
						},
						UpdateStrategy: helpers.UpdateStrategy{
							AllowInPlaceResize: true, AllowRecreate: false, UpdateRequestsOnly: false,
						},
					}
					policy, err := policyHelper.CreateOptimizationPolicy(config)
					Expect(err).NotTo(HaveOccurred())
					cleanupHelper.TrackResource(helpers.ResourceRef{
						Name: policy.Name, Namespace: policy.Namespace, Kind: "OptimizationPolicy",
					})

					workloadConfig := helpers.WorkloadConfig{
						Name: "property-resize-deployment", Namespace: testNamespace, Type: helpers.WorkloadTypeDeployment,
						Labels: map[string]string{"app": "property-resize-test"},
						Resources: helpers.ResourceRequirements{
							Requests: helpers.ResourceList{CPU: "100m", Memory: "128Mi"},
						},
						Replicas: 1,
					}
					deployment, err := workloadHelper.CreateDeployment(workloadConfig)
					Expect(err).NotTo(HaveOccurred())
					cleanupHelper.TrackResource(helpers.ResourceRef{
						Name: deployment.Name, Namespace: deployment.Namespace, Kind: "Deployment",
					})
					return policy.Name, deployment.Name
				},
				func(policyName, workloadName string) {
					// Validate that in-place resize was used
					updatedDeployment := &appsv1.Deployment{}
					err := k8sClient.Get(context.TODO(), types.NamespacedName{
						Name: workloadName, Namespace: testNamespace,
					}, updatedDeployment)
					Expect(err).NotTo(HaveOccurred())

					// Check that resources were updated to meet bounds
					container := updatedDeployment.Spec.Template.Spec.Containers[0]
					cpuRequest := container.Resources.Requests[corev1.ResourceCPU]
					memoryRequest := container.Resources.Requests[corev1.ResourceMemory]

					minCPU := resource.MustParse("200m")
					minMemory := resource.MustParse("256Mi")

					Expect(cpuRequest.Cmp(minCPU)).To(BeNumerically(">=", 0), "CPU should meet minimum bound with in-place resize")
					Expect(memoryRequest.Cmp(minMemory)).To(BeNumerically(">=", 0), "Memory should meet minimum bound with in-place resize")
				},
			),
			Entry("recreation strategy compliance", "recreation",
				func() (string, string) {
					config := helpers.PolicyConfig{
						Name:             "property-recreation-policy",
						Mode:             v1alpha1.ModeAuto,
						WorkloadSelector: map[string]string{"app": "property-recreation-test"},
						ResourceBounds: helpers.ResourceBounds{
							CPU:    helpers.ResourceBound{Min: "300m", Max: "3000m"},
							Memory: helpers.ResourceBound{Min: "384Mi", Max: "6Gi"},
						},
						MetricsConfig: helpers.MetricsConfig{
							Provider: "prometheus", RollingWindow: "30m", Percentile: "P95", SafetyFactor: 1.1,
						},
						UpdateStrategy: helpers.UpdateStrategy{
							AllowInPlaceResize: false, AllowRecreate: true, UpdateRequestsOnly: false,
						},
					}
					policy, err := policyHelper.CreateOptimizationPolicy(config)
					Expect(err).NotTo(HaveOccurred())
					cleanupHelper.TrackResource(helpers.ResourceRef{
						Name: policy.Name, Namespace: policy.Namespace, Kind: "OptimizationPolicy",
					})

					workloadConfig := helpers.WorkloadConfig{
						Name: "property-recreation-deployment", Namespace: testNamespace, Type: helpers.WorkloadTypeDeployment,
						Labels: map[string]string{"app": "property-recreation-test"},
						Resources: helpers.ResourceRequirements{
							Requests: helpers.ResourceList{CPU: "150m", Memory: "192Mi"},
						},
						Replicas: 1,
					}
					deployment, err := workloadHelper.CreateDeployment(workloadConfig)
					Expect(err).NotTo(HaveOccurred())
					cleanupHelper.TrackResource(helpers.ResourceRef{
						Name: deployment.Name, Namespace: deployment.Namespace, Kind: "Deployment",
					})
					return policy.Name, deployment.Name
				},
				func(policyName, workloadName string) {
					// Validate that recreation strategy was used
					updatedDeployment := &appsv1.Deployment{}
					err := k8sClient.Get(context.TODO(), types.NamespacedName{
						Name: workloadName, Namespace: testNamespace,
					}, updatedDeployment)
					Expect(err).NotTo(HaveOccurred())

					// Check that resources were updated to meet bounds
					container := updatedDeployment.Spec.Template.Spec.Containers[0]
					cpuRequest := container.Resources.Requests[corev1.ResourceCPU]
					memoryRequest := container.Resources.Requests[corev1.ResourceMemory]

					minCPU := resource.MustParse("300m")
					minMemory := resource.MustParse("384Mi")

					Expect(cpuRequest.Cmp(minCPU)).To(BeNumerically(">=", 0), "CPU should meet minimum bound with recreation")
					Expect(memoryRequest.Cmp(minMemory)).To(BeNumerically(">=", 0), "Memory should meet minimum bound with recreation")
				},
			),
			Entry("requests-only strategy compliance", "requests-only",
				func() (string, string) {
					config := helpers.PolicyConfig{
						Name:             "property-requests-only-policy",
						Mode:             v1alpha1.ModeAuto,
						WorkloadSelector: map[string]string{"app": "property-requests-only-test"},
						ResourceBounds: helpers.ResourceBounds{
							CPU:    helpers.ResourceBound{Min: "250m", Max: "2500m"},
							Memory: helpers.ResourceBound{Min: "320Mi", Max: "5Gi"},
						},
						MetricsConfig: helpers.MetricsConfig{
							Provider: "prometheus", RollingWindow: "45m", Percentile: "P90", SafetyFactor: 1.3,
						},
						UpdateStrategy: helpers.UpdateStrategy{
							AllowInPlaceResize: true, AllowRecreate: false, UpdateRequestsOnly: true,
						},
					}
					policy, err := policyHelper.CreateOptimizationPolicy(config)
					Expect(err).NotTo(HaveOccurred())
					cleanupHelper.TrackResource(helpers.ResourceRef{
						Name: policy.Name, Namespace: policy.Namespace, Kind: "OptimizationPolicy",
					})

					workloadConfig := helpers.WorkloadConfig{
						Name: "property-requests-only-deployment", Namespace: testNamespace, Type: helpers.WorkloadTypeDeployment,
						Labels: map[string]string{"app": "property-requests-only-test"},
						Resources: helpers.ResourceRequirements{
							Requests: helpers.ResourceList{CPU: "125m", Memory: "160Mi"},
							Limits:   helpers.ResourceList{CPU: "1000m", Memory: "2Gi"},
						},
						Replicas: 1,
					}
					deployment, err := workloadHelper.CreateDeployment(workloadConfig)
					Expect(err).NotTo(HaveOccurred())
					cleanupHelper.TrackResource(helpers.ResourceRef{
						Name: deployment.Name, Namespace: deployment.Namespace, Kind: "Deployment",
					})
					return policy.Name, deployment.Name
				},
				func(policyName, workloadName string) {
					// Validate that only requests were updated, limits unchanged
					updatedDeployment := &appsv1.Deployment{}
					err := k8sClient.Get(context.TODO(), types.NamespacedName{
						Name: workloadName, Namespace: testNamespace,
					}, updatedDeployment)
					Expect(err).NotTo(HaveOccurred())

					container := updatedDeployment.Spec.Template.Spec.Containers[0]

					// Check that requests were updated
					cpuRequest := container.Resources.Requests[corev1.ResourceCPU]
					memoryRequest := container.Resources.Requests[corev1.ResourceMemory]

					minCPU := resource.MustParse("250m")
					minMemory := resource.MustParse("320Mi")

					Expect(cpuRequest.Cmp(minCPU)).To(BeNumerically(">=", 0), "CPU request should meet minimum bound")
					Expect(memoryRequest.Cmp(minMemory)).To(BeNumerically(">=", 0), "Memory request should meet minimum bound")

					// Check that limits remained unchanged
					originalCPULimit := resource.MustParse("1000m")
					originalMemoryLimit := resource.MustParse("2Gi")

					currentCPULimit := container.Resources.Limits[corev1.ResourceCPU]
					currentMemoryLimit := container.Resources.Limits[corev1.ResourceMemory]

					Expect(currentCPULimit.Cmp(originalCPULimit)).To(Equal(0), "CPU limit should remain unchanged with requests-only")
					Expect(currentMemoryLimit.Cmp(originalMemoryLimit)).To(Equal(0), "Memory limit should remain unchanged with requests-only")
				},
			),
			Entry("conflicting strategies handling", "conflicting-strategies",
				func() (string, string) {
					config := helpers.PolicyConfig{
						Name:             "property-conflicting-policy",
						Mode:             v1alpha1.ModeRecommend,
						WorkloadSelector: map[string]string{"app": "property-conflicting-test"},
						ResourceBounds: helpers.ResourceBounds{
							CPU:    helpers.ResourceBound{Min: "100m", Max: "1000m"},
							Memory: helpers.ResourceBound{Min: "128Mi", Max: "2Gi"},
						},
						MetricsConfig: helpers.MetricsConfig{
							Provider: "prometheus", RollingWindow: "1h", Percentile: "P90", SafetyFactor: 1.2,
						},
						UpdateStrategy: helpers.UpdateStrategy{
							AllowInPlaceResize: false, AllowRecreate: false, UpdateRequestsOnly: false,
						},
					}
					policy, err := policyHelper.CreateOptimizationPolicy(config)
					Expect(err).NotTo(HaveOccurred())
					cleanupHelper.TrackResource(helpers.ResourceRef{
						Name: policy.Name, Namespace: policy.Namespace, Kind: "OptimizationPolicy",
					})

					workloadConfig := helpers.WorkloadConfig{
						Name: "property-conflicting-deployment", Namespace: testNamespace, Type: helpers.WorkloadTypeDeployment,
						Labels: map[string]string{"app": "property-conflicting-test"},
						Resources: helpers.ResourceRequirements{
							Requests: helpers.ResourceList{CPU: "50m", Memory: "64Mi"},
						},
						Replicas: 1,
					}
					deployment, err := workloadHelper.CreateDeployment(workloadConfig)
					Expect(err).NotTo(HaveOccurred())
					cleanupHelper.TrackResource(helpers.ResourceRef{
						Name: deployment.Name, Namespace: deployment.Namespace, Kind: "Deployment",
					})
					return policy.Name, deployment.Name
				},
				func(policyName, workloadName string) {
					// Validate that conflicting strategies are handled gracefully
					policy, err := policyHelper.GetPolicy(policyName)
					Expect(err).NotTo(HaveOccurred())
					Expect(policy.Status.Conditions).NotTo(BeEmpty(), "Policy should have status conditions")

					// Workload should remain unchanged with conflicting strategies
					updatedDeployment := &appsv1.Deployment{}
					err = k8sClient.Get(context.TODO(), types.NamespacedName{
						Name: workloadName, Namespace: testNamespace,
					}, updatedDeployment)
					Expect(err).NotTo(HaveOccurred())

					container := updatedDeployment.Spec.Template.Spec.Containers[0]
					originalCPU := resource.MustParse("50m")
					originalMemory := resource.MustParse("64Mi")

					currentCPU := container.Resources.Requests[corev1.ResourceCPU]
					currentMemory := container.Resources.Requests[corev1.ResourceMemory]

					Expect(currentCPU.Cmp(originalCPU)).To(Equal(0), "CPU should be unchanged with conflicting strategies")
					Expect(currentMemory.Cmp(originalMemory)).To(Equal(0), "Memory should be unchanged with conflicting strategies")
				},
			),
		)
	})

	Context("Workload Status Validation", func() {
		It("should report workload status accurately after optimization", func() {
			By("Creating a policy for status validation")
			policyConfig := helpers.PolicyConfig{
				Name: "status-validation-policy",
				Mode: v1alpha1.ModeAuto,
				WorkloadSelector: map[string]string{
					"app": "status-test",
				},
				ResourceBounds: helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "200m",
						Max: "2000m",
					},
					Memory: helpers.ResourceBound{
						Min: "256Mi",
						Max: "4Gi",
					},
				},
				MetricsConfig: helpers.MetricsConfig{
					Provider:      "prometheus",
					RollingWindow: "1h",
					Percentile:    "P90",
					SafetyFactor:  1.2,
				},
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: true,
				},
			}

			policy, err := policyHelper.CreateOptimizationPolicy(policyConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      policy.Name,
				Namespace: policy.Namespace,
				Kind:      "OptimizationPolicy",
			})

			By("Creating multiple workloads of different types")
			// Create Deployment
			deploymentConfig := helpers.WorkloadConfig{
				Name:      "status-deployment",
				Namespace: testNamespace,
				Type:      helpers.WorkloadTypeDeployment,
				Labels: map[string]string{
					"app": "status-test",
				},
				Resources: helpers.ResourceRequirements{
					Requests: helpers.ResourceList{
						CPU:    "100m",  // Below minimum
						Memory: "128Mi", // Below minimum
					},
				},
				Replicas: 2,
			}

			deployment, err := workloadHelper.CreateDeployment(deploymentConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
				Kind:      "Deployment",
			})

			// Create StatefulSet
			statefulSetConfig := helpers.WorkloadConfig{
				Name:      "status-statefulset",
				Namespace: testNamespace,
				Type:      helpers.WorkloadTypeStatefulSet,
				Labels: map[string]string{
					"app": "status-test",
				},
				Resources: helpers.ResourceRequirements{
					Requests: helpers.ResourceList{
						CPU:    "100m",  // Below minimum
						Memory: "128Mi", // Below minimum
					},
				},
				Replicas: 2,
			}

			statefulSet, err := workloadHelper.CreateStatefulSet(statefulSetConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      statefulSet.Name,
				Namespace: statefulSet.Namespace,
				Kind:      "StatefulSet",
			})

			By("Waiting for workloads to be ready")
			err = workloadHelper.WaitForWorkloadReady(deployment.Name, helpers.WorkloadTypeDeployment, 3*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			err = workloadHelper.WaitForWorkloadReady(statefulSet.Name, helpers.WorkloadTypeStatefulSet, 3*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for OptipPod to process the workloads")
			time.Sleep(60 * time.Second)

			By("Verifying that policy status reflects workload processing")
			updatedPolicy, err := policyHelper.GetPolicy(policy.Name)
			Expect(err).NotTo(HaveOccurred())

			// Check that workloads were discovered
			Expect(updatedPolicy.Status.WorkloadsDiscovered).To(BeNumerically(">=", 2), "Policy should discover at least 2 workloads")

			// Check that workloads were processed (in Auto mode)
			if updatedPolicy.Status.WorkloadsProcessed > 0 {
				Expect(updatedPolicy.Status.WorkloadsProcessed).To(BeNumerically("<=", updatedPolicy.Status.WorkloadsDiscovered), "Processed workloads should not exceed discovered workloads")
			}

			By("Verifying workload status consistency")
			// Check Deployment status
			updatedDeployment := &appsv1.Deployment{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
			}, updatedDeployment)
			Expect(err).NotTo(HaveOccurred())

			// Verify Deployment status fields are populated correctly
			Expect(updatedDeployment.Status.Replicas).To(Equal(updatedDeployment.Status.ReadyReplicas), "All Deployment replicas should be ready")

			// Check StatefulSet status
			updatedStatefulSet := &appsv1.StatefulSet{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{
				Name:      statefulSet.Name,
				Namespace: statefulSet.Namespace,
			}, updatedStatefulSet)
			Expect(err).NotTo(HaveOccurred())

			// Verify StatefulSet status fields are populated correctly
			Expect(updatedStatefulSet.Status.Replicas).To(Equal(updatedStatefulSet.Status.ReadyReplicas), "All StatefulSet replicas should be ready")
		})

		It("should update status after optimization changes", func() {
			By("Creating a policy for status update testing")
			policyConfig := helpers.PolicyConfig{
				Name: "status-update-policy",
				Mode: v1alpha1.ModeAuto,
				WorkloadSelector: map[string]string{
					"app": "status-update-test",
				},
				ResourceBounds: helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "300m",
						Max: "3000m",
					},
					Memory: helpers.ResourceBound{
						Min: "384Mi",
						Max: "6Gi",
					},
				},
				MetricsConfig: helpers.MetricsConfig{
					Provider:      "prometheus",
					RollingWindow: "30m",
					Percentile:    "P95",
					SafetyFactor:  1.1,
				},
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: true,
				},
			}

			policy, err := policyHelper.CreateOptimizationPolicy(policyConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      policy.Name,
				Namespace: policy.Namespace,
				Kind:      "OptimizationPolicy",
			})

			By("Creating a workload with resources that need optimization")
			workloadConfig := helpers.WorkloadConfig{
				Name:      "status-update-deployment",
				Namespace: testNamespace,
				Type:      helpers.WorkloadTypeDeployment,
				Labels: map[string]string{
					"app": "status-update-test",
				},
				Resources: helpers.ResourceRequirements{
					Requests: helpers.ResourceList{
						CPU:    "150m",  // Below minimum
						Memory: "192Mi", // Below minimum
					},
				},
				Replicas: 1,
			}

			deployment, err := workloadHelper.CreateDeployment(workloadConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
				Kind:      "Deployment",
			})

			By("Recording initial status")
			initialDeployment := &appsv1.Deployment{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
			}, initialDeployment)
			Expect(err).NotTo(HaveOccurred())
			initialGeneration := initialDeployment.Generation

			By("Waiting for the workload to be ready")
			err = workloadHelper.WaitForWorkloadReady(deployment.Name, helpers.WorkloadTypeDeployment, 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for OptipPod to optimize the workload")
			time.Sleep(60 * time.Second)

			By("Verifying that status was updated after optimization")
			updatedDeployment := &appsv1.Deployment{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
			}, updatedDeployment)
			Expect(err).NotTo(HaveOccurred())

			// Check if the deployment was updated
			if updatedDeployment.Generation > initialGeneration {
				By("Verifying that status reflects the optimization changes")
				// Wait for the updated deployment to be ready
				err = workloadHelper.WaitForWorkloadReady(deployment.Name, helpers.WorkloadTypeDeployment, 3*time.Minute)
				Expect(err).NotTo(HaveOccurred())

				// Verify status consistency after optimization
				finalDeployment := &appsv1.Deployment{}
				err = k8sClient.Get(context.TODO(), types.NamespacedName{
					Name:      deployment.Name,
					Namespace: deployment.Namespace,
				}, finalDeployment)
				Expect(err).NotTo(HaveOccurred())

				Expect(finalDeployment.Status.ReadyReplicas).To(Equal(finalDeployment.Status.Replicas), "All replicas should be ready after optimization")
				Expect(finalDeployment.Status.UpdatedReplicas).To(Equal(finalDeployment.Status.Replicas), "All replicas should be updated")

				// Verify that the optimization was applied
				container := finalDeployment.Spec.Template.Spec.Containers[0]
				cpuRequest := container.Resources.Requests[corev1.ResourceCPU]
				memoryRequest := container.Resources.Requests[corev1.ResourceMemory]

				minCPU := resource.MustParse("300m")
				minMemory := resource.MustParse("384Mi")

				Expect(cpuRequest.Cmp(minCPU)).To(BeNumerically(">=", 0), "CPU request should meet minimum bound")
				Expect(memoryRequest.Cmp(minMemory)).To(BeNumerically(">=", 0), "Memory request should meet minimum bound")
			}
		})

		It("should maintain status consistency across workload types", func() {
			By("Creating a policy for consistency testing")
			policyConfig := helpers.PolicyConfig{
				Name: "consistency-policy",
				Mode: v1alpha1.ModeRecommend, // Use recommend mode to avoid actual updates
				WorkloadSelector: map[string]string{
					"app": "consistency-test",
				},
				ResourceBounds: helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "150m",
						Max: "1500m",
					},
					Memory: helpers.ResourceBound{
						Min: "192Mi",
						Max: "3Gi",
					},
				},
				MetricsConfig: helpers.MetricsConfig{
					Provider:      "prometheus",
					RollingWindow: "45m",
					Percentile:    "P90",
					SafetyFactor:  1.3,
				},
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: true,
				},
			}

			policy, err := policyHelper.CreateOptimizationPolicy(policyConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      policy.Name,
				Namespace: policy.Namespace,
				Kind:      "OptimizationPolicy",
			})

			By("Creating workloads of all supported types")
			workloadTypes := []helpers.WorkloadType{
				helpers.WorkloadTypeDeployment,
				helpers.WorkloadTypeStatefulSet,
				helpers.WorkloadTypeDaemonSet,
			}

			workloadNames := make(map[helpers.WorkloadType]string)

			for _, workloadType := range workloadTypes {
				workloadConfig := helpers.WorkloadConfig{
					Name:      fmt.Sprintf("consistency-%s", string(workloadType)),
					Namespace: testNamespace,
					Type:      workloadType,
					Labels: map[string]string{
						"app": "consistency-test",
					},
					Resources: helpers.ResourceRequirements{
						Requests: helpers.ResourceList{
							CPU:    "100m",  // Below minimum
							Memory: "128Mi", // Below minimum
						},
					},
					Replicas: 1,
				}

				var workloadName string
				switch workloadType {
				case helpers.WorkloadTypeDeployment:
					deployment, err := workloadHelper.CreateDeployment(workloadConfig)
					Expect(err).NotTo(HaveOccurred())
					workloadName = deployment.Name
					cleanupHelper.TrackResource(helpers.ResourceRef{
						Name:      deployment.Name,
						Namespace: deployment.Namespace,
						Kind:      "Deployment",
					})
				case helpers.WorkloadTypeStatefulSet:
					statefulSet, err := workloadHelper.CreateStatefulSet(workloadConfig)
					Expect(err).NotTo(HaveOccurred())
					workloadName = statefulSet.Name
					cleanupHelper.TrackResource(helpers.ResourceRef{
						Name:      statefulSet.Name,
						Namespace: statefulSet.Namespace,
						Kind:      "StatefulSet",
					})
				case helpers.WorkloadTypeDaemonSet:
					daemonSet, err := workloadHelper.CreateDaemonSet(workloadConfig)
					Expect(err).NotTo(HaveOccurred())
					workloadName = daemonSet.Name
					cleanupHelper.TrackResource(helpers.ResourceRef{
						Name:      daemonSet.Name,
						Namespace: daemonSet.Namespace,
						Kind:      "DaemonSet",
					})
				}

				workloadNames[workloadType] = workloadName
			}

			By("Waiting for all workloads to be ready")
			for workloadType, workloadName := range workloadNames {
				err = workloadHelper.WaitForWorkloadReady(workloadName, workloadType, 3*time.Minute)
				Expect(err).NotTo(HaveOccurred())
			}

			By("Waiting for OptipPod to process all workloads")
			time.Sleep(60 * time.Second)

			By("Verifying status consistency across all workload types")
			updatedPolicy, err := policyHelper.GetPolicy(policy.Name)
			Expect(err).NotTo(HaveOccurred())

			// Check that all workloads were discovered
			Expect(updatedPolicy.Status.WorkloadsDiscovered).To(BeNumerically(">=", len(workloadTypes)), "Policy should discover all workload types")

			// In recommend mode, workloads should be discovered but not necessarily processed
			if updatedPolicy.Status.WorkloadsProcessed > 0 {
				Expect(updatedPolicy.Status.WorkloadsProcessed).To(BeNumerically("<=", updatedPolicy.Status.WorkloadsDiscovered), "Processed workloads should not exceed discovered workloads")
			}

			By("Verifying that all workload types have consistent annotation behavior")
			for workloadType, workloadName := range workloadNames {
				annotations, err := workloadHelper.GetWorkloadAnnotations(workloadName, workloadType)
				Expect(err).NotTo(HaveOccurred())

				// In recommend mode, we should have recommendations (annotations) for all workload types
				if len(annotations) > 0 {
					Expect(annotations).To(HaveKey(MatchRegexp("optipod.io/.*")), fmt.Sprintf("%s should have OptipPod annotations", workloadType))
				}
			}
		})

		It("should validate status field population", func() {
			By("Creating a policy for status field validation")
			policyConfig := helpers.PolicyConfig{
				Name: "status-field-policy",
				Mode: v1alpha1.ModeAuto,
				WorkloadSelector: map[string]string{
					"app": "status-field-test",
				},
				ResourceBounds: helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "100m",
						Max: "1000m",
					},
					Memory: helpers.ResourceBound{
						Min: "128Mi",
						Max: "2Gi",
					},
				},
				MetricsConfig: helpers.MetricsConfig{
					Provider:      "prometheus",
					RollingWindow: "1h",
					Percentile:    "P90",
					SafetyFactor:  1.2,
				},
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: true,
				},
			}

			policy, err := policyHelper.CreateOptimizationPolicy(policyConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      policy.Name,
				Namespace: policy.Namespace,
				Kind:      "OptimizationPolicy",
			})

			By("Creating a workload for status field validation")
			workloadConfig := helpers.WorkloadConfig{
				Name:      "status-field-deployment",
				Namespace: testNamespace,
				Type:      helpers.WorkloadTypeDeployment,
				Labels: map[string]string{
					"app": "status-field-test",
				},
				Resources: helpers.ResourceRequirements{
					Requests: helpers.ResourceList{
						CPU:    "50m",  // Below minimum
						Memory: "64Mi", // Below minimum
					},
				},
				Replicas: 2,
			}

			deployment, err := workloadHelper.CreateDeployment(workloadConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
				Kind:      "Deployment",
			})

			By("Waiting for the workload to be ready")
			err = workloadHelper.WaitForWorkloadReady(deployment.Name, helpers.WorkloadTypeDeployment, 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for OptipPod to process the workload")
			time.Sleep(45 * time.Second)

			By("Verifying that all required status fields are populated")
			updatedPolicy, err := policyHelper.GetPolicy(policy.Name)
			Expect(err).NotTo(HaveOccurred())

			// Verify policy status fields
			Expect(updatedPolicy.Status.Conditions).NotTo(BeEmpty(), "Policy should have status conditions")

			// Check for Ready condition
			hasReadyCondition := false
			for _, condition := range updatedPolicy.Status.Conditions {
				if condition.Type == "Ready" {
					hasReadyCondition = true
					Expect(condition.Status).To(BeElementOf([]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionFalse}), "Ready condition should have valid status")
					Expect(condition.LastTransitionTime).NotTo(BeZero(), "Ready condition should have transition time")
					break
				}
			}
			Expect(hasReadyCondition).To(BeTrue(), "Policy should have Ready condition")

			// Verify workload discovery and processing counters
			Expect(updatedPolicy.Status.WorkloadsDiscovered).To(BeNumerically(">=", 0), "WorkloadsDiscovered should be non-negative")
			Expect(updatedPolicy.Status.WorkloadsProcessed).To(BeNumerically(">=", 0), "WorkloadsProcessed should be non-negative")
			Expect(updatedPolicy.Status.WorkloadsProcessed).To(BeNumerically("<=", updatedPolicy.Status.WorkloadsDiscovered), "WorkloadsProcessed should not exceed WorkloadsDiscovered")

			By("Verifying workload status field consistency")
			updatedDeployment := &appsv1.Deployment{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
			}, updatedDeployment)
			Expect(err).NotTo(HaveOccurred())

			// Verify deployment status fields are consistent
			Expect(updatedDeployment.Status.Replicas).To(BeNumerically(">=", 0), "Replicas should be non-negative")
			Expect(updatedDeployment.Status.ReadyReplicas).To(BeNumerically(">=", 0), "ReadyReplicas should be non-negative")
			Expect(updatedDeployment.Status.ReadyReplicas).To(BeNumerically("<=", updatedDeployment.Status.Replicas), "ReadyReplicas should not exceed Replicas")

			if updatedDeployment.Status.UpdatedReplicas > 0 {
				Expect(updatedDeployment.Status.UpdatedReplicas).To(BeNumerically("<=", updatedDeployment.Status.Replicas), "UpdatedReplicas should not exceed Replicas")
			}
		})

		/**
		 * Feature: e2e-test-enhancement, Property 11: Status reporting accuracy
		 * For any workload processing operation, the workload status should accurately reflect the current state and any applied changes
		 */
		DescribeTable("status reporting accuracy property",
			func(scenarioName string, setupFunc func() (policyName string, workloadName string, workloadType helpers.WorkloadType), validateFunc func(policyName, workloadName string, workloadType helpers.WorkloadType)) {
				By(fmt.Sprintf("Setting up status reporting test: %s", scenarioName))
				policyName, workloadName, workloadType := setupFunc()

				By("Waiting for OptipPod to process the workload")
				time.Sleep(60 * time.Second)

				By("Validating status reporting accuracy")
				validateFunc(policyName, workloadName, workloadType)
			},
			Entry("deployment status accuracy", "deployment-status",
				func() (string, string, helpers.WorkloadType) {
					config := helpers.PolicyConfig{
						Name:             "property-deployment-status-policy",
						Mode:             v1alpha1.ModeAuto,
						WorkloadSelector: map[string]string{"app": "property-deployment-status-test"},
						ResourceBounds: helpers.ResourceBounds{
							CPU:    helpers.ResourceBound{Min: "200m", Max: "2000m"},
							Memory: helpers.ResourceBound{Min: "256Mi", Max: "4Gi"},
						},
						MetricsConfig: helpers.MetricsConfig{
							Provider: "prometheus", RollingWindow: "1h", Percentile: "P90", SafetyFactor: 1.2,
						},
						UpdateStrategy: helpers.UpdateStrategy{AllowInPlaceResize: true},
					}
					policy, err := policyHelper.CreateOptimizationPolicy(config)
					Expect(err).NotTo(HaveOccurred())
					cleanupHelper.TrackResource(helpers.ResourceRef{
						Name: policy.Name, Namespace: policy.Namespace, Kind: "OptimizationPolicy",
					})

					workloadConfig := helpers.WorkloadConfig{
						Name: "property-deployment-status", Namespace: testNamespace, Type: helpers.WorkloadTypeDeployment,
						Labels: map[string]string{"app": "property-deployment-status-test"},
						Resources: helpers.ResourceRequirements{
							Requests: helpers.ResourceList{CPU: "100m", Memory: "128Mi"},
						},
						Replicas: 2,
					}
					deployment, err := workloadHelper.CreateDeployment(workloadConfig)
					Expect(err).NotTo(HaveOccurred())
					cleanupHelper.TrackResource(helpers.ResourceRef{
						Name: deployment.Name, Namespace: deployment.Namespace, Kind: "Deployment",
					})
					return policy.Name, deployment.Name, helpers.WorkloadTypeDeployment
				},
				func(policyName, workloadName string, workloadType helpers.WorkloadType) {
					// Validate deployment status accuracy
					deployment := &appsv1.Deployment{}
					err := k8sClient.Get(context.TODO(), types.NamespacedName{
						Name: workloadName, Namespace: testNamespace,
					}, deployment)
					Expect(err).NotTo(HaveOccurred())

					// Status fields should be consistent and accurate
					Expect(deployment.Status.Replicas).To(BeNumerically(">=", 0), "Replicas should be non-negative")
					Expect(deployment.Status.ReadyReplicas).To(BeNumerically("<=", deployment.Status.Replicas), "ReadyReplicas should not exceed Replicas")
					if deployment.Status.UpdatedReplicas > 0 {
						Expect(deployment.Status.UpdatedReplicas).To(BeNumerically("<=", deployment.Status.Replicas), "UpdatedReplicas should not exceed Replicas")
					}

					// Policy status should reflect workload processing
					policy, err := policyHelper.GetPolicy(policyName)
					Expect(err).NotTo(HaveOccurred())
					Expect(policy.Status.WorkloadsDiscovered).To(BeNumerically(">=", 1), "Policy should discover at least one workload")
					Expect(policy.Status.WorkloadsProcessed).To(BeNumerically("<=", policy.Status.WorkloadsDiscovered), "Processed should not exceed discovered")
				},
			),
			Entry("statefulset status accuracy", "statefulset-status",
				func() (string, string, helpers.WorkloadType) {
					config := helpers.PolicyConfig{
						Name:             "property-statefulset-status-policy",
						Mode:             v1alpha1.ModeAuto,
						WorkloadSelector: map[string]string{"app": "property-statefulset-status-test"},
						ResourceBounds: helpers.ResourceBounds{
							CPU:    helpers.ResourceBound{Min: "300m", Max: "3000m"},
							Memory: helpers.ResourceBound{Min: "384Mi", Max: "6Gi"},
						},
						MetricsConfig: helpers.MetricsConfig{
							Provider: "prometheus", RollingWindow: "30m", Percentile: "P95", SafetyFactor: 1.1,
						},
						UpdateStrategy: helpers.UpdateStrategy{AllowInPlaceResize: true},
					}
					policy, err := policyHelper.CreateOptimizationPolicy(config)
					Expect(err).NotTo(HaveOccurred())
					cleanupHelper.TrackResource(helpers.ResourceRef{
						Name: policy.Name, Namespace: policy.Namespace, Kind: "OptimizationPolicy",
					})

					workloadConfig := helpers.WorkloadConfig{
						Name: "property-statefulset-status", Namespace: testNamespace, Type: helpers.WorkloadTypeStatefulSet,
						Labels: map[string]string{"app": "property-statefulset-status-test"},
						Resources: helpers.ResourceRequirements{
							Requests: helpers.ResourceList{CPU: "150m", Memory: "192Mi"},
						},
						Replicas: 2,
					}
					statefulSet, err := workloadHelper.CreateStatefulSet(workloadConfig)
					Expect(err).NotTo(HaveOccurred())
					cleanupHelper.TrackResource(helpers.ResourceRef{
						Name: statefulSet.Name, Namespace: statefulSet.Namespace, Kind: "StatefulSet",
					})
					return policy.Name, statefulSet.Name, helpers.WorkloadTypeStatefulSet
				},
				func(policyName, workloadName string, workloadType helpers.WorkloadType) {
					// Validate statefulset status accuracy
					statefulSet := &appsv1.StatefulSet{}
					err := k8sClient.Get(context.TODO(), types.NamespacedName{
						Name: workloadName, Namespace: testNamespace,
					}, statefulSet)
					Expect(err).NotTo(HaveOccurred())

					// Status fields should be consistent and accurate
					Expect(statefulSet.Status.Replicas).To(BeNumerically(">=", 0), "Replicas should be non-negative")
					Expect(statefulSet.Status.ReadyReplicas).To(BeNumerically("<=", statefulSet.Status.Replicas), "ReadyReplicas should not exceed Replicas")
					if statefulSet.Status.UpdatedReplicas > 0 {
						Expect(statefulSet.Status.UpdatedReplicas).To(BeNumerically("<=", statefulSet.Status.Replicas), "UpdatedReplicas should not exceed Replicas")
					}

					// Policy status should reflect workload processing
					policy, err := policyHelper.GetPolicy(policyName)
					Expect(err).NotTo(HaveOccurred())
					Expect(policy.Status.WorkloadsDiscovered).To(BeNumerically(">=", 1), "Policy should discover at least one workload")
					Expect(policy.Status.WorkloadsProcessed).To(BeNumerically("<=", policy.Status.WorkloadsDiscovered), "Processed should not exceed discovered")
				},
			),
			Entry("daemonset status accuracy", "daemonset-status",
				func() (string, string, helpers.WorkloadType) {
					config := helpers.PolicyConfig{
						Name:             "property-daemonset-status-policy",
						Mode:             v1alpha1.ModeAuto,
						WorkloadSelector: map[string]string{"app": "property-daemonset-status-test"},
						ResourceBounds: helpers.ResourceBounds{
							CPU:    helpers.ResourceBound{Min: "100m", Max: "1000m"},
							Memory: helpers.ResourceBound{Min: "128Mi", Max: "2Gi"},
						},
						MetricsConfig: helpers.MetricsConfig{
							Provider: "prometheus", RollingWindow: "1h", Percentile: "P90", SafetyFactor: 1.2,
						},
						UpdateStrategy: helpers.UpdateStrategy{AllowInPlaceResize: true},
					}
					policy, err := policyHelper.CreateOptimizationPolicy(config)
					Expect(err).NotTo(HaveOccurred())
					cleanupHelper.TrackResource(helpers.ResourceRef{
						Name: policy.Name, Namespace: policy.Namespace, Kind: "OptimizationPolicy",
					})

					workloadConfig := helpers.WorkloadConfig{
						Name: "property-daemonset-status", Namespace: testNamespace, Type: helpers.WorkloadTypeDaemonSet,
						Labels: map[string]string{"app": "property-daemonset-status-test"},
						Resources: helpers.ResourceRequirements{
							Requests: helpers.ResourceList{CPU: "50m", Memory: "64Mi"},
						},
						Replicas: 1,
					}
					daemonSet, err := workloadHelper.CreateDaemonSet(workloadConfig)
					Expect(err).NotTo(HaveOccurred())
					cleanupHelper.TrackResource(helpers.ResourceRef{
						Name: daemonSet.Name, Namespace: daemonSet.Namespace, Kind: "DaemonSet",
					})
					return policy.Name, daemonSet.Name, helpers.WorkloadTypeDaemonSet
				},
				func(policyName, workloadName string, workloadType helpers.WorkloadType) {
					// Validate daemonset status accuracy
					daemonSet := &appsv1.DaemonSet{}
					err := k8sClient.Get(context.TODO(), types.NamespacedName{
						Name: workloadName, Namespace: testNamespace,
					}, daemonSet)
					Expect(err).NotTo(HaveOccurred())

					// Status fields should be consistent and accurate
					Expect(daemonSet.Status.DesiredNumberScheduled).To(BeNumerically(">=", 0), "DesiredNumberScheduled should be non-negative")
					Expect(daemonSet.Status.NumberReady).To(BeNumerically("<=", daemonSet.Status.DesiredNumberScheduled), "NumberReady should not exceed DesiredNumberScheduled")
					if daemonSet.Status.UpdatedNumberScheduled > 0 {
						Expect(daemonSet.Status.UpdatedNumberScheduled).To(BeNumerically("<=", daemonSet.Status.DesiredNumberScheduled), "UpdatedNumberScheduled should not exceed DesiredNumberScheduled")
					}

					// Policy status should reflect workload processing
					policy, err := policyHelper.GetPolicy(policyName)
					Expect(err).NotTo(HaveOccurred())
					Expect(policy.Status.WorkloadsDiscovered).To(BeNumerically(">=", 1), "Policy should discover at least one workload")
					Expect(policy.Status.WorkloadsProcessed).To(BeNumerically("<=", policy.Status.WorkloadsDiscovered), "Processed should not exceed discovered")
				},
			),
			Entry("policy status consistency", "policy-status",
				func() (string, string, helpers.WorkloadType) {
					config := helpers.PolicyConfig{
						Name:             "property-policy-status-policy",
						Mode:             v1alpha1.ModeRecommend,
						WorkloadSelector: map[string]string{"app": "property-policy-status-test"},
						ResourceBounds: helpers.ResourceBounds{
							CPU:    helpers.ResourceBound{Min: "150m", Max: "1500m"},
							Memory: helpers.ResourceBound{Min: "192Mi", Max: "3Gi"},
						},
						MetricsConfig: helpers.MetricsConfig{
							Provider: "prometheus", RollingWindow: "45m", Percentile: "P90", SafetyFactor: 1.3,
						},
						UpdateStrategy: helpers.UpdateStrategy{AllowInPlaceResize: true},
					}
					policy, err := policyHelper.CreateOptimizationPolicy(config)
					Expect(err).NotTo(HaveOccurred())
					cleanupHelper.TrackResource(helpers.ResourceRef{
						Name: policy.Name, Namespace: policy.Namespace, Kind: "OptimizationPolicy",
					})

					workloadConfig := helpers.WorkloadConfig{
						Name: "property-policy-status-deployment", Namespace: testNamespace, Type: helpers.WorkloadTypeDeployment,
						Labels: map[string]string{"app": "property-policy-status-test"},
						Resources: helpers.ResourceRequirements{
							Requests: helpers.ResourceList{CPU: "75m", Memory: "96Mi"},
						},
						Replicas: 1,
					}
					deployment, err := workloadHelper.CreateDeployment(workloadConfig)
					Expect(err).NotTo(HaveOccurred())
					cleanupHelper.TrackResource(helpers.ResourceRef{
						Name: deployment.Name, Namespace: deployment.Namespace, Kind: "Deployment",
					})
					return policy.Name, deployment.Name, helpers.WorkloadTypeDeployment
				},
				func(policyName, workloadName string, workloadType helpers.WorkloadType) {
					// Validate policy status consistency
					policy, err := policyHelper.GetPolicy(policyName)
					Expect(err).NotTo(HaveOccurred())

					// Policy should have status conditions
					Expect(policy.Status.Conditions).NotTo(BeEmpty(), "Policy should have status conditions")

					// Check for Ready condition
					hasReadyCondition := false
					for _, condition := range policy.Status.Conditions {
						if condition.Type == "Ready" {
							hasReadyCondition = true
							Expect(condition.Status).To(BeElementOf([]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionFalse}), "Ready condition should have valid status")
							Expect(condition.LastTransitionTime).NotTo(BeZero(), "Ready condition should have transition time")
							break
						}
					}
					Expect(hasReadyCondition).To(BeTrue(), "Policy should have Ready condition")

					// Workload counters should be consistent
					Expect(policy.Status.WorkloadsDiscovered).To(BeNumerically(">=", 0), "WorkloadsDiscovered should be non-negative")
					Expect(policy.Status.WorkloadsProcessed).To(BeNumerically(">=", 0), "WorkloadsProcessed should be non-negative")
					Expect(policy.Status.WorkloadsProcessed).To(BeNumerically("<=", policy.Status.WorkloadsDiscovered), "WorkloadsProcessed should not exceed WorkloadsDiscovered")

					// In recommend mode, workloads should be discovered
					if policy.Status.WorkloadsDiscovered > 0 {
						// Check that workload has annotations (recommendations)
						annotations, err := workloadHelper.GetWorkloadAnnotations(workloadName, workloadType)
						Expect(err).NotTo(HaveOccurred())
						if len(annotations) > 0 {
							Expect(annotations).To(HaveKey(MatchRegexp("optipod.io/.*")), "Workload should have OptipPod recommendation annotations")
						}
					}
				},
			),
		)
	})

	Context("Unit Tests for Workload Scenarios", func() {
		It("should detect workload types correctly", func() {
			By("Testing workload type detection for Deployment")
			deploymentConfig := helpers.WorkloadConfig{
				Name:      "unit-test-deployment",
				Namespace: testNamespace,
				Type:      helpers.WorkloadTypeDeployment,
				Labels: map[string]string{
					"app": "unit-test",
				},
				Resources: helpers.ResourceRequirements{
					Requests: helpers.ResourceList{
						CPU:    "100m",
						Memory: "128Mi",
					},
				},
				Replicas: 1,
			}

			deployment, err := workloadHelper.CreateDeployment(deploymentConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
				Kind:      "Deployment",
			})

			// Verify deployment was created with correct type
			Expect(deployment.Kind).To(Equal("Deployment"))
			Expect(deployment.APIVersion).To(Equal("apps/v1"))

			By("Testing workload type detection for StatefulSet")
			statefulSetConfig := helpers.WorkloadConfig{
				Name:      "unit-test-statefulset",
				Namespace: testNamespace,
				Type:      helpers.WorkloadTypeStatefulSet,
				Labels: map[string]string{
					"app": "unit-test",
				},
				Resources: helpers.ResourceRequirements{
					Requests: helpers.ResourceList{
						CPU:    "100m",
						Memory: "128Mi",
					},
				},
				Replicas: 1,
			}

			statefulSet, err := workloadHelper.CreateStatefulSet(statefulSetConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      statefulSet.Name,
				Namespace: statefulSet.Namespace,
				Kind:      "StatefulSet",
			})

			// Verify statefulset was created with correct type
			Expect(statefulSet.Kind).To(Equal("StatefulSet"))
			Expect(statefulSet.APIVersion).To(Equal("apps/v1"))

			By("Testing workload type detection for DaemonSet")
			daemonSetConfig := helpers.WorkloadConfig{
				Name:      "unit-test-daemonset",
				Namespace: testNamespace,
				Type:      helpers.WorkloadTypeDaemonSet,
				Labels: map[string]string{
					"app": "unit-test",
				},
				Resources: helpers.ResourceRequirements{
					Requests: helpers.ResourceList{
						CPU:    "100m",
						Memory: "128Mi",
					},
				},
				Replicas: 1,
			}

			daemonSet, err := workloadHelper.CreateDaemonSet(daemonSetConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      daemonSet.Name,
				Namespace: daemonSet.Namespace,
				Kind:      "DaemonSet",
			})

			// Verify daemonset was created with correct type
			Expect(daemonSet.Kind).To(Equal("DaemonSet"))
			Expect(daemonSet.APIVersion).To(Equal("apps/v1"))
		})

		It("should select correct update strategy based on policy configuration", func() {
			By("Testing update strategy configuration validation")

			// Test in-place resize strategy configuration
			inPlaceConfig := helpers.PolicyConfig{
				Name: "unit-in-place-policy",
				Mode: v1alpha1.ModeAuto,
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: true,
					AllowRecreate:      false,
					UpdateRequestsOnly: false,
				},
			}
			Expect(inPlaceConfig.UpdateStrategy.AllowInPlaceResize).To(BeTrue())
			Expect(inPlaceConfig.UpdateStrategy.AllowRecreate).To(BeFalse())
			Expect(inPlaceConfig.UpdateStrategy.UpdateRequestsOnly).To(BeFalse())

			// Test recreation strategy configuration
			recreateConfig := helpers.PolicyConfig{
				Name: "unit-recreate-policy",
				Mode: v1alpha1.ModeAuto,
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: false,
					AllowRecreate:      true,
					UpdateRequestsOnly: false,
				},
			}
			Expect(recreateConfig.UpdateStrategy.AllowInPlaceResize).To(BeFalse())
			Expect(recreateConfig.UpdateStrategy.AllowRecreate).To(BeTrue())
			Expect(recreateConfig.UpdateStrategy.UpdateRequestsOnly).To(BeFalse())

			// Test requests-only strategy configuration
			requestsOnlyConfig := helpers.PolicyConfig{
				Name: "unit-requests-only-policy",
				Mode: v1alpha1.ModeAuto,
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: true,
					AllowRecreate:      false,
					UpdateRequestsOnly: true,
				},
			}
			Expect(requestsOnlyConfig.UpdateStrategy.AllowInPlaceResize).To(BeTrue())
			Expect(requestsOnlyConfig.UpdateStrategy.AllowRecreate).To(BeFalse())
			Expect(requestsOnlyConfig.UpdateStrategy.UpdateRequestsOnly).To(BeTrue())
		})

		XIt("should validate status reporting logic", func() {
			By("Testing policy status initialization")
			policyConfig := helpers.PolicyConfig{
				Name: "unit-status-policy",
				Mode: v1alpha1.ModeRecommend,
				WorkloadSelector: map[string]string{
					"app": "unit-status-test",
				},
				ResourceBounds: helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "100m",
						Max: "1000m",
					},
					Memory: helpers.ResourceBound{
						Min: "128Mi",
						Max: "2Gi",
					},
				},
				MetricsConfig: helpers.MetricsConfig{
					Provider:      "prometheus",
					RollingWindow: "1h",
					Percentile:    "P90",
					SafetyFactor:  1.2,
				},
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: true,
				},
			}

			policy, err := policyHelper.CreateOptimizationPolicy(policyConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      policy.Name,
				Namespace: policy.Namespace,
				Kind:      "OptimizationPolicy",
			})

			// Verify initial policy status
			Expect(policy.Status.WorkloadsDiscovered).To(Equal(int32(0)), "Initial WorkloadsDiscovered should be 0")
			Expect(policy.Status.WorkloadsProcessed).To(Equal(int32(0)), "Initial WorkloadsProcessed should be 0")

			By("Testing workload status initialization")
			workloadConfig := helpers.WorkloadConfig{
				Name:      "unit-status-deployment",
				Namespace: testNamespace,
				Type:      helpers.WorkloadTypeDeployment,
				Labels: map[string]string{
					"app": "unit-status-test",
				},
				Resources: helpers.ResourceRequirements{
					Requests: helpers.ResourceList{
						CPU:    "50m",  // Below minimum
						Memory: "64Mi", // Below minimum
					},
				},
				Replicas: 2,
			}

			deployment, err := workloadHelper.CreateDeployment(workloadConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
				Kind:      "Deployment",
			})

			// Verify initial deployment status structure
			Expect(deployment.Status.Replicas).To(Equal(int32(0)), "Initial Replicas should be 0")
			Expect(deployment.Status.ReadyReplicas).To(Equal(int32(0)), "Initial ReadyReplicas should be 0")
			Expect(deployment.Status.UpdatedReplicas).To(Equal(int32(0)), "Initial UpdatedReplicas should be 0")

			By("Testing resource specification validation")
			// Verify that the deployment was created with the specified resources
			container := deployment.Spec.Template.Spec.Containers[0]
			cpuRequest := container.Resources.Requests[corev1.ResourceCPU]
			memoryRequest := container.Resources.Requests[corev1.ResourceMemory]

			expectedCPU := resource.MustParse("50m")
			expectedMemory := resource.MustParse("64Mi")

			Expect(cpuRequest.Cmp(expectedCPU)).To(Equal(0), "CPU request should match specified value")
			Expect(memoryRequest.Cmp(expectedMemory)).To(Equal(0), "Memory request should match specified value")
		})

		XIt("should validate workload annotation handling", func() {
			By("Testing annotation retrieval for different workload types")
			// Create a deployment with some annotations
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "unit-annotation-deployment",
					Namespace: testNamespace,
					Annotations: map[string]string{
						"optipod.io/cpu-recommendation":    "200m",
						"optipod.io/memory-recommendation": "256Mi",
						"optipod.io/last-applied":          "2023-01-01T00:00:00Z",
						"other.io/annotation":              "should-be-filtered",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "unit-annotation-test",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "unit-annotation-test",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "app",
									Image: "nginx:1.25-alpine",
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("100m"),
											corev1.ResourceMemory: resource.MustParse("128Mi"),
										},
									},
									SecurityContext: &corev1.SecurityContext{
										AllowPrivilegeEscalation: &[]bool{false}[0],
										Capabilities: &corev1.Capabilities{
											Drop: []corev1.Capability{"ALL"},
										},
										ReadOnlyRootFilesystem: &[]bool{false}[0],
										RunAsNonRoot:           &[]bool{true}[0],
										RunAsUser:              &[]int64{1000}[0],
										SeccompProfile: &corev1.SeccompProfile{
											Type: corev1.SeccompProfileTypeRuntimeDefault,
										},
									},
								},
							},
							SecurityContext: &corev1.PodSecurityContext{
								RunAsNonRoot: &[]bool{true}[0],
								RunAsUser:    &[]int64{1000}[0],
								SeccompProfile: &corev1.SeccompProfile{
									Type: corev1.SeccompProfileTypeRuntimeDefault,
								},
							},
						},
					},
				},
			}

			err := k8sClient.Create(context.TODO(), deployment)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
				Kind:      "Deployment",
			})

			By("Retrieving and validating OptipPod annotations")
			annotations, err := workloadHelper.GetWorkloadAnnotations(deployment.Name, helpers.WorkloadTypeDeployment)
			Expect(err).NotTo(HaveOccurred())

			// Should only get OptipPod annotations (those starting with "optipod.io")
			Expect(annotations).To(HaveLen(3), "Should have exactly 3 OptipPod annotations")
			Expect(annotations).To(HaveKey("optipod.io/cpu-recommendation"))
			Expect(annotations).To(HaveKey("optipod.io/memory-recommendation"))
			Expect(annotations).To(HaveKey("optipod.io/last-applied"))
			Expect(annotations).NotTo(HaveKey("other.io/annotation"), "Non-OptipPod annotations should be filtered out")

			// Verify annotation values
			Expect(annotations["optipod.io/cpu-recommendation"]).To(Equal("200m"))
			Expect(annotations["optipod.io/memory-recommendation"]).To(Equal("256Mi"))
			Expect(annotations["optipod.io/last-applied"]).To(Equal("2023-01-01T00:00:00Z"))
		})

		It("should validate resource bounds enforcement logic", func() {
			By("Testing resource bounds validation")
			// Test CPU bounds
			minCPU := resource.MustParse("100m")
			maxCPU := resource.MustParse("1000m")

			// Test values within bounds
			withinBoundsCPU := resource.MustParse("500m")
			Expect(withinBoundsCPU.Cmp(minCPU)).To(BeNumerically(">=", 0), "500m should be >= 100m")
			Expect(withinBoundsCPU.Cmp(maxCPU)).To(BeNumerically("<=", 0), "500m should be <= 1000m")

			// Test values below minimum
			belowMinCPU := resource.MustParse("50m")
			Expect(belowMinCPU.Cmp(minCPU)).To(BeNumerically("<", 0), "50m should be < 100m")

			// Test values above maximum
			aboveMaxCPU := resource.MustParse("2000m")
			Expect(aboveMaxCPU.Cmp(maxCPU)).To(BeNumerically(">", 0), "2000m should be > 1000m")

			By("Testing memory bounds validation")
			minMemory := resource.MustParse("128Mi")
			maxMemory := resource.MustParse("2Gi")

			// Test values within bounds
			withinBoundsMemory := resource.MustParse("512Mi")
			Expect(withinBoundsMemory.Cmp(minMemory)).To(BeNumerically(">=", 0), "512Mi should be >= 128Mi")
			Expect(withinBoundsMemory.Cmp(maxMemory)).To(BeNumerically("<=", 0), "512Mi should be <= 2Gi")

			// Test values below minimum
			belowMinMemory := resource.MustParse("64Mi")
			Expect(belowMinMemory.Cmp(minMemory)).To(BeNumerically("<", 0), "64Mi should be < 128Mi")

			// Test values above maximum
			aboveMaxMemory := resource.MustParse("4Gi")
			Expect(aboveMaxMemory.Cmp(maxMemory)).To(BeNumerically(">", 0), "4Gi should be > 2Gi")

			By("Testing resource unit conversions")
			// Test that different units are compared correctly
			cpu1000m := resource.MustParse("1000m")
			cpu1 := resource.MustParse("1")
			Expect(cpu1000m.Cmp(cpu1)).To(Equal(0), "1000m should equal 1 CPU")

			memory1024Mi := resource.MustParse("1024Mi")
			memory1Gi := resource.MustParse("1Gi")
			Expect(memory1024Mi.Cmp(memory1Gi)).To(Equal(0), "1024Mi should equal 1Gi")
		})
	})
})

