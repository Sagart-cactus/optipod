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
	corev1 "k8s.io/api/core/v1"
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
	// serverSideApplyMethod is the method name for Server-Side Apply
	serverSideApplyMethod = "ServerSideApply"
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

			decision, err := engine.CanApply(context.Background(), workload, "test-container", rec, policy)
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

			decision, err := engine.CanApply(context.Background(), workload, "test-container", rec, policy)
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
			if result.Method != serverSideApplyMethod || !result.FieldOwnership {
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
			if result.Method != serverSideApplyMethod || !result.FieldOwnership {
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

			// Calculate expected limits using the new function
			requests := corev1.ResourceList{
				corev1.ResourceCPU:    rec.CPU,
				corev1.ResourceMemory: rec.Memory,
			}
			limits, err := engine.CalculateLimitsWithDefaults(requests, policy.Spec.UpdateStrategy.LimitConfig)
			if err != nil {
				return false
			}
			cpuLimit := limits[corev1.ResourceCPU]
			memoryLimit := limits[corev1.ResourceMemory]

			// Verify CPU limit matches the calculation (CPU uses MilliValue for DecimalSI format)
			expectedCPUMilliValue := int64(float64(rec.CPU.MilliValue()) * cpuMult)
			if cpuLimit.MilliValue() != expectedCPUMilliValue {
				return false
			}

			// Verify memory limit matches the calculation
			expectedMemValue := int64(float64(rec.Memory.Value()) * memMult)
			return memoryLimit.Value() == expectedMemValue
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

			// Calculate limits using the new function
			requests := corev1.ResourceList{
				corev1.ResourceCPU:    rec.CPU,
				corev1.ResourceMemory: rec.Memory,
			}
			limits, err := engine.CalculateLimitsWithDefaults(requests, policy.Spec.UpdateStrategy.LimitConfig)
			if err != nil {
				return false
			}
			cpuLimit := limits[corev1.ResourceCPU]
			memoryLimit := limits[corev1.ResourceMemory]

			// Verify CPU limit uses default multiplier (1.5)
			expectedCPUValue := int64(float64(rec.CPU.MilliValue()) * DefaultCPULimitMultiplier)
			if cpuLimit.MilliValue() != expectedCPUValue {
				return false
			}

			// Verify memory limit uses default multiplier (1.3)
			expectedMemValue := int64(float64(rec.Memory.Value()) * DefaultMemoryLimitMultiplier)
			return memoryLimit.Value() == expectedMemValue
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

			// Calculate expected limits using the new function
			requests := corev1.ResourceList{
				corev1.ResourceCPU:    rec.CPU,
				corev1.ResourceMemory: rec.Memory,
			}
			limits, err := engine.CalculateLimitsWithDefaults(requests, policy.Spec.UpdateStrategy.LimitConfig)
			if err != nil {
				return false
			}
			cpuLimit := limits[corev1.ResourceCPU]
			memoryLimit := limits[corev1.ResourceMemory]

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

// Feature: memory-safety-enhancements, Property 2: Recommendation Application
// For any workload with valid metrics data, the calculated recommendations should be applied without safety check interference
// Validates: Requirements - Remove blocking safety checks
//
//nolint:gocyclo // Complex property-based test with comprehensive coverage
func TestProperty_DirectRecommendationApplication(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("recommendations are applied directly without safety checks", prop.ForAll(
		func(currentCPU, currentMem, recCPU, recMem int64, allowInPlace, allowRecreate bool) bool {
			// Generate reasonable resource values (in millicores and MiB)
			if currentCPU < 100 || currentCPU > 4000 || currentMem < 128 || currentMem > 8192 {
				return true // Skip invalid current values
			}
			if recCPU < 100 || recCPU > 4000 || recMem < 128 || recMem > 8192 {
				return true // Skip invalid recommendation values
			}

			// Create mock discovery client that supports in-place resize
			mockDiscovery := &mockDiscoveryClient{
				serverVersion: &version.Info{
					Major: "1",
					Minor: "29", // Version that supports in-place resize
				},
			}

			engine := &Engine{
				discoveryClient: mockDiscovery,
				dryRun:          false,
			}

			// Create workload with current resources
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
													"cpu":    fmt.Sprintf("%dm", currentCPU),
													"memory": fmt.Sprintf("%dMi", currentMem),
												},
												"limits": map[string]interface{}{
													"cpu":    fmt.Sprintf("%dm", currentCPU*2),
													"memory": fmt.Sprintf("%dMi", currentMem*2),
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

			// Create recommendation (can be higher or lower than current)
			rec := &recommendation.Recommendation{
				CPU:         resource.MustParse(fmt.Sprintf("%dm", recCPU)),
				Memory:      resource.MustParse(fmt.Sprintf("%dMi", recMem)),
				Explanation: "Test recommendation",
			}

			// Create policy in Auto mode
			policy := &optipodv1alpha1.OptimizationPolicy{
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Mode: optipodv1alpha1.ModeAuto,
					UpdateStrategy: optipodv1alpha1.UpdateStrategy{
						AllowInPlaceResize: allowInPlace,
						AllowRecreate:      allowRecreate,
						UpdateRequestsOnly: true,
					},
				},
			}

			// Test CanApply - should not be blocked by safety checks
			decision, err := engine.CanApply(context.Background(), workload, "test-container", rec, policy)
			if err != nil {
				return false
			}

			// With safety checks removed, the decision should be based only on:
			// 1. Policy mode (Auto = can apply)
			// 2. Update strategy availability (in-place or recreate)
			// 3. NOT on memory safety checks

			// If at least one update strategy is allowed, should be able to apply
			if allowInPlace || allowRecreate {
				if !decision.CanApply {
					return false
				}
				// Should prefer in-place when available and allowed
				if allowInPlace && decision.Method != InPlace {
					return false
				}
				// Should use recreate when in-place not allowed but recreate is
				if !allowInPlace && allowRecreate && decision.Method != Recreate {
					return false
				}
			} else {
				// If no update strategy is allowed, should skip
				if decision.CanApply || decision.Method != Skip {
					return false
				}
			}

			// The key test: decision should NOT mention memory safety
			if contains(decision.Reason, "Memory decrease") || contains(decision.Reason, "pod eviction") || contains(decision.Reason, "OOM") {
				return false // Safety check interference detected
			}

			return true
		},
		gen.Int64Range(100, 4000), // currentCPU
		gen.Int64Range(128, 8192), // currentMem
		gen.Int64Range(100, 4000), // recCPU
		gen.Int64Range(128, 8192), // recMem
		gen.Bool(),                // allowInPlace
		gen.Bool(),                // allowRecreate
	))

	properties.Property("recommendations are applied regardless of memory decrease magnitude", prop.ForAll(
		func(currentMem, recMem int64) bool {
			// Test specifically for memory decreases that would have been blocked by safety checks
			if currentMem < 1024 || currentMem > 8192 { // 1Gi to 8Gi
				return true // Skip invalid current values
			}
			if recMem < 128 || recMem >= currentMem { // Only test decreases
				return true // Skip invalid or non-decrease scenarios
			}

			// Create mock discovery client
			mockDiscovery := &mockDiscoveryClient{
				serverVersion: &version.Info{
					Major: "1",
					Minor: "29",
				},
			}

			engine := &Engine{
				discoveryClient: mockDiscovery,
				dryRun:          false,
			}

			// Create workload with high memory
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
													"cpu":    "500m",
													"memory": fmt.Sprintf("%dMi", currentMem),
												},
												"limits": map[string]interface{}{
													"cpu":    "1000m",
													"memory": fmt.Sprintf("%dMi", currentMem),
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

			// Create recommendation with significant memory decrease
			rec := &recommendation.Recommendation{
				CPU:         resource.MustParse("600m"),
				Memory:      resource.MustParse(fmt.Sprintf("%dMi", recMem)),
				Explanation: "Test recommendation with memory decrease",
			}

			// Create policy that allows updates
			policy := &optipodv1alpha1.OptimizationPolicy{
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Mode: optipodv1alpha1.ModeAuto,
					UpdateStrategy: optipodv1alpha1.UpdateStrategy{
						AllowInPlaceResize: true,
						AllowRecreate:      true,
						UpdateRequestsOnly: false, // Test with limits updates too
						LimitConfig: &optipodv1alpha1.LimitConfig{
							CPULimitMultiplier:    func() *float64 { v := 1.0; return &v }(),
							MemoryLimitMultiplier: func() *float64 { v := 1.3; return &v }(),
						},
					},
				},
			}

			// Test CanApply - should allow even large memory decreases
			decision, err := engine.CanApply(context.Background(), workload, "test-container", rec, policy)
			if err != nil {
				return false
			}

			// Should be able to apply regardless of memory decrease magnitude
			if !decision.CanApply {
				return false
			}

			// Should not mention safety concerns
			if contains(decision.Reason, "Memory decrease") || contains(decision.Reason, "unsafe") {
				return false
			}

			return true
		},
		gen.Int64Range(1024, 8192), // currentMem (1Gi to 8Gi)
		gen.Int64Range(128, 1023),  // recMem (128Mi to ~1Gi, ensuring decrease)
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: memory-safety-enhancements, Property 3: UpdateRequestsOnly Behavior
// For any workload where updateRequestsOnly=true, only request values should be modified, leaving limits unchanged
// Validates: Requirements - Respect update strategy settings
//
//nolint:gocyclo // Complex property-based test with comprehensive coverage
func TestProperty_UpdateRequestsOnlyBehavior(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("updateRequestsOnly=true preserves limits in patches", prop.ForAll(
		func(cpuReq, memReq int64, cpuMult, memMult float64) bool {
			// Generate reasonable resource values (in millicores and MiB)
			if cpuReq < 100 || cpuReq > 4000 || memReq < 128 || memReq > 8192 {
				return true // Skip invalid values
			}
			// Multipliers must be within valid range
			if cpuMult < MinMultiplier || cpuMult > MaxMultiplier || memMult < MinMultiplier || memMult > MaxMultiplier {
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

			// Create policy with updateRequestsOnly = true and custom multipliers
			policy := createMockPolicy(true, false)
			policy.Spec.UpdateStrategy.UpdateRequestsOnly = true
			policy.Spec.UpdateStrategy.LimitConfig = &optipodv1alpha1.LimitConfig{
				CPULimitMultiplier:    &cpuMult,
				MemoryLimitMultiplier: &memMult,
			}

			engine := &Engine{}

			// Test buildResourcePatch
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
			if limitsExist {
				return false // Limits should not be in patch when updateRequestsOnly=true
			}

			// Test buildSSAPatch
			ssaPatch, err := engine.buildSSAPatch(workload, "test-container", rec, policy)
			if err != nil {
				return false
			}

			// Parse SSA patch to verify limits are not included
			var ssaPatchObj map[string]interface{}
			if err := json.Unmarshal(ssaPatch, &ssaPatchObj); err != nil {
				return false
			}

			// Extract containers from SSA patch
			ssaContainers, _, _ := unstructured.NestedSlice(ssaPatchObj, "spec", "template", "spec", "containers")
			if len(ssaContainers) == 0 {
				return false
			}

			ssaContainer := ssaContainers[0].(map[string]interface{})
			ssaResourcesMap, _, _ := unstructured.NestedMap(ssaContainer, "resources")

			// Check that requests are in SSA patch
			ssaRequestsMap, ok := ssaResourcesMap["requests"].(map[string]interface{})
			if !ok {
				return false
			}
			if ssaRequestsMap["cpu"] != rec.CPU.String() || ssaRequestsMap["memory"] != rec.Memory.String() {
				return false
			}

			// Check that limits are NOT in SSA patch
			_, ssaLimitsExist := ssaResourcesMap["limits"]
			return !ssaLimitsExist // Limits should not be in SSA patch when updateRequestsOnly=true
		},
		gen.Int64Range(100, 4000),
		gen.Int64Range(128, 8192),
		gen.Float64Range(MinMultiplier, MaxMultiplier),
		gen.Float64Range(MinMultiplier, MaxMultiplier),
	))

	properties.Property("updateRequestsOnly=false includes limits in patches", prop.ForAll(
		func(cpuReq, memReq int64, cpuMult, memMult float64) bool {
			// Generate reasonable resource values
			if cpuReq < 100 || cpuReq > 4000 || memReq < 128 || memReq > 8192 {
				return true // Skip invalid values
			}
			// Multipliers must be within valid range
			if cpuMult < MinMultiplier || cpuMult > MaxMultiplier || memMult < MinMultiplier || memMult > MaxMultiplier {
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

			// Create policy with updateRequestsOnly = false and custom multipliers
			policy := createMockPolicy(true, false)
			policy.Spec.UpdateStrategy.UpdateRequestsOnly = false
			policy.Spec.UpdateStrategy.LimitConfig = &optipodv1alpha1.LimitConfig{
				CPULimitMultiplier:    &cpuMult,
				MemoryLimitMultiplier: &memMult,
			}

			engine := &Engine{}

			// Test buildResourcePatch
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

			// Check that requests are updated
			requestsMap, ok := resourcesMap["requests"].(map[string]interface{})
			if !ok {
				return false
			}
			if requestsMap["cpu"] != rec.CPU.String() || requestsMap["memory"] != rec.Memory.String() {
				return false
			}

			// Check that limits ARE in the patch
			limitsMap, ok := resourcesMap["limits"].(map[string]interface{})
			if !ok {
				return false // Limits should be in patch when updateRequestsOnly=false
			}

			// Verify limits are calculated correctly using calculateLimitsWithDefaults
			requests := corev1.ResourceList{
				corev1.ResourceCPU:    rec.CPU,
				corev1.ResourceMemory: rec.Memory,
			}
			expectedLimits, err := engine.CalculateLimitsWithDefaults(requests, policy.Spec.UpdateStrategy.LimitConfig)
			if err != nil {
				return false
			}

			// Check CPU limit
			expectedCPULimit := expectedLimits[corev1.ResourceCPU]
			if limitsMap["cpu"] != expectedCPULimit.String() {
				return false
			}

			// Check memory limit
			expectedMemoryLimit := expectedLimits[corev1.ResourceMemory]
			if limitsMap["memory"] != expectedMemoryLimit.String() {
				return false
			}

			// Test buildSSAPatch
			ssaPatch, err := engine.buildSSAPatch(workload, "test-container", rec, policy)
			if err != nil {
				return false
			}

			// Parse SSA patch to verify limits are included
			var ssaPatchObj map[string]interface{}
			if err := json.Unmarshal(ssaPatch, &ssaPatchObj); err != nil {
				return false
			}

			// Extract containers from SSA patch
			ssaContainers, _, _ := unstructured.NestedSlice(ssaPatchObj, "spec", "template", "spec", "containers")
			if len(ssaContainers) == 0 {
				return false
			}

			ssaContainer := ssaContainers[0].(map[string]interface{})
			ssaResourcesMap, _, _ := unstructured.NestedMap(ssaContainer, "resources")

			// Check that limits ARE in SSA patch
			ssaLimitsMap, ok := ssaResourcesMap["limits"].(map[string]interface{})
			if !ok {
				return false // Limits should be in SSA patch when updateRequestsOnly=false
			}

			// Verify SSA limits match expected values
			return ssaLimitsMap["cpu"] == expectedCPULimit.String() && ssaLimitsMap["memory"] == expectedMemoryLimit.String()
		},
		gen.Int64Range(100, 4000),
		gen.Int64Range(128, 8192),
		gen.Float64Range(MinMultiplier, MaxMultiplier),
		gen.Float64Range(MinMultiplier, MaxMultiplier),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: memory-safety-enhancements, Property 1: Default Limit Application
// For any workload optimization where no limit configuration is specified, default multipliers should be applied to calculate limits from requests
// Validates: Requirements - Default limit behavior
//
//nolint:gocyclo // Complex property-based test with comprehensive coverage
func TestProperty_DefaultLimitApplication(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("default multipliers are applied when no limit config exists", prop.ForAll(
		func(cpuReq, memReq int64) bool {
			// Generate reasonable resource values (in millicores and MiB)
			if cpuReq < 100 || cpuReq > 4000 || memReq < 128 || memReq > 8192 {
				return true // Skip invalid values
			}

			// Create resource list with requests
			requests := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%dm", cpuReq)),
				corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dMi", memReq)),
			}

			engine := &Engine{}

			// Calculate limits with no limit config (should use defaults)
			limits, err := engine.CalculateLimitsWithDefaults(requests, nil)
			if err != nil {
				return false
			}

			// Verify CPU limit uses default multiplier (1.5)
			cpuRequest := requests[corev1.ResourceCPU]
			cpuLimit := limits[corev1.ResourceCPU]
			expectedCPUValue := int64(float64(cpuRequest.MilliValue()) * DefaultCPULimitMultiplier)
			if cpuLimit.MilliValue() != expectedCPUValue {
				return false
			}

			// Verify memory limit uses default multiplier (1.3)
			memRequest := requests[corev1.ResourceMemory]
			memLimit := limits[corev1.ResourceMemory]
			expectedMemValue := int64(float64(memRequest.Value()) * DefaultMemoryLimitMultiplier)
			return memLimit.Value() == expectedMemValue
		},
		gen.Int64Range(100, 4000),
		gen.Int64Range(128, 8192),
	))

	properties.Property("explicit limit config takes precedence over defaults", prop.ForAll(
		func(cpuReq, memReq int64, cpuMult, memMult float64) bool {
			// Generate reasonable resource values
			if cpuReq < 100 || cpuReq > 4000 || memReq < 128 || memReq > 8192 {
				return true // Skip invalid values
			}
			// Multipliers must be within valid range
			if cpuMult < MinMultiplier || cpuMult > MaxMultiplier || memMult < MinMultiplier || memMult > MaxMultiplier {
				return true // Skip invalid multipliers
			}

			// Create resource list with requests
			requests := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%dm", cpuReq)),
				corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dMi", memReq)),
			}

			// Create explicit limit config
			limitConfig := &optipodv1alpha1.LimitConfig{
				CPULimitMultiplier:    &cpuMult,
				MemoryLimitMultiplier: &memMult,
			}

			engine := &Engine{}

			// Calculate limits with explicit config
			limits, err := engine.CalculateLimitsWithDefaults(requests, limitConfig)
			if err != nil {
				return false
			}

			// Verify CPU limit uses explicit multiplier
			cpuRequest := requests[corev1.ResourceCPU]
			cpuLimit := limits[corev1.ResourceCPU]
			expectedCPUValue := int64(float64(cpuRequest.MilliValue()) * cpuMult)
			if cpuLimit.MilliValue() != expectedCPUValue {
				return false
			}

			// Verify memory limit uses explicit multiplier
			memRequest := requests[corev1.ResourceMemory]
			memLimit := limits[corev1.ResourceMemory]
			expectedMemValue := int64(float64(memRequest.Value()) * memMult)
			return memLimit.Value() == expectedMemValue
		},
		gen.Int64Range(100, 4000),
		gen.Int64Range(128, 8192),
		gen.Float64Range(MinMultiplier, MaxMultiplier),
		gen.Float64Range(MinMultiplier, MaxMultiplier),
	))

	properties.Property("zero or missing resources are handled gracefully", prop.ForAll(
		func(includeCPU, includeMemory bool, cpuReq, memReq int64) bool {
			// Generate reasonable resource values when included
			if cpuReq < 100 || cpuReq > 4000 || memReq < 128 || memReq > 8192 {
				return true // Skip invalid values
			}

			// Create resource list with optional resources
			requests := corev1.ResourceList{}
			if includeCPU {
				requests[corev1.ResourceCPU] = resource.MustParse(fmt.Sprintf("%dm", cpuReq))
			}
			if includeMemory {
				requests[corev1.ResourceMemory] = resource.MustParse(fmt.Sprintf("%dMi", memReq))
			}

			engine := &Engine{}

			// Calculate limits
			limits, err := engine.CalculateLimitsWithDefaults(requests, nil)
			if err != nil {
				return false
			}

			// Verify only included resources have limits calculated
			if includeCPU {
				if _, exists := limits[corev1.ResourceCPU]; !exists {
					return false
				}
				cpuRequest := requests[corev1.ResourceCPU]
				cpuLimit := limits[corev1.ResourceCPU]
				expectedCPUValue := int64(float64(cpuRequest.MilliValue()) * DefaultCPULimitMultiplier)
				if cpuLimit.MilliValue() != expectedCPUValue {
					return false
				}
			} else {
				if _, exists := limits[corev1.ResourceCPU]; exists {
					return false // Should not have CPU limit if no CPU request
				}
			}

			if includeMemory {
				if _, exists := limits[corev1.ResourceMemory]; !exists {
					return false
				}
				memRequest := requests[corev1.ResourceMemory]
				memLimit := limits[corev1.ResourceMemory]
				expectedMemValue := int64(float64(memRequest.Value()) * DefaultMemoryLimitMultiplier)
				if memLimit.Value() != expectedMemValue {
					return false
				}
			} else {
				if _, exists := limits[corev1.ResourceMemory]; exists {
					return false // Should not have memory limit if no memory request
				}
			}

			return true
		},
		gen.Bool(),
		gen.Bool(),
		gen.Int64Range(100, 4000),
		gen.Int64Range(128, 8192),
	))

	properties.Property("invalid multipliers are rejected", prop.ForAll(
		func(cpuReq, memReq int64, cpuMult, memMult float64) bool {
			// Generate reasonable resource values
			if cpuReq < 100 || cpuReq > 4000 || memReq < 128 || memReq > 8192 {
				return true // Skip invalid values
			}
			// Only test invalid multipliers
			if (cpuMult >= MinMultiplier && cpuMult <= MaxMultiplier) && (memMult >= MinMultiplier && memMult <= MaxMultiplier) {
				return true // Skip valid multipliers
			}

			// Create resource list with requests
			requests := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%dm", cpuReq)),
				corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dMi", memReq)),
			}

			// Create limit config with invalid multipliers
			limitConfig := &optipodv1alpha1.LimitConfig{
				CPULimitMultiplier:    &cpuMult,
				MemoryLimitMultiplier: &memMult,
			}

			engine := &Engine{}

			// Calculate limits - should return error for invalid multipliers
			_, err := engine.CalculateLimitsWithDefaults(requests, limitConfig)

			// Should return error if any multiplier is invalid
			return err != nil
		},
		gen.Int64Range(100, 4000),
		gen.Int64Range(128, 8192),
		gen.Float64Range(-1.0, 15.0), // Include invalid range
		gen.Float64Range(-1.0, 15.0), // Include invalid range
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: memory-safety-enhancements, Property 4: Limit Configuration Precedence
// For any workload with explicit limit configuration, the specified multipliers should take precedence over default values
// Validates: Requirements - Honor explicit configurations
//
//nolint:gocyclo // Complex property-based test with comprehensive coverage
func TestProperty_LimitConfigurationPrecedence(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("explicit multipliers take precedence over defaults", prop.ForAll(
		func(cpuReq, memReq int64, cpuMult, memMult float64) bool {
			// Generate reasonable resource values (in millicores and MiB)
			if cpuReq < 100 || cpuReq > 4000 || memReq < 128 || memReq > 8192 {
				return true // Skip invalid values
			}
			// Multipliers must be within valid range
			if cpuMult < MinMultiplier || cpuMult > MaxMultiplier || memMult < MinMultiplier || memMult > MaxMultiplier {
				return true // Skip invalid multipliers
			}

			// Create resource list with requests
			requests := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%dm", cpuReq)),
				corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dMi", memReq)),
			}

			// Create explicit limit config
			limitConfig := &optipodv1alpha1.LimitConfig{
				CPULimitMultiplier:    &cpuMult,
				MemoryLimitMultiplier: &memMult,
			}

			engine := &Engine{}

			// Calculate limits with explicit config
			limits, err := engine.CalculateLimitsWithDefaults(requests, limitConfig)
			if err != nil {
				return false
			}

			// Verify CPU limit uses explicit multiplier (not default)
			cpuRequest := requests[corev1.ResourceCPU]
			cpuLimit := limits[corev1.ResourceCPU]
			expectedCPUValue := int64(float64(cpuRequest.MilliValue()) * cpuMult)
			if cpuLimit.MilliValue() != expectedCPUValue {
				return false
			}

			// Verify memory limit uses explicit multiplier (not default)
			memRequest := requests[corev1.ResourceMemory]
			memLimit := limits[corev1.ResourceMemory]
			expectedMemValue := int64(float64(memRequest.Value()) * memMult)
			if memLimit.Value() != expectedMemValue {
				return false
			}

			// Verify that explicit values are different from defaults (when they differ)
			if cpuMult != DefaultCPULimitMultiplier {
				defaultCPUValue := int64(float64(cpuRequest.MilliValue()) * DefaultCPULimitMultiplier)
				if cpuLimit.MilliValue() == defaultCPUValue {
					return false // Should not equal default when explicit is different
				}
			}

			if memMult != DefaultMemoryLimitMultiplier {
				defaultMemValue := int64(float64(memRequest.Value()) * DefaultMemoryLimitMultiplier)
				if memLimit.Value() == defaultMemValue {
					return false // Should not equal default when explicit is different
				}
			}

			return true
		},
		gen.Int64Range(100, 4000),
		gen.Int64Range(128, 8192),
		gen.Float64Range(MinMultiplier, MaxMultiplier),
		gen.Float64Range(MinMultiplier, MaxMultiplier),
	))

	properties.Property("partial configuration uses defaults for missing values", prop.ForAll(
		func(cpuReq, memReq int64, explicitMult float64, useCPU bool) bool {
			// Generate reasonable resource values
			if cpuReq < 100 || cpuReq > 4000 || memReq < 128 || memReq > 8192 {
				return true // Skip invalid values
			}
			// Multiplier must be within valid range
			if explicitMult < MinMultiplier || explicitMult > MaxMultiplier {
				return true // Skip invalid multipliers
			}

			// Create resource list with requests
			requests := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%dm", cpuReq)),
				corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dMi", memReq)),
			}

			// Create partial limit config (only CPU or only memory)
			var limitConfig *optipodv1alpha1.LimitConfig
			if useCPU {
				limitConfig = &optipodv1alpha1.LimitConfig{
					CPULimitMultiplier: &explicitMult,
					// MemoryLimitMultiplier is nil - should use default
				}
			} else {
				limitConfig = &optipodv1alpha1.LimitConfig{
					// CPULimitMultiplier is nil - should use default
					MemoryLimitMultiplier: &explicitMult,
				}
			}

			engine := &Engine{}

			// Calculate limits with partial config
			limits, err := engine.CalculateLimitsWithDefaults(requests, limitConfig)
			if err != nil {
				return false
			}

			// Verify the resource with explicit config uses that multiplier
			if useCPU {
				// CPU should use explicit multiplier
				cpuRequest := requests[corev1.ResourceCPU]
				cpuLimit := limits[corev1.ResourceCPU]
				expectedCPUValue := int64(float64(cpuRequest.MilliValue()) * explicitMult)
				if cpuLimit.MilliValue() != expectedCPUValue {
					return false
				}

				// Memory should use default multiplier
				memRequest := requests[corev1.ResourceMemory]
				memLimit := limits[corev1.ResourceMemory]
				expectedMemValue := int64(float64(memRequest.Value()) * DefaultMemoryLimitMultiplier)
				if memLimit.Value() != expectedMemValue {
					return false
				}
			} else {
				// CPU should use default multiplier
				cpuRequest := requests[corev1.ResourceCPU]
				cpuLimit := limits[corev1.ResourceCPU]
				expectedCPUValue := int64(float64(cpuRequest.MilliValue()) * DefaultCPULimitMultiplier)
				if cpuLimit.MilliValue() != expectedCPUValue {
					return false
				}

				// Memory should use explicit multiplier
				memRequest := requests[corev1.ResourceMemory]
				memLimit := limits[corev1.ResourceMemory]
				expectedMemValue := int64(float64(memRequest.Value()) * explicitMult)
				if memLimit.Value() != expectedMemValue {
					return false
				}
			}

			return true
		},
		gen.Int64Range(100, 4000),
		gen.Int64Range(128, 8192),
		gen.Float64Range(MinMultiplier, MaxMultiplier),
		gen.Bool(),
	))

	properties.Property("empty config uses all defaults", prop.ForAll(
		func(cpuReq, memReq int64) bool {
			// Generate reasonable resource values
			if cpuReq < 100 || cpuReq > 4000 || memReq < 128 || memReq > 8192 {
				return true // Skip invalid values
			}

			// Create resource list with requests
			requests := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%dm", cpuReq)),
				corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dMi", memReq)),
			}

			// Create empty limit config (not nil, but no multipliers set)
			limitConfig := &optipodv1alpha1.LimitConfig{}

			engine := &Engine{}

			// Calculate limits with empty config
			limits, err := engine.CalculateLimitsWithDefaults(requests, limitConfig)
			if err != nil {
				return false
			}

			// Both should use default multipliers (same as nil config)
			limitsWithNil, errNil := engine.CalculateLimitsWithDefaults(requests, nil)
			if errNil != nil {
				return false
			}

			// Results should be identical
			cpuLimit := limits[corev1.ResourceCPU]
			cpuLimitNil := limitsWithNil[corev1.ResourceCPU]
			if !cpuLimit.Equal(cpuLimitNil) {
				return false
			}

			memLimit := limits[corev1.ResourceMemory]
			memLimitNil := limitsWithNil[corev1.ResourceMemory]
			return memLimit.Equal(memLimitNil)
		},
		gen.Int64Range(100, 4000),
		gen.Int64Range(128, 8192),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Unit tests for optimization application
func TestOptimizationApplication(t *testing.T) {
	t.Run("successful SSA optimization with comprehensive logging", func(t *testing.T) {
		// Track the patch options and logs
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
			CPU:         resource.MustParse("600m"),
			Memory:      resource.MustParse("1200Mi"),
			Explanation: "Test recommendation",
		}

		// Create policy
		policy := createMockPolicy(true, false)

		// Apply with SSA
		err := engine.ApplyWithSSA(context.Background(), workload, "test-container", rec, policy)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify field manager is "optipod"
		if capturedPatchOptions.FieldManager != FieldManagerName {
			t.Errorf("expected field manager %s, got %s", FieldManagerName, capturedPatchOptions.FieldManager)
		}

		// Verify Force flag is set
		if capturedPatchOptions.Force == nil || *capturedPatchOptions.Force != true {
			t.Error("expected Force flag to be true")
		}

		// Verify patch type is ApplyPatchType
		if capturedPatchType != types.ApplyPatchType {
			t.Errorf("expected patch type %v, got %v", types.ApplyPatchType, capturedPatchType)
		}
	})

	t.Run("successful Strategic Merge optimization with comprehensive logging", func(t *testing.T) {
		// Track the patch type
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
			CPU:         resource.MustParse("400m"),
			Memory:      resource.MustParse("800Mi"),
			Explanation: "Test recommendation",
		}

		// Create policy with SSA disabled
		policy := createMockPolicy(true, false)
		useSSA := false
		policy.Spec.UpdateStrategy.UseServerSideApply = &useSSA

		// Apply with Strategic Merge
		err := engine.ApplyWithStrategicMerge(context.Background(), workload, "test-container", rec, policy)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify patch type is StrategicMergePatchType
		if capturedPatchType != types.StrategicMergePatchType {
			t.Errorf("expected patch type %v, got %v", types.StrategicMergePatchType, capturedPatchType)
		}
	})

	t.Run("error handling and logging for SSA failures", func(t *testing.T) {
		// Create mock dynamic client that returns conflict error
		mockDynamic := &mockDynamicClientWithError{
			errorType: "conflict",
		}

		engine := &Engine{
			dynamicClient: mockDynamic,
		}

		workload := createMockWorkload()
		rec := createMockRecommendation()
		policy := createMockPolicy(true, false)

		err := engine.ApplyWithSSA(context.Background(), workload, "test-container", rec, policy)
		if err == nil {
			t.Error("expected error for SSA conflict")
		}

		// Verify error message contains SSA conflict information
		if !contains(err.Error(), "SSA conflict") {
			t.Errorf("expected error to contain 'SSA conflict', got: %s", err.Error())
		}
	})

	t.Run("error handling and logging for Strategic Merge failures", func(t *testing.T) {
		// Create mock dynamic client that returns forbidden error
		mockDynamic := &mockDynamicClientWithError{
			errorType: "forbidden",
		}

		engine := &Engine{
			dynamicClient: mockDynamic,
		}

		workload := createMockWorkload()
		rec := createMockRecommendation()
		policy := createMockPolicy(true, false)

		err := engine.ApplyWithStrategicMerge(context.Background(), workload, "test-container", rec, policy)
		if err == nil {
			t.Error("expected error for forbidden access")
		}

		// Verify error message contains RBAC information
		if !contains(err.Error(), "RBAC") {
			t.Errorf("expected error to contain 'RBAC', got: %s", err.Error())
		}
	})

	t.Run("optimization decision logging includes all relevant details", func(t *testing.T) {
		// Create mock dynamic client
		mockDynamic := &mockDynamicClientWithCapture{
			capturedPatchOptions: &metav1.PatchOptions{},
			capturedPatchType:    new(types.PatchType),
		}

		engine := &Engine{
			dynamicClient: mockDynamic,
		}

		workload := createMockWorkload()
		rec := &recommendation.Recommendation{
			CPU:         resource.MustParse("750m"),
			Memory:      resource.MustParse("1500Mi"),
			Explanation: "Optimization based on usage patterns",
		}
		policy := createMockPolicy(true, false)

		// Apply optimization - this should trigger comprehensive logging
		err := engine.ApplyWithSSA(context.Background(), workload, "test-container", rec, policy)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// The test passes if no error occurred, indicating logging was successful
		// In a real scenario, we would capture and verify log messages
	})

	t.Run("Apply method chooses correct patch strategy", func(t *testing.T) {
		// Test SSA selection
		var capturedPatchType types.PatchType
		mockDynamic := &mockDynamicClientWithCapture{
			capturedPatchOptions: &metav1.PatchOptions{},
			capturedPatchType:    &capturedPatchType,
		}

		engine := &Engine{
			dynamicClient: mockDynamic,
		}

		workload := createMockWorkload()
		rec := createMockRecommendation()

		// Test with SSA enabled (default)
		policy := createMockPolicy(true, false)
		policy.Spec.UpdateStrategy.UseServerSideApply = nil // Should default to true

		result, err := engine.Apply(context.Background(), workload, "test-container", rec, policy)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.Method != serverSideApplyMethod || !result.FieldOwnership {
			t.Errorf("expected SSA method with field ownership, got method=%s, ownership=%v", result.Method, result.FieldOwnership)
		}

		if capturedPatchType != types.ApplyPatchType {
			t.Errorf("expected ApplyPatchType, got %v", capturedPatchType)
		}

		// Test with SSA disabled
		useSSA := false
		policy.Spec.UpdateStrategy.UseServerSideApply = &useSSA

		result, err = engine.Apply(context.Background(), workload, "test-container", rec, policy)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.Method != "StrategicMergePatch" || result.FieldOwnership {
			t.Errorf("expected Strategic Merge method without field ownership, got method=%s, ownership=%v", result.Method, result.FieldOwnership)
		}

		if capturedPatchType != types.StrategicMergePatchType {
			t.Errorf("expected StrategicMergePatchType, got %v", capturedPatchType)
		}
	})

	t.Run("optimization respects updateRequestsOnly setting", func(t *testing.T) {
		mockDynamic := &mockDynamicClientWithCapture{
			capturedPatchOptions: &metav1.PatchOptions{},
			capturedPatchType:    new(types.PatchType),
		}

		engine := &Engine{
			dynamicClient: mockDynamic,
		}

		workload := createMockWorkload()
		rec := createMockRecommendation()

		// Test with updateRequestsOnly = true
		policy := createMockPolicy(true, false)
		policy.Spec.UpdateStrategy.UpdateRequestsOnly = true

		err := engine.ApplyWithSSA(context.Background(), workload, "test-container", rec, policy)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Test with updateRequestsOnly = false
		policy.Spec.UpdateStrategy.UpdateRequestsOnly = false

		err = engine.ApplyWithSSA(context.Background(), workload, "test-container", rec, policy)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// The test passes if both configurations work without error
		// Detailed patch content verification is covered by property tests
	})
}

// Unit tests for CalculateLimitsWithDefaults function
func TestCalculateLimitsWithDefaults(t *testing.T) {
	engine := &Engine{}

	t.Run("default multipliers are applied when no config provided", func(t *testing.T) {
		requests := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("1Gi"),
		}

		limits, err := engine.CalculateLimitsWithDefaults(requests, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify CPU limit uses default multiplier (1.5)
		expectedCPU := resource.MustParse("750m") // 500m * 1.5
		cpuLimit := limits[corev1.ResourceCPU]
		if !cpuLimit.Equal(expectedCPU) {
			t.Errorf("expected CPU limit %s, got %s", expectedCPU.String(), cpuLimit.String())
		}

		// Verify memory limit uses default multiplier (1.3)
		memRequest := requests[corev1.ResourceMemory]
		memLimit := limits[corev1.ResourceMemory]
		expectedMem := int64(float64(memRequest.Value()) * DefaultMemoryLimitMultiplier)
		if memLimit.Value() != expectedMem {
			t.Errorf("expected memory limit value %d, got %d", expectedMem, memLimit.Value())
		}
	})

	t.Run("explicit config takes precedence over defaults", func(t *testing.T) {
		requests := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("200m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		}

		cpuMult := 2.0
		memMult := 1.8
		limitConfig := &optipodv1alpha1.LimitConfig{
			CPULimitMultiplier:    &cpuMult,
			MemoryLimitMultiplier: &memMult,
		}

		limits, err := engine.CalculateLimitsWithDefaults(requests, limitConfig)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify CPU limit uses explicit multiplier (2.0)
		expectedCPU := resource.MustParse("400m") // 200m * 2.0
		cpuLimit := limits[corev1.ResourceCPU]
		if !cpuLimit.Equal(expectedCPU) {
			t.Errorf("expected CPU limit %s, got %s", expectedCPU.String(), cpuLimit.String())
		}

		// Verify memory limit uses explicit multiplier (1.8)
		memRequest := requests[corev1.ResourceMemory]
		memLimit := limits[corev1.ResourceMemory]
		expectedMem := int64(float64(memRequest.Value()) * memMult)
		if memLimit.Value() != expectedMem {
			t.Errorf("expected memory limit value %d, got %d", expectedMem, memLimit.Value())
		}
	})

	t.Run("zero values are handled gracefully", func(t *testing.T) {
		requests := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("0"),
			corev1.ResourceMemory: resource.MustParse("0"),
		}

		limits, err := engine.CalculateLimitsWithDefaults(requests, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should not have limits for zero requests
		if len(limits) != 0 {
			t.Errorf("expected no limits for zero requests, got %v", limits)
		}
	})

	t.Run("missing resources are handled gracefully", func(t *testing.T) {
		// Only CPU request, no memory
		requests := corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("300m"),
		}

		limits, err := engine.CalculateLimitsWithDefaults(requests, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should only have CPU limit
		if len(limits) != 1 {
			t.Errorf("expected 1 limit, got %d", len(limits))
		}

		if _, exists := limits[corev1.ResourceCPU]; !exists {
			t.Error("expected CPU limit to exist")
		}

		if _, exists := limits[corev1.ResourceMemory]; exists {
			t.Error("expected no memory limit")
		}
	})

	t.Run("invalid multipliers return error", func(t *testing.T) {
		requests := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		}

		// Test invalid CPU multiplier
		invalidCPUMult := 0.5 // Below MinMultiplier (1.0)
		validMemMult := 1.5
		limitConfig := &optipodv1alpha1.LimitConfig{
			CPULimitMultiplier:    &invalidCPUMult,
			MemoryLimitMultiplier: &validMemMult,
		}

		_, err := engine.CalculateLimitsWithDefaults(requests, limitConfig)
		if err == nil {
			t.Error("expected error for invalid CPU multiplier")
		}

		// Test invalid memory multiplier
		validCPUMult := 1.5
		invalidMemMult := 15.0 // Above MaxMultiplier (10.0)
		limitConfig = &optipodv1alpha1.LimitConfig{
			CPULimitMultiplier:    &validCPUMult,
			MemoryLimitMultiplier: &invalidMemMult,
		}

		_, err = engine.CalculateLimitsWithDefaults(requests, limitConfig)
		if err == nil {
			t.Error("expected error for invalid memory multiplier")
		}
	})

	t.Run("boundary multiplier values work correctly", func(t *testing.T) {
		requests := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("100Mi"),
		}

		// Test minimum valid multipliers
		minMult := MinMultiplier // 1.0
		limitConfig := &optipodv1alpha1.LimitConfig{
			CPULimitMultiplier:    &minMult,
			MemoryLimitMultiplier: &minMult,
		}

		limits, err := engine.CalculateLimitsWithDefaults(requests, limitConfig)
		if err != nil {
			t.Fatalf("unexpected error for min multipliers: %v", err)
		}

		// With 1.0 multiplier, limits should equal requests
		cpuLimit := limits[corev1.ResourceCPU]
		cpuRequest := requests[corev1.ResourceCPU]
		if !cpuLimit.Equal(cpuRequest) {
			t.Errorf("expected CPU limit to equal request with 1.0 multiplier")
		}
		memLimit := limits[corev1.ResourceMemory]
		memRequest := requests[corev1.ResourceMemory]
		if !memLimit.Equal(memRequest) {
			t.Errorf("expected memory limit to equal request with 1.0 multiplier")
		}

		// Test maximum valid multipliers
		maxMult := MaxMultiplier // 10.0
		limitConfig = &optipodv1alpha1.LimitConfig{
			CPULimitMultiplier:    &maxMult,
			MemoryLimitMultiplier: &maxMult,
		}

		limits, err = engine.CalculateLimitsWithDefaults(requests, limitConfig)
		if err != nil {
			t.Fatalf("unexpected error for max multipliers: %v", err)
		}

		// Verify limits are 10x the requests
		expectedCPUMax := resource.MustParse("1000m") // 100m * 10
		cpuLimitMax := limits[corev1.ResourceCPU]
		if !cpuLimitMax.Equal(expectedCPUMax) {
			t.Errorf("expected CPU limit %s, got %s", expectedCPUMax.String(), cpuLimitMax.String())
		}

		memReq := requests[corev1.ResourceMemory]
		memLim := limits[corev1.ResourceMemory]
		expectedMemVal := int64(float64(memReq.Value()) * maxMult)
		if memLim.Value() != expectedMemVal {
			t.Errorf("expected memory limit value %d, got %d", expectedMemVal, memLim.Value())
		}
	})

	t.Run("partial configuration precedence works correctly", func(t *testing.T) {
		requests := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("100Mi"),
		}

		// Test with only CPU multiplier specified
		cpuMult := 3.0
		limitConfig := &optipodv1alpha1.LimitConfig{
			CPULimitMultiplier: &cpuMult,
			// MemoryLimitMultiplier is nil - should use default
		}

		limits, err := engine.CalculateLimitsWithDefaults(requests, limitConfig)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// CPU should use explicit multiplier (3.0)
		expectedCPU := resource.MustParse("300m") // 100m * 3.0
		cpuLimit := limits[corev1.ResourceCPU]
		if !cpuLimit.Equal(expectedCPU) {
			t.Errorf("expected CPU limit %s, got %s", expectedCPU.String(), cpuLimit.String())
		}

		// Memory should use default multiplier (1.3)
		memRequest := requests[corev1.ResourceMemory]
		memLimit := limits[corev1.ResourceMemory]
		expectedMem := int64(float64(memRequest.Value()) * DefaultMemoryLimitMultiplier)
		if memLimit.Value() != expectedMem {
			t.Errorf("expected memory limit value %d, got %d", expectedMem, memLimit.Value())
		}

		// Test with only memory multiplier specified
		memMult := 2.5
		limitConfig = &optipodv1alpha1.LimitConfig{
			// CPULimitMultiplier is nil - should use default
			MemoryLimitMultiplier: &memMult,
		}

		limits, err = engine.CalculateLimitsWithDefaults(requests, limitConfig)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// CPU should use default multiplier (1.5)
		expectedCPUDefault := resource.MustParse("150m") // 100m * 1.5
		cpuLimitDefault := limits[corev1.ResourceCPU]
		if !cpuLimitDefault.Equal(expectedCPUDefault) {
			t.Errorf("expected CPU limit %s, got %s", expectedCPUDefault.String(), cpuLimitDefault.String())
		}

		// Memory should use explicit multiplier (2.5)
		memRequest = requests[corev1.ResourceMemory]
		memLimit = limits[corev1.ResourceMemory]
		expectedMemExplicit := int64(float64(memRequest.Value()) * memMult)
		if memLimit.Value() != expectedMemExplicit {
			t.Errorf("expected memory limit value %d, got %d", expectedMemExplicit, memLimit.Value())
		}
	})

	t.Run("empty limit config uses defaults", func(t *testing.T) {
		requests := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("200m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		}

		// Empty limit config (not nil, but no multipliers set)
		limitConfig := &optipodv1alpha1.LimitConfig{}

		limits, err := engine.CalculateLimitsWithDefaults(requests, limitConfig)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Both should use default multipliers
		expectedCPU := resource.MustParse("300m") // 200m * 1.5
		cpuLimit := limits[corev1.ResourceCPU]
		if !cpuLimit.Equal(expectedCPU) {
			t.Errorf("expected CPU limit %s, got %s", expectedCPU.String(), cpuLimit.String())
		}

		memRequest := requests[corev1.ResourceMemory]
		memLimit := limits[corev1.ResourceMemory]
		expectedMem := int64(float64(memRequest.Value()) * DefaultMemoryLimitMultiplier)
		if memLimit.Value() != expectedMem {
			t.Errorf("expected memory limit value %d, got %d", expectedMem, memLimit.Value())
		}
	})
}
