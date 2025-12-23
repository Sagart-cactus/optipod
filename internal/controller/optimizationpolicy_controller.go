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
	"math"
	"math/rand"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/discovery"
	"github.com/optipod/optipod/internal/observability"
	"github.com/optipod/optipod/internal/policy"
)

// Initialize random generator once at package level to ensure proper jitter
var rng = rand.New(rand.NewSource(time.Now().UnixNano()))

// OptimizationPolicyReconciler reconciles a OptimizationPolicy object
type OptimizationPolicyReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	Recorder          record.EventRecorder
	WorkloadProcessor *WorkloadProcessor
	EventRecorder     *observability.EventRecorder
	PolicySelector    *policy.PolicySelector
}

// +kubebuilder:rbac:groups=optipod.optipod.io,resources=optimizationpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=optipod.optipod.io,resources=optimizationpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=optipod.optipod.io,resources=optimizationpolicies/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments;statefulsets;daemonsets,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=metrics.k8s.io,resources=pods;nodes,verbs=get;list

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *OptimizationPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Track reconciliation duration
	timer := observability.ReconciliationDuration.WithLabelValues(req.Name)
	startTime := metav1.Now()
	defer func() {
		duration := metav1.Now().Sub(startTime.Time).Seconds()
		timer.Observe(duration)
	}()

	// Fetch the OptimizationPolicy instance
	optimizationPolicy := &optipodv1alpha1.OptimizationPolicy{}
	if err := r.Get(ctx, req.NamespacedName, optimizationPolicy); err != nil {
		if apierrors.IsNotFound(err) {
			// Policy was deleted, nothing to do
			log.Info("OptimizationPolicy not found, likely deleted", "name", req.NamespacedName)
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get OptimizationPolicy", "name", req.NamespacedName)
		observability.ReconciliationErrors.WithLabelValues(req.Name, "fetch_error").Inc()
		return ctrl.Result{}, err
	}

	log.Info("Starting reconciliation", "policy", optimizationPolicy.Name, "namespace", optimizationPolicy.Namespace, "mode", optimizationPolicy.Spec.Mode)

	// Validate the policy
	if err := r.validatePolicy(ctx, optimizationPolicy); err != nil {
		log.Error(err, "Policy validation failed", "policy", optimizationPolicy.Name)

		// Update status to reflect validation error
		if statusErr := r.updatePolicyStatus(ctx, optimizationPolicy, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			Reason:             "ValidationFailed",
			Message:            fmt.Sprintf("Policy validation failed: %v", err),
			LastTransitionTime: metav1.Now(),
		}); statusErr != nil {
			log.Error(statusErr, "Failed to update policy status")
			return ctrl.Result{}, statusErr
		}

		// Emit event for validation error
		if r.EventRecorder != nil {
			r.EventRecorder.RecordPolicyValidationError(optimizationPolicy, optimizationPolicy.Name, err)
		} else if r.Recorder != nil {
			r.Recorder.Event(optimizationPolicy, corev1.EventTypeWarning, "ValidationFailed",
				fmt.Sprintf("Policy validation failed: %v", err))
		}
		observability.ReconciliationErrors.WithLabelValues(optimizationPolicy.Name, "validation_error").Inc()

		// Don't requeue on validation errors - user needs to fix the policy
		return ctrl.Result{}, nil
	}

	// Policy is valid, update status
	log.Info("Policy validation passed, updating status to Ready", "policy", optimizationPolicy.Name)
	if err := r.updatePolicyStatus(ctx, optimizationPolicy, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "PolicyValid",
		Message:            "Policy is active and processing workloads",
		LastTransitionTime: metav1.Now(),
	}); err != nil {
		log.Error(err, "Failed to update policy status to Ready")
		return ctrl.Result{}, err
	}
	log.Info("Successfully updated policy status to Ready", "policy", optimizationPolicy.Name)

	// Use workload-centric processing with policy weights
	processedCount, discoveredCount, err := r.processWorkloadsWithPolicySelection(ctx, optimizationPolicy)
	if err != nil {
		log.Error(err, "Failed to process workloads with policy selection")
		return ctrl.Result{}, err
	}

	// Update policy status with summary
	if err := r.updatePolicySummary(ctx, optimizationPolicy, discoveredCount, processedCount); err != nil {
		log.Error(err, "Failed to update policy summary")
		return ctrl.Result{}, err
	}

	// Calculate requeue interval with adaptive scheduling
	requeueAfter := r.calculateRequeueInterval(optimizationPolicy, discoveredCount, processedCount)

	log.Info("Successfully reconciled OptimizationPolicy", "policy", optimizationPolicy.Name, "requeueAfter", requeueAfter)
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// validatePolicy validates the OptimizationPolicy
func (r *OptimizationPolicyReconciler) validatePolicy(ctx context.Context, pol *optipodv1alpha1.OptimizationPolicy) error { //nolint:unparam // ctx may be used in future
	return pol.ValidateCreate()
}

// updatePolicyStatus updates the policy status with the given condition
// Uses retry logic to handle concurrent modification conflicts
func (r *OptimizationPolicyReconciler) updatePolicyStatus(ctx context.Context, pol *optipodv1alpha1.OptimizationPolicy, condition metav1.Condition) error {
	log := logf.FromContext(ctx)

	// Retry configuration for status updates
	const maxRetries = 3
	const baseDelay = 50 * time.Millisecond

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		// Fetch the latest version to avoid conflicts
		latest := &optipodv1alpha1.OptimizationPolicy{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(pol), latest); err != nil {
			if apierrors.IsNotFound(err) {
				// Policy was deleted
				log.Info("Policy was deleted during status update", "policy", pol.Name)
				return nil
			}
			return err
		}

		// Update or add the condition
		updated := false
		for i, existingCondition := range latest.Status.Conditions {
			if existingCondition.Type == condition.Type {
				// Only update if the condition has actually changed
				if existingCondition.Status != condition.Status ||
					existingCondition.Reason != condition.Reason ||
					existingCondition.Message != condition.Message {
					latest.Status.Conditions[i] = condition
					updated = true
				}
				break
			}
		}

		if !updated && len(latest.Status.Conditions) == 0 {
			// Add the condition if it doesn't exist and no conditions are present
			latest.Status.Conditions = append(latest.Status.Conditions, condition)
			updated = true
		} else if !updated {
			// Check if we need to add the condition
			found := false
			for _, existingCondition := range latest.Status.Conditions {
				if existingCondition.Type == condition.Type {
					found = true
					break
				}
			}
			if !found {
				latest.Status.Conditions = append(latest.Status.Conditions, condition)
				updated = true
			}
		}

		// If no update is needed, return success
		if !updated {
			return nil
		}

		// Attempt to update the status
		if err := r.Status().Update(ctx, latest); err != nil {
			if apierrors.IsConflict(err) {
				// Conflict - retry with exponential backoff
				lastErr = err
				delay := time.Duration(float64(baseDelay) * math.Pow(2, float64(attempt)))
				if delay > time.Second {
					delay = time.Second
				}
				log.V(1).Info("Conflict updating policy status, retrying",
					"policy", pol.Name,
					"attempt", attempt+1,
					"delay", delay)
				time.Sleep(delay)
				continue
			}
			// Non-retryable error
			return err
		}

		// Success
		return nil
	}

	// All retries exhausted
	return fmt.Errorf("failed to update policy status after %d attempts, last error: %w", maxRetries, lastErr)
}

// updatePolicySummary updates the policy status with summary information
// Uses retry logic to handle concurrent modification conflicts
func (r *OptimizationPolicyReconciler) updatePolicySummary(ctx context.Context, pol *optipodv1alpha1.OptimizationPolicy, discovered, processed int) error {
	log := logf.FromContext(ctx)

	// Retry configuration for summary updates
	const maxRetries = 3
	const baseDelay = 50 * time.Millisecond

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		// Fetch the latest version to avoid conflicts
		latest := &optipodv1alpha1.OptimizationPolicy{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(pol), latest); err != nil {
			if apierrors.IsNotFound(err) {
				// Policy was deleted
				log.Info("Policy was deleted during summary update", "policy", pol.Name)
				return nil
			}
			return err
		}

		// Check if update is needed
		now := metav1.Now()
		needsUpdate := latest.Status.WorkloadsDiscovered != discovered ||
			latest.Status.WorkloadsProcessed != processed ||
			latest.Status.LastReconciliation == nil ||
			now.Sub(latest.Status.LastReconciliation.Time) > time.Minute

		if !needsUpdate {
			// No update needed
			return nil
		}

		// Update summary
		latest.Status.WorkloadsDiscovered = discovered
		latest.Status.WorkloadsProcessed = processed
		latest.Status.LastReconciliation = &now

		// Attempt to update the status
		if err := r.Status().Update(ctx, latest); err != nil {
			if apierrors.IsConflict(err) {
				// Conflict - retry with exponential backoff
				lastErr = err
				delay := time.Duration(float64(baseDelay) * math.Pow(2, float64(attempt)))
				if delay > time.Second {
					delay = time.Second
				}
				log.V(1).Info("Conflict updating policy summary, retrying",
					"policy", pol.Name,
					"attempt", attempt+1,
					"delay", delay)
				time.Sleep(delay)
				continue
			}
			// Non-retryable error
			return err
		}

		// Success
		return nil
	}

	// All retries exhausted
	return fmt.Errorf("failed to update policy summary after %d attempts, last error: %w", maxRetries, lastErr)
}

// updateWorkloadTypeCounts updates the workload type counts in the policy status
// Uses retry logic to handle concurrent modification conflicts
func (r *OptimizationPolicyReconciler) updateWorkloadTypeCounts(ctx context.Context, pol *optipodv1alpha1.OptimizationPolicy, typeCounts map[optipodv1alpha1.WorkloadType]int) error {
	log := logf.FromContext(ctx)

	// Retry configuration for workload type count updates
	const maxRetries = 3
	const baseDelay = 50 * time.Millisecond

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		// Fetch the latest version to avoid conflicts
		latest := &optipodv1alpha1.OptimizationPolicy{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(pol), latest); err != nil {
			if apierrors.IsNotFound(err) {
				// Policy was deleted
				log.Info("Policy was deleted during workload type count update", "policy", pol.Name)
				return nil
			}
			return err
		}

		// Initialize workload type status if needed
		latest.InitializeWorkloadTypeStatus()

		// Check if update is needed
		needsUpdate := false
		for workloadType, count := range typeCounts {
			if latest.GetWorkloadTypeCount(workloadType) != count {
				needsUpdate = true
				break
			}
		}

		// Also check if we need to reset counts for types not in the current discovery
		allTypes := []optipodv1alpha1.WorkloadType{
			optipodv1alpha1.WorkloadTypeDeployment,
			optipodv1alpha1.WorkloadTypeStatefulSet,
			optipodv1alpha1.WorkloadTypeDaemonSet,
		}
		for _, workloadType := range allTypes {
			expectedCount := typeCounts[workloadType] // 0 if not in map
			if latest.GetWorkloadTypeCount(workloadType) != expectedCount {
				needsUpdate = true
				break
			}
		}

		if !needsUpdate {
			// No update needed
			return nil
		}

		// Update workload type counts
		for _, workloadType := range allTypes {
			count := typeCounts[workloadType] // 0 if not in map
			latest.UpdateWorkloadTypeCount(workloadType, count)
		}

		// Attempt to update the status
		if err := r.Status().Update(ctx, latest); err != nil {
			if apierrors.IsConflict(err) {
				// Conflict - retry with exponential backoff
				lastErr = err
				delay := time.Duration(float64(baseDelay) * math.Pow(2, float64(attempt)))
				if delay > time.Second {
					delay = time.Second
				}
				log.V(1).Info("Conflict updating workload type counts, retrying",
					"policy", pol.Name,
					"attempt", attempt+1,
					"delay", delay)
				time.Sleep(delay)
				continue
			}
			// Non-retryable error
			return err
		}

		// Success
		log.V(1).Info("Updated workload type counts",
			"policy", pol.Name,
			"deployments", typeCounts[optipodv1alpha1.WorkloadTypeDeployment],
			"statefulSets", typeCounts[optipodv1alpha1.WorkloadTypeStatefulSet],
			"daemonSets", typeCounts[optipodv1alpha1.WorkloadTypeDaemonSet])
		return nil
	}

	// All retries exhausted
	return fmt.Errorf("failed to update workload type counts after %d attempts, last error: %w", maxRetries, lastErr)
}

// calculateRequeueInterval calculates an adaptive requeue interval based on workload stability
func (r *OptimizationPolicyReconciler) calculateRequeueInterval(policyObj *optipodv1alpha1.OptimizationPolicy, discovered, processed int) time.Duration {
	// Base interval from policy
	baseInterval := policyObj.Spec.ReconciliationInterval.Duration
	if baseInterval == 0 {
		baseInterval = 5 * time.Minute // Default 5 minutes
	}

	// For Disabled mode, use longer intervals
	if policyObj.Spec.Mode == optipodv1alpha1.ModeDisabled {
		return baseInterval * 4 // 20 minutes for disabled policies
	}

	// For Recommend mode, use slightly longer intervals since no changes are applied
	if policyObj.Spec.Mode == optipodv1alpha1.ModeRecommend {
		return baseInterval * 2 // 10 minutes for recommend mode
	}

	// For Auto mode, use adaptive intervals based on activity
	if discovered == 0 {
		// No workloads found, check less frequently
		return baseInterval * 3 // 15 minutes when no workloads
	}

	if processed == 0 {
		// Workloads found but none processed (likely due to errors or skipping)
		return baseInterval * 2 // 10 minutes when workloads are skipped
	}

	// Active processing - use base interval but with some jitter to avoid thundering herd
	jitter := time.Duration(float64(baseInterval) * 0.1 * (0.5 + 0.5*rng.Float64()))
	return baseInterval + jitter
}

// processWorkloadsWithPolicySelection discovers all workloads and processes them with the best matching policy
func (r *OptimizationPolicyReconciler) processWorkloadsWithPolicySelection(ctx context.Context, triggeringPolicy *optipodv1alpha1.OptimizationPolicy) (int, int, error) {
	log := logf.FromContext(ctx)

	// Initialize policy selector if not already done
	if r.PolicySelector == nil {
		r.PolicySelector = policy.NewPolicySelector(r.Client)
	}

	// Discover all workloads that match this policy
	log.Info("Starting workload discovery", "policy", triggeringPolicy.Name)
	workloads, err := discovery.DiscoverWorkloads(ctx, r.Client, triggeringPolicy)
	if err != nil {
		log.Error(err, "Failed to discover workloads", "policy", triggeringPolicy.Name)
		r.Recorder.Event(triggeringPolicy, corev1.EventTypeWarning, "DiscoveryFailed",
			fmt.Sprintf("Failed to discover workloads: %v", err))
		observability.ReconciliationErrors.WithLabelValues(triggeringPolicy.Name, "discovery_error").Inc()
		return 0, 0, err
	}

	log.Info("Discovered workloads", "policy", triggeringPolicy.Name, "count", len(workloads))

	// Count workloads by type for status reporting
	workloadTypeCounts := make(map[optipodv1alpha1.WorkloadType]int)
	for _, workload := range workloads {
		workloadType := optipodv1alpha1.WorkloadType(workload.Kind)
		workloadTypeCounts[workloadType]++
	}

	// Update workload type counts in policy status
	if err := r.updateWorkloadTypeCounts(ctx, triggeringPolicy, workloadTypeCounts); err != nil {
		log.Error(err, "Failed to update workload type counts", "policy", triggeringPolicy.Name)
		// Don't fail the reconciliation for status update errors
	}

	// Track workloads monitored
	observability.WorkloadsMonitored.WithLabelValues(triggeringPolicy.Namespace, triggeringPolicy.Name).Set(float64(len(workloads)))

	if len(workloads) == 0 {
		return 0, 0, nil
	}

	// Process each workload with the best matching policy
	processedCount := 0
	for _, workload := range workloads {
		// Find the best policy for this workload
		bestPolicy, err := r.PolicySelector.SelectBestPolicy(ctx, &workload)
		if err != nil {
			log.Error(err, "Failed to select best policy for workload",
				"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name))
			continue
		}

		// Only process if this policy is the best match
		if bestPolicy.Name != triggeringPolicy.Name {
			log.V(1).Info("Workload handled by higher priority policy, skipping",
				"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
				"triggeringPolicy", triggeringPolicy.Name,
				"triggeringWeight", triggeringPolicy.GetWeight(),
				"bestPolicy", bestPolicy.Name,
				"bestWeight", bestPolicy.GetWeight())
			continue
		}

		// Process the workload with this policy
		if r.WorkloadProcessor != nil {
			log.Info("Processing workload with selected policy",
				"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
				"policy", bestPolicy.Name,
				"weight", bestPolicy.GetWeight())

			_, err := r.WorkloadProcessor.ProcessWorkload(ctx, &workload, bestPolicy)
			if err != nil {
				log.Error(err, "Failed to process workload",
					"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
					"policy", bestPolicy.Name)
				r.Recorder.Event(triggeringPolicy, corev1.EventTypeWarning, "ProcessingFailed",
					fmt.Sprintf("Failed to process workload %s/%s: %v", workload.Namespace, workload.Name, err))
				observability.ReconciliationErrors.WithLabelValues(triggeringPolicy.Name, "processing_error").Inc()
				continue
			}
			processedCount++
		}
	}

	log.Info("Completed workload processing with policy selection",
		"policy", triggeringPolicy.Name,
		"discovered", len(workloads),
		"processed", processedCount)

	return processedCount, len(workloads), nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *OptimizationPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&optipodv1alpha1.OptimizationPolicy{}).
		Named("optimizationpolicy").
		Complete(r)
}
