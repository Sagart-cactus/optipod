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
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/coexist"
	workloaddiscovery "github.com/optipod/optipod/internal/discovery"
	"github.com/optipod/optipod/internal/history"
	"github.com/optipod/optipod/internal/observability"
	policyselection "github.com/optipod/optipod/internal/policy"
	"github.com/optipod/optipod/internal/recommendation"
	"github.com/optipod/optipod/internal/webhook"
)

// Workload kind constants
const (

	// FieldManagerName is the field manager name used by optipod
	FieldManagerName = "optipod"
)

// Unstructured patch field names and API groups.
const ()

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
	client            client.Client
	dynamicClient     dynamic.Interface
	discoveryClient   discovery.DiscoveryInterface
	dryRun            bool
	annotationManager *webhook.AnnotationManager
	rolloutController *webhook.RolloutController
}

// NewEngine creates a new application engine
func NewEngine(c client.Client, dynamicClient dynamic.Interface, discoveryClient discovery.DiscoveryInterface, dryRun bool) *Engine {
	return &Engine{
		client:            c,
		dynamicClient:     dynamicClient,
		discoveryClient:   discoveryClient,
		dryRun:            dryRun,
		annotationManager: webhook.NewAnnotationManager(c),
		rolloutController: webhook.NewRolloutController(c),
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
	if rec != nil && rec.Emergency {
		policy = emergencyPolicy(policy)
	}
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

	if workloaddiscovery.IsBatch(workload.Kind) {
		if !policy.IsWebhookStrategy() {
			return &ApplyDecision{Method: Skip, Reason: "Batch workloads require webhook admission; job templates must not be recreated"}, nil
		}
		return &ApplyDecision{CanApply: true, Method: Skip, Reason: "Batch recommendation applies to future execution pods"}, nil
	}
	if policy.IsWebhookStrategy() && policy.GetRolloutStrategy() == optipodv1alpha1.RolloutOnNextRestart {
		return &ApplyDecision{CanApply: true, Method: Skip, Reason: "Deferred admission plan; running pods unchanged"}, nil
	}
	if policy.GetStrategy() == optipodv1alpha1.StrategySSA {
		if workload.Kind == "Rollout" || workload.Kind == "ReplicaSet" {
			return &ApplyDecision{Method: Skip, Reason: "Rollout and ReplicaSet require webhook strategy for safe admission and running-pod updates"}, nil
		}
		if policy.Spec.UpdateStrategy.AllowRecreate {
			return &ApplyDecision{CanApply: true, Method: Recreate, Reason: "SSA changes the pod template and requires recreation"}, nil
		}
		return &ApplyDecision{Method: Skip, Reason: "SSA requires allowRecreate; use webhook for in-place resize"}, nil
	}
	// Detect in-place resize capability
	inPlaceSupported, err := e.detectInPlaceResize()
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
func (e *Engine) detectInPlaceResize() (bool, error) {
	if e.discoveryClient == nil {
		return false, nil
	}
	resources, err := e.discoveryClient.ServerResourcesForGroupVersion("v1")
	if err != nil {
		return false, fmt.Errorf("discover pod resize API: %w", err)
	}
	for _, r := range resources.APIResources {
		if r.Name == "pods/resize" {
			for _, v := range r.Verbs {
				if v == "patch" {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

// getCurrentResources extracts current resource requirements from a workload
func (e *Engine) getCurrentResources(workload *Workload) (map[string]corev1.ResourceRequirements, error) {
	// Keep malformed quantities actionable before typed decoding.
	for _, field := range []string{fieldContainers, fieldInitContainers} {
		containers, _, err := unstructured.NestedSlice(workload.Object.Object, append(workloaddiscovery.PodSpecPath(workload.Kind), field)...)
		if err != nil {
			return nil, err
		}
		for _, raw := range containers {
			c, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			for _, section := range []string{fieldRequests, fieldLimits} {
				for _, name := range []string{resourceCPU, "memory"} {
					value, found, err := unstructured.NestedString(c, fieldResources, section, name)
					if err != nil {
						return nil, err
					}
					if !found {
						continue
					}
					if _, err := resource.ParseQuantity(value); err != nil {
						display := name
						if name == resourceCPU {
							display = "CPU"
						}
						return nil, fmt.Errorf("invalid %s %s quantity %q: %w", display, strings.TrimSuffix(section, "s"), value, err)
					}
				}
			}
		}
	}
	t, err := workloaddiscovery.Template(workload.Object)
	if err != nil {
		return nil, err
	}
	result := map[string]corev1.ResourceRequirements{}
	for _, slot := range workloaddiscovery.ContainerSlots(&t.Spec) {
		result[slot.Container.Name] = slot.Container.Resources
	}
	return result, nil

}

// ApplyResult contains information about the apply operation
type ApplyResult struct {
	Method         string // "ServerSideApply", "StrategicMergePatch", or "Webhook"
	FieldOwnership bool   // true if SSA was used
}

// ContainerRecommendation pairs a container name with its computed
// recommendation so one apply can carry every container's resources for a
// workload.
type ContainerRecommendation struct {
	Container      string
	Recommendation *recommendation.Recommendation
}

// containerNames returns a comma-separated list of the recommended containers.
func containerNames(recs []ContainerRecommendation) string {
	names := make([]string, 0, len(recs))
	for _, cr := range recs {
		names = append(names, cr.Container)
	}
	return strings.Join(names, ",")
}

// containerBeforeValues returns the container's current CPU/memory request strings.
func containerBeforeValues(currentResources map[string]corev1.ResourceRequirements, containerName string) (cpu, memory string) {
	cpu, memory = "0", "0"
	reqs, ok := currentResources[containerName]
	if ok && reqs.Requests != nil {
		if v, exists := reqs.Requests[corev1.ResourceCPU]; exists {
			cpu = v.String()
		}
		if v, exists := reqs.Requests[corev1.ResourceMemory]; exists {
			memory = v.String()
		}
	}
	return cpu, memory
}

// describeResourceChanges returns one "container beforeCPU/beforeMemory -> afterCPU/afterMemory"
// entry per recommendation for structured logs.
func describeResourceChanges(currentResources map[string]corev1.ResourceRequirements, recs []ContainerRecommendation) string {
	parts := make([]string, 0, len(recs))
	for _, cr := range recs {
		beforeCPU, beforeMemory := containerBeforeValues(currentResources, cr.Container)
		parts = append(parts, fmt.Sprintf("%s: %s/%s -> %s/%s",
			cr.Container,
			beforeCPU, beforeMemory,
			cr.Recommendation.CPU.String(), cr.Recommendation.Memory.String()))
	}
	return strings.Join(parts, "; ")
}

// Apply applies the workload's full per-container recommendation set using the
// configured strategy in a single workload-wide operation, so sibling
// containers cannot prune each other's applied fields.
func (e *Engine) Apply(
	ctx context.Context,
	workload *Workload,
	recs []ContainerRecommendation,
	policy *optipodv1alpha1.OptimizationPolicy,
) (result *ApplyResult, applyErr error) {
	if workloaddiscovery.IsBatch(workload.Kind) {
		if !policy.IsWebhookStrategy() {
			return nil, fmt.Errorf("batch workloads require webhook admission")
		}
		policy = policy.DeepCopy()
		rollout := string(optipodv1alpha1.RolloutOnNextRestart)
		policy.Spec.UpdateStrategy.RolloutStrategy = &rollout
	}
	for _, rec := range recs {
		if rec.Recommendation.Emergency {
			policy = emergencyPolicy(policy)
			break
		}
	}
	if e.dryRun || policy.Spec.Mode != optipodv1alpha1.ModeAuto {
		return nil, fmt.Errorf("application blocked: Auto mode is required and global dry-run must be disabled")
	}
	if e.client != nil && policy.UID != "" {
		if err := e.revalidateApplication(ctx, workload, recs, policy); err != nil {
			return nil, err
		}
	}
	optimizable := false
	for _, r := range recs {
		if (policy.ControlsResource(resourceCPU) && !r.Recommendation.PreserveCPU) || (policy.ControlsResource("memory") && !r.Recommendation.PreserveMemory) {
			optimizable = true
		}
	}
	if !optimizable {
		return nil, fmt.Errorf("application blocked: no optimizable resources after final scaler checks")
	}
	if e.client != nil && policy.UID != "" {
		if err := e.preserveAutomaticLimits(ctx, workload, recs, policy); err != nil {
			return nil, err
		}
		if err := e.validateConstraints(ctx, workload, recs, policy); err != nil {
			return nil, err
		}
	}
	if len(recs) == 0 {
		return nil, fmt.Errorf("no container recommendations to apply")
	}

	if e.client != nil && policy.UID != "" && !isRollback(ctx) {
		plan, err := ResourcePlan(recs, policy)
		if err != nil {
			return nil, err
		}
		auditObject, err := e.convertToClientObject(workload)
		if err != nil {
			return nil, err
		}
		auditWorkload := &workloaddiscovery.Workload{Kind: workload.Kind, Namespace: workload.Namespace, Name: workload.Name, Object: auditObject}
		record, err := history.Prepare(ctx, e.client, auditWorkload, policy, plan)
		if err != nil {
			return nil, fmt.Errorf("prepare durable history: %w", err)
		}
		defer func() {
			method := ""
			if result != nil {
				method = result.Method
			}
			if err := history.Complete(ctx, e.client, auditWorkload, record, method, applyErr); err != nil {
				if applyErr != nil {
					applyErr = fmt.Errorf("%w; persist history: %v", applyErr, err)
				} else {
					applyErr = fmt.Errorf("mutation completed but history finalization failed: %w", err)
				}
			}
		}()
	}
	return e.applyValidated(ctx, workload, recs, policy)
}

// ApplyWithStrategicMerge applies the workload's full per-container recommendation
// set using one Strategic Merge Patch
func (e *Engine) ApplyWithStrategicMerge(
	ctx context.Context,
	workload *Workload,
	recs []ContainerRecommendation,
	policy *optipodv1alpha1.OptimizationPolicy,
) error {
	if len(recs) == 0 {
		return fmt.Errorf("no container recommendations to apply")
	}

	log := ctrl.LoggerFrom(ctx)

	// Get current resources for before/after comparison
	currentResources, err := e.getCurrentResources(workload)
	if err != nil {
		log.Error(err, "Failed to get current resources for logging",
			"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
			fieldContainers, containerNames(recs),
		)
		// Continue with empty current resources for logging
		currentResources = make(map[string]corev1.ResourceRequirements)
	}

	log.Info("Starting optimization with Strategic Merge Patch",
		"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
		"kind", workload.Kind,
		fieldContainers, containerNames(recs),
		"policy", policy.Name,
		"updateRequestsOnly", policy.Spec.UpdateStrategy.UpdateRequestsOnly,
		"resourceChanges", describeResourceChanges(currentResources, recs),
		"reason", "Direct recommendation application without safety checks",
	)

	// Build one JSON patch carrying every container's recommended resources
	patch, err := e.buildResourcePatch(workload, recs, policy)
	if err != nil {
		log.Error(err, "Failed to build Strategic Merge patch",
			"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
			fieldContainers, containerNames(recs),
			"reason", "Patch construction failed",
			"resourceChanges", describeResourceChanges(currentResources, recs),
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
			fieldContainers, containerNames(recs),
			"reason", "Patch application failed",
			"resourceChanges", describeResourceChanges(currentResources, recs),
			"updateRequestsOnly", policy.Spec.UpdateStrategy.UpdateRequestsOnly,
		)
		// Record failed Strategic Merge patch
		observability.RecordSSAPatch(
			policy.Name,
			workload.Namespace,
			workload.Name,
			workload.Kind,
			"failure",
			methodStrategicMerge,
		)
		// Record optimization failure
		observability.RecordOptimizationFailure(
			policy.Name,
			workload.Namespace,
			workload.Name,
			workload.Kind,
			methodStrategicMerge,
			"PatchApplicationFailed",
		)
		// Check for RBAC errors
		if errors.IsForbidden(err) {
			return fmt.Errorf("RBAC: insufficient permissions to update workload: %w", err)
		}
		return fmt.Errorf("failed to patch workload: %w", err)
	}

	// Record resource change magnitudes per container
	for _, cr := range recs {
		beforeCPU, beforeMemory := containerBeforeValues(currentResources, cr.Container)
		e.recordResourceChangeMagnitudes(policy.Name, workload.Namespace, workload.Name, beforeCPU, beforeMemory, cr.Recommendation)
	}

	log.Info("Successfully applied resource changes via Strategic Merge Patch",
		"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
		fieldContainers, containerNames(recs),
		"method", methodStrategicMerge,
		"policy", policy.Name,
		"reason", "Optimization completed successfully",
		"resourceChanges", describeResourceChanges(currentResources, recs),
		"updateRequestsOnly", policy.Spec.UpdateStrategy.UpdateRequestsOnly,
	)

	// Record successful Strategic Merge patch
	observability.RecordSSAPatch(
		policy.Name,
		workload.Namespace,
		workload.Name,
		workload.Kind,
		"success",
		methodStrategicMerge,
	)

	// Record optimization success
	observability.RecordOptimizationSuccess(
		policy.Name,
		workload.Namespace,
		workload.Name,
		workload.Kind,
		methodStrategicMerge,
	)

	return nil
}

// ApplyWithSSA applies the workload's full per-container recommendation set
// using ONE Server-Side Apply so sibling containers cannot prune each other's
// previously applied fields.
func (e *Engine) ApplyWithSSA(
	ctx context.Context,
	workload *Workload,
	recs []ContainerRecommendation,
	policy *optipodv1alpha1.OptimizationPolicy,
) error {
	if len(recs) == 0 {
		return fmt.Errorf("no container recommendations to apply")
	}

	log := ctrl.LoggerFrom(ctx)

	// Get current resources for before/after comparison
	currentResources, err := e.getCurrentResources(workload)
	if err != nil {
		log.Error(err, "Failed to get current resources for logging",
			"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
			fieldContainers, containerNames(recs),
		)
		// Continue with empty current resources for logging
		currentResources = make(map[string]corev1.ResourceRequirements)
	}

	log.Info("Starting optimization with Server-Side Apply",
		"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
		"kind", workload.Kind,
		fieldContainers, containerNames(recs),
		"policy", policy.Name,
		"fieldManager", FieldManagerName,
		"force", true,
		"updateRequestsOnly", policy.Spec.UpdateStrategy.UpdateRequestsOnly,
		"resourceChanges", describeResourceChanges(currentResources, recs),
		"reason", "Direct recommendation application without safety checks",
	)

	// Build one SSA patch containing every container's recommended resources
	patch, err := e.buildSSAPatch(workload, recs, policy)
	if err != nil {
		log.Error(err, "Failed to build SSA patch",
			"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
			fieldContainers, containerNames(recs),
			"reason", "SSA patch construction failed",
			"resourceChanges", describeResourceChanges(currentResources, recs),
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

	// Apply the workload's full per-container set using ONE Server-Side Apply
	_, err = e.dynamicClient.Resource(gvr).Namespace(workload.Namespace).Patch(
		ctx,
		workload.Name,
		types.ApplyPatchType,
		patch,
		metav1.PatchOptions{
			FieldManager: FieldManagerName,
			Force:        boolPtr(policy.Spec.UpdateStrategy.AllowFieldOwnershipTakeover),
		},
	)

	if err != nil {
		log.Error(err, "Server-Side Apply failed",
			"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
			"fieldManager", FieldManagerName,
			fieldContainers, containerNames(recs),
			"reason", "SSA patch application failed",
			"resourceChanges", describeResourceChanges(currentResources, recs),
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

	// Record resource change magnitudes per container
	for _, cr := range recs {
		beforeCPU, beforeMemory := containerBeforeValues(currentResources, cr.Container)
		e.recordResourceChangeMagnitudes(policy.Name, workload.Namespace, workload.Name, beforeCPU, beforeMemory, cr.Recommendation)
	}

	log.Info("Successfully applied resource changes via SSA",
		"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
		fieldContainers, containerNames(recs),
		"method", "ServerSideApply",
		"fieldManager", FieldManagerName,
		"policy", policy.Name,
		"reason", "Optimization completed successfully",
		"resourceChanges", describeResourceChanges(currentResources, recs),
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

// ApplyWithWebhook stores the workload's full per-container recommendation set
// as annotations in ONE store operation so every container's keys persist.
func (e *Engine) ApplyWithWebhook(
	ctx context.Context,
	workload *Workload,
	recs []ContainerRecommendation,
	policy *optipodv1alpha1.OptimizationPolicy,
) (*ApplyResult, error) {
	if len(recs) == 0 {
		return nil, fmt.Errorf("no container recommendations to apply")
	}

	log := ctrl.LoggerFrom(ctx)
	startTime := time.Now()

	// Get current resources for before/after comparison
	currentResources, err := e.getCurrentResources(workload)
	if err != nil {
		log.Error(err, "Failed to get current resources for logging",
			"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
			fieldContainers, containerNames(recs),
		)
		// Continue with empty current resources for logging
		currentResources = make(map[string]corev1.ResourceRequirements)
	}

	log.Info("Starting optimization with webhook strategy",
		"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
		"kind", workload.Kind,
		fieldContainers, containerNames(recs),
		"policy", policy.Name,
		"rolloutStrategy", policy.GetRolloutStrategy(),
		"updateRequestsOnly", policy.Spec.UpdateStrategy.UpdateRequestsOnly,
		"resourceChanges", describeResourceChanges(currentResources, recs),
		"reason", "Webhook strategy - storing annotations for mutating webhook",
	)

	// Convert workload to client.Object for annotation manager
	workloadObj, err := e.convertToClientObject(workload)
	if err != nil {
		log.Error(err, "Failed to convert workload to client object",
			"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
			fieldContainers, containerNames(recs),
		)
		return nil, fmt.Errorf("failed to convert workload: %w", err)
	}

	// Build the workload's full per-container recommendation set for annotation storage
	recommendations := make([]webhook.ResourceRecommendation, 0, len(recs))
	for _, cr := range recs {
		rec := cr.Recommendation
		stored := webhook.ResourceRecommendation{
			ValidUntil:    rec.ValidUntil,
			ContainerName: cr.Container,
			CPU:           &rec.CPU,
			Memory:        &rec.Memory,
		}

		if !policy.ControlsResource(resourceCPU) || rec.PreserveCPU {
			stored.CPU = nil
		}
		if !policy.ControlsResource("memory") || rec.PreserveMemory {
			stored.Memory = nil
		}
		resources, err := e.resourceMap(rec, policy)
		if err != nil {
			return nil, err
		}
		if lm, ok := resources[fieldLimits].(map[string]any); ok {
			for name, value := range lm {
				if value == nil {
					if name == resourceCPU {
						stored.RemoveCPULimit = true
					} else {
						stored.RemoveMemoryLimit = true
					}
					continue
				}
				q, err := resource.ParseQuantity(value.(string))
				if err != nil {
					return nil, err
				}
				if name == resourceCPU {
					stored.CPULimit = &q
				} else {
					stored.MemoryLimit = &q
				}
			}
		}

		if rec.ResourceOverride != nil {
			exact, err := webhook.FromResourcePlan(map[string]map[string]any{cr.Container: resources})
			if err != nil {
				return nil, err
			}
			stored = exact[0]
		}
		recommendations = append(recommendations, stored)
	}

	// Store every container's recommendations as annotations in one call
	if err := e.annotationManager.StoreRecommendations(ctx, workloadObj, recommendations, policy); err != nil {
		log.Error(err, "Failed to store recommendations as annotations",
			"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
			fieldContainers, containerNames(recs),
			"reason", "Annotation storage failed",
		)
		// Record webhook failure
		observability.RecordOptimizationFailure(
			policy.Name,
			workload.Namespace,
			workload.Name,
			workload.Kind,
			"Webhook",
			"AnnotationStorageFailed",
		)
		return nil, fmt.Errorf("failed to store annotations: %w", err)
	}

	// Handle rollout strategy
	if policy.GetRolloutStrategy() == optipodv1alpha1.RolloutImmediate {
		// StoreRecommendations updated the resourceVersion. Refresh before the
		// atomic template/hash update, and never restart for an unchanged plan.
		if err := e.client.Get(ctx, client.ObjectKeyFromObject(workloadObj), workloadObj); err != nil {
			return nil, err
		}
		a := workloadObj.GetAnnotations()
		if a[webhook.RolloutHash] == a[webhook.RecommendationHash] {
			return &ApplyResult{Method: "WebhookNoChange"}, nil
		}
		if !e.rolloutController.IsWorkloadReady(workloadObj) {
			return nil, fmt.Errorf("rollout deferred: workload is not fully ready")
		}
		a[webhook.RolloutHash] = a[webhook.RecommendationHash]
		workloadObj.SetAnnotations(a)
		if err := e.rolloutController.TriggerRollingRestart(ctx, workloadObj, policy); err != nil {
			log.Error(err, "Failed to trigger rolling restart",
				"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
				fieldContainers, containerNames(recs),
				"rolloutStrategy", policy.GetRolloutStrategy(),
			)
			return nil, fmt.Errorf("rollout not applied: %w", err)
		} else {
			log.Info("Rolling restart triggered successfully",
				"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
				"rolloutStrategy", policy.GetRolloutStrategy(),
			)
		}
	}

	// Record resource change magnitudes per container
	for _, cr := range recs {
		beforeCPU, beforeMemory := containerBeforeValues(currentResources, cr.Container)
		e.recordResourceChangeMagnitudes(policy.Name, workload.Namespace, workload.Name, beforeCPU, beforeMemory, cr.Recommendation)
	}

	duration := time.Since(startTime).Seconds()
	log.Info("Successfully applied resource changes via webhook strategy",
		"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
		fieldContainers, containerNames(recs),
		"method", "Webhook",
		"policy", policy.Name,
		"rolloutStrategy", policy.GetRolloutStrategy(),
		"duration", fmt.Sprintf("%.3fs", duration),
		"reason", "Optimization completed successfully",
		"resourceChanges", describeResourceChanges(currentResources, recs),
		"updateRequestsOnly", policy.Spec.UpdateStrategy.UpdateRequestsOnly,
	)

	// Record successful webhook operation
	observability.RecordOptimizationSuccess(
		policy.Name,
		workload.Namespace,
		workload.Name,
		workload.Kind,
		"Webhook",
	)

	// Record webhook-specific metrics
	observability.RecordOptimizationDecisionDuration(policy.Name, workload.Kind, duration)

	return &ApplyResult{
		Method:         "Webhook",
		FieldOwnership: false, // Webhooks don't use field ownership
	}, nil
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
			observability.RecordDefaultMultiplierUsage("", resourceCPU, fmt.Sprintf("%.1f", cpuMultiplier))
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

// buildResourcePatch builds a JSON patch for updating the resource requests of
// every recommended container in the workload
func (e *Engine) buildResourcePatch(
	workload *Workload,
	recs []ContainerRecommendation,
	policy *optipodv1alpha1.OptimizationPolicy,
) ([]byte, error) {
	return e.containerResourcePatch(workload, recs, policy, false)

}

// getGVR returns the GroupVersionResource for a workload kind
func (e *Engine) getGVR(kind string) (schema.GroupVersionResource, error) {
	return workloaddiscovery.GVR(kind)
}

// buildSSAPatch constructs a Server-Side Apply patch containing only resource
// fields, with one containers entry per recommended container, so one apply
// carries every container's recommended resources.
func (e *Engine) buildSSAPatch(
	workload *Workload,
	recs []ContainerRecommendation,
	policy *optipodv1alpha1.OptimizationPolicy,
) ([]byte, error) {
	return e.containerResourcePatch(workload, recs, policy, true)

}

// recordResourceChangeMagnitudes records the magnitude of resource changes for metrics
func (e *Engine) recordResourceChangeMagnitudes(policyName, namespace, workloadName, beforeCPU, beforeMemory string, rec *recommendation.Recommendation) {
	// Parse before CPU and calculate change percentage
	if beforeCPU != "0" && beforeCPU != "" {
		if beforeCPUQuantity, err := resource.ParseQuantity(beforeCPU); err == nil && !beforeCPUQuantity.IsZero() {
			beforeCPUValue := beforeCPUQuantity.MilliValue()
			afterCPUValue := rec.CPU.MilliValue()
			changePercent := float64(afterCPUValue-beforeCPUValue) / float64(beforeCPUValue) * 100
			observability.RecordResourceChangeMagnitude(policyName, namespace, workloadName, resourceCPU, changePercent)
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

// convertToClientObject converts an application.Workload to client.Object for webhook operations
func (e *Engine) convertToClientObject(workload *Workload) (client.Object, error) {
	obj, err := workloaddiscovery.WorkloadObject(workload.Kind)
	if err != nil {
		return nil, err
	}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(workload.Object.Object, obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func (e *Engine) revalidateApplication(ctx context.Context, workload *Workload, recs []ContainerRecommendation, policy *optipodv1alpha1.OptimizationPolicy) error {
	restrictions, err := coexist.Read(ctx, e.client, workload.Namespace, workload.Kind, workload.Name)
	if err != nil {
		return err
	}
	for _, rec := range recs {
		if restrictions.Reason(rec.Container, resourceCPU) != "" {
			rec.Recommendation.PreserveCPU = true
		}
		if restrictions.Reason(rec.Container, "memory") != "" {
			rec.Recommendation.PreserveMemory = true
		}
	}
	obj, err := e.convertToClientObject(workload)
	if err != nil {
		return err
	}
	if err := e.client.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
		return err
	}
	if workload.Kind == "Rollout" && !e.rolloutController.IsWorkloadReady(obj) {
		return fmt.Errorf("argo Rollout is paused, progressing, aborted, or not healthy; optimization deferred")
	}
	if obj.GetAnnotations()[history.Paused] == valueTrue && !isRollback(ctx) {
		return fmt.Errorf("application blocked: workload is paused")
	}
	if obj.GetDeletionTimestamp() != nil {
		return fmt.Errorf("application blocked: workload is terminating")
	}
	if obj.GetUID() != workload.Object.GetUID() {
		return fmt.Errorf("application blocked: workload identity changed")
	}
	live, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return err
	}
	workload.Object = &unstructured.Unstructured{Object: live}
	// Typed client reads may omit TypeMeta. Keep the explicit workload kind
	// so CronJob template paths remain correct after the final identity check.
	workload.Object.SetKind(workload.Kind)
	gvr, err := workloaddiscovery.GVR(workload.Kind)
	if err != nil {
		return err
	}
	workload.Object.SetAPIVersion(gvr.GroupVersion().String())
	winner, err := policyselection.NewPolicySelector(e.client).SelectBestPolicy(ctx, &workloaddiscovery.Workload{
		Kind: workload.Kind, Namespace: workload.Namespace, Name: workload.Name, Object: obj, Labels: obj.GetLabels(),
	})
	if err != nil {
		return fmt.Errorf("revalidate application policy: %w", err)
	}
	if winner.UID != policy.UID || winner.Generation != policy.Generation || winner.Spec.Mode != optipodv1alpha1.ModeAuto {
		return fmt.Errorf("application blocked: selected policy changed while computing recommendation")
	}
	return nil
}

func (e *Engine) applyStrategy(ctx context.Context, workload *Workload, recs []ContainerRecommendation, policy *optipodv1alpha1.OptimizationPolicy) (*ApplyResult, error) {
	log := ctrl.LoggerFrom(ctx)

	// Determine strategy - webhook is the default
	strategy := policy.GetStrategy()

	log.V(1).Info("Applying resource recommendations",
		"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
		fieldContainers, containerNames(recs),
		"strategy", strategy,
	)

	if strategy == optipodv1alpha1.StrategySSA {
		if err := e.guardMergeOwnership(workload, recs, policy); err != nil {
			return nil, err
		}
	}
	// Route to appropriate apply method based on strategy
	switch strategy {
	case optipodv1alpha1.StrategyWebhook:
		return e.ApplyWithWebhook(ctx, workload, recs, policy)
	case optipodv1alpha1.StrategySSA:
		// Resource quantities are non-nullable in SSA: a null limit becomes
		// zero. Strategic merge deletes it atomically with the request update.
		if (policy.ControlsResource(resourceCPU) && policy.LimitMode(true) == limitNone) || (policy.ControlsResource("memory") && policy.LimitMode(false) == limitNone) {
			if err := e.ApplyWithStrategicMerge(ctx, workload, recs, policy); err != nil {
				return nil, err
			}
			return &ApplyResult{Method: methodStrategicMerge, FieldOwnership: false}, nil
		}
		// Determine if SSA should be used (backward compatibility)
		useSSA := true
		if policy.Spec.UpdateStrategy.UseServerSideApply != nil {
			useSSA = *policy.Spec.UpdateStrategy.UseServerSideApply
		}

		if useSSA {
			err := e.ApplyWithSSA(ctx, workload, recs, policy)
			if err != nil {
				return nil, err
			}
			return &ApplyResult{
				Method:         "ServerSideApply",
				FieldOwnership: true,
			}, nil
		}

		// Fall back to Strategic Merge Patch
		err := e.ApplyWithStrategicMerge(ctx, workload, recs, policy)
		if err != nil {
			return nil, err
		}
		return &ApplyResult{
			Method:         methodStrategicMerge,
			FieldOwnership: false,
		}, nil
	default:
		return nil, fmt.Errorf("unknown strategy: %s", strategy)
	}
}

func (e *Engine) applyValidated(ctx context.Context, workload *Workload, recs []ContainerRecommendation, policy *optipodv1alpha1.OptimizationPolicy) (*ApplyResult, error) {
	if e.client != nil && policy.UID != "" {
		decision, err := e.CanApply(ctx, workload, recs[0].Container, recs[0].Recommendation, policy)
		if err != nil {
			return nil, err
		}
		if !decision.CanApply {
			return nil, fmt.Errorf("application blocked: %s", decision.Reason)
		}
		if decision.Method == InPlace {
			return e.applyInPlace(ctx, workload, recs, policy)
		}
		if decision.Method == Recreate {
			changed, err := e.resourceChangeNeeded(workload, recs, policy)
			if err != nil {
				return nil, err
			}
			if err := e.guardRestartIfNeeded(ctx, workload, policy.IsWebhookStrategy() || changed); err != nil {
				return nil, err
			}
		}
	}
	return e.applyStrategy(ctx, workload, recs, policy)
}
