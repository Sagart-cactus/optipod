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
	"encoding/json"
	"fmt"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/recommendation"
)

// mockDiscoveryClient is a mock implementation of discovery.DiscoveryInterface
type mockDiscoveryClient struct {
	discovery.DiscoveryInterface
	serverVersion *version.Info
}

func (m *mockDiscoveryClient) ServerVersion() (*version.Info, error) {
	return m.serverVersion, nil
}

// Feature: k8s-workload-rightsizing, Property 20: Feature gate detection
// For any Kubernetes cluster version 1.29 or higher, the system should correctly detect
// whether the InPlacePodVerticalScaling feature gate is enabled.
// Validates: Requirements 8.1
func TestProperty_FeatureGateDetection(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("versions >= 1.29 should support in-place resize", prop.ForAll(
		func(minor int) bool {
			// Generate versions from 1.29 to 1.50
			if minor < 29 || minor > 50 {
				return true // Skip invalid range
			}

			mockDiscovery := &mockDiscoveryClient{
				serverVersion: &version.Info{
					Major: "1",
					Minor: fmt.Sprintf("%d", minor),
				},
			}

			engine := &Engine{
				discoveryClient: mockDiscovery,
			}

			supported, err := engine.detectInPlaceResize(context.Background())
			if err != nil {
				return false
			}

			// Versions >= 1.29 should support in-place resize
			return supported == true
		},
		gen.IntRange(29, 50),
	))

	properties.Property("versions < 1.29 should not support in-place resize", prop.ForAll(
		func(minor int) bool {
			// Generate versions from 1.20 to 1.28
			if minor < 20 || minor >= 29 {
				return true // Skip invalid range
			}

			mockDiscovery := &mockDiscoveryClient{
				serverVersion: &version.Info{
					Major: "1",
					Minor: fmt.Sprintf("%d", minor),
				},
			}

			engine := &Engine{
				discoveryClient: mockDiscovery,
			}

			supported, err := engine.detectInPlaceResize(context.Background())
			if err != nil {
				return false
			}

			// Versions < 1.29 should not support in-place resize
			return supported == false
		},
		gen.IntRange(20, 28),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: k8s-workload-rightsizing, Property 21: In-place resize preference
// For any cluster with in-place resize enabled and a policy allowing it, the system should
// prefer in-place updates for both CPU and memory requests over pod recreation.
// Validates: Requirements 8.2, 8.3
func TestProperty_InPlaceResizePreference(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("in-place resize is preferred when supported and allowed", prop.ForAll(
		func(allowInPlace, allowRecreate bool, k8sMinor int) bool {
			// Test with versions that support in-place resize (>= 1.29)
			if k8sMinor < 29 || k8sMinor > 50 {
				return true // Skip invalid range
			}

			mockDiscovery := &mockDiscoveryClient{
				serverVersion: &version.Info{
					Major: "1",
					Minor: fmt.Sprintf("%d", k8sMinor),
				},
			}

			engine := &Engine{
				discoveryClient: mockDiscovery,
				dryRun:          false,
			}

			// Create a mock policy
			policy := createMockPolicy(allowInPlace, allowRecreate)

			// Create a mock workload
			workload := createMockWorkload()

			// Create a mock recommendation
			rec := createMockRecommendation()

			decision, err := engine.CanApply(context.Background(), workload, rec, policy)
			if err != nil {
				return false
			}

			// When in-place is supported and allowed, it should be preferred
			if allowInPlace {
				return decision.CanApply && decision.Method == InPlace
			}

			// When in-place is not allowed but recreate is, use recreate
			if !allowInPlace && allowRecreate {
				return decision.CanApply && decision.Method == Recreate
			}

			// When neither is allowed, skip
			return !decision.CanApply && decision.Method == Skip
		},
		gen.Bool(),
		gen.Bool(),
		gen.IntRange(29, 50),
	))

	properties.Property("recreate is used when in-place not supported", prop.ForAll(
		func(allowRecreate bool, k8sMinor int) bool {
			// Test with versions that don't support in-place resize (< 1.29)
			if k8sMinor < 20 || k8sMinor >= 29 {
				return true // Skip invalid range
			}

			mockDiscovery := &mockDiscoveryClient{
				serverVersion: &version.Info{
					Major: "1",
					Minor: fmt.Sprintf("%d", k8sMinor),
				},
			}

			engine := &Engine{
				discoveryClient: mockDiscovery,
				dryRun:          false,
			}

			// Create a mock policy with in-place allowed (but it won't be supported)
			policy := createMockPolicy(true, allowRecreate)

			// Create a mock workload
			workload := createMockWorkload()

			// Create a mock recommendation
			rec := createMockRecommendation()

			decision, err := engine.CanApply(context.Background(), workload, rec, policy)
			if err != nil {
				return false
			}

			// When in-place is not supported, should use recreate if allowed
			if allowRecreate {
				return decision.CanApply && decision.Method == Recreate
			}

			// Otherwise skip
			return !decision.CanApply && decision.Method == Skip
		},
		gen.Bool(),
		gen.IntRange(20, 28),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Helper functions for creating mock objects

func createMockPolicy(allowInPlace, allowRecreate bool) *optipodv1alpha1.OptimizationPolicy {
	safetyFactor := 1.2
	return &optipodv1alpha1.OptimizationPolicy{
		Spec: optipodv1alpha1.OptimizationPolicySpec{
			Mode: optipodv1alpha1.ModeAuto,
			UpdateStrategy: optipodv1alpha1.UpdateStrategy{
				AllowInPlaceResize: allowInPlace,
				AllowRecreate:      allowRecreate,
				UpdateRequestsOnly: true,
			},
			MetricsConfig: optipodv1alpha1.MetricsConfig{
				Provider:     "prometheus",
				Percentile:   "P90",
				SafetyFactor: &safetyFactor,
			},
			ResourceBounds: optipodv1alpha1.ResourceBounds{
				CPU: optipodv1alpha1.ResourceBound{
					Min: resource.MustParse("100m"),
					Max: resource.MustParse("4000m"),
				},
				Memory: optipodv1alpha1.ResourceBound{
					Min: resource.MustParse("128Mi"),
					Max: resource.MustParse("8Gi"),
				},
			},
		},
	}
}

func createMockWorkload() *Workload {
	return &Workload{
		Kind:      "Deployment",
		Namespace: "default",
		Name:      "test-deployment",
		Object: &unstructured.Unstructured{
			Object: map[string]interface{}{
				"spec": map[string]interface{}{
					"template": map[string]interface{}{
						"spec": map[string]interface{}{
							"containers": []interface{}{
								map[string]interface{}{
									"name": "test-container",
									"resources": map[string]interface{}{
										"requests": map[string]interface{}{
											"cpu":    "500m",
											"memory": "512Mi",
										},
										"limits": map[string]interface{}{
											"cpu":    "1000m",
											"memory": "1Gi",
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
}

func createMockRecommendation() *recommendation.Recommendation {
	return &recommendation.Recommendation{
		CPU:         resource.MustParse("600m"),
		Memory:      resource.MustParse("1200Mi"), // Higher than current limit to avoid unsafe decrease
		Explanation: "Test recommendation",
	}
}

// Feature: k8s-workload-rightsizing, Property 3: Updates preserve limits
// For any workload update, the system should modify only resource requests and leave
// resource limits unchanged, unless the policy explicitly configures limit updates.
// Validates: Requirements 1.4
func TestProperty_UpdatesPreserveLimits(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("limits are preserved when updateRequestsOnly is true", prop.ForAll(
		func(cpuReq, memReq, cpuLimit, memLimit int64) bool {
			// Generate reasonable resource values (in millicores and MiB)
			if cpuReq < 100 || cpuReq > 4000 || memReq < 128 || memReq > 8192 {
				return true // Skip invalid values
			}
			if cpuLimit < cpuReq || memLimit < memReq {
				return true // Skip invalid combinations
			}

			// Create workload with specific limits
			workload := &Workload{
				Kind:      "Deployment",
				Namespace: "default",
				Name:      "test-deployment",
				Object: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"spec": map[string]interface{}{
							"template": map[string]interface{}{
								"spec": map[string]interface{}{
									"containers": []interface{}{
										map[string]interface{}{
											"name": "test-container",
											"resources": map[string]interface{}{
												"requests": map[string]interface{}{
													"cpu":    fmt.Sprintf("%dm", cpuReq),
													"memory": fmt.Sprintf("%dMi", memReq),
												},
												"limits": map[string]interface{}{
													"cpu":    fmt.Sprintf("%dm", cpuLimit),
													"memory": fmt.Sprintf("%dMi", memLimit),
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

			// Create recommendation
			rec := &recommendation.Recommendation{
				CPU:         resource.MustParse(fmt.Sprintf("%dm", cpuReq+100)),
				Memory:      resource.MustParse(fmt.Sprintf("%dMi", memReq+100)),
				Explanation: "Test recommendation",
			}

			// Create policy with updateRequestsOnly = true
			policy := createMockPolicy(true, false)
			policy.Spec.UpdateStrategy.UpdateRequestsOnly = true

			engine := &Engine{}

			// Build patch
			patch, err := engine.buildResourcePatch(workload, "test-container", rec, policy)
			if err != nil {
				return false
			}

			// Parse patch to verify limits are not modified
			var patchObj map[string]interface{}
			if err := json.Unmarshal(patch, &patchObj); err != nil {
				return false
			}

			// Extract containers from patch
			containers, _, _ := unstructured.NestedSlice(patchObj, "spec", "template", "spec", "containers")
			if len(containers) == 0 {
				return false
			}

			container := containers[0].(map[string]interface{})
			resourcesMap, _, _ := unstructured.NestedMap(container, "resources")

			// Check that requests are updated
			requestsMap, ok := resourcesMap["requests"].(map[string]interface{})
			if !ok {
				return false
			}
			if requestsMap["cpu"] != rec.CPU.String() || requestsMap["memory"] != rec.Memory.String() {
				return false
			}

			// Check that limits are NOT in the patch (preserved)
			_, limitsExist := resourcesMap["limits"]
			return !limitsExist
		},
		gen.Int64Range(100, 4000),
		gen.Int64Range(128, 8192),
		gen.Int64Range(100, 4000),
		gen.Int64Range(128, 8192),
	))

	properties.Property("limits are updated when updateRequestsOnly is false", prop.ForAll(
		func(cpuReq, memReq int64) bool {
			// Generate reasonable resource values
			if cpuReq < 100 || cpuReq > 4000 || memReq < 128 || memReq > 8192 {
				return true // Skip invalid values
			}

			// Create workload
			workload := &Workload{
				Kind:      "Deployment",
				Namespace: "default",
				Name:      "test-deployment",
				Object: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"spec": map[string]interface{}{
							"template": map[string]interface{}{
								"spec": map[string]interface{}{
									"containers": []interface{}{
										map[string]interface{}{
											"name": "test-container",
											"resources": map[string]interface{}{
												"requests": map[string]interface{}{
													"cpu":    fmt.Sprintf("%dm", cpuReq),
													"memory": fmt.Sprintf("%dMi", memReq),
												},
												"limits": map[string]interface{}{
													"cpu":    fmt.Sprintf("%dm", cpuReq*2),
													"memory": fmt.Sprintf("%dMi", memReq*2),
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

			// Create recommendation
			rec := &recommendation.Recommendation{
				CPU:         resource.MustParse(fmt.Sprintf("%dm", cpuReq+100)),
				Memory:      resource.MustParse(fmt.Sprintf("%dMi", memReq+100)),
				Explanation: "Test recommendation",
			}

			// Create policy with updateRequestsOnly = false
			policy := createMockPolicy(true, false)
			policy.Spec.UpdateStrategy.UpdateRequestsOnly = false

			engine := &Engine{}

			// Build patch
			patch, err := engine.buildResourcePatch(workload, "test-container", rec, policy)
			if err != nil {
				return false
			}

			// Parse patch to verify limits ARE modified
			var patchObj map[string]interface{}
			if err := json.Unmarshal(patch, &patchObj); err != nil {
				return false
			}

			// Extract containers from patch
			containers, _, _ := unstructured.NestedSlice(patchObj, "spec", "template", "spec", "containers")
			if len(containers) == 0 {
				return false
			}

			container := containers[0].(map[string]interface{})
			resourcesMap, _, _ := unstructured.NestedMap(container, "resources")

			// Check that limits ARE in the patch
			limitsMap, ok := resourcesMap["limits"].(map[string]interface{})
			if !ok {
				return false
			}

			// Verify limits match recommendation
			return limitsMap["cpu"] == rec.CPU.String() && limitsMap["memory"] == rec.Memory.String()
		},
		gen.Int64Range(100, 4000),
		gen.Int64Range(128, 8192),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}


// Feature: k8s-workload-rightsizing, Property 28: RBAC respect
// For any workload where RBAC prevents read or update operations, the system should not
// attempt to monitor or modify it, and should surface appropriate status conditions and
// events indicating insufficient permissions.
// Validates: Requirements 13.1, 13.2, 13.3, 13.4
func TestProperty_RBACRespect(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("RBAC forbidden errors are properly detected and returned", prop.ForAll(
		func(errorCode int) bool {
			// Test various error codes
			if errorCode < 400 || errorCode > 500 {
				return true // Skip invalid error codes
			}

			// Create a mock dynamic client that returns forbidden error
			mockDynamic := &mockDynamicClient{
				shouldReturnForbidden: errorCode == 403,
			}

			engine := &Engine{
				dynamicClient: mockDynamic,
			}

			workload := createMockWorkload()
			rec := createMockRecommendation()
			policy := createMockPolicy(true, false)

			err := engine.Apply(context.Background(), workload, "test-container", rec, policy)

			// When error code is 403, should get RBAC error
			if errorCode == 403 {
				if err == nil {
					return false
				}
				// Check that error message mentions RBAC
				return contains(err.Error(), "RBAC") || contains(err.Error(), "insufficient permissions")
			}

			// For other error codes, behavior may vary
			return true
		},
		gen.IntRange(400, 500),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// mockDynamicClient is a mock implementation of dynamic.Interface
type mockDynamicClient struct {
	dynamic.Interface
	shouldReturnForbidden bool
}

func (m *mockDynamicClient) Resource(resource schema.GroupVersionResource) dynamic.NamespaceableResourceInterface {
	return &mockNamespaceableResource{shouldReturnForbidden: m.shouldReturnForbidden}
}

type mockNamespaceableResource struct {
	dynamic.NamespaceableResourceInterface
	shouldReturnForbidden bool
}

func (m *mockNamespaceableResource) Namespace(ns string) dynamic.ResourceInterface {
	return &mockResourceInterface{shouldReturnForbidden: m.shouldReturnForbidden}
}

type mockResourceInterface struct {
	dynamic.ResourceInterface
	shouldReturnForbidden bool
}

func (m *mockResourceInterface) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, options metav1.PatchOptions, subresources ...string) (*unstructured.Unstructured, error) {
	if m.shouldReturnForbidden {
		return nil, errors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "deployments"}, name, fmt.Errorf("user cannot patch resource"))
	}
	return &unstructured.Unstructured{}, nil
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
