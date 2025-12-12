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
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/observability"
	"github.com/optipod/optipod/internal/recommendation"
)

// Workload kind constants
const (
	kindDeployment  = "Deployment"
	kindStatefulSet = "StatefulSet"
	kindDaemonSet   = "DaemonSet"
	// FieldManagerName is the field manager name used by optipod
	FieldManagerName = "optipod"
)

// ApplyMethod defines how resource changes should be applied
type ApplyMethod string

const (
	// InPlace applies changes using in-place pod resize
	InPlace ApplyMethod = "InPlace"
	// Recreate applies changes by recreating pods
	Recreate ApplyMethod = "Recreate"
	// Skip skips applying changes
	Skip ApplyMethod = "Skip"
)

// ApplyDecision represents the decision about whether and how to apply changes
type ApplyDecision struct {
	CanApply bool
	Method   ApplyMethod
	Reason   string
}

// Workload represents a Kubernetes workload resource
type Workload struct {
	Kind      string
	Namespace string
	Name      string
	Object    *unstructured.Unstructured
}

// Engine handles application of resource recommendations to workloads
type Engine struct {
	client          client.Client
	dynamicClient   dynamic.Interface
	discoveryClient discovery.DiscoveryInterface
	dryRun          bool
}

// NewEngine creates a new application engine
func NewEngine(c client.Client, dynamicClient dynamic.Interface, discoveryClient discovery.DiscoveryInterface, dryRun bool) *Engine {
	return &Engine{
		client:          c,
		dynamicClient:   dynamicClient,
		discoveryClient: discoveryClient,
		dryRun:          dryRun,
	}
}

// CanApply determines if changes can be applied to a workload
func (e *Engine) CanApply(
	ctx context.Context,
	workload *Workload,
	rec *recommendation.Recommendation,
	policy *optipodv1alpha1.OptimizationPolicy,
) (*ApplyDecision, error) {
	// Check policy mode
	if policy.Spec.Mode == optipodv1alpha1.ModeRecommend {
		return &ApplyDecision{
			CanApply: false,
			Method:   Skip,
			Reason:   "Policy is in Recommend mode",
		}, nil
	}

	if policy.Spec.Mode == optipodv1alpha1.ModeDisabled {
		return &ApplyDecision{
			CanApply: false,
			Method:   Skip,
			Reason:   "Policy is disabled",
		}, nil
	}

	// Check global dry-run
	if e.dryRun {
		return &ApplyDecision{
			CanApply: false,
			Method:   Skip,
			Reason:   "Global dry-run mode is enabled",
		}, nil
	}

	// Get current container resources
	currentResources, err := e.getCurrentResources(workload)
	if err != nil {
		return nil, fmt.Errorf("failed to get current resources: %w", err)
	}

	// Check for memory decrease safety
	if e.isUnsafeMemoryDecrease(currentResources, rec) {
		return &ApplyDecision{
			CanApply: false,
			Method:   Skip,
			Reason:   "Memory decrease could cause pod eviction or OOM",
		}, nil
	}

	// Detect in-place resize capability
	inPlaceSupported, err := e.detectInPlaceResize(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to detect in-place resize capability: %w", err)
	}

	// Determine apply method based on support and policy
	if inPlaceSupported && policy.Spec.UpdateStrategy.AllowInPlaceResize {
		// In-place is supported and allowed - prefer it
		return &ApplyDecision{
			CanApply: true,
			Method:   InPlace,
			Reason:   "In-place resize is supported and allowed",
		}, nil
	}

	// In-place is either not supported or not allowed by policy
	// Check if recreate is allowed
	if policy.Spec.UpdateStrategy.AllowRecreate {
		return &ApplyDecision{
			CanApply: true,
			Method:   Recreate,
			Reason:   "Using recreate strategy",
		}, nil
	}

	// Neither in-place nor recreate is available
	return &ApplyDecision{
		CanApply: false,
		Method:   Skip,
		Reason:   "No update strategy available",
	}, nil
}

// detectInPlaceResize detects if in-place pod resize is supported
func (e *Engine) detectInPlaceResize(ctx context.Context) (bool, error) { //nolint:unparam // ctx may be used in future
	// Get server version
	serverVersion, err := e.discoveryClient.ServerVersion()
	if err != nil {
		return false, fmt.Errorf("failed to get server version: %w", err)
	}

	// Parse version
	major, err := strconv.Atoi(serverVersion.Major)
	if err != nil {
		return false, fmt.Errorf("failed to parse major version: %w", err)
	}

	minor, err := strconv.Atoi(strings.TrimSuffix(serverVersion.Minor, "+"))
	if err != nil {
		return false, fmt.Errorf("failed to parse minor version: %w", err)
	}

	// In-place resize is available in Kubernetes 1.29+ with feature gate
	// For 1.33+, it's generally available
	if major > 1 || (major == 1 && minor >= 33) {
		return true, nil
	}

	if major == 1 && minor >= 29 {
		// Check if feature gate is enabled by attempting to detect it
		// In a real implementation, we would check the feature gate status
		// For now, we'll assume it's available if version >= 1.29
		return true, nil
	}

	return false, nil
}

// getCurrentResources extracts current resource requirements from a workload
func (e *Engine) getCurrentResources(workload *Workload) (map[string]corev1.ResourceRequirements, error) {
	resources := make(map[string]corev1.ResourceRequirements)

	// Extract pod template spec based on workload kind
	var containers []interface{}
	var err error

	switch workload.Kind {
	case kindDeployment:
		containers, _, err = unstructured.NestedSlice(workload.Object.Object, "spec", "template", "spec", "containers")
	case kindStatefulSet:
		containers, _, err = unstructured.NestedSlice(workload.Object.Object, "spec", "template", "spec", "containers")
	case kindDaemonSet:
		containers, _, err = unstructured.NestedSlice(workload.Object.Object, "spec", "template", "spec", "containers")
	default:
		return nil, fmt.Errorf("unsupported workload kind: %s", workload.Kind)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to extract containers: %w", err)
	}

	for _, c := range containers {
		container, ok := c.(map[string]interface{})
		if !ok {
			continue
		}

		name, _, _ := unstructured.NestedString(container, "name")
		resourcesMap, _, _ := unstructured.NestedMap(container, "resources")

		reqs := corev1.ResourceRequirements{}

		if requestsMap, ok := resourcesMap["requests"].(map[string]interface{}); ok {
			reqs.Requests = corev1.ResourceList{}
			if cpu, ok := requestsMap["cpu"].(string); ok {
				reqs.Requests[corev1.ResourceCPU] = resource.MustParse(cpu)
			}
			if memory, ok := requestsMap["memory"].(string); ok {
				reqs.Requests[corev1.ResourceMemory] = resource.MustParse(memory)
			}
		}

		if limitsMap, ok := resourcesMap["limits"].(map[string]interface{}); ok {
			reqs.Limits = corev1.ResourceList{}
			if cpu, ok := limitsMap["cpu"].(string); ok {
				reqs.Limits[corev1.ResourceCPU] = resource.MustParse(cpu)
			}
			if memory, ok := limitsMap["memory"].(string); ok {
				reqs.Limits[corev1.ResourceMemory] = resource.MustParse(memory)
			}
		}

		resources[name] = reqs
	}

	return resources, nil
}

// isUnsafeMemoryDecrease checks if a memory decrease could be unsafe
func (e *Engine) isUnsafeMemoryDecrease(
	currentResources map[string]corev1.ResourceRequirements,
	rec *recommendation.Recommendation,
) bool {
	// For each container, check if we're decreasing memory below current limits
	for _, reqs := range currentResources {
		if memLimit, ok := reqs.Limits[corev1.ResourceMemory]; ok {
			// If recommended memory is less than current limit, it could be unsafe
			if rec.Memory.Cmp(memLimit) < 0 {
				return true
			}
		}
	}
	return false
}

// ApplyResult contains information about the apply operation
type ApplyResult struct {
	Method         string // "ServerSideApply" or "StrategicMergePatch"
	FieldOwnership bool   // true if SSA was used
}

// Apply applies resource recommendations using the configured patch strategy
func (e *Engine) Apply(
	ctx context.Context,
	workload *Workload,
	containerName string,
	rec *recommendation.Recommendation,
	policy *optipodv1alpha1.OptimizationPolicy,
) (*ApplyResult, error) {
	// Determine if SSA should be used (default to true if not specified)
	useSSA := true
	if policy.Spec.UpdateStrategy.UseServerSideApply != nil {
		useSSA = *policy.Spec.UpdateStrategy.UseServerSideApply
	}

	if useSSA {
		err := e.ApplyWithSSA(ctx, workload, containerName, rec, policy)
		if err != nil {
			return nil, err
		}
		return &ApplyResult{
			Method:         "ServerSideApply",
			FieldOwnership: true,
		}, nil
	}

	// Fall back to Strategic Merge Patch
	err := e.ApplyWithStrategicMerge(ctx, workload, containerName, rec, policy)
	if err != nil {
		return nil, err
	}
	return &ApplyResult{
		Method:         "StrategicMergePatch",
		FieldOwnership: false,
	}, nil
}

// ApplyWithStrategicMerge applies resource recommendations using Strategic Merge Patch
func (e *Engine) ApplyWithStrategicMerge(
	ctx context.Context,
	workload *Workload,
	containerName string,
	rec *recommendation.Recommendation,
	policy *optipodv1alpha1.OptimizationPolicy,
) error {
	// Build JSON patch for resource requests
	patch, err := e.buildResourcePatch(workload, containerName, rec, policy)
	if err != nil {
		return fmt.Errorf("failed to build patch: %w", err)
	}

	// Get the appropriate GVR for the workload
	gvr, err := e.getGVR(workload.Kind)
	if err != nil {
		return fmt.Errorf("failed to get GVR: %w", err)
	}

	// Apply the patch
	_, err = e.dynamicClient.Resource(gvr).Namespace(workload.Namespace).Patch(
		ctx,
		workload.Name,
		types.StrategicMergePatchType,
		patch,
		metav1.PatchOptions{},
	)

	if err != nil {
		// Record failed Strategic Merge patch
		observability.RecordSSAPatch(
			policy.Name,
			workload.Namespace,
			workload.Name,
			workload.Kind,
			"failure",
			"StrategicMergePatch",
		)
		// Check for RBAC errors
		if errors.IsForbidden(err) {
			return fmt.Errorf("RBAC: insufficient permissions to update workload: %w", err)
		}
		return fmt.Errorf("failed to patch workload: %w", err)
	}

	// Record successful Strategic Merge patch
	observability.RecordSSAPatch(
		policy.Name,
		workload.Namespace,
		workload.Name,
		workload.Kind,
		"success",
		"StrategicMergePatch",
	)

	return nil
}

// ApplyWithSSA applies resource recommendations using Server-Side Apply
func (e *Engine) ApplyWithSSA(
	ctx context.Context,
	workload *Workload,
	containerName string,
	rec *recommendation.Recommendation,
	policy *optipodv1alpha1.OptimizationPolicy,
) error {
	log := ctrl.LoggerFrom(ctx)

	log.Info("Applying resource changes using Server-Side Apply",
		"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
		"kind", workload.Kind,
		"container", containerName,
		"fieldManager", "optipod",
		"force", true,
		"cpu", rec.CPU.String(),
		"memory", rec.Memory.String(),
	)

	// Build SSA patch
	patch, err := e.buildSSAPatch(workload, containerName, rec, policy)
	if err != nil {
		log.Error(err, "Failed to build SSA patch",
			"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
		)
		return fmt.Errorf("failed to build SSA patch: %w", err)
	}

	// Get GVR for workload type
	gvr, err := e.getGVR(workload.Kind)
	if err != nil {
		log.Error(err, "Failed to get GVR",
			"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
			"kind", workload.Kind,
		)
		return fmt.Errorf("failed to get GVR: %w", err)
	}

	// Apply using Server-Side Apply
	_, err = e.dynamicClient.Resource(gvr).Namespace(workload.Namespace).Patch(
		ctx,
		workload.Name,
		types.ApplyPatchType,
		patch,
		metav1.PatchOptions{
			FieldManager: "optipod",
			Force:        boolPtr(true),
		},
	)

	if err != nil {
		log.Error(err, "Server-Side Apply failed",
			"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
			"fieldManager", "optipod",
		)
		// Record failed SSA patch
		observability.RecordSSAPatch(
			policy.Name,
			workload.Namespace,
			workload.Name,
			workload.Kind,
			"failure",
			"ServerSideApply",
		)
		return e.handleSSAError(err)
	}

	log.Info("Successfully applied resource changes via SSA",
		"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
		"container", containerName,
		"cpu", rec.CPU.String(),
		"memory", rec.Memory.String(),
	)

	// Record successful SSA patch
	observability.RecordSSAPatch(
		policy.Name,
		workload.Namespace,
		workload.Name,
		workload.Kind,
		"success",
		"ServerSideApply",
	)

	return nil
}

// handleSSAError processes SSA-specific errors and provides helpful messages
func (e *Engine) handleSSAError(err error) error {
	if errors.IsConflict(err) {
		return fmt.Errorf("SSA conflict: another field manager owns these fields. "+
			"This may indicate a configuration issue. Error: %w", err)
	}

	if errors.IsForbidden(err) {
		return fmt.Errorf("RBAC: insufficient permissions for Server-Side Apply: %w", err)
	}

	if errors.IsInvalid(err) {
		return fmt.Errorf("SSA patch validation failed: %w", err)
	}

	return fmt.Errorf("SSA patch failed: %w", err)
}

// boolPtr returns a pointer to a bool value
func boolPtr(b bool) *bool {
	return &b
}

// calculateLimits calculates resource limits based on recommendations and policy configuration
func (e *Engine) calculateLimits(rec *recommendation.Recommendation, policy *optipodv1alpha1.OptimizationPolicy) (resource.Quantity, resource.Quantity) {
	// Default multipliers
	cpuMultiplier := 1.0    // CPU limit = recommendation (no headroom by default)
	memoryMultiplier := 1.1 // Memory limit = recommendation * 1.1 (10% headroom by default)

	// Override with policy configuration if provided
	if policy.Spec.UpdateStrategy.LimitConfig != nil {
		if policy.Spec.UpdateStrategy.LimitConfig.CPULimitMultiplier != nil {
			cpuMultiplier = *policy.Spec.UpdateStrategy.LimitConfig.CPULimitMultiplier
		}
		if policy.Spec.UpdateStrategy.LimitConfig.MemoryLimitMultiplier != nil {
			memoryMultiplier = *policy.Spec.UpdateStrategy.LimitConfig.MemoryLimitMultiplier
		}
	}

	// Calculate limits by multiplying recommendations
	cpuLimit := multiplyQuantity(rec.CPU, cpuMultiplier)
	memoryLimit := multiplyQuantity(rec.Memory, memoryMultiplier)

	return cpuLimit, memoryLimit
}

// multiplyQuantity multiplies a resource quantity by a factor
func multiplyQuantity(q resource.Quantity, factor float64) resource.Quantity {
	// For CPU quantities (DecimalSI format), work with millivalue to preserve millicores
	// For Memory quantities (BinarySI format), work with value (bytes)
	if q.Format == resource.DecimalSI {
		// CPU quantity - use millivalue to preserve millicores
		milliValue := q.MilliValue()
		result := int64(float64(milliValue) * factor)
		newQuantity := resource.NewMilliQuantity(result, q.Format)
		return *newQuantity
	} else {
		// Memory quantity - use value (bytes)
		value := q.Value()
		result := int64(float64(value) * factor)
		newQuantity := resource.NewQuantity(result, q.Format)
		return *newQuantity
	}
}

// buildResourcePatch builds a JSON patch for updating resource requests
func (e *Engine) buildResourcePatch(
	workload *Workload,
	containerName string,
	rec *recommendation.Recommendation,
	policy *optipodv1alpha1.OptimizationPolicy,
) ([]byte, error) {
	// Extract containers
	var containers []interface{}
	var err error

	switch workload.Kind {
	case kindDeployment:
		containers, _, err = unstructured.NestedSlice(workload.Object.Object, "spec", "template", "spec", "containers")
	case kindStatefulSet:
		containers, _, err = unstructured.NestedSlice(workload.Object.Object, "spec", "template", "spec", "containers")
	case kindDaemonSet:
		containers, _, err = unstructured.NestedSlice(workload.Object.Object, "spec", "template", "spec", "containers")
	default:
		return nil, fmt.Errorf("unsupported workload kind: %s", workload.Kind)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to extract containers: %w", err)
	}

	// Find and update the target container
	found := false
	for i, c := range containers {
		container, ok := c.(map[string]interface{})
		if !ok {
			continue
		}

		name, _, _ := unstructured.NestedString(container, "name")
		if name != containerName {
			continue
		}

		found = true

		// Build new resources map with only what we want to update
		resourcesMap := make(map[string]interface{})

		// Always update requests
		requestsMap := make(map[string]interface{})
		requestsMap["cpu"] = rec.CPU.String()
		requestsMap["memory"] = rec.Memory.String()
		resourcesMap["requests"] = requestsMap

		// Update limits only if configured to do so
		if !policy.Spec.UpdateStrategy.UpdateRequestsOnly {
			cpuLimit, memoryLimit := e.calculateLimits(rec, policy)
			limitsMap := make(map[string]interface{})
			limitsMap["cpu"] = cpuLimit.String()
			limitsMap["memory"] = memoryLimit.String()
			resourcesMap["limits"] = limitsMap
		}

		container["resources"] = resourcesMap
		containers[i] = container
		break
	}

	if !found {
		return nil, fmt.Errorf("container %s not found in workload", containerName)
	}

	// Build the patch
	patch := map[string]interface{}{
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": containers,
				},
			},
		},
	}

	// Convert to JSON
	patchUnstructured := &unstructured.Unstructured{Object: patch}
	patchBytes, err := patchUnstructured.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("failed to encode patch: %w", err)
	}

	return patchBytes, nil
}

// getGVR returns the GroupVersionResource for a workload kind
func (e *Engine) getGVR(kind string) (schema.GroupVersionResource, error) {
	switch kind {
	case kindDeployment:
		return schema.GroupVersionResource{
			Group:    "apps",
			Version:  "v1",
			Resource: "deployments",
		}, nil
	case kindStatefulSet:
		return schema.GroupVersionResource{
			Group:    "apps",
			Version:  "v1",
			Resource: "statefulsets",
		}, nil
	case kindDaemonSet:
		return schema.GroupVersionResource{
			Group:    "apps",
			Version:  "v1",
			Resource: "daemonsets",
		}, nil
	default:
		return schema.GroupVersionResource{}, fmt.Errorf("unsupported workload kind: %s", kind)
	}
}

// getAPIVersionAndKind returns the API version and kind for a workload type
func (e *Engine) getAPIVersionAndKind(workloadKind string) (string, string) {
	switch workloadKind {
	case kindDeployment, kindStatefulSet, kindDaemonSet:
		return "apps/v1", workloadKind
	default:
		return "apps/v1", workloadKind
	}
}

// buildSSAPatch constructs a Server-Side Apply patch containing only resource fields
func (e *Engine) buildSSAPatch(
	workload *Workload,
	containerName string,
	rec *recommendation.Recommendation,
	policy *optipodv1alpha1.OptimizationPolicy,
) ([]byte, error) {
	// Determine API version and kind
	apiVersion, kind := e.getAPIVersionAndKind(workload.Kind)

	// Build resources map
	resources := map[string]interface{}{
		"requests": map[string]interface{}{
			"cpu":    rec.CPU.String(),
			"memory": rec.Memory.String(),
		},
	}

	// Include limits if configured
	if !policy.Spec.UpdateStrategy.UpdateRequestsOnly {
		cpuLimit, memoryLimit := e.calculateLimits(rec, policy)
		resources["limits"] = map[string]interface{}{
			"cpu":    cpuLimit.String(),
			"memory": memoryLimit.String(),
		}
	}

	// Build minimal patch with only resource fields
	patch := map[string]interface{}{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata": map[string]interface{}{
			"name":      workload.Name,
			"namespace": workload.Namespace,
		},
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []map[string]interface{}{
						{
							"name":      containerName,
							"resources": resources,
						},
					},
				},
			},
		},
	}

	// Serialize to JSON
	patchUnstructured := &unstructured.Unstructured{Object: patch}
	patchBytes, err := patchUnstructured.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal SSA patch: %w", err)
	}

	return patchBytes, nil
}
