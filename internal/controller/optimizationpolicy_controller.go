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
	"math/rand/v2"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	controlleroptions "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/discovery"
	"github.com/optipod/optipod/internal/observability"
	"github.com/optipod/optipod/internal/policy"
)

// OptimizationPolicyReconciler reconciles a OptimizationPolicy object
type OptimizationPolicyReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	Recorder          record.EventRecorder
	WorkloadProcessor *WorkloadProcessor
	EventRecorder     *observability.EventRecorder
	PolicySelector    *policy.PolicySelector
	PolicyWorkers     int
	AdmissionWorkers  int
	admissionSlots    chan struct{}
	acceptsPolicy     func(*optipodv1alpha1.OptimizationPolicy) bool
}

// +kubebuilder:rbac:groups=optipod.optipod.io,resources=optimizationpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=optipod.optipod.io,resources=optimizationpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=optipod.optipod.io,resources=optimizationpolicies/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=namespaces;nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=limitranges;resourcequotas,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods/resize,verbs=patch;update
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;list;watch
// +kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=keda.sh,resources=scaledobjects,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods/eviction,verbs=create
// +kubebuilder:rbac:groups=argoproj.io,resources=rollouts,verbs=get;list;watch;patch;update
// +kubebuilder:rbac:groups=batch,resources=jobs;cronjobs,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments;statefulsets;daemonsets;replicasets,verbs=get;list;watch;update;patch
// (replicasets read mirrors the shipped Helm chart: the pod mutator resolves a
// Deployment-owned pod to its parent workload through pod -> ReplicaSet ->
// Deployment to read parent annotations, for ArgoCD compatibility.)
// +kubebuilder:rbac:groups=metrics.k8s.io,resources=pods;nodes,verbs=get;list
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingadmissionwebhookconfigurations,verbs=get;list;watch;create;update;patch;delete

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

	if r.WorkloadProcessor != nil {
		if err := r.WorkloadProcessor.CleanupHorizontal(ctx, req.NamespacedName); err != nil {
			return ctrl.Result{}, err
		}
	}
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

	// A delayed item can outlive a switch between Auto and Recommend.
	if r.acceptsPolicy != nil && !r.acceptsPolicy(optimizationPolicy) {
		return ctrl.Result{}, nil
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
	requeueAfter = metricsRefreshInterval(optimizationPolicy, requeueAfter)
	if needsAdmissionRefresh(optimizationPolicy) {
		// Plans for early workloads were issued before the policy pass completed.
		requeueAfter = max(requeueAfter/4, requeueAfter-time.Since(startTime.Time))
	}

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
			optipodv1alpha1.WorkloadTypeJob, optipodv1alpha1.WorkloadTypeCronJob, optipodv1alpha1.WorkloadTypeReplicaSet, optipodv1alpha1.WorkloadTypeRollout,
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
	jitter := time.Duration(float64(baseInterval) * 0.1 * (0.5 + 0.5*rand.Float64()))
	return baseInterval + jitter
}

// processWorkloadsWithPolicySelection discovers all workloads and processes them with the best matching policy
func (r *OptimizationPolicyReconciler) processWorkloadsWithPolicySelection(ctx context.Context, triggeringPolicy *optipodv1alpha1.OptimizationPolicy) (int, int, error) {
	log := logf.FromContext(ctx)
	var retryErr error

	// Initialize policy selector if not already done
	selector := r.PolicySelector
	if selector == nil {
		selector = policy.NewPolicySelector(r.Client)
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

	// Share the configured admission worker budget across policies and their
	// members. Only deferred, request-only plans may run concurrently within
	// a policy. Immediate rollouts and horizontal updates remain sequential.
	type result struct {
		processed bool
		err       error
	}
	results := make([]result, len(workloads))
	var pending sync.WaitGroup
	for i := range workloads {
		if r.admissionSlots != nil {
			select {
			case r.admissionSlots <- struct{}{}:
			case <-ctx.Done():
				pending.Wait()
				return 0, len(workloads), ctx.Err()
			}
		}
		run := func() {
			if r.admissionSlots != nil {
				defer func() { <-r.admissionSlots }()
			}
			results[i].processed, results[i].err = r.processSelected(ctx, triggeringPolicy, selector, &workloads[i])
		}
		w := &workloads[i]
		effective := triggeringPolicy.ForWorkloadRef(string(w.Object.GetUID()), w.Namespace, w.Kind, w.Name)
		if r.admissionSlots != nil && effective.GetRolloutStrategy() == optipodv1alpha1.RolloutOnNextRestart && effective.Spec.UpdateStrategy.UpdateRequestsOnly && effective.Spec.HorizontalScaling == nil {
			pending.Go(run)
		} else {
			run()
		}
	}
	pending.Wait()
	processedCount := 0
	for _, out := range results {
		if out.processed {
			processedCount++
		}
		if out.err != nil {
			retryErr = out.err
		}
	}

	log.Info("Completed workload processing with policy selection",
		"policy", triggeringPolicy.Name,
		"discovered", len(workloads),
		"processed", processedCount)

	return processedCount, len(workloads), retryErr
}

func (r *OptimizationPolicyReconciler) processSelected(ctx context.Context, triggeringPolicy *optipodv1alpha1.OptimizationPolicy, selector *policy.PolicySelector, workload *discovery.Workload) (bool, error) {
	log := logf.FromContext(ctx)
	// Find the best policy for this workload
	bestPolicy, err := selector.SelectBestPolicy(ctx, workload)
	if err != nil {
		log.Error(err, "Failed to select best policy for workload",
			"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name))
		return false, nil
	}

	// Only process if this policy is the best match
	if bestPolicy.Name != triggeringPolicy.Name || bestPolicy.Namespace != triggeringPolicy.Namespace || bestPolicy.UID != triggeringPolicy.UID {
		log.V(1).Info("Workload handled by higher priority policy, skipping",
			"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
			"triggeringPolicy", triggeringPolicy.Name,
			"triggeringWeight", triggeringPolicy.GetWeight(),
			"bestPolicy", bestPolicy.Name,
			"bestWeight", bestPolicy.GetWeight())
		return false, nil
	}
	// A mode change may have moved the selected policy to the other worker.
	if r.acceptsPolicy != nil && (triggeringPolicy.Generation != bestPolicy.Generation || !r.acceptsPolicy(triggeringPolicy)) {
		return false, nil
	}
	// A winning Disabled policy is an authoritative exclusion. It must
	// participate in precedence, but never process or apply recommendations.
	if bestPolicy.Spec.Mode == optipodv1alpha1.ModeDisabled {
		if err := r.recordDisabledDecision(ctx, workload, bestPolicy); err != nil {
			return false, err
		}
		return false, nil
	}

	// Process the workload with this policy
	if r.WorkloadProcessor != nil {
		log.Info("Processing workload with selected policy",
			"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
			"policy", bestPolicy.Name,
			"weight", bestPolicy.GetWeight())

		status, err := r.WorkloadProcessor.ProcessWorkload(ctx, workload, bestPolicy)
		if err != nil {
			log.Error(err, "Failed to process workload",
				"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
				"policy", bestPolicy.Name)

			// Emit detailed event for processing failure
			if r.EventRecorder != nil {
				r.EventRecorder.RecordWorkloadUpdateFailure(triggeringPolicy, workload.Name, workload.Namespace, err)
			} else {
				r.Recorder.Event(triggeringPolicy, corev1.EventTypeWarning, "ProcessingFailed",
					fmt.Sprintf("Failed to process workload %s/%s: %v", workload.Namespace, workload.Name, err))
			}
			observability.ReconciliationErrors.WithLabelValues(triggeringPolicy.Name, "processing_error").Inc()
			if apierrors.IsConflict(err) || apierrors.IsTimeout(err) || apierrors.IsServerTimeout(err) || apierrors.IsTooManyRequests(err) || apierrors.IsServiceUnavailable(err) {
				return false, err
			}
			return false, nil
		}

		// Emit events based on processing outcome
		if status != nil {
			switch status.Status {
			case StatusApplied:
				if r.EventRecorder != nil {
					method := "Unknown"
					if status.LastApplyMethod != "" {
						method = status.LastApplyMethod
					}
					r.EventRecorder.RecordWorkloadUpdateSuccess(triggeringPolicy, workload.Name, workload.Namespace, method)
				} else {
					r.Recorder.Event(triggeringPolicy, corev1.EventTypeNormal, "OptimizationApplied",
						fmt.Sprintf("Successfully applied optimization to workload %s/%s", workload.Namespace, workload.Name))
				}
			case StatusRecommended:
				if r.EventRecorder != nil {
					containerCount := len(status.Recommendations)
					r.EventRecorder.RecordRecommendationGenerated(triggeringPolicy, workload.Name, workload.Namespace, containerCount)
				} else {
					r.Recorder.Event(triggeringPolicy, corev1.EventTypeNormal, "RecommendationGenerated",
						fmt.Sprintf("Generated recommendations for workload %s/%s", workload.Namespace, workload.Name))
				}
			case StatusSkipped:
				if r.EventRecorder != nil {
					r.EventRecorder.RecordWorkloadSkipped(triggeringPolicy, workload.Name, workload.Namespace, status.Reason)
				} else {
					r.Recorder.Event(triggeringPolicy, corev1.EventTypeNormal, "WorkloadSkipped",
						fmt.Sprintf("Skipped workload %s/%s: %s", workload.Namespace, workload.Name, status.Reason))
				}
			case StatusError:
				if r.EventRecorder != nil {
					r.EventRecorder.RecordWorkloadUpdateFailure(triggeringPolicy, workload.Name, workload.Namespace, fmt.Errorf("%s", status.Reason))
				} else {
					r.Recorder.Event(triggeringPolicy, corev1.EventTypeWarning, "ProcessingError",
						fmt.Sprintf("Error processing workload %s/%s: %s", workload.Namespace, workload.Name, status.Reason))
				}
			}
		}
		return true, nil
	}
	return false, nil
}

func (r *OptimizationPolicyReconciler) recordDisabledDecision(ctx context.Context, workload *discovery.Workload, p *optipodv1alpha1.OptimizationPolicy) error {
	if r.WorkloadProcessor == nil {
		return nil
	}
	status := &optipodv1alpha1.WorkloadStatus{Status: StatusSkipped, Reason: reasonDisabled}
	return r.WorkloadProcessor.recordDecision(ctx, workload, p, status, nil)
}

// SetupWithManager gives expiring admission plans a dedicated queue. Each
// queue is FIFO, so pod churn cannot starve initial policy discovery.
func (r *OptimizationPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	var inFlight sync.Map
	for _, admission := range []bool{false, true} {
		worker := *r
		worker.acceptsPolicy = func(p *optipodv1alpha1.OptimizationPolicy) bool { return needsAdmissionRefresh(p) == admission }
		name := "optimizationpolicy"
		workers := r.PolicyWorkers
		if admission {
			// Independent policies can spend most of a renewal waiting on metrics.
			// Bound concurrent queries while the workqueue serializes each key.
			workers = r.AdmissionWorkers
			name += "-admission"
		}
		if workers < 1 || workers > 32 {
			return fmt.Errorf("%s worker count must be between 1 and 32, got %d", name, workers)
		}
		if admission {
			worker.admissionSlots = make(chan struct{}, workers)
		}
		fifo := false
		err := ctrl.NewControllerManagedBy(mgr).
			WithOptions(controlleroptions.Options{UsePriorityQueue: &fifo, MaxConcurrentReconciles: workers}).
			For(&optipodv1alpha1.OptimizationPolicy{}, builder.WithPredicates(predicate.GenerationChangedPredicate{}, predicate.NewPredicateFuncs(func(obj client.Object) bool {
				return worker.acceptsPolicy(obj.(*optipodv1alpha1.OptimizationPolicy))
			}))).
			Watches(&corev1.Pod{}, handler.EnqueueRequestsFromMapFunc(worker.policyForPod), builder.WithPredicates(podLifecycleEvents)).
			Named(name).
			Complete(reconcile.Func(func(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
				// A mode change can enqueue the same policy on the other worker.
				if _, busy := inFlight.LoadOrStore(req, struct{}{}); busy {
					return ctrl.Result{RequeueAfter: time.Second}, nil
				}
				defer inFlight.Delete(req)
				return worker.Reconcile(ctx, req)
			}))
		if err != nil {
			return err
		}
	}
	return nil
}
