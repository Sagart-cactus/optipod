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
					Mode: ModeAuto,
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
