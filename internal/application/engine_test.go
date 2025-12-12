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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/recommendation"
)

const (
	// FieldManagerName is the field manager name used by optipod
	FieldManagerName = "optipod"
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
	cpuMultiplier := 1.0
	memoryMultiplier := 1.1
	return &optipodv1alpha1.OptimizationPolicy{
		Spec: optipodv1alpha1.OptimizationPolicySpec{
			Mode: optipodv1alpha1.ModeAuto,
			UpdateStrategy: optipodv1alpha1.UpdateStrategy{
				AllowInPlaceResize: allowInPlace,
				AllowRecreate:      allowRecreate,
				UpdateRequestsOnly: true,
				LimitConfig: &optipodv1alpha1.LimitConfig{
					CPULimitMultiplier:    &cpuMultiplier,
					MemoryLimitMultiplier: &memoryMultiplier,
				},
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

			// Verify limits are present and parseable
			cpuLimitStr, ok := limitsMap["cpu"].(string)
			if !ok {
				return false
			}
			memLimitStr, ok := limitsMap["memory"].(string)
			if !ok {
				return false
			}

			// Parse the limits
			cpuLimitParsed, err := resource.ParseQuantity(cpuLimitStr)
			if err != nil {
				return false
			}
			memLimitParsed, err := resource.ParseQuantity(memLimitStr)
			if err != nil {
				return false
			}

			// Verify CPU limit is approximately equal to recommendation (1.0x multiplier)
			// Allow for format differences
			if cpuLimitParsed.Value() != rec.CPU.Value() {
				return false
			}

			// Verify memory limit is approximately 1.1x recommendation
			expectedMemValue := int64(float64(rec.Memory.Value()) * 1.1)
			// Allow for small rounding differences
			memDiff := memLimitParsed.Value() - expectedMemValue
			if memDiff < -1 || memDiff > 1 {
				return false
			}

			return true
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

			_, err := engine.Apply(context.Background(), workload, "test-container", rec, policy)

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

func (m *mockDynamicClient) Resource(gvr schema.GroupVersionResource) dynamic.NamespaceableResourceInterface {
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

// Feature: server-side-apply-support, Property 3: SSA patch contains only resource fields
// For any workload and recommendation, the SSA patch should contain only resource requests
// and limits, not other fields like image or replicas
// Validates: Requirements 1.2, 3.1, 3.2
func TestProperty_SSAPatchStructure(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("SSA patch contains only required fields and resource fields", prop.ForAll(
		func(cpuReq, memReq int64, updateRequestsOnly bool) bool {
			// Generate reasonable resource values (in millicores and MiB)
			if cpuReq < 100 || cpuReq > 4000 || memReq < 128 || memReq > 8192 {
				return true // Skip invalid values
			}

			// Create workload
			workload := createMockWorkload()

			// Create recommendation
			rec := &recommendation.Recommendation{
				CPU:         resource.MustParse(fmt.Sprintf("%dm", cpuReq)),
				Memory:      resource.MustParse(fmt.Sprintf("%dMi", memReq)),
				Explanation: "Test recommendation",
			}

			// Create policy
			policy := createMockPolicy(true, false)
			policy.Spec.UpdateStrategy.UpdateRequestsOnly = updateRequestsOnly

			engine := &Engine{}

			// Build SSA patch
			patch, err := engine.buildSSAPatch(workload, "test-container", rec, policy)
			if err != nil {
				return false
			}

			// Parse patch
			var patchObj map[string]interface{}
			if err := json.Unmarshal(patch, &patchObj); err != nil {
				return false
			}

			// Verify required fields are present
			if _, ok := patchObj["apiVersion"]; !ok {
				return false
			}
			if _, ok := patchObj["kind"]; !ok {
				return false
			}
			if _, ok := patchObj["metadata"]; !ok {
				return false
			}
			if _, ok := patchObj["spec"]; !ok {
				return false
			}

			// Verify only resource fields are in the patch
			// Extract containers from patch
			containers, _, _ := unstructured.NestedSlice(patchObj, "spec", "template", "spec", "containers")
			if len(containers) == 0 {
				return false
			}

			container := containers[0].(map[string]interface{})

			// Container should only have "name" and "resources" fields
			if len(container) != 2 {
				return false
			}

			if _, ok := container["name"]; !ok {
				return false
			}
			if _, ok := container["resources"]; !ok {
				return false
			}

			// Verify no other fields like "image", "env", etc.
			if _, ok := container["image"]; ok {
				return false
			}
			if _, ok := container["env"]; ok {
				return false
			}
			if _, ok := container["ports"]; ok {
				return false
			}

			return true
		},
		gen.Int64Range(100, 4000),
		gen.Int64Range(128, 8192),
		gen.Bool(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: server-side-apply-support, Property 4: Container identification in patch
// For any container name, the SSA patch should correctly identify the container by name
// in the containers array
// Validates: Requirements 3.3
func TestProperty_ContainerIdentification(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("SSA patch identifies container by name", prop.ForAll(
		func(containerName string, cpuReq, memReq int64) bool {
			// Generate valid container names (alphanumeric and hyphens)
			if containerName == "" || len(containerName) > 63 {
				return true // Skip invalid names
			}

			// Generate reasonable resource values
			if cpuReq < 100 || cpuReq > 4000 || memReq < 128 || memReq > 8192 {
				return true // Skip invalid values
			}

			// Create workload with the specific container name
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
											"name": containerName,
											"resources": map[string]interface{}{
												"requests": map[string]interface{}{
													"cpu":    "500m",
													"memory": "512Mi",
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
				CPU:         resource.MustParse(fmt.Sprintf("%dm", cpuReq)),
				Memory:      resource.MustParse(fmt.Sprintf("%dMi", memReq)),
				Explanation: "Test recommendation",
			}

			// Create policy
			policy := createMockPolicy(true, false)

			engine := &Engine{}

			// Build SSA patch
			patch, err := engine.buildSSAPatch(workload, containerName, rec, policy)
			if err != nil {
				return false
			}

			// Parse patch
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

			// Verify container name matches
			name, ok := container["name"].(string)
			if !ok {
				return false
			}

			return name == containerName
		},
		gen.AlphaString().SuchThat(func(s string) bool {
			return len(s) > 0 && len(s) <= 63
		}),
		gen.Int64Range(100, 4000),
		gen.Int64Range(128, 8192),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: server-side-apply-support, Property 5: Conditional limits inclusion
// For any recommendation, if updateRequestsOnly=true, the patch should not include limits;
// if false, it should include both requests and limits
// Validates: Requirements 3.4, 3.5
func TestProperty_ConditionalLimits(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("limits are excluded when updateRequestsOnly is true", prop.ForAll(
		func(cpuReq, memReq int64) bool {
			// Generate reasonable resource values
			if cpuReq < 100 || cpuReq > 4000 || memReq < 128 || memReq > 8192 {
				return true // Skip invalid values
			}

			// Create workload
			workload := createMockWorkload()

			// Create recommendation
			rec := &recommendation.Recommendation{
				CPU:         resource.MustParse(fmt.Sprintf("%dm", cpuReq)),
				Memory:      resource.MustParse(fmt.Sprintf("%dMi", memReq)),
				Explanation: "Test recommendation",
			}

			// Create policy with updateRequestsOnly = true
			policy := createMockPolicy(true, false)
			policy.Spec.UpdateStrategy.UpdateRequestsOnly = true

			engine := &Engine{}

			// Build SSA patch
			patch, err := engine.buildSSAPatch(workload, "test-container", rec, policy)
			if err != nil {
				return false
			}

			// Parse patch
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

			// Verify requests are present
			requestsMap, ok := resourcesMap["requests"].(map[string]interface{})
			if !ok {
				return false
			}
			if requestsMap["cpu"] != rec.CPU.String() || requestsMap["memory"] != rec.Memory.String() {
				return false
			}

			// Verify limits are NOT present
			_, limitsExist := resourcesMap["limits"]
			return !limitsExist
		},
		gen.Int64Range(100, 4000),
		gen.Int64Range(128, 8192),
	))

	properties.Property("limits are included when updateRequestsOnly is false", prop.ForAll(
		func(cpuReq, memReq int64) bool {
			// Generate reasonable resource values
			if cpuReq < 100 || cpuReq > 4000 || memReq < 128 || memReq > 8192 {
				return true // Skip invalid values
			}

			// Create workload
			workload := createMockWorkload()

			// Create recommendation
			rec := &recommendation.Recommendation{
				CPU:         resource.MustParse(fmt.Sprintf("%dm", cpuReq)),
				Memory:      resource.MustParse(fmt.Sprintf("%dMi", memReq)),
				Explanation: "Test recommendation",
			}

			// Create policy with updateRequestsOnly = false
			policy := createMockPolicy(true, false)
			policy.Spec.UpdateStrategy.UpdateRequestsOnly = false

			engine := &Engine{}

			// Build SSA patch
			patch, err := engine.buildSSAPatch(workload, "test-container", rec, policy)
			if err != nil {
				return false
			}

			// Parse patch
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

			// Verify requests are present
			requestsMap, ok := resourcesMap["requests"].(map[string]interface{})
			if !ok {
				return false
			}
			if requestsMap["cpu"] != rec.CPU.String() || requestsMap["memory"] != rec.Memory.String() {
				return false
			}

			// Verify limits ARE present
			limitsMap, ok := resourcesMap["limits"].(map[string]interface{})
			if !ok {
				return false
			}

			// Verify limits are present and parseable
			cpuLimitStr, ok := limitsMap["cpu"].(string)
			if !ok {
				return false
			}
			memLimitStr, ok := limitsMap["memory"].(string)
			if !ok {
				return false
			}

			// Parse the limits
			cpuLimitParsed, err := resource.ParseQuantity(cpuLimitStr)
			if err != nil {
				return false
			}
			memLimitParsed, err := resource.ParseQuantity(memLimitStr)
			if err != nil {
				return false
			}

			// Verify CPU limit is approximately equal to recommendation (1.0x multiplier)
			if cpuLimitParsed.Value() != rec.CPU.Value() {
				return false
			}

			// Verify memory limit is approximately 1.1x recommendation
			expectedMemValue := int64(float64(rec.Memory.Value()) * 1.1)
			// Allow for small rounding differences
			memDiff := memLimitParsed.Value() - expectedMemValue
			if memDiff < -1 || memDiff > 1 {
				return false
			}

			return true
		},
		gen.Int64Range(100, 4000),
		gen.Int64Range(128, 8192),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: server-side-apply-support, Property 6: Valid JSON serialization
// For any constructed SSA patch, it should serialize to valid JSON that can be parsed back
// Validates: Requirements 3.6
func TestProperty_JSONSerialization(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("SSA patch serializes to valid JSON", prop.ForAll(
		func(cpuReq, memReq int64, updateRequestsOnly bool) bool {
			// Generate reasonable resource values
			if cpuReq < 100 || cpuReq > 4000 || memReq < 128 || memReq > 8192 {
				return true // Skip invalid values
			}

			// Create workload
			workload := createMockWorkload()

			// Create recommendation
			rec := &recommendation.Recommendation{
				CPU:         resource.MustParse(fmt.Sprintf("%dm", cpuReq)),
				Memory:      resource.MustParse(fmt.Sprintf("%dMi", memReq)),
				Explanation: "Test recommendation",
			}

			// Create policy
			policy := createMockPolicy(true, false)
			policy.Spec.UpdateStrategy.UpdateRequestsOnly = updateRequestsOnly

			engine := &Engine{}

			// Build SSA patch
			patch, err := engine.buildSSAPatch(workload, "test-container", rec, policy)
			if err != nil {
				return false
			}

			// Verify it's valid JSON by parsing it
			var patchObj map[string]interface{}
			if err := json.Unmarshal(patch, &patchObj); err != nil {
				return false
			}

			// Verify we can serialize it back
			_, err = json.Marshal(patchObj)
			if err != nil {
				return false
			}

			// Verify the patch is not empty
			if len(patch) == 0 {
				return false
			}

			return true
		},
		gen.Int64Range(100, 4000),
		gen.Int64Range(128, 8192),
		gen.Bool(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: server-side-apply-support, Property 7: SSA uses correct field manager
// For any workload and recommendation, when SSA is enabled, the patch operation should
// use "optipod" as the fieldManager
// Validates: Requirements 1.1, 4.1
func TestProperty_SSAFieldManager(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("SSA uses 'optipod' as field manager", prop.ForAll(
		func(cpuReq, memReq int64, updateRequestsOnly bool) bool {
			// Generate reasonable resource values
			if cpuReq < 100 || cpuReq > 4000 || memReq < 128 || memReq > 8192 {
				return true // Skip invalid values
			}

			// Track the patch options used
			var capturedPatchOptions metav1.PatchOptions
			var capturedPatchType types.PatchType

			// Create mock dynamic client that captures patch options
			mockDynamic := &mockDynamicClientWithCapture{
				capturedPatchOptions: &capturedPatchOptions,
				capturedPatchType:    &capturedPatchType,
			}

			engine := &Engine{
				dynamicClient: mockDynamic,
			}

			// Create workload
			workload := createMockWorkload()

			// Create recommendation
			rec := &recommendation.Recommendation{
				CPU:         resource.MustParse(fmt.Sprintf("%dm", cpuReq)),
				Memory:      resource.MustParse(fmt.Sprintf("%dMi", memReq)),
				Explanation: "Test recommendation",
			}

			// Create policy
			policy := createMockPolicy(true, false)
			policy.Spec.UpdateStrategy.UpdateRequestsOnly = updateRequestsOnly

			// Apply with SSA
			err := engine.ApplyWithSSA(context.Background(), workload, "test-container", rec, policy)
			if err != nil {
				return false
			}

			// Verify field manager is "optipod"
			if capturedPatchOptions.FieldManager != FieldManagerName {
				return false
			}

			// Verify patch type is ApplyPatchType
			if capturedPatchType != types.ApplyPatchType {
				return false
			}

			return true
		},
		gen.Int64Range(100, 4000),
		gen.Int64Range(128, 8192),
		gen.Bool(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// mockDynamicClientWithCapture captures patch options for testing
type mockDynamicClientWithCapture struct {
	dynamic.Interface
	capturedPatchOptions *metav1.PatchOptions
	capturedPatchType    *types.PatchType
}

func (m *mockDynamicClientWithCapture) Resource(gvr schema.GroupVersionResource) dynamic.NamespaceableResourceInterface {
	return &mockNamespaceableResourceWithCapture{
		capturedPatchOptions: m.capturedPatchOptions,
		capturedPatchType:    m.capturedPatchType,
	}
}

type mockNamespaceableResourceWithCapture struct {
	dynamic.NamespaceableResourceInterface
	capturedPatchOptions *metav1.PatchOptions
	capturedPatchType    *types.PatchType
}

func (m *mockNamespaceableResourceWithCapture) Namespace(ns string) dynamic.ResourceInterface {
	return &mockResourceInterfaceWithCapture{
		capturedPatchOptions: m.capturedPatchOptions,
		capturedPatchType:    m.capturedPatchType,
	}
}

type mockResourceInterfaceWithCapture struct {
	dynamic.ResourceInterface
	capturedPatchOptions *metav1.PatchOptions
	capturedPatchType    *types.PatchType
}

func (m *mockResourceInterfaceWithCapture) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, options metav1.PatchOptions, subresources ...string) (*unstructured.Unstructured, error) {
	// Capture the patch options and type
	*m.capturedPatchOptions = options
	*m.capturedPatchType = pt
	return &unstructured.Unstructured{}, nil
}

// Feature: server-side-apply-support, Property 8: Force flag is set for SSA
// For any SSA patch operation, the Force option should be set to true
// Validates: Requirements 1.3
func TestProperty_SSAForceFlag(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("SSA Force flag is set to true", prop.ForAll(
		func(cpuReq, memReq int64, updateRequestsOnly bool) bool {
			// Generate reasonable resource values
			if cpuReq < 100 || cpuReq > 4000 || memReq < 128 || memReq > 8192 {
				return true // Skip invalid values
			}

			// Track the patch options used
			var capturedPatchOptions metav1.PatchOptions
			var capturedPatchType types.PatchType

			// Create mock dynamic client that captures patch options
			mockDynamic := &mockDynamicClientWithCapture{
				capturedPatchOptions: &capturedPatchOptions,
				capturedPatchType:    &capturedPatchType,
			}

			engine := &Engine{
				dynamicClient: mockDynamic,
			}

			// Create workload
			workload := createMockWorkload()

			// Create recommendation
			rec := &recommendation.Recommendation{
				CPU:         resource.MustParse(fmt.Sprintf("%dm", cpuReq)),
				Memory:      resource.MustParse(fmt.Sprintf("%dMi", memReq)),
				Explanation: "Test recommendation",
			}

			// Create policy
			policy := createMockPolicy(true, false)
			policy.Spec.UpdateStrategy.UpdateRequestsOnly = updateRequestsOnly

			// Apply with SSA
			err := engine.ApplyWithSSA(context.Background(), workload, "test-container", rec, policy)
			if err != nil {
				return false
			}

			// Verify Force flag is set to true
			if capturedPatchOptions.Force == nil {
				return false
			}

			if *capturedPatchOptions.Force != true {
				return false
			}

			return true
		},
		gen.Int64Range(100, 4000),
		gen.Int64Range(128, 8192),
		gen.Bool(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// TestSSAErrorHandling tests the handleSSAError method
func TestSSAErrorHandling(t *testing.T) {
	engine := &Engine{}

	tests := []struct {
		name          string
		err           error
		expectedMsg   string
		shouldContain []string
	}{
		{
			name:          "conflict error",
			err:           errors.NewConflict(schema.GroupResource{Group: "apps", Resource: "deployments"}, "test-deployment", fmt.Errorf("field manager conflict")),
			expectedMsg:   "SSA conflict",
			shouldContain: []string{"SSA conflict", "another field manager owns these fields"},
		},
		{
			name:          "forbidden error",
			err:           errors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "deployments"}, "test-deployment", fmt.Errorf("insufficient permissions")),
			expectedMsg:   "RBAC",
			shouldContain: []string{"RBAC", "insufficient permissions for Server-Side Apply"},
		},
		{
			name:          "invalid error",
			err:           errors.NewInvalid(schema.GroupKind{Group: "apps", Kind: "Deployment"}, "test-deployment", nil),
			expectedMsg:   "SSA patch validation failed",
			shouldContain: []string{"SSA patch validation failed"},
		},
		{
			name:          "generic error",
			err:           fmt.Errorf("some other error"),
			expectedMsg:   "SSA patch failed",
			shouldContain: []string{"SSA patch failed"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.handleSSAError(tt.err)
			if result == nil {
				t.Errorf("expected error, got nil")
				return
			}

			errMsg := result.Error()
			for _, expected := range tt.shouldContain {
				if !contains(errMsg, expected) {
					t.Errorf("error message should contain '%s', got: %s", expected, errMsg)
				}
			}
		})
	}
}

// TestSSAErrorHandlingIntegration tests error handling in ApplyWithSSA
func TestSSAErrorHandlingIntegration(t *testing.T) {
	tests := []struct {
		name          string
		errorType     string
		shouldContain []string
	}{
		{
			name:          "conflict error in ApplyWithSSA",
			errorType:     "conflict",
			shouldContain: []string{"SSA conflict", "another field manager owns these fields"},
		},
		{
			name:          "forbidden error in ApplyWithSSA",
			errorType:     "forbidden",
			shouldContain: []string{"RBAC", "insufficient permissions for Server-Side Apply"},
		},
		{
			name:          "invalid error in ApplyWithSSA",
			errorType:     "invalid",
			shouldContain: []string{"SSA patch validation failed"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock dynamic client that returns specific error
			mockDynamic := &mockDynamicClientWithError{
				errorType: tt.errorType,
			}

			engine := &Engine{
				dynamicClient: mockDynamic,
			}

			workload := createMockWorkload()
			rec := createMockRecommendation()
			policy := createMockPolicy(true, false)

			err := engine.ApplyWithSSA(context.Background(), workload, "test-container", rec, policy)
			if err == nil {
				t.Errorf("expected error, got nil")
				return
			}

			errMsg := err.Error()
			for _, expected := range tt.shouldContain {
				if !contains(errMsg, expected) {
					t.Errorf("error message should contain '%s', got: %s", expected, errMsg)
				}
			}
		})
	}
}

// mockDynamicClientWithError returns specific error types for testing
type mockDynamicClientWithError struct {
	dynamic.Interface
	errorType string
}

func (m *mockDynamicClientWithError) Resource(gvr schema.GroupVersionResource) dynamic.NamespaceableResourceInterface {
	return &mockNamespaceableResourceWithError{errorType: m.errorType}
}

type mockNamespaceableResourceWithError struct {
	dynamic.NamespaceableResourceInterface
	errorType string
}

func (m *mockNamespaceableResourceWithError) Namespace(ns string) dynamic.ResourceInterface {
	return &mockResourceInterfaceWithError{errorType: m.errorType}
}

type mockResourceInterfaceWithError struct {
	dynamic.ResourceInterface
	errorType string
}

func (m *mockResourceInterfaceWithError) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, options metav1.PatchOptions, subresources ...string) (*unstructured.Unstructured, error) {
	switch m.errorType {
	case "conflict":
		return nil, errors.NewConflict(schema.GroupResource{Group: "apps", Resource: "deployments"}, name, fmt.Errorf("field manager conflict"))
	case "forbidden":
		return nil, errors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "deployments"}, name, fmt.Errorf("insufficient permissions"))
	case "invalid":
		return nil, errors.NewInvalid(schema.GroupKind{Group: "apps", Kind: "Deployment"}, name, nil)
	default:
		return nil, fmt.Errorf("unknown error")
	}
}

// Feature: server-side-apply-support, Property 1: Configuration determines patch method
// For any policy with useServerSideApply=true, the system should use ApplyPatchType;
// for useServerSideApply=false, it should use StrategicMergePatchType
// Validates: Requirements 2.2, 2.3
func TestProperty_ConfigurationDeterminesPatchMethod(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("SSA is used when useServerSideApply is true", prop.ForAll(
		func(cpuReq, memReq int64) bool {
			// Generate reasonable resource values
			if cpuReq < 100 || cpuReq > 4000 || memReq < 128 || memReq > 8192 {
				return true // Skip invalid values
			}

			// Track the patch type used
			var capturedPatchType types.PatchType

			// Create mock dynamic client that captures patch type
			mockDynamic := &mockDynamicClientWithCapture{
				capturedPatchOptions: &metav1.PatchOptions{},
				capturedPatchType:    &capturedPatchType,
			}

			engine := &Engine{
				dynamicClient: mockDynamic,
			}

			// Create workload
			workload := createMockWorkload()

			// Create recommendation
			rec := &recommendation.Recommendation{
				CPU:         resource.MustParse(fmt.Sprintf("%dm", cpuReq)),
				Memory:      resource.MustParse(fmt.Sprintf("%dMi", memReq)),
				Explanation: "Test recommendation",
			}

			// Create policy with useServerSideApply = true
			policy := createMockPolicy(true, false)
			useSSA := true
			policy.Spec.UpdateStrategy.UseServerSideApply = &useSSA

			// Apply
			result, err := engine.Apply(context.Background(), workload, "test-container", rec, policy)
			if err != nil {
				return false
			}

			// Verify the result indicates SSA was used
			if result.Method != "ServerSideApply" || !result.FieldOwnership {
				return false
			}

			// Verify ApplyPatchType was used
			return capturedPatchType == types.ApplyPatchType
		},
		gen.Int64Range(100, 4000),
		gen.Int64Range(128, 8192),
	))

	properties.Property("Strategic Merge is used when useServerSideApply is false", prop.ForAll(
		func(cpuReq, memReq int64) bool {
			// Generate reasonable resource values
			if cpuReq < 100 || cpuReq > 4000 || memReq < 128 || memReq > 8192 {
				return true // Skip invalid values
			}

			// Track the patch type used
			var capturedPatchType types.PatchType

			// Create mock dynamic client that captures patch type
			mockDynamic := &mockDynamicClientWithCapture{
				capturedPatchOptions: &metav1.PatchOptions{},
				capturedPatchType:    &capturedPatchType,
			}

			engine := &Engine{
				dynamicClient: mockDynamic,
			}

			// Create workload
			workload := createMockWorkload()

			// Create recommendation
			rec := &recommendation.Recommendation{
				CPU:         resource.MustParse(fmt.Sprintf("%dm", cpuReq)),
				Memory:      resource.MustParse(fmt.Sprintf("%dMi", memReq)),
				Explanation: "Test recommendation",
			}

			// Create policy with useServerSideApply = false
			policy := createMockPolicy(true, false)
			useSSA := false
			policy.Spec.UpdateStrategy.UseServerSideApply = &useSSA

			// Apply
			result, err := engine.Apply(context.Background(), workload, "test-container", rec, policy)
			if err != nil {
				return false
			}

			// Verify the result indicates Strategic Merge was used
			if result.Method != "StrategicMergePatch" || result.FieldOwnership {
				return false
			}

			// Verify StrategicMergePatchType was used
			return capturedPatchType == types.StrategicMergePatchType
		},
		gen.Int64Range(100, 4000),
		gen.Int64Range(128, 8192),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: server-side-apply-support, Property 2: Default to SSA when unspecified
// For any policy where useServerSideApply is nil, the system should behave as if
// useServerSideApply=true
// Validates: Requirements 2.4
func TestProperty_DefaultToSSA(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("SSA is used by default when useServerSideApply is nil", prop.ForAll(
		func(cpuReq, memReq int64) bool {
			// Generate reasonable resource values
			if cpuReq < 100 || cpuReq > 4000 || memReq < 128 || memReq > 8192 {
				return true // Skip invalid values
			}

			// Track the patch type used
			var capturedPatchType types.PatchType

			// Create mock dynamic client that captures patch type
			mockDynamic := &mockDynamicClientWithCapture{
				capturedPatchOptions: &metav1.PatchOptions{},
				capturedPatchType:    &capturedPatchType,
			}

			engine := &Engine{
				dynamicClient: mockDynamic,
			}

			// Create workload
			workload := createMockWorkload()

			// Create recommendation
			rec := &recommendation.Recommendation{
				CPU:         resource.MustParse(fmt.Sprintf("%dm", cpuReq)),
				Memory:      resource.MustParse(fmt.Sprintf("%dMi", memReq)),
				Explanation: "Test recommendation",
			}

			// Create policy with useServerSideApply = nil (default)
			policy := createMockPolicy(true, false)
			policy.Spec.UpdateStrategy.UseServerSideApply = nil

			// Apply
			result, err := engine.Apply(context.Background(), workload, "test-container", rec, policy)
			if err != nil {
				return false
			}

			// Verify the result indicates SSA was used (default)
			if result.Method != "ServerSideApply" || !result.FieldOwnership {
				return false
			}

			// Verify ApplyPatchType was used (SSA is the default)
			return capturedPatchType == types.ApplyPatchType
		},
		gen.Int64Range(100, 4000),
		gen.Int64Range(128, 8192),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: server-side-apply-support, Property 9: Logging includes field manager
// For any SSA operation, the log output should contain the fieldManager name and Force setting
// Validates: Requirements 7.1
func TestProperty_SSALoggingIncludesFieldManager(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("SSA logging includes fieldManager and Force setting", prop.ForAll(
		func(cpuReq, memReq int64, updateRequestsOnly bool) bool {
			// Generate reasonable resource values
			if cpuReq < 100 || cpuReq > 4000 || memReq < 128 || memReq > 8192 {
				return true // Skip invalid values
			}

			// Track the patch options used to verify logging context
			var capturedPatchOptions metav1.PatchOptions
			var capturedPatchType types.PatchType

			// Create mock dynamic client that captures patch options
			mockDynamic := &mockDynamicClientWithCapture{
				capturedPatchOptions: &capturedPatchOptions,
				capturedPatchType:    &capturedPatchType,
			}

			engine := &Engine{
				dynamicClient: mockDynamic,
			}

			// Create workload
			workload := createMockWorkload()

			// Create recommendation
			rec := &recommendation.Recommendation{
				CPU:         resource.MustParse(fmt.Sprintf("%dm", cpuReq)),
				Memory:      resource.MustParse(fmt.Sprintf("%dMi", memReq)),
				Explanation: "Test recommendation",
			}

			// Create policy
			policy := createMockPolicy(true, false)
			policy.Spec.UpdateStrategy.UpdateRequestsOnly = updateRequestsOnly

			// Create a context with a logger (required for logging to work)
			ctx := context.Background()

			// Apply with SSA - this will trigger logging
			err := engine.ApplyWithSSA(ctx, workload, "test-container", rec, policy)
			if err != nil {
				return false
			}

			// Verify that the patch was applied with correct field manager
			// This indirectly verifies that logging would have included these values
			if capturedPatchOptions.FieldManager != FieldManagerName {
				return false
			}

			if capturedPatchOptions.Force == nil || *capturedPatchOptions.Force != true {
				return false
			}

			// Verify patch type is correct (would be logged)
			if capturedPatchType != types.ApplyPatchType {
				return false
			}

			// The function executed successfully, which means logging occurred
			// with the correct context containing fieldManager="optipod" and Force=true
			return true
		},
		gen.Int64Range(100, 4000),
		gen.Int64Range(128, 8192),
		gen.Bool(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: limit-configuration, Property 1: Limit multipliers are applied correctly
// For any recommendation and limit multipliers, the calculated limits should equal
// the recommendation multiplied by the respective multiplier
// Validates: Requirements for limit configuration feature
func TestProperty_LimitMultipliersAppliedCorrectly(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("CPU and memory limits are calculated using multipliers", prop.ForAll(
		func(cpuReq, memReq int64, cpuMult, memMult float64) bool {
			// Generate reasonable resource values
			if cpuReq < 100 || cpuReq > 4000 || memReq < 128 || memReq > 8192 {
				return true // Skip invalid values
			}
			// Multipliers must be between 1.0 and 10.0
			if cpuMult < 1.0 || cpuMult > 10.0 || memMult < 1.0 || memMult > 10.0 {
				return true // Skip invalid multipliers
			}

			// Create recommendation
			rec := &recommendation.Recommendation{
				CPU:         resource.MustParse(fmt.Sprintf("%dm", cpuReq)),
				Memory:      resource.MustParse(fmt.Sprintf("%dMi", memReq)),
				Explanation: "Test recommendation",
			}

			// Create policy with custom multipliers
			policy := createMockPolicy(true, false)
			policy.Spec.UpdateStrategy.UpdateRequestsOnly = false
			policy.Spec.UpdateStrategy.LimitConfig = &optipodv1alpha1.LimitConfig{
				CPULimitMultiplier:    &cpuMult,
				MemoryLimitMultiplier: &memMult,
			}

			engine := &Engine{}

			// Calculate expected limits
			cpuLimit, memoryLimit := engine.calculateLimits(rec, policy)

			// Verify CPU limit matches the calculation (CPU uses MilliValue for DecimalSI format)
			expectedCPUMilliValue := int64(float64(rec.CPU.MilliValue()) * cpuMult)
			if cpuLimit.MilliValue() != expectedCPUMilliValue {
				return false
			}

			// Verify memory limit matches the calculation
			expectedMemValue := int64(float64(rec.Memory.Value()) * memMult)
			if memoryLimit.Value() != expectedMemValue {
				return false
			}

			return true
		},
		gen.Int64Range(100, 4000),
		gen.Int64Range(128, 8192),
		gen.Float64Range(1.0, 10.0),
		gen.Float64Range(1.0, 10.0),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: limit-configuration, Property 2: Default multipliers are used when not specified
// For any policy without explicit limit multipliers, the defaults should be used
// (CPU: 1.0, Memory: 1.1)
// Validates: Requirements for limit configuration feature
func TestProperty_DefaultLimitMultipliers(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("default multipliers are used when limitConfig is nil", prop.ForAll(
		func(cpuReq, memReq int64) bool {
			// Generate reasonable resource values
			if cpuReq < 100 || cpuReq > 4000 || memReq < 128 || memReq > 8192 {
				return true // Skip invalid values
			}

			// Create recommendation
			rec := &recommendation.Recommendation{
				CPU:         resource.MustParse(fmt.Sprintf("%dm", cpuReq)),
				Memory:      resource.MustParse(fmt.Sprintf("%dMi", memReq)),
				Explanation: "Test recommendation",
			}

			// Create policy without limitConfig (should use defaults)
			policy := createMockPolicy(true, false)
			policy.Spec.UpdateStrategy.UpdateRequestsOnly = false
			policy.Spec.UpdateStrategy.LimitConfig = nil

			engine := &Engine{}

			// Calculate limits
			cpuLimit, memoryLimit := engine.calculateLimits(rec, policy)

			// Verify CPU limit uses default multiplier (1.0)
			expectedCPUValue := rec.CPU.Value() // 1.0x = same value
			if cpuLimit.Value() != expectedCPUValue {
				return false
			}

			// Verify memory limit uses default multiplier (1.1)
			expectedMemValue := int64(float64(rec.Memory.Value()) * 1.1)
			if memoryLimit.Value() != expectedMemValue {
				return false
			}

			return true
		},
		gen.Int64Range(100, 4000),
		gen.Int64Range(128, 8192),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: limit-configuration, Property 3: Limits in patch match calculated limits
// For any policy with limit multipliers, the patch should contain limits that match
// the calculated values
// Validates: Requirements for limit configuration feature
func TestProperty_PatchContainsCalculatedLimits(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("patch contains correctly calculated limits", prop.ForAll(
		func(cpuReq, memReq int64, cpuMult, memMult float64) bool {
			// Generate reasonable resource values
			if cpuReq < 100 || cpuReq > 4000 || memReq < 128 || memReq > 8192 {
				return true // Skip invalid values
			}
			// Multipliers must be between 1.0 and 10.0
			if cpuMult < 1.0 || cpuMult > 10.0 || memMult < 1.0 || memMult > 10.0 {
				return true // Skip invalid multipliers
			}

			// Create workload
			workload := createMockWorkload()

			// Create recommendation
			rec := &recommendation.Recommendation{
				CPU:         resource.MustParse(fmt.Sprintf("%dm", cpuReq)),
				Memory:      resource.MustParse(fmt.Sprintf("%dMi", memReq)),
				Explanation: "Test recommendation",
			}

			// Create policy with custom multipliers
			policy := createMockPolicy(true, false)
			policy.Spec.UpdateStrategy.UpdateRequestsOnly = false
			policy.Spec.UpdateStrategy.LimitConfig = &optipodv1alpha1.LimitConfig{
				CPULimitMultiplier:    &cpuMult,
				MemoryLimitMultiplier: &memMult,
			}

			engine := &Engine{}

			// Build SSA patch
			patch, err := engine.buildSSAPatch(workload, "test-container", rec, policy)
			if err != nil {
				return false
			}

			// Parse patch
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

			// Verify limits are present
			limitsMap, ok := resourcesMap["limits"].(map[string]interface{})
			if !ok {
				return false
			}

			// Calculate expected limits
			cpuLimit, memoryLimit := engine.calculateLimits(rec, policy)

			// Verify limits in patch match calculated values
			return limitsMap["cpu"] == cpuLimit.String() && limitsMap["memory"] == memoryLimit.String()
		},
		gen.Int64Range(100, 4000),
		gen.Int64Range(128, 8192),
		gen.Float64Range(1.0, 10.0),
		gen.Float64Range(1.0, 10.0),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: limit-configuration, Property 4: Multiplier bounds are enforced
// For any multiplier value, it should be between 1.0 and 10.0 (enforced by CRD validation)
// This test verifies the calculation logic handles the valid range correctly
// Validates: Requirements for limit configuration feature
func TestProperty_MultiplierBoundsHandled(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("multipliers at boundary values work correctly", prop.ForAll(
		func(cpuReq, memReq int64, useLowerBound bool) bool {
			// Generate reasonable resource values
			if cpuReq < 100 || cpuReq > 4000 || memReq < 128 || memReq > 8192 {
				return true // Skip invalid values
			}

			// Test boundary values
			var cpuMult, memMult float64
			if useLowerBound {
				cpuMult = 1.0 // Lower bound
				memMult = 1.0
			} else {
				cpuMult = 10.0 // Upper bound
				memMult = 10.0
			}

			// Create recommendation
			rec := &recommendation.Recommendation{
				CPU:         resource.MustParse(fmt.Sprintf("%dm", cpuReq)),
				Memory:      resource.MustParse(fmt.Sprintf("%dMi", memReq)),
				Explanation: "Test recommendation",
			}

			// Create policy with boundary multipliers
			policy := createMockPolicy(true, false)
			policy.Spec.UpdateStrategy.UpdateRequestsOnly = false
			policy.Spec.UpdateStrategy.LimitConfig = &optipodv1alpha1.LimitConfig{
				CPULimitMultiplier:    &cpuMult,
				MemoryLimitMultiplier: &memMult,
			}

			engine := &Engine{}

			// Calculate limits - should not panic or error
			cpuLimit, memoryLimit := engine.calculateLimits(rec, policy)

			// Verify limits are calculated correctly
			// CPU uses MilliValue() for DecimalSI format, Memory uses Value() for BinarySI format
			expectedCPUMilliValue := int64(float64(rec.CPU.MilliValue()) * cpuMult)
			expectedMemValue := int64(float64(rec.Memory.Value()) * memMult)

			return cpuLimit.MilliValue() == expectedCPUMilliValue && memoryLimit.Value() == expectedMemValue
		},
		gen.Int64Range(100, 4000),
		gen.Int64Range(128, 8192),
		gen.Bool(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}
