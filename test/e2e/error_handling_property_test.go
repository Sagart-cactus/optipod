//go:build e2e
// +build e2e

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

// **Feature: e2e-test-enhancement, Property 6: Error handling robustness**
var _ = Describe("Error Handling Property Tests", Ordered, func() {
	var (
		policyNamespace string
		k8sClient       client.Client
	)

	BeforeAll(func() {
		By("setting up test environment")
		// Set up namespaces
		policyNamespace = "optipod-system"

		// Initialize Kubernetes client
		cfg, err := config.GetConfig()
		Expect(err).NotTo(HaveOccurred())

		// Add OptipPod scheme
		s := runtime.NewScheme()
		Expect(scheme.AddToScheme(s)).To(Succeed())
		Expect(v1alpha1.AddToScheme(s)).To(Succeed())

		k8sClient, err = client.New(cfg, client.Options{Scheme: s})
		Expect(err).NotTo(HaveOccurred())
	})

	// Property 6: Error handling robustness
	// For any invalid configuration or error condition, OptipPod should handle the error gracefully,
	// provide clear error messages, and maintain system stability
	DescribeTable("should handle invalid configurations robustly",
		func(configGenerator func(string) helpers.PolicyConfig, expectedErrorSubstring string) {
			By("Generating invalid configuration")
			config := configGenerator(fmt.Sprintf("error-test-%d", time.Now().Unix()))

			By("Creating OptimizationPolicy object for validation")
			policy := &v1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      config.Name,
					Namespace: policyNamespace,
				},
				Spec: v1alpha1.OptimizationPolicySpec{
					Mode: config.Mode,
					Selector: v1alpha1.WorkloadSelector{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: config.NamespaceSelector,
						},
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: config.WorkloadSelector,
						},
					},
					MetricsConfig: v1alpha1.MetricsConfig{
						Provider:      config.MetricsConfig.Provider,
						RollingWindow: parseDuration(config.MetricsConfig.RollingWindow),
						Percentile:    config.MetricsConfig.Percentile,
						SafetyFactor:  &config.MetricsConfig.SafetyFactor,
					},
					UpdateStrategy: v1alpha1.UpdateStrategy{
						AllowInPlaceResize: config.UpdateStrategy.AllowInPlaceResize,
						AllowRecreate:      config.UpdateStrategy.AllowRecreate,
						UpdateRequestsOnly: config.UpdateStrategy.UpdateRequestsOnly,
					},
				},
			}

			// Set resource bounds
			policy.Spec.ResourceBounds = v1alpha1.ResourceBounds{
				CPU: v1alpha1.ResourceBound{
					Min: resource.MustParse(config.ResourceBounds.CPU.Min),
					Max: resource.MustParse(config.ResourceBounds.CPU.Max),
				},
				Memory: v1alpha1.ResourceBound{
					Min: resource.MustParse(config.ResourceBounds.Memory.Min),
					Max: resource.MustParse(config.ResourceBounds.Memory.Max),
				},
			}

			By("Testing validation logic directly")
			err := policy.ValidateCreate()

			By("Verifying that error is handled gracefully")
			Expect(err).To(HaveOccurred(), "Invalid configuration should be rejected")
			Expect(err.Error()).To(ContainSubstring(expectedErrorSubstring),
				fmt.Sprintf("Error message should contain '%s', got: %s", expectedErrorSubstring, err.Error()))

			By("Verifying system stability after error")
			// Create a valid policy object to ensure validation logic is still functional
			validConfig := helpers.PolicyConfig{
				Name: fmt.Sprintf("valid-after-error-%d", time.Now().Unix()),
				Mode: v1alpha1.ModeRecommend,
				WorkloadSelector: map[string]string{
					"app": "test",
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

			validPolicy := &v1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      validConfig.Name,
					Namespace: policyNamespace,
				},
				Spec: v1alpha1.OptimizationPolicySpec{
					Mode: validConfig.Mode,
					Selector: v1alpha1.WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: validConfig.WorkloadSelector,
						},
					},
					MetricsConfig: v1alpha1.MetricsConfig{
						Provider:      validConfig.MetricsConfig.Provider,
						RollingWindow: parseDuration(validConfig.MetricsConfig.RollingWindow),
						Percentile:    validConfig.MetricsConfig.Percentile,
						SafetyFactor:  &validConfig.MetricsConfig.SafetyFactor,
					},
					UpdateStrategy: v1alpha1.UpdateStrategy{
						AllowInPlaceResize: validConfig.UpdateStrategy.AllowInPlaceResize,
					},
				},
			}

			validPolicy.Spec.ResourceBounds = v1alpha1.ResourceBounds{
				CPU: v1alpha1.ResourceBound{
					Min: resource.MustParse(validConfig.ResourceBounds.CPU.Min),
					Max: resource.MustParse(validConfig.ResourceBounds.CPU.Max),
				},
				Memory: v1alpha1.ResourceBound{
					Min: resource.MustParse(validConfig.ResourceBounds.Memory.Min),
					Max: resource.MustParse(validConfig.ResourceBounds.Memory.Max),
				},
			}

			err = validPolicy.ValidateCreate()
			Expect(err).NotTo(HaveOccurred(), "System should remain functional after handling invalid configuration")
		},
		Entry("CPU min greater than max", func(name string) helpers.PolicyConfig {
			return helpers.PolicyConfig{
				Name:             name,
				Mode:             v1alpha1.ModeAuto,
				WorkloadSelector: map[string]string{"app": "test"},
				ResourceBounds: helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "2000m", // 2 cores
						Max: "1000m", // 1 core - invalid!
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
		}, "CPU min"),
		Entry("Memory min greater than max", func(name string) helpers.PolicyConfig {
			return helpers.PolicyConfig{
				Name:             name,
				Mode:             v1alpha1.ModeAuto,
				WorkloadSelector: map[string]string{"app": "test"},
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
		}, "memory min"),
		Entry("Zero CPU minimum", func(name string) helpers.PolicyConfig {
			return helpers.PolicyConfig{
				Name:             name,
				Mode:             v1alpha1.ModeAuto,
				WorkloadSelector: map[string]string{"app": "test"},
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
		}, "greater than zero"),
		Entry("Invalid safety factor", func(name string) helpers.PolicyConfig {
			return helpers.PolicyConfig{
				Name:             name,
				Mode:             v1alpha1.ModeAuto,
				WorkloadSelector: map[string]string{"app": "test"},
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
		}, "safety factor"),
		Entry("Missing selectors", func(name string) helpers.PolicyConfig {
			return helpers.PolicyConfig{
				Name: name,
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
		}, "selector"),
	)

	// **Feature: e2e-test-enhancement, Property 8: Memory decrease safety**
	DescribeTable("should enforce memory decrease safety",
		func(configGenerator func(string) helpers.PolicyConfig, workloadGenerator func(string) helpers.WorkloadConfig, expectedBehavior string) {
			By("Generating test configuration")
			testName := fmt.Sprintf("memory-safety-test-%d", time.Now().Unix())
			config := configGenerator(testName)
			workloadConfig := workloadGenerator(testName)

			By("Creating test namespace")
			testNamespace := fmt.Sprintf("memory-safety-%d", time.Now().Unix())
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: testNamespace,
				},
			}
			Expect(k8sClient.Create(context.TODO(), ns)).To(Succeed())
			defer func() {
				k8sClient.Delete(context.TODO(), ns)
			}()

			By("Creating policy helper")
			policyHelper := helpers.NewPolicyHelper(k8sClient, policyNamespace)
			workloadHelper := helpers.NewWorkloadHelper(k8sClient, testNamespace)

			By("Creating optimization policy")
			policy, err := policyHelper.CreateOptimizationPolicy(config)
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				k8sClient.Delete(context.TODO(), policy)
			}()

			By("Creating workload with high memory allocation")
			workloadConfig.Namespace = testNamespace
			deployment, err := workloadHelper.CreateDeployment(workloadConfig)
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				k8sClient.Delete(context.TODO(), deployment)
			}()

			By("Waiting for controller processing")
			time.Sleep(30 * time.Second)

			By("Verifying memory safety behavior")
			updatedDeployment := &appsv1.Deployment{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
			}, updatedDeployment)
			Expect(err).NotTo(HaveOccurred())

			originalMemory := resource.MustParse(workloadConfig.Resources.Requests.Memory)
			currentMemory := updatedDeployment.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceMemory]

			switch expectedBehavior {
			case "prevent_unsafe_decrease":
				// Memory should not decrease by more than 50% without safety warnings
				if currentMemory.Cmp(originalMemory) < 0 {
					ratio := float64(currentMemory.Value()) / float64(originalMemory.Value())
					Expect(ratio).To(BeNumerically(">=", 0.5), "Memory decrease should not exceed 50% without safety warnings")

					// Check for safety warnings in annotations
					annotations := updatedDeployment.Annotations
					if ratio < 0.7 { // If significant decrease, should have warnings
						Expect(annotations).To(HaveKey("optipod.io/memory-safety-warning"), "Significant memory decrease should have safety warning")
					}
				}
			case "flag_unsafe_decrease":
				// Should have safety flags/warnings for unsafe decreases
				annotations := updatedDeployment.Annotations
				if currentMemory.Cmp(originalMemory) < 0 {
					ratio := float64(currentMemory.Value()) / float64(originalMemory.Value())
					if ratio < 0.5 {
						Expect(annotations).To(HaveKey("optipod.io/memory-safety-warning"), "Unsafe memory decrease should be flagged")
					}
				}
			case "maintain_safety_threshold":
				// Memory should not go below safety threshold
				minSafeMemory := resource.MustParse("64Mi") // Minimum safe memory
				Expect(currentMemory.Cmp(minSafeMemory)).To(BeNumerically(">=", 0), "Memory should not go below safety threshold")
			}
		},
		Entry("prevent unsafe memory decrease with high safety factor", func(name string) helpers.PolicyConfig {
			return helpers.PolicyConfig{
				Name: name,
				Mode: v1alpha1.ModeAuto,
				WorkloadSelector: map[string]string{
					"app": "memory-safety-test",
				},
				ResourceBounds: helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "100m",
						Max: "2000m",
					},
					Memory: helpers.ResourceBound{
						Min: "64Mi",
						Max: "4Gi",
					},
				},
				MetricsConfig: helpers.MetricsConfig{
					Provider:      "prometheus",
					RollingWindow: "1h",
					Percentile:    "95",
					SafetyFactor:  1.5, // High safety factor should prevent aggressive decreases
				},
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: true,
				},
			}
		}, func(name string) helpers.WorkloadConfig {
			return helpers.WorkloadConfig{
				Name: name + "-workload",
				Type: helpers.WorkloadTypeDeployment,
				Labels: map[string]string{
					"app": "memory-safety-test",
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
		}, "prevent_unsafe_decrease"),
		Entry("flag unsafe memory decrease with low safety factor", func(name string) helpers.PolicyConfig {
			return helpers.PolicyConfig{
				Name: name,
				Mode: v1alpha1.ModeRecommend, // Use recommend mode to avoid actual updates
				WorkloadSelector: map[string]string{
					"app": "memory-safety-test",
				},
				ResourceBounds: helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "50m",
						Max: "1000m",
					},
					Memory: helpers.ResourceBound{
						Min: "32Mi",
						Max: "2Gi",
					},
				},
				MetricsConfig: helpers.MetricsConfig{
					Provider:      "prometheus",
					RollingWindow: "1h",
					Percentile:    "50", // Low percentile might suggest lower memory
					SafetyFactor:  0.8,  // Low safety factor might trigger decreases
				},
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: true,
				},
			}
		}, func(name string) helpers.WorkloadConfig {
			return helpers.WorkloadConfig{
				Name: name + "-workload",
				Type: helpers.WorkloadTypeDeployment,
				Labels: map[string]string{
					"app": "memory-safety-test",
				},
				Resources: helpers.ResourceRequirements{
					Requests: helpers.ResourceList{
						CPU:    "100m",
						Memory: "1Gi", // Moderate memory allocation
					},
					Limits: helpers.ResourceList{
						CPU:    "200m",
						Memory: "1Gi",
					},
				},
				Replicas: 1,
			}
		}, "flag_unsafe_decrease"),
		Entry("maintain safety threshold with minimal bounds", func(name string) helpers.PolicyConfig {
			return helpers.PolicyConfig{
				Name: name,
				Mode: v1alpha1.ModeAuto,
				WorkloadSelector: map[string]string{
					"app": "memory-safety-test",
				},
				ResourceBounds: helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "10m",
						Max: "500m",
					},
					Memory: helpers.ResourceBound{
						Min: "64Mi", // Minimum safety threshold
						Max: "1Gi",
					},
				},
				MetricsConfig: helpers.MetricsConfig{
					Provider:      "prometheus",
					RollingWindow: "30m",
					Percentile:    "50",
					SafetyFactor:  0.9, // Low safety factor
				},
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: true,
				},
			}
		}, func(name string) helpers.WorkloadConfig {
			return helpers.WorkloadConfig{
				Name: name + "-workload",
				Type: helpers.WorkloadTypeDeployment,
				Labels: map[string]string{
					"app": "memory-safety-test",
				},
				Resources: helpers.ResourceRequirements{
					Requests: helpers.ResourceList{
						CPU:    "50m",
						Memory: "128Mi", // Small memory allocation
					},
					Limits: helpers.ResourceList{
						CPU:    "100m",
						Memory: "128Mi",
					},
				},
				Replicas: 1,
			}
		}, "maintain_safety_threshold"),
	)
})

// parseDuration parses a duration string and returns a metav1.Duration
func parseDuration(durationStr string) metav1.Duration {
	if durationStr == "" {
		return metav1.Duration{Duration: time.Hour} // Default to 1h
	}

	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		return metav1.Duration{Duration: time.Hour} // Default to 1h on error
	}

	return metav1.Duration{Duration: duration}
}
