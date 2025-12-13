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
	"testing"

	"github.com/optipod/optipod/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestInvalidConfigurationStandalone tests invalid policy configurations without requiring Kubernetes
func TestInvalidConfigurationStandalone(t *testing.T) {
	t.Run("Policy Validation", func(t *testing.T) {
		t.Run("should reject policies with invalid CPU resource bounds (min > max)", func(t *testing.T) {
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
			if err == nil {
				t.Error("Policy creation should fail with invalid CPU bounds")
			} else {
				errorMsg := err.Error()
				if !containsSubstring(errorMsg, "CPU min") {
					t.Errorf("Error should mention CPU min/max validation, got: %s", errorMsg)
				}
				if !containsSubstring(errorMsg, "max") {
					t.Errorf("Error should mention max validation, got: %s", errorMsg)
				}
			}
		})

		t.Run("should reject policies with invalid memory resource bounds (min > max)", func(t *testing.T) {
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
			if err == nil {
				t.Error("Policy creation should fail with invalid memory bounds")
			} else {
				errorMsg := err.Error()
				if !containsSubstring(errorMsg, "memory min") {
					t.Errorf("Error should mention memory min/max validation, got: %s", errorMsg)
				}
				if !containsSubstring(errorMsg, "max") {
					t.Errorf("Error should mention max validation, got: %s", errorMsg)
				}
			}
		})

		t.Run("should reject policies with zero resource bounds", func(t *testing.T) {
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
			if err == nil {
				t.Error("Policy creation should fail with zero CPU bounds")
			} else {
				errorMsg := err.Error()
				if !containsSubstring(errorMsg, "greater than zero") {
					t.Errorf("Error should mention zero validation, got: %s", errorMsg)
				}
			}
		})

		t.Run("should reject policies with invalid safety factor", func(t *testing.T) {
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
			if err == nil {
				t.Error("Policy creation should fail with invalid safety factor")
			} else {
				errorMsg := err.Error()
				if !containsSubstring(errorMsg, "safety factor") {
					t.Errorf("Error should mention safety factor validation, got: %s", errorMsg)
				}
			}
		})

		t.Run("should reject policies without selectors", func(t *testing.T) {
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
			if err == nil {
				t.Error("Policy creation should fail without selectors")
			} else {
				errorMsg := err.Error()
				if !containsSubstring(errorMsg, "selector") {
					t.Errorf("Error should mention selector requirement, got: %s", errorMsg)
				}
			}
		})

		t.Run("should accept policies with valid configuration edge cases", func(t *testing.T) {
			t.Run("Testing policy with extremely large resource bounds", func(t *testing.T) {
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
				if err != nil {
					t.Errorf("Policy with large but valid bounds should be accepted, got error: %v", err)
				}
			})

			t.Run("Testing policy with very small resource bounds", func(t *testing.T) {
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

				err := smallPolicy.ValidateCreate()
				if err != nil {
					t.Errorf("Policy with small but valid bounds should be accepted, got error: %v", err)
				}
			})

			t.Run("Testing policy with maximum safety factor", func(t *testing.T) {
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

				err := maxSafetyPolicy.ValidateCreate()
				if err != nil {
					t.Errorf("Policy with high but valid safety factor should be accepted, got error: %v", err)
				}
			})
		})
	})
}

// containsSubstring checks if a string contains a substring
func containsSubstring(str, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(str) < len(substr) {
		return false
	}
	for i := 0; i <= len(str)-len(substr); i++ {
		if str[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
