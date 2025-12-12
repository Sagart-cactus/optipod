//go:build e2e
// +build e2e

package e2e

import (
	"testing"

	"github.com/optipod/optipod/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestValidationLogic tests the OptimizationPolicy validation logic directly
func TestValidationLogic(t *testing.T) {
	tests := []struct {
		name          string
		policy        *v1alpha1.OptimizationPolicy
		expectedError string
	}{
		{
			name: "CPU min greater than max",
			policy: &v1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "optipod-system",
				},
				Spec: v1alpha1.OptimizationPolicySpec{
					Mode: v1alpha1.ModeAuto,
					Selector: v1alpha1.WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
					},
					MetricsConfig: v1alpha1.MetricsConfig{
						Provider: "prometheus",
					},
					ResourceBounds: v1alpha1.ResourceBounds{
						CPU: v1alpha1.ResourceBound{
							Min: resource.MustParse("2000m"), // 2 cores
							Max: resource.MustParse("1000m"), // 1 core - invalid!
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
			},
			expectedError: "CPU min",
		},
		{
			name: "Memory min greater than max",
			policy: &v1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "optipod-system",
				},
				Spec: v1alpha1.OptimizationPolicySpec{
					Mode: v1alpha1.ModeAuto,
					Selector: v1alpha1.WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
					},
					MetricsConfig: v1alpha1.MetricsConfig{
						Provider: "prometheus",
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
			},
			expectedError: "memory min",
		},
		{
			name: "Missing selectors",
			policy: &v1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "optipod-system",
				},
				Spec: v1alpha1.OptimizationPolicySpec{
					Mode: v1alpha1.ModeAuto,
					// No selectors specified - should be required
					MetricsConfig: v1alpha1.MetricsConfig{
						Provider: "prometheus",
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
			},
			expectedError: "selector",
		},
		{
			name: "Valid policy should pass",
			policy: &v1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "optipod-system",
				},
				Spec: v1alpha1.OptimizationPolicySpec{
					Mode: v1alpha1.ModeRecommend,
					Selector: v1alpha1.WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
					},
					MetricsConfig: v1alpha1.MetricsConfig{
						Provider: "prometheus",
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
			},
			expectedError: "", // No error expected
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.policy.ValidateCreate()

			if tt.expectedError == "" {
				// Valid case - should not error
				if err != nil {
					t.Errorf("Expected no error for valid policy, got: %v", err)
				}
			} else {
				// Invalid case - should error with expected substring
				if err == nil {
					t.Errorf("Expected error containing '%s', but got no error", tt.expectedError)
				} else if err.Error() == "" || len(err.Error()) == 0 {
					t.Errorf("Expected error containing '%s', but got empty error", tt.expectedError)
				} else {
					// Check if error message contains expected substring
					found := false
					errorMsg := err.Error()
					if len(errorMsg) > 0 {
						// Simple substring check
						for i := 0; i <= len(errorMsg)-len(tt.expectedError); i++ {
							if errorMsg[i:i+len(tt.expectedError)] == tt.expectedError {
								found = true
								break
							}
						}
					}
					if !found {
						t.Errorf("Expected error containing '%s', got: %s", tt.expectedError, errorMsg)
					}
				}
			}
		})
	}
}
