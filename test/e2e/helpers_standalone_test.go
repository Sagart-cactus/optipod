//go:build e2e

package e2e

import (
	"testing"

	"github.com/optipod/optipod/test/e2e/helpers"
)

// TestHelpersStandalone tests that all helper components can be instantiated correctly
func TestHelpersStandalone(t *testing.T) {
	t.Run("Helper Component Instantiation", func(t *testing.T) {
		t.Run("should create PolicyHelper without client", func(t *testing.T) {
			policyHelper := helpers.NewPolicyHelper(nil, "test-namespace")
			if policyHelper == nil {
				t.Error("PolicyHelper should be created successfully")
			}
		})

		t.Run("should create WorkloadHelper without client", func(t *testing.T) {
			workloadHelper := helpers.NewWorkloadHelper(nil, "test-namespace")
			if workloadHelper == nil {
				t.Error("WorkloadHelper should be created successfully")
			}
		})

		t.Run("should create ValidationHelper without client", func(t *testing.T) {
			validationHelper := helpers.NewValidationHelper(nil)
			if validationHelper == nil {
				t.Error("ValidationHelper should be created successfully")
			}
		})

		t.Run("should create CleanupHelper without client", func(t *testing.T) {
			cleanupHelper := helpers.NewCleanupHelper(nil)
			if cleanupHelper == nil {
				t.Error("CleanupHelper should be created successfully")
			}
		})
	})

	t.Run("Helper Component Configuration Types", func(t *testing.T) {
		t.Run("should create PolicyConfig with all fields", func(t *testing.T) {
			config := helpers.PolicyConfig{
				Name: "test-policy",
				Mode: "Auto",
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

			if config.Name != "test-policy" {
				t.Errorf("Expected policy name 'test-policy', got '%s'", config.Name)
			}
			if config.ResourceBounds.CPU.Min != "100m" {
				t.Errorf("Expected CPU min '100m', got '%s'", config.ResourceBounds.CPU.Min)
			}
			if config.MetricsConfig.SafetyFactor != 1.2 {
				t.Errorf("Expected safety factor 1.2, got %f", config.MetricsConfig.SafetyFactor)
			}
		})

		t.Run("should create WorkloadConfig with all fields", func(t *testing.T) {
			config := helpers.WorkloadConfig{
				Name:      "test-workload",
				Namespace: "test-namespace",
				Type:      "Deployment",
				Labels: map[string]string{
					"app": "test",
				},
				Replicas: 3,
			}

			if config.Name != "test-workload" {
				t.Errorf("Expected workload name 'test-workload', got '%s'", config.Name)
			}
			if config.Replicas != 3 {
				t.Errorf("Expected replicas 3, got %d", config.Replicas)
			}
		})
	})

	t.Run("Helper Component Resource Types", func(t *testing.T) {
		t.Run("should create ResourceBounds with CPU and Memory", func(t *testing.T) {
			bounds := helpers.ResourceBounds{
				CPU: helpers.ResourceBound{
					Min: "100m",
					Max: "2000m",
				},
				Memory: helpers.ResourceBound{
					Min: "256Mi",
					Max: "4Gi",
				},
			}

			if bounds.CPU.Min != "100m" {
				t.Errorf("Expected CPU min '100m', got '%s'", bounds.CPU.Min)
			}
			if bounds.Memory.Max != "4Gi" {
				t.Errorf("Expected Memory max '4Gi', got '%s'", bounds.Memory.Max)
			}
		})

		t.Run("should create MetricsConfig with all fields", func(t *testing.T) {
			config := helpers.MetricsConfig{
				Provider:      "prometheus",
				RollingWindow: "24h",
				Percentile:    "P99",
				SafetyFactor:  1.5,
			}

			if config.Provider != "prometheus" {
				t.Errorf("Expected provider 'prometheus', got '%s'", config.Provider)
			}
			if config.Percentile != "P99" {
				t.Errorf("Expected percentile 'P99', got '%s'", config.Percentile)
			}
		})

		t.Run("should create UpdateStrategy with all fields", func(t *testing.T) {
			strategy := helpers.UpdateStrategy{
				AllowInPlaceResize: true,
				AllowRecreate:      false,
				UpdateRequestsOnly: true,
			}

			if !strategy.AllowInPlaceResize {
				t.Error("Expected AllowInPlaceResize to be true")
			}
			if strategy.AllowRecreate {
				t.Error("Expected AllowRecreate to be false")
			}
			if !strategy.UpdateRequestsOnly {
				t.Error("Expected UpdateRequestsOnly to be true")
			}
		})
	})
}
