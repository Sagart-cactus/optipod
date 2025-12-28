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
	"time"

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

// Default limit multiplier constants
const (
	// DefaultMemoryLimitMultiplier is the default multiplier for memory limits (30% buffer above requests)
	DefaultMemoryLimitMultiplier = 1.3
	// DefaultCPULimitMultiplier is the default multiplier for CPU limits (50% buffer above requests)
	DefaultCPULimitMultiplier = 1.5
	// MinMultiplier is the minimum allowed multiplier value
	MinMultiplier = 1.0
	// MaxMultiplier is the maximum allowed multiplier value
	MaxMultiplier = 10.0
	// noneValue represents the absence of a resource value
	noneValue = "none"
)

// validateMultiplier validates that a multiplier is within the allowed range
func validateMultiplier(multiplier float64, name string) error {
	if multiplier < MinMultiplier || multiplier > MaxMultiplier {
		return fmt.Errorf("%s multiplier %.2f is out of range [%.1f, %.1f]", name, multiplier, MinMultiplier, MaxMultiplier)
	}
	return nil
}

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
	containerName string,
	rec *recommendation.Recommendation,
	policy *optipodv1alpha1.OptimizationPolicy,
) (*ApplyDecision, error) {
	log := ctrl.LoggerFrom(ctx)
	startTime := time.Now()

	defer func() {
		duration := time.Since(startTime).Seconds()
		observability.RecordOptimizationDecisionDuration(policy.Name, workload.Kind, duration)
	}()

	log.Info("Evaluating optimization applicability",
		"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
		"container", containerName,
		"policy", policy.Name,
		"policyMode", string(policy.Spec.Mode),
		"globalDryRun", e.dryRun,
		"allowInPlaceResize", policy.Spec.UpdateStrategy.AllowInPlaceResize,
		"allowRecreate", policy.Spec.UpdateStrategy.AllowRecreate,
	)

	// Check policy mode
	if policy.Spec.Mode == optipodv1alpha1.ModeRecommend {
		decision := &ApplyDecision{
			CanApply: false,
			Method:   Skip,
			Reason:   "Policy is in Recommend mode",
		}
		log.Info("Optimization decision made",
			"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
			"container", containerName,
			"canApply", decision.CanApply,
			"method", string(decision.Method),
			"reason", decision.Reason,
		)
		return decision, nil
	}

	if policy.Spec.Mode == optipodv1alpha1.ModeDisabled {
		decision := &ApplyDecision{
			CanApply: false,
			Method:   Skip,
			Reason:   "Policy is disabled",
		}
		log.Info("Optimization decision made",
			"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
			"container", containerName,
			"canApply", decision.CanApply,
			"method", string(decision.Method),
			"reason", decision.Reason,
		)
		return decision, nil
	}

	// Check global dry-run
	if e.dryRun {
		decision := &ApplyDecision{
			CanApply: false,
			Method:   Skip,
			Reason:   "Global dry-run mode is enabled",
		}
		log.Info("Optimization decision made",
			"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
			"container", containerName,
			"canApply", decision.CanApply,
			"method", string(decision.Method),
			"reason", decision.Reason,
		)
		return decision, nil
	}

	// Get current container resources
	_, err := e.getCurrentResources(workload)
	if err != nil {
		log.Error(err, "Failed to get current resources during applicability check",
			"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
			"container", containerName,
		)
		return nil, fmt.Errorf("failed to get current resources: %w", err)
	}

	// Detect in-place resize capability
	inPlaceSupported, err := e.detectInPlaceResize(ctx)
	if err != nil {
		log.Error(err, "Failed to detect in-place resize capability",
			"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
			"container", containerName,
		)
		return nil, fmt.Errorf("failed to detect in-place resize capability: %w", err)
	}

	log.Info("In-place resize capability detected",
		"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
		"container", containerName,
		"inPlaceSupported", inPlaceSupported,
	)

	// Determine apply method based on support and policy
	if inPlaceSupported && policy.Spec.UpdateStrategy.AllowInPlaceResize {
		// In-place is supported and allowed - prefer it
		decision := &ApplyDecision{
			CanApply: true,
			Method:   InPlace,
			Reason:   "In-place resize is supported and allowed",
		}
		log.Info("Optimization decision made",
			"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
			"container", containerName,
			"canApply", decision.CanApply,
			"method", string(decision.Method),
			"reason", decision.Reason,
			"inPlaceSupported", inPlaceSupported,
			"allowInPlaceResize", policy.Spec.UpdateStrategy.AllowInPlaceResize,
		)
		return decision, nil
	}

	// In-place is either not supported or not allowed by policy
	// Check if recreate is allowed
	if policy.Spec.UpdateStrategy.AllowRecreate {
		decision := &ApplyDecision{
			CanApply: true,
			Method:   Recreate,
			Reason:   "Using recreate strategy",
		}
		log.Info("Optimization decision made",
			"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
			"container", containerName,
			"canApply", decision.CanApply,
			"method", string(decision.Method),
			"reason", decision.Reason,
			"inPlaceSupported", inPlaceSupported,
			"allowInPlaceResize", policy.Spec.UpdateStrategy.AllowInPlaceResize,
			"allowRecreate", policy.Spec.UpdateStrategy.AllowRecreate,
		)
		return decision, nil
	}

	// Neither in-place nor recreate is available
	decision := &ApplyDecision{
		CanApply: false,
		Method:   Skip,
		Reason:   "No update strategy available",
	}
	log.Info("Optimization decision made",
		"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
		"container", containerName,
		"canApply", decision.CanApply,
		"method", string(decision.Method),
		"reason", decision.Reason,
		"inPlaceSupported", inPlaceSupported,
		"allowInPlaceResize", policy.Spec.UpdateStrategy.AllowInPlaceResize,
		"allowRecreate", policy.Spec.UpdateStrategy.AllowRecreate,
	)
	return decision, nil
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
				if cpuQuantity, err := resource.ParseQuantity(cpu); err != nil {
					return nil, fmt.Errorf("invalid CPU request quantity %q in container %s: %w", cpu, name, err)
				} else {
					reqs.Requests[corev1.ResourceCPU] = cpuQuantity
				}
			}
			if memory, ok := requestsMap["memory"].(string); ok {
				if memoryQuantity, err := resource.ParseQuantity(memory); err != nil {
					return nil, fmt.Errorf("invalid memory request quantity %q in container %s: %w", memory, name, err)
				} else {
					reqs.Requests[corev1.ResourceMemory] = memoryQuantity
				}
			}
		}

		if limitsMap, ok := resourcesMap["limits"].(map[string]interface{}); ok {
			reqs.Limits = corev1.ResourceList{}
			if cpu, ok := limitsMap["cpu"].(string); ok {
				if cpuQuantity, err := resource.ParseQuantity(cpu); err != nil {
					return nil, fmt.Errorf("invalid CPU limit quantity %q in container %s: %w", cpu, name, err)
				} else {
					reqs.Limits[corev1.ResourceCPU] = cpuQuantity
				}
			}
			if memory, ok := limitsMap["memory"].(string); ok {
				if memoryQuantity, err := resource.ParseQuantity(memory); err != nil {
					return nil, fmt.Errorf("invalid memory limit quantity %q in container %s: %w", memory, name, err)
				} else {
					reqs.Limits[corev1.ResourceMemory] = memoryQuantity
				}
			}
		}

		resources[name] = reqs
	}

	return resources, nil
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
	log := ctrl.LoggerFrom(ctx)

	// Get current resources for before/after comparison
	currentResources, err := e.getCurrentResources(workload)
	if err != nil {
		log.Error(err, "Failed to get current resources for logging",
			"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
			"container", containerName,
		)
		// Continue with empty current resources for logging
		currentResources = make(map[string]corev1.ResourceRequirements)
	}

	currentReqs := currentResources[containerName]
	beforeCPU := "0"
	beforeMemory := "0"
	beforeCPULimit := noneValue
	beforeMemoryLimit := noneValue

	if currentReqs.Requests != nil {
		if cpu, exists := currentReqs.Requests[corev1.ResourceCPU]; exists {
			beforeCPU = cpu.String()
		}
		if memory, exists := currentReqs.Requests[corev1.ResourceMemory]; exists {
			beforeMemory = memory.String()
		}
	}
	if currentReqs.Limits != nil {
		if cpu, exists := currentReqs.Limits[corev1.ResourceCPU]; exists {
			beforeCPULimit = cpu.String()
		}
		if memory, exists := currentReqs.Limits[corev1.ResourceMemory]; exists {
			beforeMemoryLimit = memory.String()
		}
	}

	log.Info("Starting optimization with Strategic Merge Patch",
		"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
		"kind", workload.Kind,
		"container", containerName,
		"policy", policy.Name,
		"updateRequestsOnly", policy.Spec.UpdateStrategy.UpdateRequestsOnly,
		"beforeCPURequest", beforeCPU,
		"beforeMemoryRequest", beforeMemory,
		"beforeCPULimit", beforeCPULimit,
		"beforeMemoryLimit", beforeMemoryLimit,
		"afterCPURequest", rec.CPU.String(),
		"afterMemoryRequest", rec.Memory.String(),
		"reason", "Direct recommendation application without safety checks",
	)

	// Build JSON patch for resource requests
	patch, err := e.buildResourcePatch(workload, containerName, rec, policy)
	if err != nil {
		log.Error(err, "Failed to build Strategic Merge patch",
			"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
			"container", containerName,
			"reason", "Patch construction failed",
			"beforeCPURequest", beforeCPU,
			"beforeMemoryRequest", beforeMemory,
			"targetCPURequest", rec.CPU.String(),
			"targetMemoryRequest", rec.Memory.String(),
		)
		return fmt.Errorf("failed to build patch: %w", err)
	}

	// Get the appropriate GVR for the workload
	gvr, err := e.getGVR(workload.Kind)
	if err != nil {
		log.Error(err, "Failed to get GVR for Strategic Merge patch",
			"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
			"kind", workload.Kind,
			"reason", "GVR resolution failed",
		)
		return fmt.Errorf("failed to get GVR: %w", err)
	}

	// Calculate expected limits for logging
	expectedLimits := make(map[string]string)
	if !policy.Spec.UpdateStrategy.UpdateRequestsOnly {
		requests := corev1.ResourceList{
			corev1.ResourceCPU:    rec.CPU,
			corev1.ResourceMemory: rec.Memory,
		}
		limits, limitErr := e.CalculateLimitsWithDefaults(requests, policy.Spec.UpdateStrategy.LimitConfig)
		if limitErr == nil {
			if cpuLimit, exists := limits[corev1.ResourceCPU]; exists {
				expectedLimits["cpu"] = cpuLimit.String()
			}
			if memoryLimit, exists := limits[corev1.ResourceMemory]; exists {
				expectedLimits["memory"] = memoryLimit.String()
			}
		}
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
		log.Error(err, "Strategic Merge Patch failed",
			"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
			"container", containerName,
			"reason", "Patch application failed",
			"beforeCPURequest", beforeCPU,
			"beforeMemoryRequest", beforeMemory,
			"beforeCPULimit", beforeCPULimit,
			"beforeMemoryLimit", beforeMemoryLimit,
			"targetCPURequest", rec.CPU.String(),
			"targetMemoryRequest", rec.Memory.String(),
			"targetCPULimit", expectedLimits["cpu"],
			"targetMemoryLimit", expectedLimits["memory"],
			"updateRequestsOnly", policy.Spec.UpdateStrategy.UpdateRequestsOnly,
		)
		// Record failed Strategic Merge patch
		observability.RecordSSAPatch(
			policy.Name,
			workload.Namespace,
			workload.Name,
			workload.Kind,
			"failure",
			"StrategicMergePatch",
		)
		// Record optimization failure
		observability.RecordOptimizationFailure(
			policy.Name,
			workload.Namespace,
			workload.Name,
			workload.Kind,
			"StrategicMergePatch",
			"PatchApplicationFailed",
		)
		// Check for RBAC errors
		if errors.IsForbidden(err) {
			return fmt.Errorf("RBAC: insufficient permissions to update workload: %w", err)
		}
		return fmt.Errorf("failed to patch workload: %w", err)
	}

	// Record resource change magnitudes
	e.recordResourceChangeMagnitudes(policy.Name, workload.Namespace, workload.Name, beforeCPU, beforeMemory, rec)

	log.Info("Successfully applied resource changes via Strategic Merge Patch",
		"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
		"container", containerName,
		"method", "StrategicMergePatch",
		"policy", policy.Name,
		"reason", "Optimization completed successfully",
		"beforeCPURequest", beforeCPU,
		"beforeMemoryRequest", beforeMemory,
		"beforeCPULimit", beforeCPULimit,
		"beforeMemoryLimit", beforeMemoryLimit,
		"afterCPURequest", rec.CPU.String(),
		"afterMemoryRequest", rec.Memory.String(),
		"afterCPULimit", expectedLimits["cpu"],
		"afterMemoryLimit", expectedLimits["memory"],
		"updateRequestsOnly", policy.Spec.UpdateStrategy.UpdateRequestsOnly,
	)

	// Record successful Strategic Merge patch
	observability.RecordSSAPatch(
		policy.Name,
		workload.Namespace,
		workload.Name,
		workload.Kind,
		"success",
		"StrategicMergePatch",
	)

	// Record optimization success
	observability.RecordOptimizationSuccess(
		policy.Name,
		workload.Namespace,
		workload.Name,
		workload.Kind,
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

	// Get current resources for before/after comparison
	currentResources, err := e.getCurrentResources(workload)
	if err != nil {
		log.Error(err, "Failed to get current resources for logging",
			"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
			"container", containerName,
		)
		// Continue with empty current resources for logging
		currentResources = make(map[string]corev1.ResourceRequirements)
	}

	currentReqs := currentResources[containerName]
	beforeCPU := "0"
	beforeMemory := "0"
	beforeCPULimit := noneValue
	beforeMemoryLimit := noneValue

	if currentReqs.Requests != nil {
		if cpu, exists := currentReqs.Requests[corev1.ResourceCPU]; exists {
			beforeCPU = cpu.String()
		}
		if memory, exists := currentReqs.Requests[corev1.ResourceMemory]; exists {
			beforeMemory = memory.String()
		}
	}
	if currentReqs.Limits != nil {
		if cpu, exists := currentReqs.Limits[corev1.ResourceCPU]; exists {
			beforeCPULimit = cpu.String()
		}
		if memory, exists := currentReqs.Limits[corev1.ResourceMemory]; exists {
			beforeMemoryLimit = memory.String()
		}
	}

	log.Info("Starting optimization with Server-Side Apply",
		"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
		"kind", workload.Kind,
		"container", containerName,
		"policy", policy.Name,
		"fieldManager", "optipod",
		"force", true,
		"updateRequestsOnly", policy.Spec.UpdateStrategy.UpdateRequestsOnly,
		"beforeCPURequest", beforeCPU,
		"beforeMemoryRequest", beforeMemory,
		"beforeCPULimit", beforeCPULimit,
		"beforeMemoryLimit", beforeMemoryLimit,
		"afterCPURequest", rec.CPU.String(),
		"afterMemoryRequest", rec.Memory.String(),
		"reason", "Direct recommendation application without safety checks",
	)

	// Build SSA patch
	patch, err := e.buildSSAPatch(workload, containerName, rec, policy)
	if err != nil {
		log.Error(err, "Failed to build SSA patch",
			"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
			"container", containerName,
			"reason", "SSA patch construction failed",
			"beforeCPURequest", beforeCPU,
			"beforeMemoryRequest", beforeMemory,
			"targetCPURequest", rec.CPU.String(),
			"targetMemoryRequest", rec.Memory.String(),
		)
		return fmt.Errorf("failed to build SSA patch: %w", err)
	}

	// Get GVR for workload type
	gvr, err := e.getGVR(workload.Kind)
	if err != nil {
		log.Error(err, "Failed to get GVR",
			"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
			"kind", workload.Kind,
			"reason", "GVR resolution failed",
		)
		return fmt.Errorf("failed to get GVR: %w", err)
	}

	// Calculate expected limits for logging
	expectedLimits := make(map[string]string)
	if !policy.Spec.UpdateStrategy.UpdateRequestsOnly {
		requests := corev1.ResourceList{
			corev1.ResourceCPU:    rec.CPU,
			corev1.ResourceMemory: rec.Memory,
		}
		limits, limitErr := e.CalculateLimitsWithDefaults(requests, policy.Spec.UpdateStrategy.LimitConfig)
		if limitErr == nil {
			if cpuLimit, exists := limits[corev1.ResourceCPU]; exists {
				expectedLimits["cpu"] = cpuLimit.String()
			}
			if memoryLimit, exists := limits[corev1.ResourceMemory]; exists {
				expectedLimits["memory"] = memoryLimit.String()
			}
		}
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
			"container", containerName,
			"reason", "SSA patch application failed",
			"beforeCPURequest", beforeCPU,
			"beforeMemoryRequest", beforeMemory,
			"beforeCPULimit", beforeCPULimit,
			"beforeMemoryLimit", beforeMemoryLimit,
			"targetCPURequest", rec.CPU.String(),
			"targetMemoryRequest", rec.Memory.String(),
			"targetCPULimit", expectedLimits["cpu"],
			"targetMemoryLimit", expectedLimits["memory"],
			"updateRequestsOnly", policy.Spec.UpdateStrategy.UpdateRequestsOnly,
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
		// Record optimization failure
		observability.RecordOptimizationFailure(
			policy.Name,
			workload.Namespace,
			workload.Name,
			workload.Kind,
			"ServerSideApply",
			"SSAPatchApplicationFailed",
		)
		return e.handleSSAError(err)
	}

	// Record resource change magnitudes
	e.recordResourceChangeMagnitudes(policy.Name, workload.Namespace, workload.Name, beforeCPU, beforeMemory, rec)

	log.Info("Successfully applied resource changes via SSA",
		"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
		"container", containerName,
		"method", "ServerSideApply",
		"fieldManager", "optipod",
		"policy", policy.Name,
		"reason", "Optimization completed successfully",
		"beforeCPURequest", beforeCPU,
		"beforeMemoryRequest", beforeMemory,
		"beforeCPULimit", beforeCPULimit,
		"beforeMemoryLimit", beforeMemoryLimit,
		"afterCPURequest", rec.CPU.String(),
		"afterMemoryRequest", rec.Memory.String(),
		"afterCPULimit", expectedLimits["cpu"],
		"afterMemoryLimit", expectedLimits["memory"],
		"updateRequestsOnly", policy.Spec.UpdateStrategy.UpdateRequestsOnly,
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

	// Record optimization success
	observability.RecordOptimizationSuccess(
		policy.Name,
		workload.Namespace,
		workload.Name,
		workload.Kind,
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

// CalculateLimitsWithDefaults calculates resource limits with proper configuration precedence.
//
// Precedence rules (highest to lowest):
// 1. Explicit limit configuration multipliers (limitConfig.CPULimitMultiplier, limitConfig.MemoryLimitMultiplier)
// 2. Default multipliers (DefaultCPULimitMultiplier, DefaultMemoryLimitMultiplier)
//
// The function handles partial configurations where only some resources have explicit configs.
// For example, if only CPU multiplier is specified, memory will use the default multiplier.
// This maintains backward compatibility with existing configurations.
func (e *Engine) CalculateLimitsWithDefaults(requests corev1.ResourceList, limitConfig *optipodv1alpha1.LimitConfig) (corev1.ResourceList, error) {
	limits := make(corev1.ResourceList)

	// Determine CPU multiplier with explicit precedence handling
	cpuMultiplier, usingDefaultCPU := e.getCPUMultiplier(limitConfig)
	if err := validateMultiplier(cpuMultiplier, "CPU"); err != nil {
		return nil, err
	}

	// Determine memory multiplier with explicit precedence handling
	memoryMultiplier, usingDefaultMemory := e.getMemoryMultiplier(limitConfig)
	if err := validateMultiplier(memoryMultiplier, "memory"); err != nil {
		return nil, err
	}

	// Calculate CPU limit if CPU request exists and is not zero
	if cpuRequest, exists := requests[corev1.ResourceCPU]; exists && !cpuRequest.IsZero() {
		cpuLimit := multiplyQuantity(cpuRequest, cpuMultiplier)
		limits[corev1.ResourceCPU] = cpuLimit

		// Record default multiplier usage if applicable
		if usingDefaultCPU {
			observability.RecordDefaultMultiplierUsage("", "cpu", fmt.Sprintf("%.1f", cpuMultiplier))
		}
	}

	// Calculate memory limit if memory request exists and is not zero
	if memoryRequest, exists := requests[corev1.ResourceMemory]; exists && !memoryRequest.IsZero() {
		memoryLimit := multiplyQuantity(memoryRequest, memoryMultiplier)
		limits[corev1.ResourceMemory] = memoryLimit

		// Record default multiplier usage if applicable
		if usingDefaultMemory {
			observability.RecordDefaultMultiplierUsage("", "memory", fmt.Sprintf("%.1f", memoryMultiplier))
		}
	}

	return limits, nil
}

// getCPUMultiplier returns the CPU multiplier to use, with explicit precedence handling.
// Returns (multiplier, usingDefault) where usingDefault indicates if default was used.
func (e *Engine) getCPUMultiplier(limitConfig *optipodv1alpha1.LimitConfig) (float64, bool) {
	// Explicit configuration takes precedence over defaults
	if limitConfig != nil && limitConfig.CPULimitMultiplier != nil {
		return *limitConfig.CPULimitMultiplier, false
	}

	// Fall back to default multiplier
	return DefaultCPULimitMultiplier, true
}

// getMemoryMultiplier returns the memory multiplier to use, with explicit precedence handling.
// Returns (multiplier, usingDefault) where usingDefault indicates if default was used.
func (e *Engine) getMemoryMultiplier(limitConfig *optipodv1alpha1.LimitConfig) (float64, bool) {
	// Explicit configuration takes precedence over defaults
	if limitConfig != nil && limitConfig.MemoryLimitMultiplier != nil {
		return *limitConfig.MemoryLimitMultiplier, false
	}

	// Fall back to default multiplier
	return DefaultMemoryLimitMultiplier, true
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
			// Use CalculateLimitsWithDefaults for consistent limit calculation
			requests := corev1.ResourceList{
				corev1.ResourceCPU:    rec.CPU,
				corev1.ResourceMemory: rec.Memory,
			}
			limits, err := e.CalculateLimitsWithDefaults(requests, policy.Spec.UpdateStrategy.LimitConfig)
			if err != nil {
				return nil, fmt.Errorf("failed to calculate limits: %w", err)
			}

			limitsMap := make(map[string]interface{})
			if cpuLimit, exists := limits[corev1.ResourceCPU]; exists {
				limitsMap["cpu"] = cpuLimit.String()
			}
			if memoryLimit, exists := limits[corev1.ResourceMemory]; exists {
				limitsMap["memory"] = memoryLimit.String()
			}
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

// getKind returns the kind for a workload type (API version is always apps/v1)
func (e *Engine) getKind(workloadKind string) string {
	return workloadKind
}

// buildSSAPatch constructs a Server-Side Apply patch containing only resource fields
func (e *Engine) buildSSAPatch(
	workload *Workload,
	containerName string,
	rec *recommendation.Recommendation,
	policy *optipodv1alpha1.OptimizationPolicy,
) ([]byte, error) {
	// Determine kind (API version is always apps/v1 for workloads)
	kind := e.getKind(workload.Kind)

	// Build resources map
	resources := map[string]interface{}{
		"requests": map[string]interface{}{
			"cpu":    rec.CPU.String(),
			"memory": rec.Memory.String(),
		},
	}

	// Include limits if configured
	if !policy.Spec.UpdateStrategy.UpdateRequestsOnly {
		// Use CalculateLimitsWithDefaults for consistent limit calculation
		requests := corev1.ResourceList{
			corev1.ResourceCPU:    rec.CPU,
			corev1.ResourceMemory: rec.Memory,
		}
		limits, err := e.CalculateLimitsWithDefaults(requests, policy.Spec.UpdateStrategy.LimitConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to calculate limits: %w", err)
		}

		limitsMap := make(map[string]interface{})
		if cpuLimit, exists := limits[corev1.ResourceCPU]; exists {
			limitsMap["cpu"] = cpuLimit.String()
		}
		if memoryLimit, exists := limits[corev1.ResourceMemory]; exists {
			limitsMap["memory"] = memoryLimit.String()
		}
		resources["limits"] = limitsMap
	}

	// Build minimal patch with only resource fields
	patch := map[string]interface{}{
		"apiVersion": "apps/v1",
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

// recordResourceChangeMagnitudes records the magnitude of resource changes for metrics
func (e *Engine) recordResourceChangeMagnitudes(policyName, namespace, workloadName, beforeCPU, beforeMemory string, rec *recommendation.Recommendation) {
	// Parse before CPU and calculate change percentage
	if beforeCPU != "0" && beforeCPU != "" {
		if beforeCPUQuantity, err := resource.ParseQuantity(beforeCPU); err == nil && !beforeCPUQuantity.IsZero() {
			beforeCPUValue := beforeCPUQuantity.MilliValue()
			afterCPUValue := rec.CPU.MilliValue()
			changePercent := float64(afterCPUValue-beforeCPUValue) / float64(beforeCPUValue) * 100
			observability.RecordResourceChangeMagnitude(policyName, namespace, workloadName, "cpu", changePercent)
		}
	}

	// Parse before memory and calculate change percentage
	if beforeMemory != "0" && beforeMemory != "" {
		if beforeMemoryQuantity, err := resource.ParseQuantity(beforeMemory); err == nil && !beforeMemoryQuantity.IsZero() {
			beforeMemoryValue := beforeMemoryQuantity.Value()
			afterMemoryValue := rec.Memory.Value()
			changePercent := float64(afterMemoryValue-beforeMemoryValue) / float64(beforeMemoryValue) * 100
			observability.RecordResourceChangeMagnitude(policyName, namespace, workloadName, "memory", changePercent)
		}
	}
}
