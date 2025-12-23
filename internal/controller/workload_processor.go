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
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/application"
	"github.com/optipod/optipod/internal/discovery"
	"github.com/optipod/optipod/internal/metrics"
	"github.com/optipod/optipod/internal/observability"
	"github.com/optipod/optipod/internal/recommendation"
)

// ApplicationEngine defines the interface for applying resource changes
type ApplicationEngine interface {
	CanApply(ctx context.Context, workload *application.Workload, rec *recommendation.Recommendation, policy *optipodv1alpha1.OptimizationPolicy) (*application.ApplyDecision, error)
	Apply(ctx context.Context, workload *application.Workload, containerName string, rec *recommendation.Recommendation, policy *optipodv1alpha1.OptimizationPolicy) (*application.ApplyResult, error)
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
	return &WorkloadProcessor{
		metricsProvider:      metricsProvider,
		recommendationEngine: recommendationEngine,
		applicationEngine:    applicationEngine,
		metricsProviderType:  "metrics-server", // Default, can be made configurable
		client:               k8sClient,
	}
}

// ProcessWorkload processes a single workload according to the policy
// It coordinates metrics collection, recommendation computation, and application
func (wp *WorkloadProcessor) ProcessWorkload(
	ctx context.Context,
	workload *discovery.Workload,
	policy *optipodv1alpha1.OptimizationPolicy,
) (*optipodv1alpha1.WorkloadStatus, error) {
	status := &optipodv1alpha1.WorkloadStatus{
		Name:      workload.Name,
		Namespace: workload.Namespace,
		Kind:      workload.Kind,
	}

	// Handle mode-specific behavior
	switch policy.Spec.Mode {
	case optipodv1alpha1.ModeDisabled:
		// Skip processing but preserve status
		status.Status = StatusSkipped
		status.Reason = "Policy is disabled"
		return status, nil

	case optipodv1alpha1.ModeRecommend, optipodv1alpha1.ModeAuto:
		// Continue with processing
	default:
		return nil, fmt.Errorf("unknown policy mode: %s", policy.Spec.Mode)
	}

	// Get containers from workload
	containers, err := wp.getContainers(workload)
	if err != nil {
		status.Status = StatusError
		status.Reason = fmt.Sprintf("Failed to extract containers: %v", err)
		return status, err
	}

	// Process each container
	var recommendations []optipodv1alpha1.ContainerRecommendation //nolint:prealloc // Size unknown
	hasMetricsError := false
	metricsErrorMsg := ""

	for _, container := range containers {
		// Collect metrics for this container
		// For simplicity, we'll query metrics for the first pod of the workload
		podName, err := wp.getFirstPodName(workload)
		if err != nil {
			hasMetricsError = true
			metricsErrorMsg = fmt.Sprintf("Failed to get pod name for container %s: %v", container.Name, err)
			// Log the error for debugging
			fmt.Printf("DEBUG: Failed to get pod name for workload %s/%s container %s: %v\n",
				workload.Namespace, workload.Name, container.Name, err)
			continue
		}

		// Get rolling window from policy
		rollingWindow := 24 * time.Hour
		if policy.Spec.MetricsConfig.RollingWindow.Duration > 0 {
			rollingWindow = policy.Spec.MetricsConfig.RollingWindow.Duration
		}

		// Track metrics collection duration
		metricsTimer := observability.MetricsCollectionDuration.WithLabelValues(wp.metricsProviderType)
		metricsStartTime := time.Now()

		containerMetrics, err := wp.metricsProvider.GetContainerMetrics(
			ctx,
			workload.Namespace,
			podName,
			container.Name,
			rollingWindow,
		)

		metricsTimer.Observe(time.Since(metricsStartTime).Seconds())

		if err != nil {
			// Handle missing metrics error
			hasMetricsError = true
			metricsErrorMsg = fmt.Sprintf("Failed to collect metrics for container %s: %v", container.Name, err)
			continue
		}

		// Compute recommendation
		rec, err := wp.recommendationEngine.ComputeRecommendation(containerMetrics, policy)
		if err != nil {
			status.Status = StatusError
			status.Reason = fmt.Sprintf("Failed to compute recommendation for container %s: %v", container.Name, err)
			return status, err
		}

		// Store recommendation
		// Make copies of the quantities to avoid any pointer aliasing issues
		cpuCopy := rec.CPU.DeepCopy()
		memoryCopy := rec.Memory.DeepCopy()

		recommendations = append(recommendations, optipodv1alpha1.ContainerRecommendation{
			Container:   container.Name,
			CPU:         &cpuCopy,
			Memory:      &memoryCopy,
			Explanation: rec.Explanation,
		})
	}

	// If we have metrics errors, prevent changes
	if hasMetricsError {
		status.Status = StatusSkipped
		status.Reason = fmt.Sprintf("Missing metrics: %s", metricsErrorMsg)
		status.Recommendations = recommendations
		now := metav1.Now()
		status.LastRecommendation = &now
		return status, nil
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

	// In Recommend mode, we only store recommendations (via annotations)
	if policy.Spec.Mode == optipodv1alpha1.ModeRecommend {
		status.Status = StatusRecommended
		status.Reason = "Recommendations computed, not applied (Recommend mode)"
		return status, nil
	}

	// In Auto mode, attempt to apply changes
	if policy.Spec.Mode == optipodv1alpha1.ModeAuto {
		// Track apply result for status updates
		var lastApplyResult *application.ApplyResult

		// For each container, apply the recommendation
		for i, rec := range recommendations {
			// Convert workload to application.Workload format
			appWorkload, err := wp.convertToApplicationWorkload(workload)
			if err != nil {
				status.Status = StatusError
				status.Reason = fmt.Sprintf("Failed to convert workload: %v", err)
				return status, err
			}

			// Create recommendation object
			appRec := &recommendation.Recommendation{
				CPU:    *rec.CPU,
				Memory: *rec.Memory,
			}

			// Check if we can apply
			decision, err := wp.applicationEngine.CanApply(ctx, appWorkload, appRec, policy)
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

			// Apply the changes
			applyResult, err := wp.applicationEngine.Apply(ctx, appWorkload, rec.Container, appRec, policy)
			if err != nil {
				status.Status = StatusError
				status.Reason = fmt.Sprintf("Failed to apply changes to container %s: %v", rec.Container, err)
				return status, err
			}

			// Store the apply result
			lastApplyResult = applyResult

			// Update last applied timestamp (only once after all containers)
			if i == len(recommendations)-1 {
				now := metav1.Now()
				status.LastApplied = &now
			}
		}

		// Update status with SSA information
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

// getContainers extracts container information from a workload
func (wp *WorkloadProcessor) getContainers(workload *discovery.Workload) ([]corev1.Container, error) {
	var containers []corev1.Container

	switch obj := workload.Object.(type) {
	case *appsv1.Deployment:
		containers = obj.Spec.Template.Spec.Containers
	case *appsv1.StatefulSet:
		containers = obj.Spec.Template.Spec.Containers
	case *appsv1.DaemonSet:
		containers = obj.Spec.Template.Spec.Containers
	default:
		return nil, fmt.Errorf("unsupported workload type: %T", workload.Object)
	}

	return containers, nil
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
		obj, err := wp.getWorkloadObject(workload)
		if err != nil {
			// Non-retryable error
			return false, fmt.Errorf("failed to get workload object: %w", err)
		}

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
		annotations[optipodv1alpha1.AnnotationLastRecommendation] = time.Now().Format(time.RFC3339)

		// Add per-container recommendations (requests)
		for _, rec := range recommendations {
			if rec.CPU != nil {
				annotationKey := fmt.Sprintf("%s.%s.cpu-request", optipodv1alpha1.AnnotationRecommendationPrefix, rec.Container)
				annotations[annotationKey] = rec.CPU.String()
			}
			if rec.Memory != nil {
				annotationKey := fmt.Sprintf("%s.%s.memory-request", optipodv1alpha1.AnnotationRecommendationPrefix, rec.Container)
				annotations[annotationKey] = rec.Memory.String()
			}
		}

		// Add limit annotations if limits are being updated
		if !policy.Spec.UpdateStrategy.UpdateRequestsOnly {
			for _, rec := range recommendations {
				if rec.CPU != nil && rec.Memory != nil {
					// Calculate limits using the same logic as the application engine
					cpuLimit, memoryLimit := wp.calculateLimitsForAnnotation(rec.CPU, rec.Memory, policy)

					cpuLimitKey := fmt.Sprintf("%s.%s.cpu-limit", optipodv1alpha1.AnnotationRecommendationPrefix, rec.Container)
					annotations[cpuLimitKey] = cpuLimit.String()

					memoryLimitKey := fmt.Sprintf("%s.%s.memory-limit", optipodv1alpha1.AnnotationRecommendationPrefix, rec.Container)
					annotations[memoryLimitKey] = memoryLimit.String()
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

// calculateLimitsForAnnotation calculates resource limits for annotation display
func (wp *WorkloadProcessor) calculateLimitsForAnnotation(cpuRequest, memoryRequest *resource.Quantity, policy *optipodv1alpha1.OptimizationPolicy) (resource.Quantity, resource.Quantity) {
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

	// Calculate limits - use MilliValue for CPU to preserve millicores
	cpuMilliValue := cpuRequest.MilliValue()
	cpuLimitMilliValue := int64(float64(cpuMilliValue) * cpuMultiplier)
	cpuLimit := resource.NewMilliQuantity(cpuLimitMilliValue, cpuRequest.Format)

	// For memory, use Value and preserve format
	memoryValue := memoryRequest.Value()
	memoryLimitValue := int64(float64(memoryValue) * memoryMultiplier)
	memoryLimit := resource.NewQuantity(memoryLimitValue, memoryRequest.Format)

	return *cpuLimit, *memoryLimit
}

// getWorkloadObject returns the workload as a client.Object for updating
func (wp *WorkloadProcessor) getWorkloadObject(workload *discovery.Workload) (client.Object, error) {
	switch obj := workload.Object.(type) {
	case *appsv1.Deployment:
		return obj, nil
	case *appsv1.StatefulSet:
		return obj, nil
	case *appsv1.DaemonSet:
		return obj, nil
	default:
		return nil, fmt.Errorf("unsupported workload type: %T", workload.Object)
	}
}
