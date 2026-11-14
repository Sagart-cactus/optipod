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

package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/application"
	"github.com/optipod/optipod/internal/coexist"
	"github.com/optipod/optipod/internal/discovery"
	"github.com/optipod/optipod/internal/history"
	"github.com/optipod/optipod/internal/metrics"
	"github.com/optipod/optipod/internal/observability"
	"github.com/optipod/optipod/internal/recommendation"
	"github.com/optipod/optipod/internal/safety"
	"github.com/optipod/optipod/internal/webhook"
)

// ApplicationEngine defines the interface for applying resource changes
type ApplicationEngine interface {
	CanApply(ctx context.Context, workload *application.Workload, containerName string, rec *recommendation.Recommendation, policy *optipodv1alpha1.OptimizationPolicy) (*application.ApplyDecision, error)
	Apply(ctx context.Context, workload *application.Workload, recommendations []application.ContainerRecommendation, policy *optipodv1alpha1.OptimizationPolicy) (*application.ApplyResult, error)
}

// WorkloadProcessor handles the processing of individual workloads
type WorkloadProcessor struct {
	metricsProvider      metrics.MetricsProvider
	recommendationEngine *recommendation.Engine
	applicationEngine    ApplicationEngine
	metricsProviderType  string
	client               client.Client
}

// NewWorkloadProcessor creates a new workload processor
func NewWorkloadProcessor(
	metricsProvider metrics.MetricsProvider,
	recommendationEngine *recommendation.Engine,
	applicationEngine ApplicationEngine,
	k8sClient client.Client,
) *WorkloadProcessor {
	providerName := providerMetricsServer
	switch metricsProvider.(type) {
	case *metrics.PrometheusProvider:
		providerName = "prometheus"
	}
	return &WorkloadProcessor{
		metricsProvider:      metricsProvider,
		recommendationEngine: recommendationEngine,
		applicationEngine:    applicationEngine,
		metricsProviderType:  providerName,
		client:               k8sClient,
	}
}

// ProcessWorkload processes a single workload according to the policy
// It coordinates metrics collection, recommendation computation, and application
func (wp *WorkloadProcessor) ProcessWorkload(
	ctx context.Context,
	workload *discovery.Workload,
	policy *optipodv1alpha1.OptimizationPolicy,
) (_ *optipodv1alpha1.WorkloadStatus, processErr error) {
	status := &optipodv1alpha1.WorkloadStatus{
		Name:      workload.Name,
		Namespace: workload.Namespace,
		Kind:      workload.Kind,
	}
	defer func() { processErr = wp.recordDecision(ctx, workload, policy, status, processErr) }()

	if handled, err := wp.processWithoutMetrics(ctx, workload, policy, status); handled || err != nil {
		return status, err
	}

	if wp.client != nil {
		if err := webhook.MarkReadySpec(ctx, wp.client, workload.Object); err != nil {
			return status, err
		}
	}
	// Get containers from workload
	containers, err := wp.selectedContainers(ctx, workload, policy)
	if err != nil {
		status.Status = StatusError
		status.Reason = fmt.Sprintf("Failed to extract containers: %v", err)
		return status, err
	}

	var safetyState *safety.State
	if wp.client != nil {
		safetyState, err = safety.Load(ctx, wp.client, workload, containers)
		if err != nil {
			return status, err
		}
		safetyState.ObserveOOM(policy, time.Now())
		if err = safetyState.Save(ctx, wp.client, workload); err != nil {
			return status, err
		}
		if err = safetyState.RecoveryError(policy); err != nil {
			if policy.Spec.Mode == optipodv1alpha1.ModeAuto {
				if invalidateErr := webhook.InvalidateRecommendations(ctx, wp.client, workload.Object, policy); invalidateErr != nil {
					return status, invalidateErr
				}
			}
			status.Status = StatusSkipped
			status.Reason = err.Error()
			return status, nil
		}
	}
	// Process each container
	var restrictions *coexist.Restrictions
	if wp.client != nil {
		restrictions, err = coexist.Read(ctx, wp.client, workload.Namespace, workload.Kind, workload.Name)
		if err != nil {
			return status, err
		}
	}

	recommendations, hasMetricsError, metricsErrorMsg, err := wp.computeRecommendations(ctx, workload, policy, status, containers, safetyState, restrictions)
	if err != nil {
		return status, err
	}

	if len(recommendations) == 0 && !hasMetricsError {
		if wp.client != nil && policy.Spec.Mode == optipodv1alpha1.ModeAuto {
			if err := webhook.InvalidateRecommendations(ctx, wp.client, workload.Object, policy); err != nil {
				return status, err
			}
		}
		status.Status = StatusSkipped
		status.Reason = "No optimizable resources: exclusions or horizontal scalers reserve all dimensions"
		return status, nil
	}

	// If we have metrics errors, prevent changes
	if hasMetricsError && safetyState != nil {
		emergency := emergencyRecommendations(containers, safetyState, policy)
		if len(emergency) > 0 {
			recommendations = emergency
			hasMetricsError = false
		}
	}
	if hasMetricsError {
		if wp.client != nil && policy.Spec.Mode == optipodv1alpha1.ModeAuto {
			if err := webhook.InvalidateRecommendations(ctx, wp.client, workload.Object, policy); err != nil {
				return status, err
			}
		}
		status.Status = StatusSkipped
		status.Reason = fmt.Sprintf("Missing metrics: %s", metricsErrorMsg)
		status.Recommendations = recommendations
		now := metav1.Now()
		status.LastRecommendation = &now
		return status, nil
	}

	if safetyState != nil {
		safetyState.Guard(recommendations, policy, time.Now())
		safetyState.ApplyRecovery(recommendations, policy, time.Now())
	}
	// Update status with recommendations
	status.Recommendations = recommendations
	now := metav1.Now()
	status.LastRecommendation = &now

	// Add annotations to workload for visibility
	// In test mode (when client is nil), skip annotations to avoid test failures
	if wp.client != nil {
		if err := wp.addRecommendationAnnotations(ctx, workload, recommendations, policy); err != nil {
			// Log the error but don't fail the whole operation
			// The error will be visible in the status
			status.Status = StatusError
			status.Reason = fmt.Sprintf("Failed to add annotations: %v", err)
			return status, err
		}
	}

	return wp.publishRecommendations(ctx, workload, policy, status, recommendations, safetyState)
}

// getFirstPodName gets the name of the first pod for a workload by querying the actual pods
func (wp *WorkloadProcessor) getFirstPodName(workload *discovery.Workload) (string, error) {
	// For StatefulSets, we can use the predictable pod naming
	if workload.Kind == "StatefulSet" {
		return fmt.Sprintf("%s-0", workload.Name), nil
	}

	// If client is nil (e.g., in tests), use a simple naming convention
	if wp.client == nil {
		return fmt.Sprintf("%s-test-pod", workload.Name), nil
	}

	// Get the full pod selector from the workload (including MatchExpressions)
	var labelSelector *metav1.LabelSelector

	switch obj := workload.Object.(type) {
	case *appsv1.Deployment:
		labelSelector = obj.Spec.Selector
		fmt.Printf("DEBUG: Looking for pods with selector %+v in namespace %s\n", labelSelector, workload.Namespace)
	case *appsv1.DaemonSet:
		labelSelector = obj.Spec.Selector
		fmt.Printf("DEBUG: Looking for pods with selector %+v in namespace %s\n", labelSelector, workload.Namespace)
	default:
		return "", fmt.Errorf("unsupported workload type: %T", workload.Object)
	}

	// Create a context with timeout that respects cancellation
	ctxWithTimeout, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	podList := &corev1.PodList{}

	// Convert LabelSelector to labels.Selector for proper handling of MatchExpressions
	selector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		return "", fmt.Errorf("failed to convert label selector: %w", err)
	}

	// Use MatchingLabelsSelector to handle both MatchLabels and MatchExpressions
	listOpts := []client.ListOption{
		client.InNamespace(workload.Namespace),
		client.MatchingLabelsSelector{
			Selector: selector,
		},
		client.Limit(1), // We only need one pod
	}

	if err := wp.client.List(ctxWithTimeout, podList, listOpts...); err != nil {
		fmt.Printf("DEBUG: Failed to list pods: %v\n", err)
		return "", fmt.Errorf("failed to list pods: %w", err)
	}

	fmt.Printf("DEBUG: Found %d pods for workload %s/%s\n", len(podList.Items), workload.Namespace, workload.Name)

	if len(podList.Items) == 0 {
		return "", fmt.Errorf("no pods found for workload %s/%s with selector %v", workload.Namespace, workload.Name, selector)
	}

	podName := podList.Items[0].Name
	fmt.Printf("DEBUG: Using pod %s for workload %s/%s\n", podName, workload.Namespace, workload.Name)
	return podName, nil
}

// convertToApplicationWorkload converts a discovery.Workload to application.Workload
func (wp *WorkloadProcessor) convertToApplicationWorkload(workload *discovery.Workload) (*application.Workload, error) {
	// Convert the typed object to unstructured
	unstructuredObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(workload.Object)
	if err != nil {
		return nil, fmt.Errorf("failed to convert to unstructured: %w", err)
	}

	return &application.Workload{
		Kind:      workload.Kind,
		Namespace: workload.Namespace,
		Name:      workload.Name,
		Object:    &unstructured.Unstructured{Object: unstructuredObj},
	}, nil
}

// addRecommendationAnnotations adds annotations to the workload with recommendation details
// Uses retry logic with exponential backoff to handle concurrent modification conflicts
func (wp *WorkloadProcessor) addRecommendationAnnotations(ctx context.Context, workload *discovery.Workload, recommendations []optipodv1alpha1.ContainerRecommendation, policy *optipodv1alpha1.OptimizationPolicy) error {
	if wp.client == nil {
		return fmt.Errorf("client is nil, cannot add annotations")
	}

	log := logf.FromContext(ctx)

	// Retry configuration
	const maxRetries = 5
	const baseDelay = 100 * time.Millisecond
	const maxDelay = 5 * time.Second

	// Retry function with exponential backoff
	return wait.ExponentialBackoff(wait.Backoff{
		Duration: baseDelay,
		Factor:   2.0,
		Jitter:   0.1,
		Steps:    maxRetries,
		Cap:      maxDelay,
	}, func() (bool, error) {
		// Get the workload object
		obj := workload.Object

		// Fetch the latest version from the API server to avoid conflicts
		objKey := client.ObjectKeyFromObject(obj)
		if err := wp.client.Get(ctx, objKey, obj); err != nil {
			if apierrors.IsNotFound(err) {
				// Workload was deleted, stop retrying
				log.Info("Workload was deleted during annotation update", "workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name))
				return false, nil
			}
			// Retryable error
			log.V(1).Info("Failed to fetch workload, retrying", "workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name), "error", err)
			return false, nil
		}

		// Check if annotations have already been updated by another reconciliation
		existingAnnotations := obj.GetAnnotations()
		if existingAnnotations != nil {
			if lastRec, exists := existingAnnotations[optipodv1alpha1.AnnotationLastRecommendation]; exists {
				// Parse the existing timestamp
				if lastRecTime, err := time.Parse(time.RFC3339, lastRec); err == nil {
					// If the existing annotation is very recent (within 30 seconds), skip update
					if time.Since(lastRecTime) < 30*time.Second {
						log.V(1).Info("Annotations recently updated by another reconciliation, skipping",
							"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
							"lastUpdate", lastRecTime)
						return true, nil // Success, no need to update
					}
				}
			}
		}

		// Prepare annotations
		annotations := obj.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}

		// Add management annotations
		annotations[optipodv1alpha1.AnnotationManaged] = "true"
		annotations[optipodv1alpha1.AnnotationPolicy] = policy.Name
		annotations[optipodv1alpha1.AnnotationPolicyUID] = string(policy.UID)
		annotations[optipodv1alpha1.AnnotationLastRecommendation] = time.Now().Format(time.RFC3339)

		// Use the exact application plan for display, including independent modes,
		// omitted resources and explicit removals. Clear stale recommendation keys.
		plan, err := application.ResourcePlan(applicationRecommendations(recommendations), policy)
		if err != nil {
			return false, err
		}
		for key := range annotations {
			if strings.HasPrefix(key, optipodv1alpha1.AnnotationRecommendationPrefix+".") {
				delete(annotations, key)
			}
		}
		for container, sections := range plan {
			for section, raw := range sections {
				for name, value := range raw.(map[string]any) {
					if value == nil {
						continue
					}
					key := fmt.Sprintf("%s.%s.%s-%s", optipodv1alpha1.AnnotationRecommendationPrefix, container, name, strings.TrimSuffix(section, "s"))
					annotations[key] = value.(string)
				}
			}
		}

		// Update annotations
		obj.SetAnnotations(annotations)

		// Attempt to update the workload
		if err := wp.client.Update(ctx, obj); err != nil {
			if apierrors.IsConflict(err) {
				// Conflict error - retry with exponential backoff
				log.V(1).Info("Conflict updating workload annotations, retrying",
					"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
					"error", err)
				return false, nil // Retry
			}

			if apierrors.IsNotFound(err) {
				// Workload was deleted
				log.Info("Workload was deleted during annotation update", "workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name))
				return false, nil // Don't retry, but don't fail
			}

			// Other errors are not retryable
			return false, fmt.Errorf("failed to update workload annotations: %w", err)
		}

		// Success
		log.V(1).Info("Successfully updated workload annotations", "workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name))
		return true, nil
	})
}

func applicationRecommendations(recommendations []optipodv1alpha1.ContainerRecommendation) []application.ContainerRecommendation {
	recs := make([]application.ContainerRecommendation, 0, len(recommendations))
	for _, r := range recommendations {
		rec := &recommendation.Recommendation{PreserveCPU: r.CPU == nil, PreserveMemory: r.Memory == nil, MinimumMemoryLimit: r.MinimumMemoryLimit}
		if r.CPU != nil {
			rec.CPU = *r.CPU
		}
		if r.Memory != nil {
			rec.Memory = *r.Memory
		}
		recs = append(recs, application.ContainerRecommendation{Container: r.Container, Recommendation: rec})
	}
	return recs
}

func (wp *WorkloadProcessor) processWithoutMetrics(ctx context.Context, workload *discovery.Workload, policy *optipodv1alpha1.OptimizationPolicy, status *optipodv1alpha1.WorkloadStatus) (bool, error) {
	if wp.client != nil {
		if h, ok := wp.applicationEngine.(interface {
			ReconcileHorizontal(context.Context, *discovery.Workload, *optipodv1alpha1.OptimizationPolicy) error
		}); ok {
			if err := h.ReconcileHorizontal(ctx, workload, policy); err != nil {
				status.Status = StatusError
				status.Reason = err.Error()
				return true, err
			}
		}
	}
	if wp.client != nil {
		if target := workload.Object.GetAnnotations()[history.RollbackTo]; target != "" {
			engine, ok := wp.applicationEngine.(interface {
				Rollback(context.Context, *discovery.Workload, *optipodv1alpha1.OptimizationPolicy, string) (*application.ApplyResult, error)
			})
			if !ok {
				return true, fmt.Errorf("application engine does not support rollback")
			}
			result, err := engine.Rollback(ctx, workload, policy, target)
			if err != nil {
				status.Status = StatusError
				status.Reason = err.Error()
				return true, err
			}
			status.Status = StatusSkipped
			status.Reason = "Workload paused after rollback"
			if result != nil && result.Method != "RollbackNoChange" {
				status.Status = StatusApplied
				status.LastApplyMethod = result.Method
				status.Reason = result.Method
			}
			return true, nil
		}
		if workload.Object.GetAnnotations()[history.Paused] == "true" {
			status.Status = StatusSkipped
			status.Reason = "Workload is paused"
			return true, nil
		}
	}
	if policy.Spec.DaemonSetSizing != nil {
		if policy.Spec.Mode == optipodv1alpha1.ModeDisabled {
			status.Status = StatusSkipped
			status.Reason = reasonDisabled
			return true, nil
		}
		engine, ok := wp.applicationEngine.(interface {
			ReconcileNodeSizing(context.Context, *discovery.Workload, *optipodv1alpha1.OptimizationPolicy) error
		})
		if !ok {
			return true, fmt.Errorf("application engine does not support node sizing")
		}
		if err := engine.ReconcileNodeSizing(ctx, workload, policy); err != nil {
			return true, err
		}
		status.Status = StatusSkipped
		status.Reason = "Node sizing preview recorded; Auto sizes new DaemonSet pods at admission"
		return true, nil
	}
	actualProvider := ""
	switch wp.metricsProvider.(type) {
	case *metrics.PrometheusProvider:
		actualProvider = "prometheus"
	case *metrics.MetricsServerProvider:
		actualProvider = providerMetricsServer
	}
	if actualProvider != "" && policy.Spec.MetricsConfig.Provider != actualProvider {
		if wp.client != nil && policy.Spec.Mode == optipodv1alpha1.ModeAuto {
			if err := webhook.InvalidateRecommendations(ctx, wp.client, workload.Object, policy); err != nil {
				return true, err
			}
		}
		status.Status = StatusSkipped
		status.Reason = fmt.Sprintf("policy metrics provider %s differs from configured provider %s", policy.Spec.MetricsConfig.Provider, actualProvider)
		return true, nil
	}

	// Handle mode-specific behavior
	switch policy.Spec.Mode {
	case optipodv1alpha1.ModeDisabled:
		// Skip processing but preserve status
		status.Status = StatusSkipped
		status.Reason = reasonDisabled
		return true, nil

	case optipodv1alpha1.ModeRecommend, optipodv1alpha1.ModeAuto:
		// Continue with processing
	default:
		return true, fmt.Errorf("unknown policy mode: %s", policy.Spec.Mode)
	}

	return false, nil
}

func (wp *WorkloadProcessor) publishRecommendations(ctx context.Context, workload *discovery.Workload, policy *optipodv1alpha1.OptimizationPolicy, status *optipodv1alpha1.WorkloadStatus, recommendations []optipodv1alpha1.ContainerRecommendation, safetyState *safety.State) (*optipodv1alpha1.WorkloadStatus, error) {
	// In Recommend mode, we only store recommendations (via annotations)
	if policy.Spec.Mode == optipodv1alpha1.ModeRecommend {
		if wp.client != nil && policy.UID != "" {
			recs := make([]application.ContainerRecommendation, 0, len(recommendations))
			for _, r := range recommendations {
				rec := &recommendation.Recommendation{PreserveCPU: r.CPU == nil, PreserveMemory: r.Memory == nil, MinimumMemoryLimit: r.MinimumMemoryLimit}
				if r.CPU != nil {
					rec.CPU = *r.CPU
				}
				if r.Memory != nil {
					rec.Memory = *r.Memory
				}
				recs = append(recs, application.ContainerRecommendation{Container: r.Container, Recommendation: rec})
			}
			plan, err := application.ResourcePlan(recs, policy)
			if err != nil {
				return status, err
			}
			record, err := history.Prepare(ctx, wp.client, workload, policy, plan)
			if err != nil {
				return status, err
			}
			if err := history.Complete(ctx, wp.client, workload, record, "Recommend", nil); err != nil {
				return status, err
			}
		}
		status.Status = StatusRecommended
		status.Reason = "Recommendations computed, not applied (Recommend mode)"
		return status, nil
	}

	// In Auto mode, attempt to apply changes
	if policy.Spec.Mode == optipodv1alpha1.ModeAuto {
		// Convert workload to application.Workload format
		appWorkload, err := wp.convertToApplicationWorkload(workload)
		if err != nil {
			status.Status = StatusError
			status.Reason = fmt.Sprintf("Failed to convert workload: %v", err)
			return status, err
		}

		// Build the workload's full per-container recommendation set and check
		// applicability per container before applying.
		appRecs := make([]application.ContainerRecommendation, 0, len(recommendations))
		for _, rec := range recommendations {
			// Create recommendation object
			appRec := &recommendation.Recommendation{}
			appRec.PreserveCPU = rec.CPU == nil
			appRec.PreserveMemory = rec.Memory == nil
			appRec.Emergency = rec.Emergency
			appRec.MinimumMemoryLimit = rec.MinimumMemoryLimit
			if rec.ValidUntil != nil {
				appRec.ValidUntil = rec.ValidUntil.Time
			}
			if rec.CPU != nil {
				appRec.CPU = *rec.CPU
			}
			if rec.Memory != nil {
				appRec.Memory = *rec.Memory
			}
			appRecs = append(appRecs, application.ContainerRecommendation{
				Container:      rec.Container,
				Recommendation: appRec,
			})

			// Log the strategy being used for this policy
			strategy := policy.GetStrategy()
			log := logf.FromContext(ctx)
			log.V(1).Info("Applying recommendation with strategy",
				"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
				"container", rec.Container,
				"policy", policy.Name,
				"strategy", strategy,
				"rolloutStrategy", policy.GetRolloutStrategy(),
			)

			// Check if we can apply
			decision, err := wp.applicationEngine.CanApply(ctx, appWorkload, rec.Container, appRec, policy)
			if err != nil {
				status.Status = StatusError
				status.Reason = fmt.Sprintf("Failed to determine if changes can be applied: %v", err)
				return status, err
			}

			if !decision.CanApply {
				status.Status = StatusSkipped
				status.Reason = decision.Reason
				return status, nil
			}
		}

		// Apply the workload's full per-container set in ONE workload-wide
		// operation per strategy so sibling containers cannot prune each
		// other's applied fields (multi-container convergence).
		var lastApplyResult *application.ApplyResult
		if len(appRecs) > 0 {
			applyResult, err := wp.applicationEngine.Apply(ctx, appWorkload, appRecs, policy)
			if err != nil {
				status.Status = StatusError
				status.Reason = fmt.Sprintf("Failed to apply changes: %v", err)
				return status, err
			}

			// Store the apply result
			lastApplyResult = applyResult
			if safetyState != nil {
				if err := safetyState.Accept(ctx, wp.client, workload, recommendations, time.Now()); err != nil {
					return status, err
				}
			}

			// Update last applied timestamp (after the workload-wide apply)
			now := metav1.Now()
			status.LastApplied = &now
		}

		// Update status with apply method information
		if lastApplyResult != nil {
			status.LastApplyMethod = lastApplyResult.Method
			status.FieldOwnership = lastApplyResult.FieldOwnership
		}

		status.Status = StatusApplied
		status.Reason = "Recommendations applied successfully"
		return status, nil
	}

	return status, nil
}

func (wp *WorkloadProcessor) minimumSamples(containerMetrics *metrics.ContainerMetrics, policy, containerPolicy *optipodv1alpha1.OptimizationPolicy, container string) string {
	// Enforce per-policy minimum samples requirement for metrics-server.
	// For other providers, this is ignored.
	if wp.metricsProviderType == providerMetricsServer {
		required := 0
		if policy.Spec.MetricsConfig.MetricsServer != nil && policy.Spec.MetricsConfig.MetricsServer.MinSamplesRequired != nil {
			required = int(*policy.Spec.MetricsConfig.MetricsServer.MinSamplesRequired)
		} else {
			type defaultMinSampler interface {
				DefaultMinSamplesRequired() int
			}
			if provider, ok := wp.metricsProvider.(defaultMinSampler); ok {
				required = provider.DefaultMinSamplesRequired()
			}
		}

		if required > 0 {
			samples := containerMetrics.CPU.Samples
			if !containerPolicy.ControlsResource("cpu") || (containerPolicy.ControlsResource("memory") && containerMetrics.Memory.Samples < samples) {
				samples = containerMetrics.Memory.Samples
			}
			if samples < required {
				return fmt.Sprintf("Insufficient metrics samples for container %s: have %d, need %d", container, samples, required)
			}
		}
	}

	return ""
}

func (wp *WorkloadProcessor) computeRecommendations(ctx context.Context, workload *discovery.Workload, policy *optipodv1alpha1.OptimizationPolicy, status *optipodv1alpha1.WorkloadStatus, containers []corev1.Container, safetyState *safety.State, restrictions *coexist.Restrictions) ([]optipodv1alpha1.ContainerRecommendation, bool, string, error) {
	var recommendations []optipodv1alpha1.ContainerRecommendation //nolint:prealloc // Size unknown
	hasMetricsError := false
	metricsErrorMsg := ""

	for _, container := range containers {
		if policy.ExcludesContainer(container.Name) {
			continue
		}
		containerPolicy := policy
		if restrictions != nil {
			containerPolicy = restrictions.ContainerPolicy(policy, container.Name)
			if containerPolicy == nil {
				continue
			}
		}
		metricsTimer := observability.MetricsCollectionDuration.WithLabelValues(wp.metricsProviderType)
		metricsStartTime := time.Now()
		containerMetrics, err := wp.collectWorkloadMetrics(ctx, workload, container.Name, containerPolicy)

		metricsTimer.Observe(time.Since(metricsStartTime).Seconds())

		if err != nil {
			if safetyState != nil {
				if rec := safetyState.EmergencyRecommendation(container.Name, policy, time.Now()); rec != nil {
					recommendations = append(recommendations, *rec)
					continue
				}
			}
			var insufficient *metrics.ErrInsufficientSamples
			if errors.As(err, &insufficient) {
				hasMetricsError = true
				metricsErrorMsg = fmt.Sprintf("Insufficient metrics samples for container %s: %v", container.Name, err)
				continue
			}

			// Handle missing metrics error
			hasMetricsError = true
			metricsErrorMsg = fmt.Sprintf("Failed to collect metrics for container %s: %v", container.Name, err)
			continue
		}

		if missing := wp.minimumSamples(containerMetrics, policy, containerPolicy, container.Name); missing != "" {
			hasMetricsError = true
			metricsErrorMsg = missing
			continue
		}

		// Compute recommendation
		rec, err := wp.recommendationEngine.ComputeRecommendation(containerMetrics, containerPolicy)
		if err != nil {
			if wp.client != nil && policy.Spec.Mode == optipodv1alpha1.ModeAuto {
				if revokeErr := webhook.InvalidateRecommendations(ctx, wp.client, workload.Object, policy); revokeErr != nil {
					return nil, false, "", fmt.Errorf("%w; revoke unsafe recommendation: %v", err, revokeErr)
				}
			}
			status.Status = StatusError
			status.Reason = fmt.Sprintf("Failed to compute recommendation for container %s: %v", container.Name, err)
			return nil, false, "", err
		}

		// Store recommendation
		// Make copies of the quantities to avoid any pointer aliasing issues
		cpuCopy := rec.CPU.DeepCopy()
		memoryCopy := rec.Memory.DeepCopy()

		entry := optipodv1alpha1.ContainerRecommendation{
			Container:   container.Name,
			CPU:         &cpuCopy,
			Memory:      &memoryCopy,
			Explanation: rec.Explanation,
		}
		if !rec.ValidUntil.IsZero() {
			t := metav1.NewTime(rec.ValidUntil)
			entry.ValidUntil = &t
		}
		if !containerPolicy.ControlsResource("cpu") {
			entry.CPU = nil
		}
		if !containerPolicy.ControlsResource("memory") {
			entry.Memory = nil
		}
		recommendations = append(recommendations, entry)
	}
	return recommendations, hasMetricsError, metricsErrorMsg, nil
}

func emergencyRecommendations(containers []corev1.Container, safetyState *safety.State, policy *optipodv1alpha1.OptimizationPolicy) []optipodv1alpha1.ContainerRecommendation {
	var emergency []optipodv1alpha1.ContainerRecommendation
	for _, container := range containers {
		if policy.ExcludesContainer(container.Name) {
			continue
		}
		if rec := safetyState.EmergencyRecommendation(container.Name, policy, time.Now()); rec != nil {
			emergency = append(emergency, *rec)
		}
	}
	return emergency
}

func (wp *WorkloadProcessor) recordDecision(ctx context.Context, workload *discovery.Workload, policy *optipodv1alpha1.OptimizationPolicy, status *optipodv1alpha1.WorkloadStatus, processingErr error) error {
	if wp.client == nil || policy.UID == "" || workload.Object.GetUID() == "" {
		return processingErr
	}
	if err := history.ObserveDecision(ctx, wp.client, workload, policy, status, processingErr); err != nil {
		return errors.Join(processingErr, fmt.Errorf("persist decision history: %w", err))
	}
	return processingErr
}
