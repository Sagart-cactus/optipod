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
	"fmt"
	"testing"
	"time"

	"github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/helpers"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// **Feature: e2e-test-enhancement, Property 6: Error handling robustness**
// TestErrorHandlingPropertyStandalone tests the property-based error handling robustness
// For any invalid configuration or error condition, OptipPod should handle the error gracefully,
// provide clear error messages, and maintain system stability
func TestErrorHandlingPropertyStandalone(t *testing.T) {
	policyNamespace := "optipod-system"

	testCases := []struct {
		name                   string
		configGenerator        func(string) helpers.PolicyConfig
		expectedErrorSubstring string
	}{
		{
			name: "CPU min greater than max",
			configGenerator: func(name string) helpers.PolicyConfig {
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
			},
			expectedErrorSubstring: "CPU min",
		},
		{
			name: "Memory min greater than max",
			configGenerator: func(name string) helpers.PolicyConfig {
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
			},
			expectedErrorSubstring: "memory min",
		},
		{
			name: "Zero CPU minimum",
			configGenerator: func(name string) helpers.PolicyConfig {
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
			},
			expectedErrorSubstring: "greater than zero",
		},
		{
			name: "Invalid safety factor",
			configGenerator: func(name string) helpers.PolicyConfig {
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
			},
			expectedErrorSubstring: "safety factor",
		},
		{
			name: "Missing selectors",
			configGenerator: func(name string) helpers.PolicyConfig {
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
			},
			expectedErrorSubstring: "selector",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Generate invalid configuration
			config := tc.configGenerator(fmt.Sprintf("error-test-%d", time.Now().Unix()))

			// Create OptimizationPolicy object for validation
			policy := &v1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      config.Name,
					Namespace: policyNamespace,
				},
				Spec: v1alpha1.OptimizationPolicySpec{
					Mode:     config.Mode,
					Selector: v1alpha1.WorkloadSelector{
						// Only set selectors if they have values
					},
					MetricsConfig: v1alpha1.MetricsConfig{
						Provider:      config.MetricsConfig.Provider,
						RollingWindow: parseDurationStandalone(config.MetricsConfig.RollingWindow),
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

			// Set selectors only if they have values
			if len(config.NamespaceSelector) > 0 {
				policy.Spec.Selector.NamespaceSelector = &metav1.LabelSelector{
					MatchLabels: config.NamespaceSelector,
				}
			}
			if len(config.WorkloadSelector) > 0 {
				policy.Spec.Selector.WorkloadSelector = &metav1.LabelSelector{
					MatchLabels: config.WorkloadSelector,
				}
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

			// Test validation logic directly
			err := policy.ValidateCreate()

			// Verify that error is handled gracefully
			if err == nil {
				t.Errorf("Expected error for invalid configuration, but got none")
				return
			}

			errorMsg := err.Error()
			if errorMsg == "" {
				t.Errorf("Expected error message containing '%s', but got empty error", tc.expectedErrorSubstring)
				return
			}

			// Check if error message contains expected substring
			found := false
			for i := 0; i <= len(errorMsg)-len(tc.expectedErrorSubstring); i++ {
				if errorMsg[i:i+len(tc.expectedErrorSubstring)] == tc.expectedErrorSubstring {
					found = true
					break
				}
			}

			if !found {
				t.Errorf("Error message should contain '%s', got: %s", tc.expectedErrorSubstring, errorMsg)
			}

			// Verify system stability after error by testing a valid policy
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
						RollingWindow: parseDurationStandalone(validConfig.MetricsConfig.RollingWindow),
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
			if err != nil {
				t.Errorf("System should remain functional after handling invalid configuration, but got error: %v", err)
			}
		})
	}
}

// parseDurationStandalone parses a duration string and returns a metav1.Duration
func parseDurationStandalone(durationStr string) metav1.Duration {
	if durationStr == "" {
		return metav1.Duration{Duration: time.Hour} // Default to 1h
	}

	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		return metav1.Duration{Duration: time.Hour} // Default to 1h on error
	}

	return metav1.Duration{Duration: duration}
}
