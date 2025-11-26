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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

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
	Apply(ctx context.Context, workload *application.Workload, containerName string, rec *recommendation.Recommendation, policy *optipodv1alpha1.OptimizationPolicy) error
}

// WorkloadProcessor handles the processing of individual workloads
type WorkloadProcessor struct {
	metricsProvider      metrics.MetricsProvider
	recommendationEngine *recommendation.Engine
	applicationEngine    ApplicationEngine
	metricsProviderType  string
}

// NewWorkloadProcessor creates a new workload processor
func NewWorkloadProcessor(
	metricsProvider metrics.MetricsProvider,
	recommendationEngine *recommendation.Engine,
	applicationEngine ApplicationEngine,
) *WorkloadProcessor {
	return &WorkloadProcessor{
		metricsProvider:      metricsProvider,
		recommendationEngine: recommendationEngine,
		applicationEngine:    applicationEngine,
		metricsProviderType:  "metrics-server", // Default, can be made configurable
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
	var recommendations []optipodv1alpha1.ContainerRecommendation
	hasMetricsError := false
	metricsErrorMsg := ""

	for _, container := range containers {
		// Collect metrics for this container
		// For simplicity, we'll query metrics for the first pod of the workload
		podName, err := wp.getFirstPodName(workload)
		if err != nil {
			hasMetricsError = true
			metricsErrorMsg = fmt.Sprintf("Failed to get pod name: %v", err)
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
		recommendations = append(recommendations, optipodv1alpha1.ContainerRecommendation{
			Container:   container.Name,
			CPU:         &rec.CPU,
			Memory:      &rec.Memory,
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

	// In Recommend mode, we only store recommendations
	if policy.Spec.Mode == optipodv1alpha1.ModeRecommend {
		status.Status = StatusRecommended
		status.Reason = "Recommendations computed, not applied (Recommend mode)"
		return status, nil
	}

	// In Auto mode, attempt to apply changes
	if policy.Spec.Mode == optipodv1alpha1.ModeAuto {
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
			err = wp.applicationEngine.Apply(ctx, appWorkload, rec.Container, appRec, policy)
			if err != nil {
				status.Status = StatusError
				status.Reason = fmt.Sprintf("Failed to apply changes to container %s: %v", rec.Container, err)
				return status, err
			}

			// Update last applied timestamp (only once after all containers)
			if i == len(recommendations)-1 {
				now := metav1.Now()
				status.LastApplied = &now
			}
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

// getFirstPodName gets the name of the first pod for a workload
// This is a simplified implementation - in production, you'd want to query actual pods
func (wp *WorkloadProcessor) getFirstPodName(workload *discovery.Workload) (string, error) {
	// For now, we'll construct a pod name based on the workload name
	// In a real implementation, we'd query the API for actual pods
	return fmt.Sprintf("%s-0", workload.Name), nil
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
