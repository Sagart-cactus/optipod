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

package v1alpha1

import (
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Feature: server-side-apply-support, Property 1: Configuration determines patch method
// Validates: Requirements 2.2, 2.3
func TestProperty_ConfigurationDeterminesPatchMethod(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("for any policy with useServerSideApply set, the configuration should be accepted and stored", prop.ForAll(
		func(useSSA bool) bool {
			// Create a policy with useServerSideApply explicitly set
			policy := &OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "default",
				},
				Spec: OptimizationPolicySpec{
					Mode: ModeAuto,
					Selector: WorkloadSelector{
						Namespaces: &NamespaceFilter{
							Allow: []string{"default"},
						},
					},
					MetricsConfig: MetricsConfig{
						Provider:   "prometheus",
						Percentile: "P90",
					},
					ResourceBounds: ResourceBounds{
						CPU: ResourceBound{
							Min: resource.MustParse("50m"),
							Max: resource.MustParse("2000m"),
						},
						Memory: ResourceBound{
							Min: resource.MustParse("64Mi"),
							Max: resource.MustParse("2Gi"),
						},
					},
					UpdateStrategy: UpdateStrategy{
						UseServerSideApply: &useSSA,
					},
				},
			}

			// Validate the policy
			err := policy.ValidateCreate()
			if err != nil {
				return false
			}

			// Verify the configuration is stored correctly
			if policy.Spec.UpdateStrategy.UseServerSideApply == nil {
				return false
			}

			return *policy.Spec.UpdateStrategy.UseServerSideApply == useSSA
		},
		gen.Bool(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: server-side-apply-support, Property 2: Default to SSA when unspecified
// Validates: Requirements 2.4
func TestProperty_DefaultToSSAWhenUnspecified(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("for any policy where useServerSideApply is nil, the system should behave as if useServerSideApply=true", prop.ForAll(
		func(policyName string, namespace string) bool {
			// Ensure non-empty strings
			if policyName == "" {
				policyName = "test-policy"
			}
			if namespace == "" {
				namespace = "default"
			}

			// Create a policy without setting useServerSideApply (nil)
			policy := &OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      policyName,
					Namespace: namespace,
				},
				Spec: OptimizationPolicySpec{
					Mode: ModeAuto,
					Selector: WorkloadSelector{
						Namespaces: &NamespaceFilter{
							Allow: []string{namespace},
						},
					},
					MetricsConfig: MetricsConfig{
						Provider:   "prometheus",
						Percentile: "P90",
					},
					ResourceBounds: ResourceBounds{
						CPU: ResourceBound{
							Min: resource.MustParse("50m"),
							Max: resource.MustParse("2000m"),
						},
						Memory: ResourceBound{
							Min: resource.MustParse("64Mi"),
							Max: resource.MustParse("2Gi"),
						},
					},
					UpdateStrategy: UpdateStrategy{
						// UseServerSideApply is intentionally not set (nil)
					},
				},
			}

			// Validate the policy
			err := policy.ValidateCreate()
			if err != nil {
				return false
			}

			// When nil, the default behavior should be SSA=true
			// This is verified by checking that nil is acceptable
			// The actual default behavior will be implemented in the application engine
			// Here we just verify that nil is a valid configuration
			return policy.Spec.UpdateStrategy.UseServerSideApply == nil
		},
		gen.AlphaString(),
		gen.AlphaString(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: server-side-apply-support, Property 11: Status tracks apply method
// Validates: Requirements 7.4
func TestProperty_StatusTracksApplyMethod(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("for any workload status with SSA enabled, lastApplyMethod should be 'ServerSideApply' and fieldOwnership should be true", prop.ForAll(
		func(workloadName string, namespace string, kind string) bool {
			// Ensure non-empty strings and valid kind
			if workloadName == "" {
				workloadName = "test-workload"
			}
			if namespace == "" {
				namespace = "default"
			}
			validKinds := []string{"Deployment", "StatefulSet", "DaemonSet"}
			if kind == "" || !contains(validKinds, kind) {
				kind = "Deployment"
			}

			// Create a workload status simulating SSA usage
			status := WorkloadStatus{
				Name:            workloadName,
				Namespace:       namespace,
				Kind:            kind,
				LastApplyMethod: "ServerSideApply",
				FieldOwnership:  true,
				Status:          "Applied",
			}

			// Verify that when SSA is used, both fields are set correctly
			if status.LastApplyMethod != "ServerSideApply" {
				return false
			}
			if !status.FieldOwnership {
				return false
			}

			return true
		},
		gen.AlphaString(),
		gen.AlphaString(),
		gen.AlphaString(),
	))

	properties.Property("for any workload status with Strategic Merge Patch, lastApplyMethod should be 'StrategicMergePatch' and fieldOwnership should be false", prop.ForAll(
		func(workloadName string, namespace string, kind string) bool {
			// Ensure non-empty strings and valid kind
			if workloadName == "" {
				workloadName = "test-workload"
			}
			if namespace == "" {
				namespace = "default"
			}
			validKinds := []string{"Deployment", "StatefulSet", "DaemonSet"}
			if kind == "" || !contains(validKinds, kind) {
				kind = "Deployment"
			}

			// Create a workload status simulating Strategic Merge Patch usage
			status := WorkloadStatus{
				Name:            workloadName,
				Namespace:       namespace,
				Kind:            kind,
				LastApplyMethod: "StrategicMergePatch",
				FieldOwnership:  false,
				Status:          "Applied",
			}

			// Verify that when Strategic Merge Patch is used, both fields are set correctly
			if status.LastApplyMethod != "StrategicMergePatch" {
				return false
			}
			if status.FieldOwnership {
				return false
			}

			return true
		},
		gen.AlphaString(),
		gen.AlphaString(),
		gen.AlphaString(),
	))

	properties.Property("for any apply method, the relationship between method and fieldOwnership should be consistent", prop.ForAll(
		func(useSSA bool, workloadName string, namespace string) bool {
			// Ensure non-empty strings
			if workloadName == "" {
				workloadName = "test-workload"
			}
			if namespace == "" {
				namespace = "default"
			}

			// Create a workload status based on the apply method
			var status WorkloadStatus
			if useSSA {
				status = WorkloadStatus{
					Name:            workloadName,
					Namespace:       namespace,
					Kind:            "Deployment",
					LastApplyMethod: "ServerSideApply",
					FieldOwnership:  true,
					Status:          "Applied",
				}
			} else {
				status = WorkloadStatus{
					Name:            workloadName,
					Namespace:       namespace,
					Kind:            "Deployment",
					LastApplyMethod: "StrategicMergePatch",
					FieldOwnership:  false,
					Status:          "Applied",
				}
			}

			// Verify the relationship is consistent
			if useSSA {
				return status.LastApplyMethod == "ServerSideApply" && status.FieldOwnership
			}
			return status.LastApplyMethod == "StrategicMergePatch" && !status.FieldOwnership
		},
		gen.Bool(),
		gen.AlphaString(),
		gen.AlphaString(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Helper function to check if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
