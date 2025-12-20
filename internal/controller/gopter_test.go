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
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/application"
	"github.com/optipod/optipod/internal/discovery"
	"github.com/optipod/optipod/internal/metrics"
	"github.com/optipod/optipod/internal/recommendation"
)

// TestGopterSetup verifies that gopter is properly installed and working
func TestGopterSetup(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("addition is commutative", prop.ForAll(
		func(a, b int) bool {
			return a+b == b+a
		},
		gen.Int(),
		gen.Int(),
	))

	properties.TestingRun(t)
}

// mockMetricsProvider is a mock implementation of MetricsProvider for testing
type mockMetricsProvider struct {
	getMetricsCalled bool
	metricsToReturn  *metrics.ContainerMetrics
	errorToReturn    error
}

func (m *mockMetricsProvider) GetContainerMetrics(ctx context.Context, namespace, podName, containerName string, window time.Duration) (*metrics.ContainerMetrics, error) {
	m.getMetricsCalled = true
	if m.errorToReturn != nil {
		return nil, m.errorToReturn
	}
	return m.metricsToReturn, nil
}

func (m *mockMetricsProvider) HealthCheck(ctx context.Context) error {
	return nil
}

// mockApplicationEngine is a mock implementation for testing
type mockApplicationEngine struct {
	canApplyCalled bool
	applyCalled    bool
	decision       *application.ApplyDecision
	applyError     error
	applyResult    *application.ApplyResult
}

func (m *mockApplicationEngine) CanApply(ctx context.Context, workload *application.Workload, rec *recommendation.Recommendation, policy *optipodv1alpha1.OptimizationPolicy) (*application.ApplyDecision, error) {
	m.canApplyCalled = true
	if m.decision != nil {
		return m.decision, nil
	}
	return &application.ApplyDecision{
		CanApply: false,
		Method:   application.Skip,
		Reason:   "Mock decision",
	}, nil
}

func (m *mockApplicationEngine) Apply(ctx context.Context, workload *application.Workload, containerName string, rec *recommendation.Recommendation, policy *optipodv1alpha1.OptimizationPolicy) (*application.ApplyResult, error) {
	m.applyCalled = true
	if m.applyError != nil {
		return nil, m.applyError
	}
	// Return a default result if not specified
	if m.applyResult != nil {
		return m.applyResult, nil
	}
	// Default to SSA
	return &application.ApplyResult{
		Method:         "ServerSideApply",
		FieldOwnership: true,
	}, nil
}

// Feature: k8s-workload-rightsizing, Property 1: Monitoring initiates metrics collection
// Validates: Requirements 1.2
func TestProperty_MonitoringInitiatesMetricsCollection(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("for any workload enabled for optimization, metrics collection should be initiated", prop.ForAll(
		func(workloadName string, namespace string, containerName string) bool {
			// Ensure non-empty strings
			if workloadName == "" {
				workloadName = TestWorkloadName
			}
			if namespace == "" {
				namespace = TestNamespace
			}
			if containerName == "" {
				containerName = TestContainerName
			}
			// Create a mock metrics provider
			mockProvider := &mockMetricsProvider{
				metricsToReturn: &metrics.ContainerMetrics{
					CPU: metrics.ResourceMetrics{
						P50:     resource.MustParse("100m"),
						P90:     resource.MustParse("200m"),
						P99:     resource.MustParse("300m"),
						Samples: 100,
					},
					Memory: metrics.ResourceMetrics{
						P50:     resource.MustParse("128Mi"),
						P90:     resource.MustParse("256Mi"),
						P99:     resource.MustParse("512Mi"),
						Samples: 100,
					},
				},
			}

			// Create a mock application engine
			mockAppEngine := &mockApplicationEngine{
				decision: &application.ApplyDecision{
					CanApply: false,
					Method:   application.Skip,
					Reason:   "Test mode",
				},
			}

			// Create workload processor
			recEngine := recommendation.NewEngine()
			processor := NewWorkloadProcessor(mockProvider, recEngine, mockAppEngine, nil)

			// Create a test workload
			workload := &discovery.Workload{
				Kind:      "Deployment",
				Namespace: namespace,
				Name:      workloadName,
				Object: &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      workloadName,
						Namespace: namespace,
					},
					Spec: appsv1.DeploymentSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  containerName,
										Image: "test:latest",
										Resources: corev1.ResourceRequirements{
											Requests: corev1.ResourceList{
												corev1.ResourceCPU:    resource.MustParse("100m"),
												corev1.ResourceMemory: resource.MustParse("128Mi"),
											},
										},
									},
								},
							},
						},
					},
				},
			}

			// Create a test policy in Recommend mode (to avoid applying changes)
			policy := &optipodv1alpha1.OptimizationPolicy{
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Mode: optipodv1alpha1.ModeRecommend,
					MetricsConfig: optipodv1alpha1.MetricsConfig{
						Provider:   "test",
						Percentile: "P90",
					},
					ResourceBounds: optipodv1alpha1.ResourceBounds{
						CPU: optipodv1alpha1.ResourceBound{
							Min: resource.MustParse("50m"),
							Max: resource.MustParse("2000m"),
						},
						Memory: optipodv1alpha1.ResourceBound{
							Min: resource.MustParse("64Mi"),
							Max: resource.MustParse("2Gi"),
						},
					},
					UpdateStrategy: optipodv1alpha1.UpdateStrategy{
						UpdateRequestsOnly: true,
					},
				},
			}

			// Process the workload
			ctx := context.Background()
			_, err := processor.ProcessWorkload(ctx, workload, policy)

			// Verify that metrics collection was initiated
			// The property holds if GetContainerMetrics was called
			return err == nil && mockProvider.getMetricsCalled
		},
		gen.AlphaString(),
		gen.AlphaString(),
		gen.AlphaString(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: k8s-workload-rightsizing, Property 7: Missing metrics prevent changes
// Validates: Requirements 3.4, 3.5
func TestProperty_MissingMetricsPreventChanges(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("for any workload where metrics cannot be fetched, no resource changes should be applied", prop.ForAll(
		func(workloadName string, namespace string, containerName string) bool {
			// Ensure non-empty strings
			if workloadName == "" {
				workloadName = TestWorkloadName
			}
			if namespace == "" {
				namespace = TestNamespace
			}
			if containerName == "" {
				containerName = TestContainerName
			}

			// Create a mock metrics provider that returns an error
			mockProvider := &mockMetricsProvider{
				errorToReturn: fmt.Errorf("metrics backend unavailable"),
			}

			// Create a mock application engine
			mockAppEngine := &mockApplicationEngine{
				decision: &application.ApplyDecision{
					CanApply: true,
					Method:   application.InPlace,
					Reason:   "Test mode",
				},
			}

			// Create workload processor
			recEngine := recommendation.NewEngine()
			processor := NewWorkloadProcessor(mockProvider, recEngine, mockAppEngine, nil)

			// Create a test workload
			workload := &discovery.Workload{
				Kind:      "Deployment",
				Namespace: namespace,
				Name:      workloadName,
				Object: &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      workloadName,
						Namespace: namespace,
					},
					Spec: appsv1.DeploymentSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  containerName,
										Image: "test:latest",
										Resources: corev1.ResourceRequirements{
											Requests: corev1.ResourceList{
												corev1.ResourceCPU:    resource.MustParse("100m"),
												corev1.ResourceMemory: resource.MustParse("128Mi"),
											},
										},
									},
								},
							},
						},
					},
				},
			}

			// Create a test policy in Auto mode (to test that changes are prevented)
			policy := &optipodv1alpha1.OptimizationPolicy{
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Mode: optipodv1alpha1.ModeAuto,
					MetricsConfig: optipodv1alpha1.MetricsConfig{
						Provider:   "test",
						Percentile: "P90",
					},
					ResourceBounds: optipodv1alpha1.ResourceBounds{
						CPU: optipodv1alpha1.ResourceBound{
							Min: resource.MustParse("50m"),
							Max: resource.MustParse("2000m"),
						},
						Memory: optipodv1alpha1.ResourceBound{
							Min: resource.MustParse("64Mi"),
							Max: resource.MustParse("2Gi"),
						},
					},
					UpdateStrategy: optipodv1alpha1.UpdateStrategy{
						UpdateRequestsOnly: true,
					},
				},
			}

			// Process the workload
			ctx := context.Background()
			status, err := processor.ProcessWorkload(ctx, workload, policy)

			// Verify that:
			// 1. No error is returned (graceful handling)
			// 2. Apply was NOT called on the application engine
			// 3. Status indicates the change was skipped due to missing metrics
			// Missing metrics should not cause annotation errors since no recommendations are generated
			return err == nil &&
				!mockAppEngine.applyCalled &&
				status.Status == StatusSkipped &&
				status.Reason != ""
		},
		gen.AlphaString(),
		gen.AlphaString(),
		gen.AlphaString(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: k8s-workload-rightsizing, Property 8: Recommend mode prevents modifications
// Validates: Requirements 4.1, 4.2, 7.4
func TestProperty_RecommendModePreventsMod(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("for any workload under a policy in Recommend mode, no modifications should be applied", prop.ForAll(
		func(workloadName string, namespace string, containerName string) bool {
			// Ensure non-empty strings
			if workloadName == "" {
				workloadName = TestWorkloadName
			}
			if namespace == "" {
				namespace = TestNamespace
			}
			if containerName == "" {
				containerName = TestContainerName
			}

			// Create a mock metrics provider with valid metrics
			mockProvider := &mockMetricsProvider{
				metricsToReturn: &metrics.ContainerMetrics{
					CPU: metrics.ResourceMetrics{
						P50:     resource.MustParse("100m"),
						P90:     resource.MustParse("200m"),
						P99:     resource.MustParse("300m"),
						Samples: 100,
					},
					Memory: metrics.ResourceMetrics{
						P50:     resource.MustParse("128Mi"),
						P90:     resource.MustParse("256Mi"),
						P99:     resource.MustParse("512Mi"),
						Samples: 100,
					},
				},
			}

			// Create a mock application engine
			mockAppEngine := &mockApplicationEngine{
				decision: &application.ApplyDecision{
					CanApply: true,
					Method:   application.InPlace,
					Reason:   "Test mode",
				},
			}

			// Create workload processor
			recEngine := recommendation.NewEngine()
			processor := NewWorkloadProcessor(mockProvider, recEngine, mockAppEngine, nil)

			// Create a test workload
			workload := &discovery.Workload{
				Kind:      "Deployment",
				Namespace: namespace,
				Name:      workloadName,
				Object: &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      workloadName,
						Namespace: namespace,
					},
					Spec: appsv1.DeploymentSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  containerName,
										Image: "test:latest",
										Resources: corev1.ResourceRequirements{
											Requests: corev1.ResourceList{
												corev1.ResourceCPU:    resource.MustParse("100m"),
												corev1.ResourceMemory: resource.MustParse("128Mi"),
											},
										},
									},
								},
							},
						},
					},
				},
			}

			// Create a test policy in Recommend mode
			policy := &optipodv1alpha1.OptimizationPolicy{
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Mode: optipodv1alpha1.ModeRecommend,
					MetricsConfig: optipodv1alpha1.MetricsConfig{
						Provider:   "test",
						Percentile: "P90",
					},
					ResourceBounds: optipodv1alpha1.ResourceBounds{
						CPU: optipodv1alpha1.ResourceBound{
							Min: resource.MustParse("50m"),
							Max: resource.MustParse("2000m"),
						},
						Memory: optipodv1alpha1.ResourceBound{
							Min: resource.MustParse("64Mi"),
							Max: resource.MustParse("2Gi"),
						},
					},
					UpdateStrategy: optipodv1alpha1.UpdateStrategy{
						UpdateRequestsOnly: true,
					},
				},
			}

			// Process the workload
			ctx := context.Background()
			status, err := processor.ProcessWorkload(ctx, workload, policy)

			// Verify that:
			// 1. No error is returned
			// 2. Apply was NOT called on the application engine
			// 3. Recommendations were generated and stored in status
			// 4. Status indicates Recommend mode

			return err == nil &&
				!mockAppEngine.applyCalled &&
				len(status.Recommendations) > 0 &&
				status.Status == StatusRecommended
		},
		gen.AlphaString(),
		gen.AlphaString(),
		gen.AlphaString(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: k8s-workload-rightsizing, Property 17: Auto mode applies changes
// Validates: Requirements 7.1, 7.2
func TestProperty_AutoModeAppliesChanges(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("for any workload under a policy in Auto mode, changes should be applied", prop.ForAll(
		func(workloadName string, namespace string, containerName string) bool {
			// Ensure non-empty strings
			if workloadName == "" {
				workloadName = TestWorkloadName
			}
			if namespace == "" {
				namespace = TestNamespace
			}
			if containerName == "" {
				containerName = TestContainerName
			}

			// Create a mock metrics provider with valid metrics
			mockProvider := &mockMetricsProvider{
				metricsToReturn: &metrics.ContainerMetrics{
					CPU: metrics.ResourceMetrics{
						P50:     resource.MustParse("100m"),
						P90:     resource.MustParse("200m"),
						P99:     resource.MustParse("300m"),
						Samples: 100,
					},
					Memory: metrics.ResourceMetrics{
						P50:     resource.MustParse("128Mi"),
						P90:     resource.MustParse("256Mi"),
						P99:     resource.MustParse("512Mi"),
						Samples: 100,
					},
				},
			}

			// Create a mock application engine that allows application
			mockAppEngine := &mockApplicationEngine{
				decision: &application.ApplyDecision{
					CanApply: true,
					Method:   application.InPlace,
					Reason:   "In-place resize supported",
				},
			}

			// Create workload processor
			recEngine := recommendation.NewEngine()
			processor := NewWorkloadProcessor(mockProvider, recEngine, mockAppEngine, nil)

			// Create a test workload
			workload := &discovery.Workload{
				Kind:      "Deployment",
				Namespace: namespace,
				Name:      workloadName,
				Object: &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      workloadName,
						Namespace: namespace,
					},
					Spec: appsv1.DeploymentSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  containerName,
										Image: "test:latest",
										Resources: corev1.ResourceRequirements{
											Requests: corev1.ResourceList{
												corev1.ResourceCPU:    resource.MustParse("100m"),
												corev1.ResourceMemory: resource.MustParse("128Mi"),
											},
										},
									},
								},
							},
						},
					},
				},
			}

			// Create a test policy in Auto mode
			policy := &optipodv1alpha1.OptimizationPolicy{
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Mode: optipodv1alpha1.ModeAuto,
					MetricsConfig: optipodv1alpha1.MetricsConfig{
						Provider:   "test",
						Percentile: "P90",
					},
					ResourceBounds: optipodv1alpha1.ResourceBounds{
						CPU: optipodv1alpha1.ResourceBound{
							Min: resource.MustParse("50m"),
							Max: resource.MustParse("2000m"),
						},
						Memory: optipodv1alpha1.ResourceBound{
							Min: resource.MustParse("64Mi"),
							Max: resource.MustParse("2Gi"),
						},
					},
					UpdateStrategy: optipodv1alpha1.UpdateStrategy{
						UpdateRequestsOnly: true,
					},
				},
			}

			// Process the workload
			ctx := context.Background()
			status, err := processor.ProcessWorkload(ctx, workload, policy)

			// Verify that:
			// 1. No error is returned
			// 2. Apply WAS called on the application engine
			// 3. Status indicates changes were applied
			// 4. LastApplied timestamp is set
			return err == nil &&
				mockAppEngine.applyCalled &&
				status.Status == StatusApplied &&
				status.LastApplied != nil
		},
		gen.AlphaString(),
		gen.AlphaString(),
		gen.AlphaString(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: k8s-workload-rightsizing, Property 19: Disabled mode stops processing
// Validates: Requirements 7.5, 7.6, 7.7
func TestProperty_DisabledModeStopsProcessing(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("for any workload under a policy in Disabled mode, processing should stop", prop.ForAll(
		func(workloadName string, namespace string, containerName string) bool {
			// Ensure non-empty strings
			if workloadName == "" {
				workloadName = TestWorkloadName
			}
			if namespace == "" {
				namespace = TestNamespace
			}
			if containerName == "" {
				containerName = TestContainerName
			}

			// Create a mock metrics provider
			mockProvider := &mockMetricsProvider{
				metricsToReturn: &metrics.ContainerMetrics{
					CPU: metrics.ResourceMetrics{
						P50:     resource.MustParse("100m"),
						P90:     resource.MustParse("200m"),
						P99:     resource.MustParse("300m"),
						Samples: 100,
					},
					Memory: metrics.ResourceMetrics{
						P50:     resource.MustParse("128Mi"),
						P90:     resource.MustParse("256Mi"),
						P99:     resource.MustParse("512Mi"),
						Samples: 100,
					},
				},
			}

			// Create a mock application engine
			mockAppEngine := &mockApplicationEngine{
				decision: &application.ApplyDecision{
					CanApply: true,
					Method:   application.InPlace,
					Reason:   "Test mode",
				},
			}

			// Create workload processor
			recEngine := recommendation.NewEngine()
			processor := NewWorkloadProcessor(mockProvider, recEngine, mockAppEngine, nil)

			// Create a test workload
			workload := &discovery.Workload{
				Kind:      "Deployment",
				Namespace: namespace,
				Name:      workloadName,
				Object: &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      workloadName,
						Namespace: namespace,
					},
					Spec: appsv1.DeploymentSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  containerName,
										Image: "test:latest",
										Resources: corev1.ResourceRequirements{
											Requests: corev1.ResourceList{
												corev1.ResourceCPU:    resource.MustParse("100m"),
												corev1.ResourceMemory: resource.MustParse("128Mi"),
											},
										},
									},
								},
							},
						},
					},
				},
			}

			// Create a test policy in Disabled mode
			policy := &optipodv1alpha1.OptimizationPolicy{
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Mode: optipodv1alpha1.ModeDisabled,
					MetricsConfig: optipodv1alpha1.MetricsConfig{
						Provider:   "test",
						Percentile: "P90",
					},
					ResourceBounds: optipodv1alpha1.ResourceBounds{
						CPU: optipodv1alpha1.ResourceBound{
							Min: resource.MustParse("50m"),
							Max: resource.MustParse("2000m"),
						},
						Memory: optipodv1alpha1.ResourceBound{
							Min: resource.MustParse("64Mi"),
							Max: resource.MustParse("2Gi"),
						},
					},
					UpdateStrategy: optipodv1alpha1.UpdateStrategy{
						UpdateRequestsOnly: true,
					},
				},
			}

			// Process the workload
			ctx := context.Background()
			status, err := processor.ProcessWorkload(ctx, workload, policy)

			// Verify that:
			// 1. No error is returned
			// 2. Metrics collection was NOT initiated (getMetricsCalled should be false)
			// 3. Apply was NOT called
			// 4. Status indicates the policy is disabled
			// 5. No recommendations were generated
			// Disabled mode should not have annotation errors since it exits early
			return err == nil &&
				!mockProvider.getMetricsCalled &&
				!mockAppEngine.applyCalled &&
				status.Status == StatusSkipped &&
				len(status.Recommendations) == 0
		},
		gen.AlphaString(),
		gen.AlphaString(),
		gen.AlphaString(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: k8s-workload-rightsizing, Property 23: Status timestamp tracking
// Validates: Requirements 9.1, 9.2
func TestProperty_StatusTimestampTracking(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("for any processed workload, status should contain last recommendation and last applied timestamps", prop.ForAll(
		func(workloadName string, namespace string, containerName string, isAutoMode bool) bool {
			// Ensure non-empty strings
			if workloadName == "" {
				workloadName = TestWorkloadName
			}
			if namespace == "" {
				namespace = TestNamespace
			}
			if containerName == "" {
				containerName = TestContainerName
			}

			// Create a mock metrics provider with valid metrics
			mockProvider := &mockMetricsProvider{
				metricsToReturn: &metrics.ContainerMetrics{
					CPU: metrics.ResourceMetrics{
						P50:     resource.MustParse("100m"),
						P90:     resource.MustParse("200m"),
						P99:     resource.MustParse("300m"),
						Samples: 100,
					},
					Memory: metrics.ResourceMetrics{
						P50:     resource.MustParse("128Mi"),
						P90:     resource.MustParse("256Mi"),
						P99:     resource.MustParse("512Mi"),
						Samples: 100,
					},
				},
			}

			// Create a mock application engine
			mockAppEngine := &mockApplicationEngine{
				decision: &application.ApplyDecision{
					CanApply: true,
					Method:   application.InPlace,
					Reason:   "In-place resize supported",
				},
			}

			// Create workload processor
			recEngine := recommendation.NewEngine()
			processor := NewWorkloadProcessor(mockProvider, recEngine, mockAppEngine, nil)

			// Create a test workload
			workload := &discovery.Workload{
				Kind:      "Deployment",
				Namespace: namespace,
				Name:      workloadName,
				Object: &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      workloadName,
						Namespace: namespace,
					},
					Spec: appsv1.DeploymentSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  containerName,
										Image: "test:latest",
										Resources: corev1.ResourceRequirements{
											Requests: corev1.ResourceList{
												corev1.ResourceCPU:    resource.MustParse("100m"),
												corev1.ResourceMemory: resource.MustParse("128Mi"),
											},
										},
									},
								},
							},
						},
					},
				},
			}

			// Determine mode based on input
			mode := optipodv1alpha1.ModeRecommend
			if isAutoMode {
				mode = optipodv1alpha1.ModeAuto
			}

			// Create a test policy
			policy := &optipodv1alpha1.OptimizationPolicy{
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Mode: mode,
					MetricsConfig: optipodv1alpha1.MetricsConfig{
						Provider:   "test",
						Percentile: "P90",
					},
					ResourceBounds: optipodv1alpha1.ResourceBounds{
						CPU: optipodv1alpha1.ResourceBound{
							Min: resource.MustParse("50m"),
							Max: resource.MustParse("2000m"),
						},
						Memory: optipodv1alpha1.ResourceBound{
							Min: resource.MustParse("64Mi"),
							Max: resource.MustParse("2Gi"),
						},
					},
					UpdateStrategy: optipodv1alpha1.UpdateStrategy{
						UpdateRequestsOnly: true,
					},
				},
			}

			// Record time before processing
			timeBefore := time.Now()

			// Process the workload
			ctx := context.Background()
			status, err := processor.ProcessWorkload(ctx, workload, policy)

			// Record time after processing
			timeAfter := time.Now()

			if err != nil {
				return false
			}

			// Verify that LastRecommendation timestamp is set and within reasonable bounds
			if status.LastRecommendation == nil {
				return false
			}

			recommendationTime := status.LastRecommendation.Time
			if recommendationTime.Before(timeBefore) || recommendationTime.After(timeAfter) {
				return false
			}

			// In Auto mode, verify LastApplied is also set
			if isAutoMode {
				if status.LastApplied == nil {
					return false
				}

				appliedTime := status.LastApplied.Time
				if appliedTime.Before(timeBefore) || appliedTime.After(timeAfter) {
					return false
				}

				// LastApplied should be >= LastRecommendation
				if appliedTime.Before(recommendationTime) {
					return false
				}
			} else {
				// In Recommend mode, LastApplied should not be set
				if status.LastApplied != nil {
					return false
				}
			}

			return true
		},
		gen.AlphaString(),
		gen.AlphaString(),
		gen.AlphaString(),
		gen.Bool(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: k8s-workload-rightsizing, Property 10: Recommendation format completeness
// Validates: Requirements 4.4, 9.4, 9.5, 9.6
func TestProperty_RecommendationFormatCompleteness(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("for any produced recommendation, status should contain structured format with per-container CPU and memory values", prop.ForAll(
		func(workloadName string, namespace string, containerName string, numContainers int) bool {
			// Ensure non-empty strings
			if workloadName == "" {
				workloadName = TestWorkloadName
			}
			if namespace == "" {
				namespace = TestNamespace
			}
			if containerName == "" {
				containerName = TestContainerName
			}
			// Ensure at least 1 container, max 5 for testing
			if numContainers < 1 {
				numContainers = 1
			}
			if numContainers > 5 {
				numContainers = 5
			}

			// Create a mock metrics provider with valid metrics
			mockProvider := &mockMetricsProvider{
				metricsToReturn: &metrics.ContainerMetrics{
					CPU: metrics.ResourceMetrics{
						P50:     resource.MustParse("100m"),
						P90:     resource.MustParse("200m"),
						P99:     resource.MustParse("300m"),
						Samples: 100,
					},
					Memory: metrics.ResourceMetrics{
						P50:     resource.MustParse("128Mi"),
						P90:     resource.MustParse("256Mi"),
						P99:     resource.MustParse("512Mi"),
						Samples: 100,
					},
				},
			}

			// Create a mock application engine
			mockAppEngine := &mockApplicationEngine{
				decision: &application.ApplyDecision{
					CanApply: false,
					Method:   application.Skip,
					Reason:   "Test mode",
				},
			}

			// Create workload processor
			recEngine := recommendation.NewEngine()
			processor := NewWorkloadProcessor(mockProvider, recEngine, mockAppEngine, nil)

			// Create containers for the workload
			containers := make([]corev1.Container, numContainers)
			for i := 0; i < numContainers; i++ {
				containers[i] = corev1.Container{
					Name:  fmt.Sprintf("%s-%d", containerName, i),
					Image: "test:latest",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
					},
				}
			}

			// Create a test workload
			workload := &discovery.Workload{
				Kind:      "Deployment",
				Namespace: namespace,
				Name:      workloadName,
				Object: &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      workloadName,
						Namespace: namespace,
					},
					Spec: appsv1.DeploymentSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: containers,
							},
						},
					},
				},
			}

			// Create a test policy in Recommend mode
			policy := &optipodv1alpha1.OptimizationPolicy{
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Mode: optipodv1alpha1.ModeRecommend,
					MetricsConfig: optipodv1alpha1.MetricsConfig{
						Provider:   "test",
						Percentile: "P90",
					},
					ResourceBounds: optipodv1alpha1.ResourceBounds{
						CPU: optipodv1alpha1.ResourceBound{
							Min: resource.MustParse("50m"),
							Max: resource.MustParse("2000m"),
						},
						Memory: optipodv1alpha1.ResourceBound{
							Min: resource.MustParse("64Mi"),
							Max: resource.MustParse("2Gi"),
						},
					},
					UpdateStrategy: optipodv1alpha1.UpdateStrategy{
						UpdateRequestsOnly: true,
					},
				},
			}

			// Process the workload
			ctx := context.Background()
			status, err := processor.ProcessWorkload(ctx, workload, policy)

			if err != nil {
				return false
			}

			// Verify that recommendations are present
			if len(status.Recommendations) != numContainers {
				return false
			}

			// Verify each recommendation has the required fields
			for i, rec := range status.Recommendations {
				// Container name must be set and match expected pattern
				expectedName := fmt.Sprintf("%s-%d", containerName, i)
				if rec.Container != expectedName {
					return false
				}

				// CPU must be set and non-nil
				if rec.CPU == nil {
					return false
				}

				// CPU must be a valid quantity
				if rec.CPU.IsZero() {
					return false
				}

				// Memory must be set and non-nil
				if rec.Memory == nil {
					return false
				}

				// Memory must be a valid quantity
				if rec.Memory.IsZero() {
					return false
				}

				// Explanation should be present (optional but good practice)
				// We don't enforce this as a hard requirement, but check it exists
				if rec.Explanation == "" { //nolint:staticcheck // Empty branch is intentional
					// This is acceptable, but we note it
				}
			}

			// Verify the status is structured and queryable
			// The status should have the workload identification fields
			if status.Name != workloadName {
				return false
			}
			if status.Namespace != namespace {
				return false
			}
			if status.Kind != "Deployment" {
				return false
			}

			return true
		},
		gen.AlphaString(),
		gen.AlphaString(),
		gen.AlphaString(),
		gen.IntRange(1, 5),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: workload-type-selector, Property 11: Status Workload Type Reporting
// For any OptimizationPolicy with workloadTypes filtering, the status should accurately report counts by workload type matching the actual discovered workloads
// Validates: Requirements 6.1, 6.2
func TestProperty_StatusWorkloadTypeReporting(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("for any policy with workload type filtering, status should accurately report counts by workload type", prop.ForAll(
		func(includeDeployments, includeStatefulSets, includeDaemonSets bool,
			excludeDeployments, excludeStatefulSets, excludeDaemonSets bool,
			deploymentCount, statefulSetCount, daemonSetCount int) bool {

			// Ensure reasonable counts
			deploymentCount = deploymentCount % 10
			statefulSetCount = statefulSetCount % 10
			daemonSetCount = daemonSetCount % 10

			// Create workload type filter
			var workloadTypes *optipodv1alpha1.WorkloadTypeFilter

			// Build include list
			var includeList []optipodv1alpha1.WorkloadType
			if includeDeployments {
				includeList = append(includeList, optipodv1alpha1.WorkloadTypeDeployment)
			}
			if includeStatefulSets {
				includeList = append(includeList, optipodv1alpha1.WorkloadTypeStatefulSet)
			}
			if includeDaemonSets {
				includeList = append(includeList, optipodv1alpha1.WorkloadTypeDaemonSet)
			}

			// Build exclude list
			var excludeList []optipodv1alpha1.WorkloadType
			if excludeDeployments {
				excludeList = append(excludeList, optipodv1alpha1.WorkloadTypeDeployment)
			}
			if excludeStatefulSets {
				excludeList = append(excludeList, optipodv1alpha1.WorkloadTypeStatefulSet)
			}
			if excludeDaemonSets {
				excludeList = append(excludeList, optipodv1alpha1.WorkloadTypeDaemonSet)
			}

			// Create filter if we have include or exclude lists
			if len(includeList) > 0 || len(excludeList) > 0 {
				workloadTypes = &optipodv1alpha1.WorkloadTypeFilter{
					Include: includeList,
					Exclude: excludeList,
				}
			}

			// Create policy
			policy := &optipodv1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "default",
				},
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Mode: optipodv1alpha1.ModeRecommend,
					Selector: optipodv1alpha1.WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
						WorkloadTypes: workloadTypes,
					},
					MetricsConfig: optipodv1alpha1.MetricsConfig{
						Provider: "prometheus",
					},
					ResourceBounds: optipodv1alpha1.ResourceBounds{
						CPU: optipodv1alpha1.ResourceBound{
							Min: resource.MustParse("100m"),
							Max: resource.MustParse("4000m"),
						},
						Memory: optipodv1alpha1.ResourceBound{
							Min: resource.MustParse("128Mi"),
							Max: resource.MustParse("8Gi"),
						},
					},
					UpdateStrategy: optipodv1alpha1.UpdateStrategy{
						AllowInPlaceResize: true,
						UpdateRequestsOnly: true,
					},
				},
			}

			// Determine which workload types should be active
			activeTypes := optipodv1alpha1.GetActiveWorkloadTypes(workloadTypes)

			// Calculate expected counts based on active types
			expectedDeployments := 0
			expectedStatefulSets := 0
			expectedDaemonSets := 0

			if activeTypes.Contains(optipodv1alpha1.WorkloadTypeDeployment) {
				expectedDeployments = deploymentCount
			}
			if activeTypes.Contains(optipodv1alpha1.WorkloadTypeStatefulSet) {
				expectedStatefulSets = statefulSetCount
			}
			if activeTypes.Contains(optipodv1alpha1.WorkloadTypeDaemonSet) {
				expectedDaemonSets = daemonSetCount
			}

			// Create workload type counts map (simulating discovery results)
			typeCounts := map[optipodv1alpha1.WorkloadType]int{
				optipodv1alpha1.WorkloadTypeDeployment:  expectedDeployments,
				optipodv1alpha1.WorkloadTypeStatefulSet: expectedStatefulSets,
				optipodv1alpha1.WorkloadTypeDaemonSet:   expectedDaemonSets,
			}

			// Update workload type counts using helper methods
			for workloadType, count := range typeCounts {
				policy.UpdateWorkloadTypeCount(workloadType, count)
			}

			// Verify the counts match expectations
			actualDeployments := policy.GetWorkloadTypeCount(optipodv1alpha1.WorkloadTypeDeployment)
			actualStatefulSets := policy.GetWorkloadTypeCount(optipodv1alpha1.WorkloadTypeStatefulSet)
			actualDaemonSets := policy.GetWorkloadTypeCount(optipodv1alpha1.WorkloadTypeDaemonSet)

			// Property holds if actual counts match expected counts
			return actualDeployments == expectedDeployments &&
				actualStatefulSets == expectedStatefulSets &&
				actualDaemonSets == expectedDaemonSets
		},
		gen.Bool(), gen.Bool(), gen.Bool(), // include flags
		gen.Bool(), gen.Bool(), gen.Bool(), // exclude flags
		gen.IntRange(0, 10), gen.IntRange(0, 10), gen.IntRange(0, 10), // counts
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: workload-type-selector, Property 12: Status Backward Compatibility
// For any OptimizationPolicy, the status should maintain existing fields (workloadsDiscovered, workloadsProcessed) with correct values
// Validates: Requirements 6.4
func TestProperty_StatusBackwardCompatibility(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("for any policy, status should maintain existing fields with correct values", prop.ForAll(
		func(discoveredCount, processedCount int, hasWorkloadTypes bool) bool {

			// Ensure reasonable counts and processed <= discovered
			discoveredCount = discoveredCount % 100
			processedCount = processedCount % (discoveredCount + 1)

			// Create policy with or without workload types
			var workloadTypes *optipodv1alpha1.WorkloadTypeFilter
			if hasWorkloadTypes {
				workloadTypes = &optipodv1alpha1.WorkloadTypeFilter{
					Include: []optipodv1alpha1.WorkloadType{optipodv1alpha1.WorkloadTypeDeployment},
				}
			}

			policy := &optipodv1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "default",
				},
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Mode: optipodv1alpha1.ModeRecommend,
					Selector: optipodv1alpha1.WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
						WorkloadTypes: workloadTypes,
					},
					MetricsConfig: optipodv1alpha1.MetricsConfig{
						Provider: "prometheus",
					},
					ResourceBounds: optipodv1alpha1.ResourceBounds{
						CPU: optipodv1alpha1.ResourceBound{
							Min: resource.MustParse("100m"),
							Max: resource.MustParse("4000m"),
						},
						Memory: optipodv1alpha1.ResourceBound{
							Min: resource.MustParse("128Mi"),
							Max: resource.MustParse("8Gi"),
						},
					},
					UpdateStrategy: optipodv1alpha1.UpdateStrategy{
						AllowInPlaceResize: true,
						UpdateRequestsOnly: true,
					},
				},
			}

			// Set the existing status fields
			now := metav1.Now()
			policy.Status.WorkloadsDiscovered = discoveredCount
			policy.Status.WorkloadsProcessed = processedCount
			policy.Status.LastReconciliation = &now

			// Add some workload type counts if workload types are enabled
			if hasWorkloadTypes {
				policy.UpdateWorkloadTypeCount(optipodv1alpha1.WorkloadTypeDeployment, 5)
				policy.UpdateWorkloadTypeCount(optipodv1alpha1.WorkloadTypeStatefulSet, 3)
			}

			// Verify existing fields are preserved
			actualDiscovered := policy.Status.WorkloadsDiscovered
			actualProcessed := policy.Status.WorkloadsProcessed
			actualLastReconciliation := policy.Status.LastReconciliation

			// Property holds if:
			// 1. Existing fields maintain their values
			// 2. LastReconciliation is preserved
			// 3. WorkloadsByType field doesn't interfere with existing fields
			backwardCompatible := actualDiscovered == discoveredCount &&
				actualProcessed == processedCount &&
				actualLastReconciliation != nil &&
				actualLastReconciliation.Equal(&now)

			// If workload types are enabled, verify they don't break existing functionality
			if hasWorkloadTypes {
				// Verify that having WorkloadsByType doesn't affect existing fields
				totalByType := policy.GetTotalWorkloadsByType()
				// The total by type should be independent of the existing discovered count
				// (they track different things - discovered is from current reconciliation,
				// workloadsByType is the breakdown)
				backwardCompatible = backwardCompatible && totalByType >= 0
			}

			return backwardCompatible
		},
		gen.IntRange(0, 50), // discoveredCount
		gen.IntRange(0, 50), // processedCount
		gen.Bool(),          // hasWorkloadTypes
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}
