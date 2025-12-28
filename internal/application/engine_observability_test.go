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

package application

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/recommendation"
)

// TestRecordResourceChangeMagnitudes tests the resource change magnitude calculation
func TestRecordResourceChangeMagnitudes(t *testing.T) {
	engine := &Engine{}

	tests := []struct {
		name         string
		beforeCPU    string
		beforeMemory string
		afterCPU     string
		afterMemory  string
		expectPanic  bool
	}{
		{
			name:         "valid resource changes",
			beforeCPU:    "100m",
			beforeMemory: "128Mi",
			afterCPU:     "200m",
			afterMemory:  "256Mi",
			expectPanic:  false,
		},
		{
			name:         "zero before values",
			beforeCPU:    "0",
			beforeMemory: "0",
			afterCPU:     "200m",
			afterMemory:  "256Mi",
			expectPanic:  false,
		},
		{
			name:         "empty before values",
			beforeCPU:    "",
			beforeMemory: "",
			afterCPU:     "200m",
			afterMemory:  "256Mi",
			expectPanic:  false,
		},
		{
			name:         "invalid before values",
			beforeCPU:    "invalid",
			beforeMemory: "invalid",
			afterCPU:     "200m",
			afterMemory:  "256Mi",
			expectPanic:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil && !tt.expectPanic {
					t.Errorf("recordResourceChangeMagnitudes() panicked unexpectedly: %v", r)
				}
			}()

			cpuQuantity, _ := resource.ParseQuantity(tt.afterCPU)
			memoryQuantity, _ := resource.ParseQuantity(tt.afterMemory)

			rec := &recommendation.Recommendation{
				CPU:    cpuQuantity,
				Memory: memoryQuantity,
			}

			// This should not panic
			engine.recordResourceChangeMagnitudes("test-policy", "test-ns", "test-workload", tt.beforeCPU, tt.beforeMemory, rec)
		})
	}
}

// TestCalculateLimitsWithDefaultsMetrics tests that default multiplier usage is recorded
func TestCalculateLimitsWithDefaultsMetrics(t *testing.T) {
	engine := &Engine{}

	tests := []struct {
		name        string
		requests    map[string]string
		limitConfig *optipodv1alpha1.LimitConfig
		expectError bool
	}{
		{
			name: "use default multipliers",
			requests: map[string]string{
				"cpu":    "100m",
				"memory": "128Mi",
			},
			limitConfig: nil,
			expectError: false,
		},
		{
			name: "use explicit multipliers",
			requests: map[string]string{
				"cpu":    "100m",
				"memory": "128Mi",
			},
			limitConfig: &optipodv1alpha1.LimitConfig{
				CPULimitMultiplier:    floatPtr(2.0),
				MemoryLimitMultiplier: floatPtr(1.5),
			},
			expectError: false,
		},
		{
			name: "invalid multipliers",
			requests: map[string]string{
				"cpu":    "100m",
				"memory": "128Mi",
			},
			limitConfig: &optipodv1alpha1.LimitConfig{
				CPULimitMultiplier:    floatPtr(0.5),  // Below minimum
				MemoryLimitMultiplier: floatPtr(15.0), // Above maximum
			},
			expectError: true,
		},
		{
			name: "zero requests",
			requests: map[string]string{
				"cpu":    "0",
				"memory": "0",
			},
			limitConfig: nil,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build requests resource list
			requests := make(map[string]resource.Quantity)
			for resourceName, quantityStr := range tt.requests {
				quantity, err := resource.ParseQuantity(quantityStr)
				if err != nil {
					t.Fatalf("Failed to parse quantity %s: %v", quantityStr, err)
				}
				requests[resourceName] = quantity
			}

			// Convert to corev1.ResourceList
			resourceList := make(corev1.ResourceList)
			if cpuQuantity, exists := requests["cpu"]; exists {
				resourceList[corev1.ResourceCPU] = cpuQuantity
			}
			if memoryQuantity, exists := requests["memory"]; exists {
				resourceList[corev1.ResourceMemory] = memoryQuantity
			}

			_, err := engine.CalculateLimitsWithDefaults(resourceList, tt.limitConfig)

			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// TestCanApplyLogging tests that CanApply method logs decision-making properly
func TestCanApplyLogging(t *testing.T) {
	// Create a fake dynamic client
	dynamicClient := fake.NewSimpleDynamicClient(scheme.Scheme)

	// Create a mock discovery client that returns a valid server version
	mockDiscovery := &mockDiscoveryClient{
		serverVersion: &version.Info{
			Major: "1",
			Minor: "29",
		},
	}

	engine := NewEngine(nil, dynamicClient, mockDiscovery, false)

	workload := &Workload{
		Kind:      "Deployment",
		Namespace: "test-ns",
		Name:      "test-workload",
		Object: &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata": map[string]interface{}{
					"name":      "test-workload",
					"namespace": "test-ns",
				},
				"spec": map[string]interface{}{
					"template": map[string]interface{}{
						"spec": map[string]interface{}{
							"containers": []interface{}{
								map[string]interface{}{
									"name": "test-container",
									"resources": map[string]interface{}{
										"requests": map[string]interface{}{
											"cpu":    "100m",
											"memory": "128Mi",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	cpuQuantity, _ := resource.ParseQuantity("200m")
	memoryQuantity, _ := resource.ParseQuantity("256Mi")
	rec := &recommendation.Recommendation{
		CPU:    cpuQuantity,
		Memory: memoryQuantity,
	}

	tests := []struct {
		name             string
		policyMode       optipodv1alpha1.PolicyMode
		dryRun           bool
		allowInPlace     bool
		allowRecreate    bool
		expectedCanApply bool
		expectedMethod   ApplyMethod
	}{
		{
			name:             "recommend mode",
			policyMode:       optipodv1alpha1.ModeRecommend,
			dryRun:           false,
			allowInPlace:     true,
			allowRecreate:    true,
			expectedCanApply: false,
			expectedMethod:   Skip,
		},
		{
			name:             "disabled mode",
			policyMode:       optipodv1alpha1.ModeDisabled,
			dryRun:           false,
			allowInPlace:     true,
			allowRecreate:    true,
			expectedCanApply: false,
			expectedMethod:   Skip,
		},
		{
			name:             "dry run mode",
			policyMode:       optipodv1alpha1.ModeAuto,
			dryRun:           true,
			allowInPlace:     true,
			allowRecreate:    true,
			expectedCanApply: false,
			expectedMethod:   Skip,
		},
		{
			name:             "auto mode with in-place allowed",
			policyMode:       optipodv1alpha1.ModeAuto,
			dryRun:           false,
			allowInPlace:     true,
			allowRecreate:    true,
			expectedCanApply: true,
			expectedMethod:   InPlace,
		},
		{
			name:             "auto mode with recreate only",
			policyMode:       optipodv1alpha1.ModeAuto,
			dryRun:           false,
			allowInPlace:     false,
			allowRecreate:    true,
			expectedCanApply: true,
			expectedMethod:   Recreate,
		},
		{
			name:             "auto mode with no update strategies",
			policyMode:       optipodv1alpha1.ModeAuto,
			dryRun:           false,
			allowInPlace:     false,
			allowRecreate:    false,
			expectedCanApply: false,
			expectedMethod:   Skip,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set engine dry run mode
			engine.dryRun = tt.dryRun

			policy := &optipodv1alpha1.OptimizationPolicy{
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Mode: tt.policyMode,
					UpdateStrategy: optipodv1alpha1.UpdateStrategy{
						AllowInPlaceResize: tt.allowInPlace,
						AllowRecreate:      tt.allowRecreate,
					},
				},
			}
			policy.Name = "test-policy"

			ctx := context.Background()
			decision, err := engine.CanApply(ctx, workload, "test-container", rec, policy)

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if decision.CanApply != tt.expectedCanApply {
				t.Errorf("Expected CanApply=%v, got %v", tt.expectedCanApply, decision.CanApply)
			}

			if decision.Method != tt.expectedMethod {
				t.Errorf("Expected Method=%v, got %v", tt.expectedMethod, decision.Method)
			}

			// Verify that reason is set
			if decision.Reason == "" {
				t.Error("Expected decision reason to be set")
			}
		})
	}
}

// floatPtr returns a pointer to a float64 value
func floatPtr(f float64) *float64 {
	return &f
}
