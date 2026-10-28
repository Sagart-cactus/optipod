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

package webhook

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
)

const (
	restartTriggeredAnnotation = "optipod.io/restart-triggered"
)

var rolloutLog = logf.Log.WithName("rollout-controller")

// RolloutController handles rolling restart operations for workloads
type RolloutController struct {
	client client.Client
}

// NewRolloutController creates a new rollout controller instance
func NewRolloutController(k8sClient client.Client) *RolloutController {
	return &RolloutController{
		client: k8sClient,
	}
}

// TriggerRollingRestart triggers a rolling restart for the specified workload based on policy configuration
func (rc *RolloutController) TriggerRollingRestart(ctx context.Context, workload client.Object, policy *optipodv1alpha1.OptimizationPolicy) error {
	log := rolloutLog.WithValues(
		"workload", fmt.Sprintf("%s/%s", workload.GetNamespace(), workload.GetName()),
		"kind", workload.GetObjectKind().GroupVersionKind().Kind,
		"policy", policy.Name,
	)

	log.V(1).Info("Processing rolling restart request")

	// Check if policy uses webhook strategy
	if !policy.IsWebhookStrategy() {
		log.V(1).Info("Policy does not use webhook strategy, skipping rolling restart")
		return nil
	}

	// Check rollout strategy
	rolloutStrategy := policy.GetRolloutStrategy()
	if rolloutStrategy != optipodv1alpha1.RolloutImmediate {
		log.V(1).Info("Rollout strategy is not immediate, skipping rolling restart", "strategy", rolloutStrategy)
		return nil
	}

	// Validate workload type supports rolling restart
	if !rc.SupportsRollingRestart(workload) {
		return fmt.Errorf("workload type %s does not support rolling restart", workload.GetObjectKind().GroupVersionKind().Kind)
	}

	// Perform rolling restart by updating pod template
	if err := rc.UpdatePodTemplate(ctx, workload); err != nil {
		log.Error(err, "Failed to trigger rolling restart")
		return fmt.Errorf("failed to trigger rolling restart: %w", err)
	}

	log.Info("Rolling restart triggered successfully")
	return nil
}

// SupportsRollingRestart checks if the workload type supports rolling restart
func (rc *RolloutController) SupportsRollingRestart(workload client.Object) bool {
	switch workload.(type) {
	case *appsv1.Deployment, *appsv1.StatefulSet, *appsv1.DaemonSet:
		return true
	default:
		return false
	}
}

// UpdatePodTemplate updates the workload's pod template to trigger a rolling restart
func (rc *RolloutController) UpdatePodTemplate(ctx context.Context, workload client.Object) error {
	log := rolloutLog.WithValues(
		"workload", fmt.Sprintf("%s/%s", workload.GetNamespace(), workload.GetName()),
		"kind", workload.GetObjectKind().GroupVersionKind().Kind,
	)

	log.V(1).Info("Updating pod template to trigger rolling restart")

	// Get the current workload to ensure we have the latest version
	current := workload.DeepCopyObject().(client.Object)
	if err := rc.client.Get(ctx, client.ObjectKeyFromObject(workload), current); err != nil {
		return fmt.Errorf("failed to get current workload: %w", err)
	}

	// Update pod template based on workload type
	switch obj := current.(type) {
	case *appsv1.Deployment:
		return rc.updateDeploymentPodTemplate(ctx, obj)
	case *appsv1.StatefulSet:
		return rc.updateStatefulSetPodTemplate(ctx, obj)
	case *appsv1.DaemonSet:
		return rc.updateDaemonSetPodTemplate(ctx, obj)
	default:
		return fmt.Errorf("unsupported workload type: %T", obj)
	}
}

// updateDeploymentPodTemplate updates a Deployment's pod template to trigger rolling restart
func (rc *RolloutController) updateDeploymentPodTemplate(ctx context.Context, deployment *appsv1.Deployment) error {
	log := rolloutLog.WithValues("deployment", fmt.Sprintf("%s/%s", deployment.Namespace, deployment.Name))

	// Add or update restart annotation to trigger rolling restart
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = make(map[string]string)
	}

	// Use current timestamp to ensure the annotation value changes
	deployment.Spec.Template.Annotations[restartTriggeredAnnotation] = time.Now().Format(time.RFC3339)

	// Update the deployment
	if err := rc.client.Update(ctx, deployment); err != nil {
		return fmt.Errorf("failed to update deployment: %w", err)
	}

	log.Info("Deployment pod template updated to trigger rolling restart")
	return nil
}

// updateStatefulSetPodTemplate updates a StatefulSet's pod template to trigger rolling restart
func (rc *RolloutController) updateStatefulSetPodTemplate(ctx context.Context, statefulSet *appsv1.StatefulSet) error {
	log := rolloutLog.WithValues("statefulset", fmt.Sprintf("%s/%s", statefulSet.Namespace, statefulSet.Name))

	// Add or update restart annotation to trigger rolling restart
	if statefulSet.Spec.Template.Annotations == nil {
		statefulSet.Spec.Template.Annotations = make(map[string]string)
	}

	// Use current timestamp to ensure the annotation value changes
	statefulSet.Spec.Template.Annotations[restartTriggeredAnnotation] = time.Now().Format(time.RFC3339)

	// Update the statefulset
	if err := rc.client.Update(ctx, statefulSet); err != nil {
		return fmt.Errorf("failed to update statefulset: %w", err)
	}

	log.Info("StatefulSet pod template updated to trigger rolling restart")
	return nil
}

// updateDaemonSetPodTemplate updates a DaemonSet's pod template to trigger rolling restart
func (rc *RolloutController) updateDaemonSetPodTemplate(ctx context.Context, daemonSet *appsv1.DaemonSet) error {
	log := rolloutLog.WithValues("daemonset", fmt.Sprintf("%s/%s", daemonSet.Namespace, daemonSet.Name))

	// Add or update restart annotation to trigger rolling restart
	if daemonSet.Spec.Template.Annotations == nil {
		daemonSet.Spec.Template.Annotations = make(map[string]string)
	}

	// Use current timestamp to ensure the annotation value changes
	daemonSet.Spec.Template.Annotations[restartTriggeredAnnotation] = time.Now().Format(time.RFC3339)

	// Update the daemonset
	if err := rc.client.Update(ctx, daemonSet); err != nil {
		return fmt.Errorf("failed to update daemonset: %w", err)
	}

	log.Info("DaemonSet pod template updated to trigger rolling restart")
	return nil
}

// GetSupportedWorkloadTypes returns a list of workload types that support rolling restart
func (rc *RolloutController) GetSupportedWorkloadTypes() []optipodv1alpha1.WorkloadType {
	return []optipodv1alpha1.WorkloadType{
		optipodv1alpha1.WorkloadTypeDeployment,
		optipodv1alpha1.WorkloadTypeStatefulSet,
		optipodv1alpha1.WorkloadTypeDaemonSet,
	}
}

// ValidateWorkloadType validates that a workload type supports rolling restart
func (rc *RolloutController) ValidateWorkloadType(workloadType optipodv1alpha1.WorkloadType) error {
	supportedTypes := rc.GetSupportedWorkloadTypes()

	for _, supportedType := range supportedTypes {
		if workloadType == supportedType {
			return nil
		}
	}

	return fmt.Errorf("workload type %s does not support rolling restart, supported types: %v", workloadType, supportedTypes)
}

// HandleRolloutStrategyConfiguration processes rollout strategy configuration for a policy
func (rc *RolloutController) HandleRolloutStrategyConfiguration(ctx context.Context, policy *optipodv1alpha1.OptimizationPolicy, workloads []client.Object) error {
	log := rolloutLog.WithValues("policy", policy.Name, "workloads", len(workloads))

	// Only process webhook strategy policies
	if !policy.IsWebhookStrategy() {
		log.V(1).Info("Policy does not use webhook strategy, skipping rollout strategy configuration")
		return nil
	}

	rolloutStrategy := policy.GetRolloutStrategy()
	log.V(1).Info("Processing rollout strategy configuration", "strategy", rolloutStrategy)

	switch rolloutStrategy {
	case optipodv1alpha1.RolloutImmediate:
		// Trigger rolling restart for all workloads
		return rc.processImmediateRollout(ctx, policy, workloads)
	case optipodv1alpha1.RolloutOnNextRestart:
		// Only apply annotations, no restart needed
		return rc.processOnNextRestartRollout(policy)
	default:
		return fmt.Errorf("unknown rollout strategy: %s", rolloutStrategy)
	}
}

// processImmediateRollout handles immediate rollout strategy
func (rc *RolloutController) processImmediateRollout(ctx context.Context, policy *optipodv1alpha1.OptimizationPolicy, workloads []client.Object) error {
	log := rolloutLog.WithValues("policy", policy.Name, "strategy", "immediate")

	var errors []error
	successCount := 0

	for _, workload := range workloads {
		if err := rc.TriggerRollingRestart(ctx, workload, policy); err != nil {
			log.Error(err, "Failed to trigger rolling restart for workload",
				"workload", fmt.Sprintf("%s/%s", workload.GetNamespace(), workload.GetName()))
			errors = append(errors, err)
		} else {
			successCount++
		}
	}

	log.Info("Immediate rollout processing completed",
		"successful", successCount,
		"failed", len(errors),
		"total", len(workloads))

	if len(errors) > 0 {
		return fmt.Errorf("failed to trigger rolling restart for %d out of %d workloads", len(errors), len(workloads))
	}

	return nil
}

// processOnNextRestartRollout handles onNextRestart rollout strategy
func (rc *RolloutController) processOnNextRestartRollout(policy *optipodv1alpha1.OptimizationPolicy) error {
	log := rolloutLog.WithValues("policy", policy.Name, "strategy", "onNextRestart")

	// For onNextRestart strategy, we only need to ensure annotations are applied
	// The actual resource changes will take effect when pods are naturally restarted
	log.V(1).Info("OnNextRestart strategy - annotations will be applied, no immediate restart triggered")

	// Validate that all workloads have the necessary annotations
	// This is handled by the annotation manager, so we just log the strategy
	log.Info("OnNextRestart rollout processing completed - resource changes will take effect on next pod restart")

	return nil
}

// GetWorkloadKind returns the Kubernetes kind for a workload object
func (rc *RolloutController) GetWorkloadKind(workload client.Object) string {
	switch workload.(type) {
	case *appsv1.Deployment:
		return "Deployment"
	case *appsv1.StatefulSet:
		return "StatefulSet"
	case *appsv1.DaemonSet:
		return "DaemonSet"
	default:
		return workload.GetObjectKind().GroupVersionKind().Kind
	}
}

// IsWorkloadReady checks if a workload is ready for rolling restart
func (rc *RolloutController) IsWorkloadReady(workload client.Object) bool {
	switch obj := workload.(type) {
	case *appsv1.Deployment:
		return rc.isDeploymentReady(obj)
	case *appsv1.StatefulSet:
		return rc.isStatefulSetReady(obj)
	case *appsv1.DaemonSet:
		return rc.isDaemonSetReady(obj)
	default:
		// For unknown types, assume ready
		return true
	}
}

// isDeploymentReady checks if a Deployment is ready
func (rc *RolloutController) isDeploymentReady(deployment *appsv1.Deployment) bool {
	// Check if deployment has desired replicas ready
	if deployment.Status.ReadyReplicas == 0 {
		return false
	}

	// Check if deployment is not in the middle of a rollout
	if deployment.Status.UpdatedReplicas != deployment.Status.Replicas {
		return false
	}

	return true
}

// isStatefulSetReady checks if a StatefulSet is ready
func (rc *RolloutController) isStatefulSetReady(statefulSet *appsv1.StatefulSet) bool {
	// Check if statefulset has desired replicas ready
	if statefulSet.Status.ReadyReplicas == 0 {
		return false
	}

	// Check if statefulset is not in the middle of a rollout
	if statefulSet.Status.UpdatedReplicas != statefulSet.Status.Replicas {
		return false
	}

	return true
}

// isDaemonSetReady checks if a DaemonSet is ready
func (rc *RolloutController) isDaemonSetReady(daemonSet *appsv1.DaemonSet) bool {
	// Check if daemonset has desired number ready
	if daemonSet.Status.NumberReady == 0 {
		return false
	}

	// Check if daemonset is not in the middle of a rollout
	if daemonSet.Status.UpdatedNumberScheduled != daemonSet.Status.DesiredNumberScheduled {
		return false
	}

	return true
}

// SafeTriggerRollingRestart safely triggers a rolling restart with error handling
func (rc *RolloutController) SafeTriggerRollingRestart(ctx context.Context, workload client.Object, policy *optipodv1alpha1.OptimizationPolicy) error {
	log := rolloutLog.WithValues(
		"workload", fmt.Sprintf("%s/%s", workload.GetNamespace(), workload.GetName()),
		"policy", policy.Name,
	)

	// Check if workload is ready for restart
	if !rc.IsWorkloadReady(workload) {
		log.Info("Workload is not ready for rolling restart, skipping")
		return nil
	}

	// Perform the rolling restart with error recovery
	if err := rc.TriggerRollingRestart(ctx, workload, policy); err != nil {
		log.Error(err, "Rolling restart failed, will retry on next reconciliation")
		return err
	}

	return nil
}

// GetRestartAnnotationKey returns the annotation key used for triggering restarts
func (rc *RolloutController) GetRestartAnnotationKey() string {
	return restartTriggeredAnnotation
}

// HasPendingRestart checks if a workload has a pending restart annotation
func (rc *RolloutController) HasPendingRestart(workload client.Object) bool {
	annotations := workload.GetAnnotations()
	if annotations == nil {
		return false
	}

	_, exists := annotations[rc.GetRestartAnnotationKey()]
	return exists
}

// ClearRestartAnnotation removes the restart annotation from a workload
func (rc *RolloutController) ClearRestartAnnotation(ctx context.Context, workload client.Object) error {
	annotations := workload.GetAnnotations()
	if annotations == nil {
		return nil
	}

	restartKey := rc.GetRestartAnnotationKey()
	if _, exists := annotations[restartKey]; !exists {
		return nil
	}

	// Remove the restart annotation
	delete(annotations, restartKey)
	workload.SetAnnotations(annotations)

	// Update the workload
	if err := rc.client.Update(ctx, workload); err != nil {
		return fmt.Errorf("failed to clear restart annotation: %w", err)
	}

	return nil
}

// ValidateRolloutConfiguration validates rollout configuration for a policy
func (rc *RolloutController) ValidateRolloutConfiguration(policy *optipodv1alpha1.OptimizationPolicy) error {
	// Only validate webhook strategy policies
	if !policy.IsWebhookStrategy() {
		return nil
	}

	rolloutStrategy := policy.GetRolloutStrategy()

	// Validate rollout strategy value
	switch rolloutStrategy {
	case optipodv1alpha1.RolloutImmediate, optipodv1alpha1.RolloutOnNextRestart:
		// Valid strategies
		return nil
	default:
		return fmt.Errorf("invalid rollout strategy: %s, must be one of: immediate, onNextRestart", rolloutStrategy)
	}
}

// GetRolloutStatus returns the rollout status for a workload
func (rc *RolloutController) GetRolloutStatus(workload client.Object) string {
	if !rc.SupportsRollingRestart(workload) {
		return "Unsupported"
	}

	if !rc.IsWorkloadReady(workload) {
		return "NotReady"
	}

	if rc.HasPendingRestart(workload) {
		return "PendingRestart"
	}

	return "Ready"
}
