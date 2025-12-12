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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/optipod/optipod/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Invalid Configuration Unit Tests", func() {
	Context("Policy Validation", func() {
		It("should reject policies with invalid CPU resource bounds (min > max)", func() {
			policy := &v1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-cpu-bounds",
					Namespace: "test",
				},
				Spec: v1alpha1.OptimizationPolicySpec{
					Mode: v1alpha1.ModeAuto,
					Selector: v1alpha1.WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
					},
					MetricsConfig: v1alpha1.MetricsConfig{
						Provider:   "prometheus",
						Percentile: "P90",
					},
					ResourceBounds: v1alpha1.ResourceBounds{
						CPU: v1alpha1.ResourceBound{
							Min: resource.MustParse("2000m"), // 2 cores
							Max: resource.MustParse("1000m"), // 1 core - invalid!
						},
						Memory: v1alpha1.ResourceBound{
							Min: resource.MustParse("1Gi"),
							Max: resource.MustParse("2Gi"),
						},
					},
					UpdateStrategy: v1alpha1.UpdateStrategy{
						AllowInPlaceResize: true,
					},
				},
			}

			err := policy.ValidateCreate()
			Expect(err).To(HaveOccurred(), "Policy creation should fail with invalid CPU bounds")
			Expect(err.Error()).To(ContainSubstring("CPU min"), "Error should mention CPU min/max validation")
			Expect(err.Error()).To(ContainSubstring("max"), "Error should mention max validation")
		})

		It("should reject policies with invalid memory resource bounds (min > max)", func() {
			policy := &v1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-memory-bounds",
					Namespace: "test",
				},
				Spec: v1alpha1.OptimizationPolicySpec{
					Mode: v1alpha1.ModeAuto,
					Selector: v1alpha1.WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
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
							Min: resource.MustParse("4Gi"), // 4GB
							Max: resource.MustParse("2Gi"), // 2GB - invalid!
						},
					},
					UpdateStrategy: v1alpha1.UpdateStrategy{
						AllowInPlaceResize: true,
					},
				},
			}

			err := policy.ValidateCreate()
			Expect(err).To(HaveOccurred(), "Policy creation should fail with invalid memory bounds")
			Expect(err.Error()).To(ContainSubstring("memory min"), "Error should mention memory min/max validation")
			Expect(err.Error()).To(ContainSubstring("max"), "Error should mention max validation")
		})

		It("should reject policies with zero resource bounds", func() {
			policy := &v1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "zero-cpu-min",
					Namespace: "test",
				},
				Spec: v1alpha1.OptimizationPolicySpec{
					Mode: v1alpha1.ModeAuto,
					Selector: v1alpha1.WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
					},
					MetricsConfig: v1alpha1.MetricsConfig{
						Provider:   "prometheus",
						Percentile: "P90",
					},
					ResourceBounds: v1alpha1.ResourceBounds{
						CPU: v1alpha1.ResourceBound{
							Min: resource.MustParse("0m"), // Zero CPU - invalid!
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

			err := policy.ValidateCreate()
			Expect(err).To(HaveOccurred(), "Policy creation should fail with zero CPU bounds")
			Expect(err.Error()).To(ContainSubstring("greater than zero"), "Error should mention zero validation")
		})

		It("should reject policies with invalid safety factor", func() {
			policy := &v1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-safety-factor",
					Namespace: "test",
				},
				Spec: v1alpha1.OptimizationPolicySpec{
					Mode: v1alpha1.ModeAuto,
					Selector: v1alpha1.WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
					},
					MetricsConfig: v1alpha1.MetricsConfig{
						Provider:     "prometheus",
						Percentile:   "P90",
						SafetyFactor: func() *float64 { f := 0.5; return &f }(), // Invalid - must be >= 1.0
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

			err := policy.ValidateCreate()
			Expect(err).To(HaveOccurred(), "Policy creation should fail with invalid safety factor")
			Expect(err.Error()).To(ContainSubstring("safety factor"), "Error should mention safety factor validation")
		})

		It("should reject policies without selectors", func() {
			policy := &v1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-selectors",
					Namespace: "test",
				},
				Spec: v1alpha1.OptimizationPolicySpec{
					Mode:     v1alpha1.ModeAuto,
					Selector: v1alpha1.WorkloadSelector{
						// No selectors specified - should be required
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

			err := policy.ValidateCreate()
			Expect(err).To(HaveOccurred(), "Policy creation should fail without selectors")
			Expect(err.Error()).To(ContainSubstring("selector"), "Error should mention selector requirement")
		})

		It("should accept policies with valid configuration edge cases", func() {
			By("Testing policy with extremely large resource bounds")
			largePolicy := &v1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "large-bounds",
					Namespace: "test",
				},
				Spec: v1alpha1.OptimizationPolicySpec{
					Mode: v1alpha1.ModeAuto,
					Selector: v1alpha1.WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
					},
					MetricsConfig: v1alpha1.MetricsConfig{
						Provider:   "prometheus",
						Percentile: "P90",
					},
					ResourceBounds: v1alpha1.ResourceBounds{
						CPU: v1alpha1.ResourceBound{
							Min: resource.MustParse("1m"),
							Max: resource.MustParse("1000000m"), // 1000 cores - very large but valid
						},
						Memory: v1alpha1.ResourceBound{
							Min: resource.MustParse("1Mi"),
							Max: resource.MustParse("1000Gi"), // 1TB - very large but valid
						},
					},
					UpdateStrategy: v1alpha1.UpdateStrategy{
						AllowInPlaceResize: true,
					},
				},
			}

			err := largePolicy.ValidateCreate()
			Expect(err).NotTo(HaveOccurred(), "Policy with large but valid bounds should be accepted")

			By("Testing policy with very small resource bounds")
			smallPolicy := &v1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "small-bounds",
					Namespace: "test",
				},
				Spec: v1alpha1.OptimizationPolicySpec{
					Mode: v1alpha1.ModeAuto,
					Selector: v1alpha1.WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
					},
					MetricsConfig: v1alpha1.MetricsConfig{
						Provider:   "prometheus",
						Percentile: "P90",
					},
					ResourceBounds: v1alpha1.ResourceBounds{
						CPU: v1alpha1.ResourceBound{
							Min: resource.MustParse("1m"),  // 1 millicore - very small but valid
							Max: resource.MustParse("10m"), // 10 millicores
						},
						Memory: v1alpha1.ResourceBound{
							Min: resource.MustParse("1Mi"),  // 1MB - very small but valid
							Max: resource.MustParse("10Mi"), // 10MB
						},
					},
					UpdateStrategy: v1alpha1.UpdateStrategy{
						AllowInPlaceResize: true,
					},
				},
			}

			err = smallPolicy.ValidateCreate()
			Expect(err).NotTo(HaveOccurred(), "Policy with small but valid bounds should be accepted")

			By("Testing policy with maximum safety factor")
			maxSafetyPolicy := &v1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "max-safety-factor",
					Namespace: "test",
				},
				Spec: v1alpha1.OptimizationPolicySpec{
					Mode: v1alpha1.ModeAuto,
					Selector: v1alpha1.WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
					},
					MetricsConfig: v1alpha1.MetricsConfig{
						Provider:     "prometheus",
						Percentile:   "P90",
						SafetyFactor: func() *float64 { f := 10.0; return &f }(), // Very high but valid safety factor
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

			err = maxSafetyPolicy.ValidateCreate()
			Expect(err).NotTo(HaveOccurred(), "Policy with high but valid safety factor should be accepted")
		})
	})
})

// TestInvalidConfigurationUnit is handled by the main e2e suite
