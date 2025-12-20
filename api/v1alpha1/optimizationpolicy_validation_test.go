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

func TestOptimizationPolicy_ValidateCreate(t *testing.T) {
	tests := []struct {
		name    string
		policy  *OptimizationPolicy
		wantErr bool
	}{
		{
			name: "valid policy",
			policy: &OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "default",
				},
				Spec: OptimizationPolicySpec{
					Mode: ModeAuto,
					Selector: WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
					},
					MetricsConfig: MetricsConfig{
						Provider:   "prometheus",
						Percentile: "P90",
					},
					ResourceBounds: ResourceBounds{
						CPU: ResourceBound{
							Min: resource.MustParse("100m"),
							Max: resource.MustParse("4000m"),
						},
						Memory: ResourceBound{
							Min: resource.MustParse("128Mi"),
							Max: resource.MustParse("8Gi"),
						},
					},
					UpdateStrategy: UpdateStrategy{
						AllowInPlaceResize: true,
						UpdateRequestsOnly: true,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid CPU bounds - min > max",
			policy: &OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "default",
				},
				Spec: OptimizationPolicySpec{
					Mode: ModeAuto,
					Selector: WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
					},
					MetricsConfig: MetricsConfig{
						Provider:   "prometheus",
						Percentile: "P90",
					},
					ResourceBounds: ResourceBounds{
						CPU: ResourceBound{
							Min: resource.MustParse("4000m"),
							Max: resource.MustParse("100m"),
						},
						Memory: ResourceBound{
							Min: resource.MustParse("128Mi"),
							Max: resource.MustParse("8Gi"),
						},
					},
					UpdateStrategy: UpdateStrategy{
						AllowInPlaceResize: true,
						UpdateRequestsOnly: true,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid memory bounds - min > max",
			policy: &OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "default",
				},
				Spec: OptimizationPolicySpec{
					Mode: ModeAuto,
					Selector: WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
					},
					MetricsConfig: MetricsConfig{
						Provider:   "prometheus",
						Percentile: "P90",
					},
					ResourceBounds: ResourceBounds{
						CPU: ResourceBound{
							Min: resource.MustParse("100m"),
							Max: resource.MustParse("4000m"),
						},
						Memory: ResourceBound{
							Min: resource.MustParse("8Gi"),
							Max: resource.MustParse("128Mi"),
						},
					},
					UpdateStrategy: UpdateStrategy{
						AllowInPlaceResize: true,
						UpdateRequestsOnly: true,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid safety factor - less than 1.0",
			policy: &OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "default",
				},
				Spec: OptimizationPolicySpec{
					Mode: ModeAuto,
					Selector: WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
					},
					MetricsConfig: MetricsConfig{
						Provider:     "prometheus",
						Percentile:   "P90",
						SafetyFactor: func() *float64 { f := 0.5; return &f }(),
					},
					ResourceBounds: ResourceBounds{
						CPU: ResourceBound{
							Min: resource.MustParse("100m"),
							Max: resource.MustParse("4000m"),
						},
						Memory: ResourceBound{
							Min: resource.MustParse("128Mi"),
							Max: resource.MustParse("8Gi"),
						},
					},
					UpdateStrategy: UpdateStrategy{
						AllowInPlaceResize: true,
						UpdateRequestsOnly: true,
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.policy.ValidateCreate()
			if (err != nil) != tt.wantErr {
				t.Errorf("OptimizationPolicy.ValidateCreate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestOptimizationPolicy_ValidateUpdate(t *testing.T) {
	validPolicy := &OptimizationPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-policy",
			Namespace: "default",
		},
		Spec: OptimizationPolicySpec{
			Mode: ModeAuto,
			Selector: WorkloadSelector{
				WorkloadSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "test"},
				},
			},
			MetricsConfig: MetricsConfig{
				Provider:   "prometheus",
				Percentile: "P90",
			},
			ResourceBounds: ResourceBounds{
				CPU: ResourceBound{
					Min: resource.MustParse("100m"),
					Max: resource.MustParse("4000m"),
				},
				Memory: ResourceBound{
					Min: resource.MustParse("128Mi"),
					Max: resource.MustParse("8Gi"),
				},
			},
			UpdateStrategy: UpdateStrategy{
				AllowInPlaceResize: true,
				UpdateRequestsOnly: true,
			},
		},
	}

	invalidPolicy := &OptimizationPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-policy",
			Namespace: "default",
		},
		Spec: OptimizationPolicySpec{
			Mode: ModeAuto,
			Selector: WorkloadSelector{
				WorkloadSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "test"},
				},
			},
			MetricsConfig: MetricsConfig{
				Provider:   "prometheus",
				Percentile: "P90",
			},
			ResourceBounds: ResourceBounds{
				CPU: ResourceBound{
					Min: resource.MustParse("4000m"),
					Max: resource.MustParse("100m"),
				},
				Memory: ResourceBound{
					Min: resource.MustParse("128Mi"),
					Max: resource.MustParse("8Gi"),
				},
			},
			UpdateStrategy: UpdateStrategy{
				AllowInPlaceResize: true,
				UpdateRequestsOnly: true,
			},
		},
	}

	tests := []struct {
		name    string
		policy  *OptimizationPolicy
		old     *OptimizationPolicy
		wantErr bool
	}{
		{
			name:    "valid update",
			policy:  validPolicy,
			old:     validPolicy,
			wantErr: false,
		},
		{
			name:    "invalid update",
			policy:  invalidPolicy,
			old:     validPolicy,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.policy.ValidateUpdate(tt.old)
			if (err != nil) != tt.wantErr {
				t.Errorf("OptimizationPolicy.ValidateUpdate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// Feature: k8s-workload-rightsizing, Property 15: Policy validation
// For any OptimizationPolicy resource creation, the system should validate all required fields
// are present and well-formed, rejecting invalid configurations with descriptive error messages.
// Validates: Requirements 6.1, 6.2
func TestProperty_PolicyValidation(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	// Property: Valid policies with proper bounds should always pass validation
	properties.Property("valid policies with min <= max bounds pass validation", prop.ForAll(
		func(cpuMin, cpuMax, memMin, memMax int64) bool {
			// Ensure min <= max
			if cpuMin > cpuMax {
				cpuMin, cpuMax = cpuMax, cpuMin
			}
			if memMin > memMax {
				memMin, memMax = memMax, memMin
			}

			// Ensure positive values
			if cpuMin <= 0 {
				cpuMin = 1
			}
			if cpuMax <= 0 {
				cpuMax = cpuMin + 1
			}
			if memMin <= 0 {
				memMin = 1
			}
			if memMax <= 0 {
				memMax = memMin + 1
			}

			policy := &OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "default",
				},
				Spec: OptimizationPolicySpec{
					Mode: ModeAuto,
					Selector: WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
					},
					MetricsConfig: MetricsConfig{
						Provider:   "prometheus",
						Percentile: "P90",
					},
					ResourceBounds: ResourceBounds{
						CPU: ResourceBound{
							Min: *resource.NewMilliQuantity(cpuMin, resource.DecimalSI),
							Max: *resource.NewMilliQuantity(cpuMax, resource.DecimalSI),
						},
						Memory: ResourceBound{
							Min: *resource.NewQuantity(memMin, resource.BinarySI),
							Max: *resource.NewQuantity(memMax, resource.BinarySI),
						},
					},
					UpdateStrategy: UpdateStrategy{
						AllowInPlaceResize: true,
						UpdateRequestsOnly: true,
					},
				},
			}

			err := policy.ValidateCreate()
			return err == nil
		},
		gen.Int64Range(1, 10000),    // cpuMin in millicores
		gen.Int64Range(1, 10000),    // cpuMax in millicores
		gen.Int64Range(1, 10000000), // memMin in bytes
		gen.Int64Range(1, 10000000), // memMax in bytes
	))

	// Property: Policies with min > max bounds should always fail validation
	properties.Property("policies with min > max bounds fail validation", prop.ForAll(
		func(cpuMin, cpuMax, memMin, memMax int64) bool {
			// Ensure positive values
			if cpuMin <= 0 {
				cpuMin = 1
			}
			if cpuMax <= 0 {
				cpuMax = 1
			}
			if memMin <= 0 {
				memMin = 1
			}
			if memMax <= 0 {
				memMax = 1
			}

			// Ensure min > max for at least one resource
			if cpuMin <= cpuMax {
				cpuMin = cpuMax + 1
			}

			policy := &OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "default",
				},
				Spec: OptimizationPolicySpec{
					Mode: ModeAuto,
					Selector: WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
					},
					MetricsConfig: MetricsConfig{
						Provider:   "prometheus",
						Percentile: "P90",
					},
					ResourceBounds: ResourceBounds{
						CPU: ResourceBound{
							Min: *resource.NewMilliQuantity(cpuMin, resource.DecimalSI),
							Max: *resource.NewMilliQuantity(cpuMax, resource.DecimalSI),
						},
						Memory: ResourceBound{
							Min: *resource.NewQuantity(memMin, resource.BinarySI),
							Max: *resource.NewQuantity(memMax, resource.BinarySI),
						},
					},
					UpdateStrategy: UpdateStrategy{
						AllowInPlaceResize: true,
						UpdateRequestsOnly: true,
					},
				},
			}

			err := policy.ValidateCreate()
			return err != nil
		},
		gen.Int64Range(1, 10000),    // cpuMin in millicores
		gen.Int64Range(1, 10000),    // cpuMax in millicores
		gen.Int64Range(1, 10000000), // memMin in bytes
		gen.Int64Range(1, 10000000), // memMax in bytes
	))

	// Property: Policies with safety factor < 1.0 should always fail validation
	properties.Property("policies with safety factor < 1.0 fail validation", prop.ForAll(
		func(safetyFactor float64) bool {
			policy := &OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "default",
				},
				Spec: OptimizationPolicySpec{
					Mode: ModeAuto,
					Selector: WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
					},
					MetricsConfig: MetricsConfig{
						Provider:     "prometheus",
						Percentile:   "P90",
						SafetyFactor: &safetyFactor,
					},
					ResourceBounds: ResourceBounds{
						CPU: ResourceBound{
							Min: resource.MustParse("100m"),
							Max: resource.MustParse("4000m"),
						},
						Memory: ResourceBound{
							Min: resource.MustParse("128Mi"),
							Max: resource.MustParse("8Gi"),
						},
					},
					UpdateStrategy: UpdateStrategy{
						AllowInPlaceResize: true,
						UpdateRequestsOnly: true,
					},
				},
			}

			err := policy.ValidateCreate()
			return err != nil
		},
		gen.Float64Range(0.0, 0.99),
	))

	// Property: Policies without required selectors should fail validation
	properties.Property("policies without selectors fail validation", prop.ForAll(
		func() bool {
			policy := &OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "default",
				},
				Spec: OptimizationPolicySpec{
					Mode:     ModeAuto,
					Selector: WorkloadSelector{
						// No selectors specified
					},
					MetricsConfig: MetricsConfig{
						Provider:   "prometheus",
						Percentile: "P90",
					},
					ResourceBounds: ResourceBounds{
						CPU: ResourceBound{
							Min: resource.MustParse("100m"),
							Max: resource.MustParse("4000m"),
						},
						Memory: ResourceBound{
							Min: resource.MustParse("128Mi"),
							Max: resource.MustParse("8Gi"),
						},
					},
					UpdateStrategy: UpdateStrategy{
						AllowInPlaceResize: true,
						UpdateRequestsOnly: true,
					},
				},
			}

			err := policy.ValidateCreate()
			return err != nil
		},
	))

	properties.TestingRun(t)
}

// Feature: workload-type-selector, Property 8: Workload Type Validation
// For any OptimizationPolicy with invalid workload type names in workloadTypes.include or workloadTypes.exclude,
// the validation should reject the policy with a descriptive error
// Validates: Requirements 4.1, 4.2, 4.3
func TestProperty_WorkloadTypeValidation(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	// Property: Policies with valid workload types should pass validation
	properties.Property("policies with valid workload types pass validation", prop.ForAll(
		func(includeDeployment, includeStatefulSet, includeDaemonSet, excludeDeployment, excludeStatefulSet, excludeDaemonSet bool) bool {
			var includeTypes []WorkloadType
			var excludeTypes []WorkloadType

			if includeDeployment {
				includeTypes = append(includeTypes, WorkloadTypeDeployment)
			}
			if includeStatefulSet {
				includeTypes = append(includeTypes, WorkloadTypeStatefulSet)
			}
			if includeDaemonSet {
				includeTypes = append(includeTypes, WorkloadTypeDaemonSet)
			}

			if excludeDeployment {
				excludeTypes = append(excludeTypes, WorkloadTypeDeployment)
			}
			if excludeStatefulSet {
				excludeTypes = append(excludeTypes, WorkloadTypeStatefulSet)
			}
			if excludeDaemonSet {
				excludeTypes = append(excludeTypes, WorkloadTypeDaemonSet)
			}

			policy := &OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "default",
				},
				Spec: OptimizationPolicySpec{
					Mode: ModeAuto,
					Selector: WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
						WorkloadTypes: &WorkloadTypeFilter{
							Include: includeTypes,
							Exclude: excludeTypes,
						},
					},
					MetricsConfig: MetricsConfig{
						Provider:   "prometheus",
						Percentile: "P90",
					},
					ResourceBounds: ResourceBounds{
						CPU: ResourceBound{
							Min: resource.MustParse("100m"),
							Max: resource.MustParse("4000m"),
						},
						Memory: ResourceBound{
							Min: resource.MustParse("128Mi"),
							Max: resource.MustParse("8Gi"),
						},
					},
					UpdateStrategy: UpdateStrategy{
						AllowInPlaceResize: true,
						UpdateRequestsOnly: true,
					},
				},
			}

			err := policy.ValidateCreate()
			return err == nil
		},
		gen.Bool(), // includeDeployment
		gen.Bool(), // includeStatefulSet
		gen.Bool(), // includeDaemonSet
		gen.Bool(), // excludeDeployment
		gen.Bool(), // excludeStatefulSet
		gen.Bool(), // excludeDaemonSet
	))

	// Property: Policies with invalid workload types should fail validation
	properties.Property("policies with invalid workload types fail validation", prop.ForAll(
		func(invalidType string) bool {
			// Skip if the invalid type happens to be a valid one
			if invalidType == "Deployment" || invalidType == "StatefulSet" || invalidType == "DaemonSet" {
				return true
			}

			policy := &OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "default",
				},
				Spec: OptimizationPolicySpec{
					Mode: ModeAuto,
					Selector: WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
						WorkloadTypes: &WorkloadTypeFilter{
							Include: []WorkloadType{WorkloadType(invalidType)},
						},
					},
					MetricsConfig: MetricsConfig{
						Provider:   "prometheus",
						Percentile: "P90",
					},
					ResourceBounds: ResourceBounds{
						CPU: ResourceBound{
							Min: resource.MustParse("100m"),
							Max: resource.MustParse("4000m"),
						},
						Memory: ResourceBound{
							Min: resource.MustParse("128Mi"),
							Max: resource.MustParse("8Gi"),
						},
					},
					UpdateStrategy: UpdateStrategy{
						AllowInPlaceResize: true,
						UpdateRequestsOnly: true,
					},
				},
			}

			err := policy.ValidateCreate()
			return err != nil
		},
		gen.OneConstOf("Job", "CronJob", "Pod", "ReplicaSet", "InvalidType", ""),
	))

	// Property: Policies without workloadTypes field should pass validation (backward compatibility)
	properties.Property("policies without workloadTypes field pass validation", prop.ForAll(
		func() bool {
			policy := &OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "default",
				},
				Spec: OptimizationPolicySpec{
					Mode: ModeAuto,
					Selector: WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
						// WorkloadTypes is nil (not specified)
					},
					MetricsConfig: MetricsConfig{
						Provider:   "prometheus",
						Percentile: "P90",
					},
					ResourceBounds: ResourceBounds{
						CPU: ResourceBound{
							Min: resource.MustParse("100m"),
							Max: resource.MustParse("4000m"),
						},
						Memory: ResourceBound{
							Min: resource.MustParse("128Mi"),
							Max: resource.MustParse("8Gi"),
						},
					},
					UpdateStrategy: UpdateStrategy{
						AllowInPlaceResize: true,
						UpdateRequestsOnly: true,
					},
				},
			}

			err := policy.ValidateCreate()
			return err == nil
		},
	))

	properties.TestingRun(t)
}

// Feature: workload-type-selector, Property 9: Empty Filter Configuration Validity
// For any OptimizationPolicy where workloadTypes configuration results in no discoverable workload types,
// the policy should remain valid for creation
// Validates: Requirements 4.4
func TestProperty_EmptyFilterConfigurationValidity(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	// Property: Policies that result in no discoverable workload types should remain valid
	properties.Property("policies with empty result set remain valid", prop.ForAll(
		func() bool {
			// Create a policy where all workload types are excluded
			policy := &OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "default",
				},
				Spec: OptimizationPolicySpec{
					Mode: ModeAuto,
					Selector: WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
						WorkloadTypes: &WorkloadTypeFilter{
							Exclude: []WorkloadType{WorkloadTypeDeployment, WorkloadTypeStatefulSet, WorkloadTypeDaemonSet},
						},
					},
					MetricsConfig: MetricsConfig{
						Provider:   "prometheus",
						Percentile: "P90",
					},
					ResourceBounds: ResourceBounds{
						CPU: ResourceBound{
							Min: resource.MustParse("100m"),
							Max: resource.MustParse("4000m"),
						},
						Memory: ResourceBound{
							Min: resource.MustParse("128Mi"),
							Max: resource.MustParse("8Gi"),
						},
					},
					UpdateStrategy: UpdateStrategy{
						AllowInPlaceResize: true,
						UpdateRequestsOnly: true,
					},
				},
			}

			// The policy should be valid even though it results in no discoverable workload types
			err := policy.ValidateCreate()
			return err == nil
		},
	))

	// Property: Policies with conflicting include/exclude should remain valid
	properties.Property("policies with conflicting include/exclude remain valid", prop.ForAll(
		func(workloadType WorkloadType) bool {
			// Create a policy where the same type is both included and excluded
			policy := &OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "default",
				},
				Spec: OptimizationPolicySpec{
					Mode: ModeAuto,
					Selector: WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
						WorkloadTypes: &WorkloadTypeFilter{
							Include: []WorkloadType{workloadType},
							Exclude: []WorkloadType{workloadType},
						},
					},
					MetricsConfig: MetricsConfig{
						Provider:   "prometheus",
						Percentile: "P90",
					},
					ResourceBounds: ResourceBounds{
						CPU: ResourceBound{
							Min: resource.MustParse("100m"),
							Max: resource.MustParse("4000m"),
						},
						Memory: ResourceBound{
							Min: resource.MustParse("128Mi"),
							Max: resource.MustParse("8Gi"),
						},
					},
					UpdateStrategy: UpdateStrategy{
						AllowInPlaceResize: true,
						UpdateRequestsOnly: true,
					},
				},
			}

			// The policy should be valid even though it results in no discoverable workload types
			err := policy.ValidateCreate()
			return err == nil
		},
		gen.OneConstOf(WorkloadTypeDeployment, WorkloadTypeStatefulSet, WorkloadTypeDaemonSet),
	))

	properties.TestingRun(t)
}

// TestWorkloadTypeSet tests the WorkloadTypeSet utility functions
func TestWorkloadTypeSet(t *testing.T) {
	t.Run("NewWorkloadTypeSet creates set with given types", func(t *testing.T) {
		set := NewWorkloadTypeSet(WorkloadTypeDeployment, WorkloadTypeStatefulSet)

		if !set.Contains(WorkloadTypeDeployment) {
			t.Error("Expected set to contain Deployment")
		}
		if !set.Contains(WorkloadTypeStatefulSet) {
			t.Error("Expected set to contain StatefulSet")
		}
		if set.Contains(WorkloadTypeDaemonSet) {
			t.Error("Expected set to not contain DaemonSet")
		}
		if set.Size() != 2 {
			t.Errorf("Expected set size to be 2, got %d", set.Size())
		}
	})

	t.Run("NewWorkloadTypeSet with no types creates empty set", func(t *testing.T) {
		set := NewWorkloadTypeSet()

		if !set.IsEmpty() {
			t.Error("Expected empty set")
		}
		if set.Size() != 0 {
			t.Errorf("Expected set size to be 0, got %d", set.Size())
		}
	})

	t.Run("Add adds workload type to set", func(t *testing.T) {
		set := NewWorkloadTypeSet()
		set.Add(WorkloadTypeDeployment)

		if !set.Contains(WorkloadTypeDeployment) {
			t.Error("Expected set to contain Deployment after adding")
		}
		if set.Size() != 1 {
			t.Errorf("Expected set size to be 1, got %d", set.Size())
		}
	})

	t.Run("Add duplicate type does not increase size", func(t *testing.T) {
		set := NewWorkloadTypeSet(WorkloadTypeDeployment)
		set.Add(WorkloadTypeDeployment)

		if set.Size() != 1 {
			t.Errorf("Expected set size to remain 1 after adding duplicate, got %d", set.Size())
		}
	})

	t.Run("Remove removes workload type from set", func(t *testing.T) {
		set := NewWorkloadTypeSet(WorkloadTypeDeployment, WorkloadTypeStatefulSet)
		set.Remove(WorkloadTypeDeployment)

		if set.Contains(WorkloadTypeDeployment) {
			t.Error("Expected set to not contain Deployment after removal")
		}
		if !set.Contains(WorkloadTypeStatefulSet) {
			t.Error("Expected set to still contain StatefulSet")
		}
		if set.Size() != 1 {
			t.Errorf("Expected set size to be 1, got %d", set.Size())
		}
	})

	t.Run("Remove non-existent type does not affect set", func(t *testing.T) {
		set := NewWorkloadTypeSet(WorkloadTypeDeployment)
		set.Remove(WorkloadTypeStatefulSet)

		if !set.Contains(WorkloadTypeDeployment) {
			t.Error("Expected set to still contain Deployment")
		}
		if set.Size() != 1 {
			t.Errorf("Expected set size to remain 1, got %d", set.Size())
		}
	})

	t.Run("ToSlice returns all workload types", func(t *testing.T) {
		set := NewWorkloadTypeSet(WorkloadTypeDeployment, WorkloadTypeStatefulSet)
		slice := set.ToSlice()

		if len(slice) != 2 {
			t.Errorf("Expected slice length to be 2, got %d", len(slice))
		}

		// Check that both types are present (order doesn't matter)
		found := make(map[WorkloadType]bool)
		for _, wt := range slice {
			found[wt] = true
		}

		if !found[WorkloadTypeDeployment] {
			t.Error("Expected slice to contain Deployment")
		}
		if !found[WorkloadTypeStatefulSet] {
			t.Error("Expected slice to contain StatefulSet")
		}
	})

	t.Run("IsEmpty returns true for empty set", func(t *testing.T) {
		set := NewWorkloadTypeSet()
		if !set.IsEmpty() {
			t.Error("Expected empty set to return true for IsEmpty()")
		}

		set.Add(WorkloadTypeDeployment)
		if set.IsEmpty() {
			t.Error("Expected non-empty set to return false for IsEmpty()")
		}
	})
}

// TestGetActiveWorkloadTypes tests the GetActiveWorkloadTypes function
func TestGetActiveWorkloadTypes(t *testing.T) {
	t.Run("nil filter returns all types", func(t *testing.T) {
		activeTypes := GetActiveWorkloadTypes(nil)

		if !activeTypes.Contains(WorkloadTypeDeployment) {
			t.Error("Expected all types to include Deployment")
		}
		if !activeTypes.Contains(WorkloadTypeStatefulSet) {
			t.Error("Expected all types to include StatefulSet")
		}
		if !activeTypes.Contains(WorkloadTypeDaemonSet) {
			t.Error("Expected all types to include DaemonSet")
		}
		if activeTypes.Size() != 3 {
			t.Errorf("Expected all types to have size 3, got %d", activeTypes.Size())
		}
	})

	t.Run("empty filter returns all types", func(t *testing.T) {
		filter := &WorkloadTypeFilter{}
		activeTypes := GetActiveWorkloadTypes(filter)

		if activeTypes.Size() != 3 {
			t.Errorf("Expected all types to have size 3, got %d", activeTypes.Size())
		}
	})

	t.Run("include filter only includes specified types", func(t *testing.T) {
		filter := &WorkloadTypeFilter{
			Include: []WorkloadType{WorkloadTypeDeployment, WorkloadTypeStatefulSet},
		}
		activeTypes := GetActiveWorkloadTypes(filter)

		if !activeTypes.Contains(WorkloadTypeDeployment) {
			t.Error("Expected active types to include Deployment")
		}
		if !activeTypes.Contains(WorkloadTypeStatefulSet) {
			t.Error("Expected active types to include StatefulSet")
		}
		if activeTypes.Contains(WorkloadTypeDaemonSet) {
			t.Error("Expected active types to not include DaemonSet")
		}
		if activeTypes.Size() != 2 {
			t.Errorf("Expected active types to have size 2, got %d", activeTypes.Size())
		}
	})

	t.Run("exclude filter excludes specified types", func(t *testing.T) {
		filter := &WorkloadTypeFilter{
			Exclude: []WorkloadType{WorkloadTypeStatefulSet},
		}
		activeTypes := GetActiveWorkloadTypes(filter)

		if !activeTypes.Contains(WorkloadTypeDeployment) {
			t.Error("Expected active types to include Deployment")
		}
		if activeTypes.Contains(WorkloadTypeStatefulSet) {
			t.Error("Expected active types to not include StatefulSet")
		}
		if !activeTypes.Contains(WorkloadTypeDaemonSet) {
			t.Error("Expected active types to include DaemonSet")
		}
		if activeTypes.Size() != 2 {
			t.Errorf("Expected active types to have size 2, got %d", activeTypes.Size())
		}
	})

	t.Run("exclude takes precedence over include", func(t *testing.T) {
		filter := &WorkloadTypeFilter{
			Include: []WorkloadType{WorkloadTypeDeployment, WorkloadTypeStatefulSet},
			Exclude: []WorkloadType{WorkloadTypeStatefulSet},
		}
		activeTypes := GetActiveWorkloadTypes(filter)

		if !activeTypes.Contains(WorkloadTypeDeployment) {
			t.Error("Expected active types to include Deployment")
		}
		if activeTypes.Contains(WorkloadTypeStatefulSet) {
			t.Error("Expected active types to not include StatefulSet (exclude takes precedence)")
		}
		if activeTypes.Contains(WorkloadTypeDaemonSet) {
			t.Error("Expected active types to not include DaemonSet")
		}
		if activeTypes.Size() != 1 {
			t.Errorf("Expected active types to have size 1, got %d", activeTypes.Size())
		}
	})

	t.Run("exclude all types results in empty set", func(t *testing.T) {
		filter := &WorkloadTypeFilter{
			Exclude: []WorkloadType{WorkloadTypeDeployment, WorkloadTypeStatefulSet, WorkloadTypeDaemonSet},
		}
		activeTypes := GetActiveWorkloadTypes(filter)

		if !activeTypes.IsEmpty() {
			t.Error("Expected active types to be empty when all types are excluded")
		}
	})

	t.Run("include and exclude same type results in exclusion", func(t *testing.T) {
		filter := &WorkloadTypeFilter{
			Include: []WorkloadType{WorkloadTypeDeployment},
			Exclude: []WorkloadType{WorkloadTypeDeployment},
		}
		activeTypes := GetActiveWorkloadTypes(filter)

		if !activeTypes.IsEmpty() {
			t.Error("Expected active types to be empty when same type is included and excluded")
		}
	})
}

// Feature: workload-type-selector, Property 6: Exclude Precedence Over Include
// For any OptimizationPolicy with both workloadTypes.include and workloadTypes.exclude,
// workload types in the exclude list should not be discovered even if they appear in the include list
// Validates: Requirements 3.1, 3.2
func TestProperty_ExcludePrecedenceOverInclude(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	// Property: Exclude always takes precedence over include
	properties.Property("exclude takes precedence over include", prop.ForAll(
		func(includeDeployment, includeStatefulSet, includeDaemonSet, excludeDeployment, excludeStatefulSet, excludeDaemonSet bool) bool {
			var includeTypes []WorkloadType
			var excludeTypes []WorkloadType

			if includeDeployment {
				includeTypes = append(includeTypes, WorkloadTypeDeployment)
			}
			if includeStatefulSet {
				includeTypes = append(includeTypes, WorkloadTypeStatefulSet)
			}
			if includeDaemonSet {
				includeTypes = append(includeTypes, WorkloadTypeDaemonSet)
			}

			if excludeDeployment {
				excludeTypes = append(excludeTypes, WorkloadTypeDeployment)
			}
			if excludeStatefulSet {
				excludeTypes = append(excludeTypes, WorkloadTypeStatefulSet)
			}
			if excludeDaemonSet {
				excludeTypes = append(excludeTypes, WorkloadTypeDaemonSet)
			}

			filter := &WorkloadTypeFilter{
				Include: includeTypes,
				Exclude: excludeTypes,
			}

			activeTypes := GetActiveWorkloadTypes(filter)

			// Check that no excluded type is in the active set
			for _, excludedType := range excludeTypes {
				if activeTypes.Contains(excludedType) {
					return false // Exclude precedence violated
				}
			}

			// If include list is empty, should start with all types then apply excludes
			if len(includeTypes) == 0 {
				allTypes := []WorkloadType{WorkloadTypeDeployment, WorkloadTypeStatefulSet, WorkloadTypeDaemonSet}
				for _, workloadType := range allTypes {
					isExcluded := false
					for _, excludedType := range excludeTypes {
						if workloadType == excludedType {
							isExcluded = true
							break
						}
					}
					if !isExcluded && !activeTypes.Contains(workloadType) {
						return false // Should contain non-excluded types
					}
				}
			} else {
				// If include list is not empty, should only contain included types that are not excluded
				for _, includedType := range includeTypes {
					isExcluded := false
					for _, excludedType := range excludeTypes {
						if includedType == excludedType {
							isExcluded = true
							break
						}
					}
					if !isExcluded && !activeTypes.Contains(includedType) {
						return false // Should contain included types that are not excluded
					}
				}
			}

			return true
		},
		gen.Bool(), // includeDeployment
		gen.Bool(), // includeStatefulSet
		gen.Bool(), // includeDaemonSet
		gen.Bool(), // excludeDeployment
		gen.Bool(), // excludeStatefulSet
		gen.Bool(), // excludeDaemonSet
	))

	properties.TestingRun(t)
}
