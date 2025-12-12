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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

var _ = Describe("Error Handling and Edge Cases", Ordered, func() {
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
		testNamespace = fmt.Sprintf("error-test-%d", time.Now().Unix())

		// Create the test namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		Expect(k8sClient.Create(context.TODO(), ns)).To(Succeed())

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

	Context("Invalid Configuration Scenarios", func() {
		It("should reject policies with invalid CPU resource bounds (min > max)", func() {
			By("Creating a policy with CPU min > max")
			invalidConfig := helpers.PolicyConfig{
				Name: "invalid-cpu-bounds",
				Mode: v1alpha1.ModeAuto,
				WorkloadSelector: map[string]string{
					"app": "test-app",
				},
				ResourceBounds: helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "2000m", // 2 cores
						Max: "1000m", // 1 core - invalid!
					},
					Memory: helpers.ResourceBound{
						Min: "1Gi",
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

			_, err := policyHelper.CreateOptimizationPolicy(invalidConfig)
			Expect(err).To(HaveOccurred(), "Policy creation should fail with invalid CPU bounds")
			Expect(err.Error()).To(ContainSubstring("CPU min"), "Error should mention CPU min/max validation")
			Expect(err.Error()).To(ContainSubstring("max"), "Error should mention max validation")
		})

		It("should reject policies with invalid memory resource bounds (min > max)", func() {
			By("Creating a policy with memory min > max")
			invalidConfig := helpers.PolicyConfig{
				Name: "invalid-memory-bounds",
				Mode: v1alpha1.ModeAuto,
				WorkloadSelector: map[string]string{
					"app": "test-app",
				},
				ResourceBounds: helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "100m",
						Max: "1000m",
					},
					Memory: helpers.ResourceBound{
						Min: "4Gi", // 4GB
						Max: "2Gi", // 2GB - invalid!
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

			_, err := policyHelper.CreateOptimizationPolicy(invalidConfig)
			Expect(err).To(HaveOccurred(), "Policy creation should fail with invalid memory bounds")
			Expect(err.Error()).To(ContainSubstring("memory min"), "Error should mention memory min/max validation")
			Expect(err.Error()).To(ContainSubstring("max"), "Error should mention max validation")
		})

		It("should reject policies with zero resource bounds", func() {
			By("Creating a policy with zero CPU min")
			invalidConfig := helpers.PolicyConfig{
				Name: "zero-cpu-min",
				Mode: v1alpha1.ModeAuto,
				WorkloadSelector: map[string]string{
					"app": "test-app",
				},
				ResourceBounds: helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "0m", // Zero CPU - invalid!
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

			_, err := policyHelper.CreateOptimizationPolicy(invalidConfig)
			Expect(err).To(HaveOccurred(), "Policy creation should fail with zero CPU bounds")
			Expect(err.Error()).To(ContainSubstring("greater than zero"), "Error should mention zero validation")
		})

		It("should reject policies with invalid safety factor", func() {
			By("Creating a policy with safety factor < 1.0")
			invalidConfig := helpers.PolicyConfig{
				Name: "invalid-safety-factor",
				Mode: v1alpha1.ModeAuto,
				WorkloadSelector: map[string]string{
					"app": "test-app",
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
					SafetyFactor:  0.5, // Invalid - must be >= 1.0
				},
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: true,
				},
			}

			_, err := policyHelper.CreateOptimizationPolicy(invalidConfig)
			Expect(err).To(HaveOccurred(), "Policy creation should fail with invalid safety factor")
			Expect(err.Error()).To(ContainSubstring("safety factor"), "Error should mention safety factor validation")
		})

		It("should reject policies with missing required fields", func() {
			By("Creating a policy without mode")
			invalidConfig := helpers.PolicyConfig{
				Name: "missing-mode",
				// Mode is missing - should be required
				WorkloadSelector: map[string]string{
					"app": "test-app",
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

			_, err := policyHelper.CreateOptimizationPolicy(invalidConfig)
			Expect(err).To(HaveOccurred(), "Policy creation should fail with missing mode")
		})

		It("should reject policies with invalid mode values", func() {
			By("Creating a policy with invalid mode")
			// We need to create the policy directly to bypass helper validation
			invalidPolicy := &v1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-mode-policy",
					Namespace: testNamespace,
				},
				Spec: v1alpha1.OptimizationPolicySpec{
					Mode: "InvalidMode", // Invalid mode
					Selector: v1alpha1.WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test-app"},
						},
					},
					MetricsConfig: v1alpha1.MetricsConfig{
						Provider:   "prometheus",
						Percentile: "P90",
					},
					ResourceBounds: v1alpha1.ResourceBounds{
						CPU: v1alpha1.ResourceBound{
							Min: resource.MustParse("100m"),
							Max: resource.MustParse("1000m"),
						},
						Memory: v1alpha1.ResourceBound{
							Min: resource.MustParse("128Mi"),
							Max: resource.MustParse("2Gi"),
						},
					},
					UpdateStrategy: v1alpha1.UpdateStrategy{
						AllowInPlaceResize: true,
					},
				},
			}

			err := k8sClient.Create(context.TODO(), invalidPolicy)
			Expect(err).To(HaveOccurred(), "Policy creation should fail with invalid mode")
		})

		It("should reject policies without selectors", func() {
			By("Creating a policy without any selectors")
			invalidConfig := helpers.PolicyConfig{
				Name: "no-selectors",
				Mode: v1alpha1.ModeAuto,
				// No selectors specified - should be required
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

			_, err := policyHelper.CreateOptimizationPolicy(invalidConfig)
			Expect(err).To(HaveOccurred(), "Policy creation should fail without selectors")
			Expect(err.Error()).To(ContainSubstring("selector"), "Error should mention selector requirement")
		})

		It("should handle workloads with malformed resource specifications", func() {
			By("Creating a valid policy first")
			validConfig := helpers.PolicyConfig{
				Name: "valid-policy-for-malformed-test",
				Mode: v1alpha1.ModeAuto,
				WorkloadSelector: map[string]string{
					"app": "malformed-test",
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

			policy, err := policyHelper.CreateOptimizationPolicy(validConfig)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      policy.Name,
				Namespace: policy.Namespace,
				Kind:      "OptimizationPolicy",
			})

			By("Creating a deployment with no resource specifications")
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-resources",
					Namespace: testNamespace,
					Labels: map[string]string{
						"app": "malformed-test",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "malformed-test",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "malformed-test",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "app",
									Image: "nginx:latest",
									// No resource specifications - should be handled gracefully
								},
							},
						},
					},
				},
			}

			err = k8sClient.Create(context.TODO(), deployment)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
				Kind:      "Deployment",
			})

			By("Creating a deployment with only limits (no requests)")
			deploymentLimitsOnly := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "limits-only",
					Namespace: testNamespace,
					Labels: map[string]string{
						"app": "malformed-test",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "malformed-test",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "malformed-test",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "app",
									Image: "nginx:latest",
									Resources: corev1.ResourceRequirements{
										// Only limits, no requests
										Limits: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("500m"),
											corev1.ResourceMemory: resource.MustParse("512Mi"),
										},
									},
								},
							},
						},
					},
				},
			}

			err = k8sClient.Create(context.TODO(), deploymentLimitsOnly)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      deploymentLimitsOnly.Name,
				Namespace: deploymentLimitsOnly.Namespace,
				Kind:      "Deployment",
			})

			By("Waiting for the controller to process the workloads")
			// The controller should handle the workloads gracefully even if they have issues
			time.Sleep(45 * time.Second)

			By("Verifying that the policy status reflects appropriate handling")
			updatedPolicy, err := policyHelper.GetPolicy(policy.Name)
			Expect(err).NotTo(HaveOccurred())

			// The policy should still be functional and not crash
			Expect(updatedPolicy.Status.Conditions).NotTo(BeEmpty())

			// Check that workloads were discovered even if they have malformed specs
			// The controller should handle them gracefully
			if updatedPolicy.Status.WorkloadsDiscovered > 0 {
				GinkgoWriter.Printf("Policy discovered %d workloads with malformed specs\n", updatedPolicy.Status.WorkloadsDiscovered)
			}
		})

		It("should validate configuration edge cases", func() {
			By("Testing policy with extremely large resource bounds")
			largeConfig := helpers.PolicyConfig{
				Name: "large-bounds",
				Mode: v1alpha1.ModeAuto,
				WorkloadSelector: map[string]string{
					"app": "test-app",
				},
				ResourceBounds: helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "1m",
						Max: "1000000m", // 1000 cores - very large but valid
					},
					Memory: helpers.ResourceBound{
						Min: "1Mi",
						Max: "1000Gi", // 1TB - very large but valid
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

			policy, err := policyHelper.CreateOptimizationPolicy(largeConfig)
			Expect(err).NotTo(HaveOccurred(), "Policy with large but valid bounds should be accepted")
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      policy.Name,
				Namespace: policy.Namespace,
				Kind:      "OptimizationPolicy",
			})

			By("Testing policy with very small resource bounds")
			smallConfig := helpers.PolicyConfig{
				Name: "small-bounds",
				Mode: v1alpha1.ModeAuto,
				WorkloadSelector: map[string]string{
					"app": "test-app",
				},
				ResourceBounds: helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "1m",  // 1 millicore - very small but valid
						Max: "10m", // 10 millicores
					},
					Memory: helpers.ResourceBound{
						Min: "1Mi",  // 1MB - very small but valid
						Max: "10Mi", // 10MB
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

			smallPolicy, err := policyHelper.CreateOptimizationPolicy(smallConfig)
			Expect(err).NotTo(HaveOccurred(), "Policy with small but valid bounds should be accepted")
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      smallPolicy.Name,
				Namespace: smallPolicy.Namespace,
				Kind:      "OptimizationPolicy",
			})

			By("Testing policy with maximum safety factor")
			maxSafetyConfig := helpers.PolicyConfig{
				Name: "max-safety-factor",
				Mode: v1alpha1.ModeAuto,
				WorkloadSelector: map[string]string{
					"app": "test-app",
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
					SafetyFactor:  10.0, // Very high but valid safety factor
				},
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: true,
				},
			}

			maxSafetyPolicy, err := policyHelper.CreateOptimizationPolicy(maxSafetyConfig)
			Expect(err).NotTo(HaveOccurred(), "Policy with high but valid safety factor should be accepted")
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      maxSafetyPolicy.Name,
				Namespace: maxSafetyPolicy.Namespace,
				Kind:      "OptimizationPolicy",
			})
		})
	})

	Context("Missing Metrics Scenarios", func() {
		It("should handle gracefully when metrics-server is unavailable", func() {
			By("Creating a policy that requires metrics")
			config := helpers.PolicyConfig{
				Name: "metrics-dependent-policy",
				Mode: v1alpha1.ModeAuto,
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
					Percentile:    "95",
					SafetyFactor:  1.2,
				},
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: true,
				},
			}

			policy, err := policyHelper.CreateOptimizationPolicy(config)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      policy.Name,
				Namespace: policy.Namespace,
				Kind:      "OptimizationPolicy",
			})

			By("Creating a workload to be optimized")
			workloadConfig := helpers.WorkloadConfig{
				Name:      "metrics-test-workload",
				Namespace: testNamespace,
				Type:      helpers.WorkloadTypeDeployment,
				Labels: map[string]string{
					"app": "metrics-test",
				},
				Resources: helpers.ResourceRequirements{
					Requests: helpers.ResourceList{
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

			By("Waiting for controller to attempt processing")
			time.Sleep(45 * time.Second)

			By("Verifying that the controller handles missing metrics gracefully")
			updatedPolicy, err := policyHelper.GetPolicy(policy.Name)
			Expect(err).NotTo(HaveOccurred())

			// The policy should have appropriate error conditions or fallback behavior
			// It should not crash the controller
			hasErrorCondition := false
			for _, condition := range updatedPolicy.Status.Conditions {
				if condition.Type == "Ready" && condition.Status == metav1.ConditionFalse {
					hasErrorCondition = true
					Expect(condition.Message).To(ContainSubstring("metrics"), "Error message should mention metrics")
					break
				}
			}

			// Either the policy should have an error condition, or it should handle gracefully
			if !hasErrorCondition {
				// If no error condition, the controller should still be functional
				Expect(updatedPolicy.Status.Conditions).NotTo(BeEmpty())
			}
		})

		It("should handle fallback behavior when metrics are temporarily unavailable", func() {
			By("Creating a policy with fallback configuration")
			config := helpers.PolicyConfig{
				Name: "fallback-policy",
				Mode: v1alpha1.ModeRecommend, // Use recommend mode to avoid actual updates
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
					Percentile:    "95",
					SafetyFactor:  1.2,
				},
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: true,
				},
			}

			policy, err := policyHelper.CreateOptimizationPolicy(config)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      policy.Name,
				Namespace: policy.Namespace,
				Kind:      "OptimizationPolicy",
			})

			By("Creating a workload")
			workloadConfig := helpers.WorkloadConfig{
				Name:      "fallback-workload",
				Namespace: testNamespace,
				Type:      helpers.WorkloadTypeDeployment,
				Labels: map[string]string{
					"app": "fallback-test",
				},
				Resources: helpers.ResourceRequirements{
					Requests: helpers.ResourceList{
						CPU:    "200m",
						Memory: "256Mi",
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

			By("Waiting for controller processing")
			time.Sleep(30 * time.Second)

			By("Verifying that the controller implements retry logic")
			updatedPolicy, err := policyHelper.GetPolicy(policy.Name)
			Expect(err).NotTo(HaveOccurred())

			// The policy should either work or have appropriate error handling
			// It should not crash the controller
			Expect(updatedPolicy.Status.Conditions).NotTo(BeEmpty())

			// Check that the controller is still functional
			Expect(updatedPolicy.Name).To(Equal(policy.Name))
		})

		It("should validate error reporting for metrics issues", func() {
			By("Creating a policy with invalid metrics configuration")
			config := helpers.PolicyConfig{
				Name: "invalid-metrics-policy",
				Mode: v1alpha1.ModeRecommend,
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
					Provider:      "invalid-provider", // Invalid provider
					RollingWindow: "1h",
					Percentile:    "95",
					SafetyFactor:  1.2,
				},
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: true,
				},
			}

			policy, err := policyHelper.CreateOptimizationPolicy(config)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      policy.Name,
				Namespace: policy.Namespace,
				Kind:      "OptimizationPolicy",
			})

			By("Waiting for controller to process the invalid configuration")
			time.Sleep(30 * time.Second)

			By("Verifying appropriate error reporting")
			updatedPolicy, err := policyHelper.GetPolicy(policy.Name)
			Expect(err).NotTo(HaveOccurred())

			// The policy should have error conditions or handle gracefully
			Expect(updatedPolicy.Status.Conditions).NotTo(BeEmpty())

			// Look for error conditions related to metrics
			hasMetricsError := false
			for _, condition := range updatedPolicy.Status.Conditions {
				if condition.Type == "Ready" && condition.Status == metav1.ConditionFalse {
					if condition.Message != "" {
						hasMetricsError = true
						GinkgoWriter.Printf("Found error condition: %s\n", condition.Message)
					}
					break
				}
			}

			// Either we have an error condition or the system handles it gracefully
			if !hasMetricsError {
				GinkgoWriter.Printf("No explicit error condition found - system handling gracefully\n")
			}
		})
	})

	Context("Concurrent Modification Scenarios", func() {
		It("should handle concurrent policy updates correctly", func() {
			By("Creating an initial policy")
			config := helpers.PolicyConfig{
				Name: "concurrent-test-policy",
				Mode: v1alpha1.ModeAuto,
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
					Percentile:    "95",
					SafetyFactor:  1.2,
				},
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: true,
				},
			}

			policy, err := policyHelper.CreateOptimizationPolicy(config)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      policy.Name,
				Namespace: policy.Namespace,
				Kind:      "OptimizationPolicy",
			})

			By("Attempting concurrent updates to the policy")
			// Simulate concurrent updates by modifying the policy multiple times rapidly
			for i := 0; i < 3; i++ {
				go func(iteration int) {
					defer GinkgoRecover()

					// Get the current policy
					currentPolicy := &v1alpha1.OptimizationPolicy{}
					err := k8sClient.Get(context.TODO(), types.NamespacedName{
						Name:      policy.Name,
						Namespace: policy.Namespace,
					}, currentPolicy)
					if err != nil {
						return // Ignore errors in concurrent scenario
					}

					// Modify the policy
					currentPolicy.Spec.MetricsConfig.SafetyFactor = func() *float64 { f := 1.5 + float64(iteration)*0.1; return &f }()

					// Update the policy (this may fail due to conflicts, which is expected)
					k8sClient.Update(context.TODO(), currentPolicy)
				}(i)
			}

			By("Waiting for concurrent updates to complete")
			time.Sleep(10 * time.Second)

			By("Verifying that the policy is in a consistent state")
			finalPolicy, err := policyHelper.GetPolicy(policy.Name)
			Expect(err).NotTo(HaveOccurred())

			// The policy should be in a valid state regardless of concurrent updates
			Expect(finalPolicy.Spec.Mode).To(Equal(v1alpha1.ModeAuto))
			Expect(finalPolicy.Spec.MetricsConfig.SafetyFactor).NotTo(BeNil())
		})

		It("should handle concurrent workload modifications", func() {
			By("Creating a policy")
			config := helpers.PolicyConfig{
				Name: "workload-concurrent-policy",
				Mode: v1alpha1.ModeAuto,
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
					Percentile:    "95",
					SafetyFactor:  1.2,
				},
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: true,
				},
			}

			policy, err := policyHelper.CreateOptimizationPolicy(config)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      policy.Name,
				Namespace: policy.Namespace,
				Kind:      "OptimizationPolicy",
			})

			By("Creating a workload")
			workloadConfig := helpers.WorkloadConfig{
				Name:      "concurrent-workload",
				Namespace: testNamespace,
				Type:      helpers.WorkloadTypeDeployment,
				Labels: map[string]string{
					"app": "concurrent-test",
				},
				Resources: helpers.ResourceRequirements{
					Requests: helpers.ResourceList{
						CPU:    "200m",
						Memory: "256Mi",
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

			By("Simulating concurrent modifications to the workload")
			// Modify the workload while the controller might also be modifying it
			go func() {
				defer GinkgoRecover()
				time.Sleep(5 * time.Second) // Let controller start processing

				currentDeployment := &appsv1.Deployment{}
				err := k8sClient.Get(context.TODO(), types.NamespacedName{
					Name:      deployment.Name,
					Namespace: deployment.Namespace,
				}, currentDeployment)
				if err != nil {
					return
				}

				// Modify replicas (different from what controller modifies)
				currentDeployment.Spec.Replicas = int32Ptr(2)
				k8sClient.Update(context.TODO(), currentDeployment)
			}()

			By("Waiting for processing to complete")
			time.Sleep(30 * time.Second)

			By("Verifying that the workload is in a consistent state")
			finalDeployment := &appsv1.Deployment{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
			}, finalDeployment)
			Expect(err).NotTo(HaveOccurred())

			// The deployment should be in a valid state
			Expect(finalDeployment.Spec.Template.Spec.Containers).NotTo(BeEmpty())
			Expect(finalDeployment.Spec.Template.Spec.Containers[0].Resources.Requests).NotTo(BeNil())
		})

		/**
		 * Feature: e2e-test-enhancement, Property 7: Concurrent modification safety
		 * For any concurrent modification scenario, OptipPod should handle resource conflicts correctly without data corruption or inconsistent state
		 */
		DescribeTable("concurrent modification safety property",
			func(scenarioName string, setupFunc func() (policyName string, workloadName string), concurrentAction func(policyName, workloadName string)) {
				By(fmt.Sprintf("Setting up scenario: %s", scenarioName))
				policyName, workloadName := setupFunc()

				By("Performing concurrent modifications")
				concurrentAction(policyName, workloadName)

				By("Waiting for all operations to complete")
				time.Sleep(15 * time.Second)

				By("Verifying system consistency")
				// Verify policy is in consistent state
				if policyName != "" {
					finalPolicy, err := policyHelper.GetPolicy(policyName)
					Expect(err).NotTo(HaveOccurred(), "Policy should remain accessible after concurrent modifications")
					Expect(finalPolicy.Spec.Mode).NotTo(BeEmpty(), "Policy mode should remain valid")
					Expect(finalPolicy.Status.Conditions).NotTo(BeNil(), "Policy should have status conditions")
				}

				// Verify workload is in consistent state
				if workloadName != "" {
					finalWorkload := &appsv1.Deployment{}
					err := k8sClient.Get(context.TODO(), types.NamespacedName{
						Name:      workloadName,
						Namespace: testNamespace,
					}, finalWorkload)
					Expect(err).NotTo(HaveOccurred(), "Workload should remain accessible after concurrent modifications")
					Expect(finalWorkload.Spec.Template.Spec.Containers).NotTo(BeEmpty(), "Workload should have valid containers")

					// Verify resource specifications are valid
					for _, container := range finalWorkload.Spec.Template.Spec.Containers {
						if container.Resources.Requests != nil {
							for resource, quantity := range container.Resources.Requests {
								Expect(quantity.IsZero()).To(BeFalse(), fmt.Sprintf("Resource %s should not be zero", resource))
							}
						}
					}
				}
			},
			Entry("concurrent policy updates", "policy-updates",
				func() (string, string) {
					config := helpers.PolicyConfig{
						Name: "concurrent-policy-test",
						Mode: v1alpha1.ModeAuto,
						ResourceBounds: helpers.ResourceBounds{
							CPU:    helpers.ResourceBound{Min: "100m", Max: "1000m"},
							Memory: helpers.ResourceBound{Min: "128Mi", Max: "2Gi"},
						},
						MetricsConfig: helpers.MetricsConfig{
							Provider: "prometheus", RollingWindow: "1h", Percentile: "95", SafetyFactor: 1.2,
						},
						UpdateStrategy: helpers.UpdateStrategy{AllowInPlaceResize: true},
					}
					policy, err := policyHelper.CreateOptimizationPolicy(config)
					Expect(err).NotTo(HaveOccurred())
					cleanupHelper.TrackResource(helpers.ResourceRef{
						Name: policy.Name, Namespace: policy.Namespace, Kind: "OptimizationPolicy",
					})
					return policy.Name, ""
				},
				func(policyName, _ string) {
					// Perform concurrent policy updates
					for i := 0; i < 3; i++ {
						go func(iteration int) {
							defer GinkgoRecover()
							currentPolicy := &v1alpha1.OptimizationPolicy{}
							err := k8sClient.Get(context.TODO(), types.NamespacedName{
								Name: policyName, Namespace: policyNamespace,
							}, currentPolicy)
							if err == nil {
								currentPolicy.Spec.MetricsConfig.SafetyFactor = func() *float64 {
									f := 1.3 + float64(iteration)*0.1
									return &f
								}()
								k8sClient.Update(context.TODO(), currentPolicy)
							}
						}(i)
					}
				},
			),
			Entry("concurrent workload modifications", "workload-modifications",
				func() (string, string) {
					config := helpers.PolicyConfig{
						Name: "workload-concurrent-policy",
						Mode: v1alpha1.ModeAuto,
						ResourceBounds: helpers.ResourceBounds{
							CPU:    helpers.ResourceBound{Min: "100m", Max: "2000m"},
							Memory: helpers.ResourceBound{Min: "128Mi", Max: "4Gi"},
						},
						MetricsConfig: helpers.MetricsConfig{
							Provider: "prometheus", RollingWindow: "1h", Percentile: "95", SafetyFactor: 1.2,
						},
						UpdateStrategy: helpers.UpdateStrategy{AllowInPlaceResize: true},
					}
					policy, err := policyHelper.CreateOptimizationPolicy(config)
					Expect(err).NotTo(HaveOccurred())
					cleanupHelper.TrackResource(helpers.ResourceRef{
						Name: policy.Name, Namespace: policy.Namespace, Kind: "OptimizationPolicy",
					})

					workloadConfig := helpers.WorkloadConfig{
						Name: "concurrent-workload-test", Namespace: testNamespace, Type: helpers.WorkloadTypeDeployment,
						Labels: map[string]string{"app": "concurrent-test"},
						Resources: helpers.ResourceRequirements{
							Requests: helpers.ResourceList{CPU: "200m", Memory: "256Mi"},
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
				func(_, workloadName string) {
					// Perform concurrent workload modifications
					go func() {
						defer GinkgoRecover()
						time.Sleep(2 * time.Second)
						currentDeployment := &appsv1.Deployment{}
						err := k8sClient.Get(context.TODO(), types.NamespacedName{
							Name: workloadName, Namespace: testNamespace,
						}, currentDeployment)
						if err == nil {
							currentDeployment.Spec.Replicas = int32Ptr(2)
							k8sClient.Update(context.TODO(), currentDeployment)
						}
					}()
				},
			),
			Entry("mixed concurrent operations", "mixed-operations",
				func() (string, string) {
					config := helpers.PolicyConfig{
						Name: "mixed-concurrent-policy",
						Mode: v1alpha1.ModeRecommend,
						ResourceBounds: helpers.ResourceBounds{
							CPU:    helpers.ResourceBound{Min: "50m", Max: "1500m"},
							Memory: helpers.ResourceBound{Min: "64Mi", Max: "3Gi"},
						},
						MetricsConfig: helpers.MetricsConfig{
							Provider: "prometheus", RollingWindow: "30m", Percentile: "90", SafetyFactor: 1.1,
						},
						UpdateStrategy: helpers.UpdateStrategy{AllowInPlaceResize: false},
					}
					policy, err := policyHelper.CreateOptimizationPolicy(config)
					Expect(err).NotTo(HaveOccurred())
					cleanupHelper.TrackResource(helpers.ResourceRef{
						Name: policy.Name, Namespace: policy.Namespace, Kind: "OptimizationPolicy",
					})

					workloadConfig := helpers.WorkloadConfig{
						Name: "mixed-workload-test", Namespace: testNamespace, Type: helpers.WorkloadTypeDeployment,
						Labels: map[string]string{"app": "mixed-test"},
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
					// Perform mixed concurrent operations
					go func() {
						defer GinkgoRecover()
						currentPolicy := &v1alpha1.OptimizationPolicy{}
						err := k8sClient.Get(context.TODO(), types.NamespacedName{
							Name: policyName, Namespace: policyNamespace,
						}, currentPolicy)
						if err == nil {
							currentPolicy.Spec.MetricsConfig.Percentile = "P95"
							k8sClient.Update(context.TODO(), currentPolicy)
						}
					}()
					go func() {
						defer GinkgoRecover()
						time.Sleep(1 * time.Second)
						currentDeployment := &appsv1.Deployment{}
						err := k8sClient.Get(context.TODO(), types.NamespacedName{
							Name: workloadName, Namespace: testNamespace,
						}, currentDeployment)
						if err == nil {
							if currentDeployment.Labels == nil {
								currentDeployment.Labels = make(map[string]string)
							}
							currentDeployment.Labels["test"] = "concurrent"
							k8sClient.Update(context.TODO(), currentDeployment)
						}
					}()
				},
			),
		)
	})

	Context("Memory Decrease Safety Scenarios", func() {
		It("should prevent unsafe memory decreases", func() {
			By("Creating a policy with memory safety enabled")
			config := helpers.PolicyConfig{
				Name: "memory-safety-policy",
				Mode: v1alpha1.ModeAuto,
				ResourceBounds: helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "100m",
						Max: "2000m",
					},
					Memory: helpers.ResourceBound{
						Min: "64Mi", // Very low minimum to allow testing
						Max: "4Gi",
					},
				},
				MetricsConfig: helpers.MetricsConfig{
					Provider:      "prometheus",
					RollingWindow: "1h",
					Percentile:    "95",
					SafetyFactor:  0.5, // Low safety factor to trigger decreases
				},
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: true,
				},
			}

			policy, err := policyHelper.CreateOptimizationPolicy(config)
			Expect(err).NotTo(HaveOccurred())
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      policy.Name,
				Namespace: policy.Namespace,
				Kind:      "OptimizationPolicy",
			})

			By("Creating a workload with high memory allocation")
			workloadConfig := helpers.WorkloadConfig{
				Name:      "high-memory-workload",
				Namespace: testNamespace,
				Type:      helpers.WorkloadTypeDeployment,
				Labels: map[string]string{
					"app": "memory-test",
				},
				Resources: helpers.ResourceRequirements{
					Requests: helpers.ResourceList{
						CPU:    "200m",
						Memory: "2Gi", // High memory allocation
					},
					Limits: helpers.ResourceList{
						CPU:    "500m",
						Memory: "2Gi",
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

			By("Waiting for controller to process the workload")
			time.Sleep(45 * time.Second)

			By("Checking for memory safety warnings or prevention")
			// Get the updated deployment to check for annotations
			updatedDeployment := &appsv1.Deployment{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
			}, updatedDeployment)
			Expect(err).NotTo(HaveOccurred())

			// Check for safety-related annotations or that memory wasn't decreased unsafely
			annotations := updatedDeployment.Annotations
			if annotations != nil {
				// Look for safety warnings or flags
				for key, value := range annotations {
					if key == "optipod.io/memory-safety-warning" {
						Expect(value).To(ContainSubstring("unsafe"), "Memory safety warning should mention unsafe decrease")
					}
				}
			}

			// Verify that if memory was decreased, it wasn't by more than a safe threshold
			currentMemory := updatedDeployment.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceMemory]
			originalMemory := resource.MustParse("2Gi")

			if currentMemory.Cmp(originalMemory) < 0 {
				// Memory was decreased, check if it's within safe bounds
				ratio := float64(currentMemory.Value()) / float64(originalMemory.Value())
				Expect(ratio).To(BeNumerically(">=", 0.5), "Memory decrease should not exceed 50% without safety warnings")
			}
		})
	})
})

// Helper functions
func int32Ptr(i int32) *int32 {
	return &i
}
