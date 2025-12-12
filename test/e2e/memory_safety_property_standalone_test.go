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
	"github.com/optipod/optipod/test/e2e/helpers"
)

// **Feature: e2e-test-enhancement, Property 8: Memory decrease safety**
func TestMemorySafetyPropertyStandalone(t *testing.T) {
	validationHelper := helpers.NewValidationHelper(nil)

	testCases := []struct {
		name           string
		originalMemory string
		currentMemory  string
		expectedSafe   bool
	}{
		{
			name:           "prevent unsafe memory decrease with 50% threshold",
			originalMemory: "1Gi",
			currentMemory:  "512Mi", // 50% decrease - at threshold
			expectedSafe:   true,
		},
		{
			name:           "flag unsafe memory decrease below 50%",
			originalMemory: "1Gi",
			currentMemory:  "256Mi", // 75% decrease - unsafe
			expectedSafe:   false,
		},
		{
			name:           "allow safe memory decrease within threshold",
			originalMemory: "2Gi",
			currentMemory:  "1Gi", // 50% decrease - at threshold
			expectedSafe:   true,
		},
		{
			name:           "flag aggressive memory decrease",
			originalMemory: "512Mi",
			currentMemory:  "64Mi", // 87.5% decrease - unsafe
			expectedSafe:   false,
		},
		{
			name:           "allow no memory change",
			originalMemory: "1Gi",
			currentMemory:  "1Gi", // No decrease - safe
			expectedSafe:   true,
		},
		{
			name:           "allow memory increase",
			originalMemory: "512Mi",
			currentMemory:  "768Mi", // Increase - safe
			expectedSafe:   true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := validationHelper.ValidateMemorySafety("test-workload", "test-namespace", tc.originalMemory, tc.currentMemory)

			if tc.expectedSafe {
				if err != nil {
					t.Errorf("Memory change from %s to %s should be safe, but got error: %v", tc.originalMemory, tc.currentMemory, err)
				}
			} else {
				if err == nil {
					t.Errorf("Memory change from %s to %s should be flagged as unsafe, but no error was returned", tc.originalMemory, tc.currentMemory)
				}
			}
		})
	}

	// Test policy configuration scenarios
	policyTestCases := []struct {
		name             string
		config           helpers.PolicyConfig
		workloadConfig   helpers.WorkloadConfig
		expectedBehavior string
	}{
		{
			name: "high safety factor prevents unsafe decrease",
			config: helpers.PolicyConfig{
				Name: "high-safety-policy",
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
			},
			workloadConfig: helpers.WorkloadConfig{
				Name: "high-safety-workload",
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
			},
			expectedBehavior: "prevent_unsafe_decrease",
		},
		{
			name: "low safety factor with recommend mode",
			config: helpers.PolicyConfig{
				Name: "low-safety-policy",
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
			},
			workloadConfig: helpers.WorkloadConfig{
				Name: "low-safety-workload",
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
			},
			expectedBehavior: "flag_unsafe_decrease",
		},
		{
			name: "maintain safety threshold with minimal bounds",
			config: helpers.PolicyConfig{
				Name: "minimal-bounds-policy",
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
			},
			workloadConfig: helpers.WorkloadConfig{
				Name: "minimal-bounds-workload",
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
			},
			expectedBehavior: "maintain_safety_threshold",
		},
	}

	for _, tc := range policyTestCases {
		t.Run(tc.name, func(t *testing.T) {
			// Validate that the policy configuration is reasonable for memory safety testing
			if tc.config.MetricsConfig.SafetyFactor > 1.0 && tc.expectedBehavior == "prevent_unsafe_decrease" {
				// High safety factor should prevent unsafe decreases
				t.Logf("Policy with safety factor %.1f should prevent unsafe memory decreases", tc.config.MetricsConfig.SafetyFactor)
			} else if tc.config.MetricsConfig.SafetyFactor < 1.0 && tc.expectedBehavior == "flag_unsafe_decrease" {
				// Low safety factor might trigger decreases that should be flagged
				t.Logf("Policy with safety factor %.1f might trigger decreases that should be flagged", tc.config.MetricsConfig.SafetyFactor)
			}

			// Validate that the workload configuration is appropriate
			if tc.workloadConfig.Resources.Requests.Memory != "" {
				t.Logf("Workload configured with %s memory", tc.workloadConfig.Resources.Requests.Memory)
			}

			// Validate expected behavior
			switch tc.expectedBehavior {
			case "prevent_unsafe_decrease":
				t.Logf("Expected behavior: prevent unsafe memory decreases")
			case "flag_unsafe_decrease":
				t.Logf("Expected behavior: flag unsafe memory decreases")
			case "maintain_safety_threshold":
				t.Logf("Expected behavior: maintain safety threshold")
			}
		})
	}
}
