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
)

// OptimizationPolicyReconciler reconciles a OptimizationPolicy object
type OptimizationPolicyReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	Recorder          record.EventRecorder
	WorkloadProcessor *WorkloadProcessor
	EventRecorder     *observability.EventRecorder
}

// +kubebuilder:rbac:groups=optipod.optipod.io,resources=optimizationpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=optipod.optipod.io,resources=optimizationpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=optipod.optipod.io,resources=optimizationpolicies/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

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
	policy := &optipodv1alpha1.OptimizationPolicy{}
	if err := r.Get(ctx, req.NamespacedName, policy); err != nil {
		if apierrors.IsNotFound(err) {
			// Policy was deleted, nothing to do
			log.Info("OptimizationPolicy not found, likely deleted", "name", req.NamespacedName)
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get OptimizationPolicy", "name", req.NamespacedName)
		observability.ReconciliationErrors.WithLabelValues(req.Name, "fetch_error").Inc()
		return ctrl.Result{}, err
	}

	// Validate the policy
	if err := r.validatePolicy(ctx, policy); err != nil {
		log.Error(err, "Policy validation failed", "policy", policy.Name)

		// Update status to reflect validation error
		if statusErr := r.updatePolicyStatus(ctx, policy, metav1.Condition{
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
			r.EventRecorder.RecordPolicyValidationError(policy, policy.Name, err)
		} else if r.Recorder != nil {
			r.Recorder.Event(policy, corev1.EventTypeWarning, "ValidationFailed",
				fmt.Sprintf("Policy validation failed: %v", err))
		}
		observability.ReconciliationErrors.WithLabelValues(policy.Name, "validation_error").Inc()

		// Don't requeue on validation errors - user needs to fix the policy
		return ctrl.Result{}, nil
	}

	// Policy is valid, update status
	if err := r.updatePolicyStatus(ctx, policy, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "PolicyValid",
		Message:            "Policy is active and processing workloads",
		LastTransitionTime: metav1.Now(),
	}); err != nil {
		log.Error(err, "Failed to update policy status")
		return ctrl.Result{}, err
	}

	// Discover workloads matching the policy
	workloads, err := discovery.DiscoverWorkloads(ctx, r.Client, policy)
	if err != nil {
		log.Error(err, "Failed to discover workloads", "policy", policy.Name)
		r.Recorder.Event(policy, corev1.EventTypeWarning, "DiscoveryFailed",
			fmt.Sprintf("Failed to discover workloads: %v", err))
		observability.ReconciliationErrors.WithLabelValues(policy.Name, "discovery_error").Inc()
		return ctrl.Result{}, err
	}

	log.Info("Discovered workloads", "policy", policy.Name, "count", len(workloads))

	// Track workloads monitored
	observability.WorkloadsMonitored.WithLabelValues(policy.Namespace, policy.Name).Set(float64(len(workloads)))

	// Process each workload
	var workloadStatuses []optipodv1alpha1.WorkloadStatus //nolint:prealloc // Size unknown
	updatedCount := 0
	skippedCount := make(map[string]int)

	for _, workload := range workloads {
		if r.WorkloadProcessor == nil {
			log.Info("WorkloadProcessor not configured, skipping workload processing")
			break
		}

		status, err := r.WorkloadProcessor.ProcessWorkload(ctx, &workload, policy)
		if err != nil {
			log.Error(err, "Failed to process workload",
				"workload", workload.Name,
				"namespace", workload.Namespace,
				"kind", workload.Kind)
			// Continue processing other workloads even if one fails
			r.Recorder.Event(policy, corev1.EventTypeWarning, "ProcessingFailed",
				fmt.Sprintf("Failed to process workload %s/%s: %v", workload.Namespace, workload.Name, err))
			observability.ReconciliationErrors.WithLabelValues(policy.Name, "processing_error").Inc()
			continue
		}

		workloadStatuses = append(workloadStatuses, *status)

		// Emit events and track metrics based on status
		if status.Status == StatusApplied { //nolint:staticcheck // Simple if is clearer than switch
			if r.EventRecorder != nil {
				r.EventRecorder.RecordWorkloadUpdateSuccess(policy, workload.Name, workload.Namespace, "InPlace")
			}
			updatedCount++
			observability.ApplicationsTotal.WithLabelValues(policy.Name, "InPlace").Inc()
		} else if status.Status == StatusSkipped {
			log.Info("Skipped workload", "workload", workload.Name, "reason", status.Reason)
			if r.EventRecorder != nil {
				r.EventRecorder.RecordWorkloadSkipped(policy, workload.Name, workload.Namespace, status.Reason)
			}
			skippedCount[status.Reason]++
		} else if status.Status == "Recommended" {
			if r.EventRecorder != nil {
				r.EventRecorder.RecordRecommendationGenerated(policy, workload.Name, workload.Namespace, len(status.Recommendations))
			}
			observability.RecommendationsTotal.WithLabelValues(policy.Name).Inc()
		}
	}

	// Update metrics for workloads updated and skipped
	observability.WorkloadsUpdated.WithLabelValues(policy.Namespace, policy.Name).Set(float64(updatedCount))
	for reason, count := range skippedCount {
		observability.WorkloadsSkipped.WithLabelValues(policy.Namespace, policy.Name, reason).Set(float64(count))
	}

	// Update policy status with workload statuses
	if err := r.updateWorkloadStatuses(ctx, policy, workloadStatuses); err != nil {
		log.Error(err, "Failed to update workload statuses")
		return ctrl.Result{}, err
	}

	// Requeue after reconciliation interval
	requeueAfter := policy.Spec.ReconciliationInterval.Duration
	if requeueAfter == 0 {
		requeueAfter = 5 * 60 * 1000000000 // 5 minutes in nanoseconds
	}

	log.Info("Successfully reconciled OptimizationPolicy", "policy", policy.Name, "requeueAfter", requeueAfter)
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// validatePolicy validates the OptimizationPolicy
func (r *OptimizationPolicyReconciler) validatePolicy(ctx context.Context, policy *optipodv1alpha1.OptimizationPolicy) error { //nolint:unparam // ctx may be used in future
	return policy.ValidateCreate()
}

// updatePolicyStatus updates the policy status with the given condition
func (r *OptimizationPolicyReconciler) updatePolicyStatus(ctx context.Context, policy *optipodv1alpha1.OptimizationPolicy, condition metav1.Condition) error {
	// Fetch the latest version to avoid conflicts
	latest := &optipodv1alpha1.OptimizationPolicy{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(policy), latest); err != nil {
		return err
	}

	// Update or add the condition
	updated := false
	for i, existingCondition := range latest.Status.Conditions {
		if existingCondition.Type == condition.Type {
			latest.Status.Conditions[i] = condition
			updated = true
			break
		}
	}

	if !updated {
		latest.Status.Conditions = append(latest.Status.Conditions, condition)
	}

	// Update the status
	return r.Status().Update(ctx, latest)
}

// updateWorkloadStatuses updates the policy status with workload statuses
func (r *OptimizationPolicyReconciler) updateWorkloadStatuses(ctx context.Context, policy *optipodv1alpha1.OptimizationPolicy, workloadStatuses []optipodv1alpha1.WorkloadStatus) error {
	// Fetch the latest version to avoid conflicts
	latest := &optipodv1alpha1.OptimizationPolicy{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(policy), latest); err != nil {
		return err
	}

	// Update workload statuses
	latest.Status.Workloads = workloadStatuses

	// Update the status
	return r.Status().Update(ctx, latest)
}

// SetupWithManager sets up the controller with the Manager.
func (r *OptimizationPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&optipodv1alpha1.OptimizationPolicy{}).
		Named("optimizationpolicy").
		Complete(r)
}
