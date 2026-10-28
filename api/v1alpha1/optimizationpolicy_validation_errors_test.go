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
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// validValidationPolicy returns a policy that passes every validation rule so
// individual table cases can mutate one field at a time. The invalid-config
// assertions displaced from the legacy Kind suite (inventory rows S038,
// S051–S056, S083) now live here against the production validator.
func validValidationPolicy() *OptimizationPolicy {
	return &OptimizationPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "field-error-policy",
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
}

// TestOptimizationPolicy_ValidateCreate_FieldErrors drives the production
// ValidateCreate entry point for invalid modes, selectors, factors, bounds,
// weights, strategies and rollout strategies, asserting that each error names
// the offending field so the failure is actionable rather than a bare wantErr.
func TestOptimizationPolicy_ValidateCreate_FieldErrors(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	i := func(v int32) *int32 { return &v }
	n := func(v int) *int { return &v }
	str := func(v string) *string { return &v }

	tests := []struct {
		name          string
		mutate        func(*OptimizationPolicy)
		wantSubstring []string
	}{
		{
			name:          "empty mode is rejected",
			mutate:        func(p *OptimizationPolicy) { p.Spec.Mode = "" },
			wantSubstring: []string{"mode is required"},
		},
		{
			name:          "unsupported mode is rejected with the supported set",
			mutate:        func(p *OptimizationPolicy) { p.Spec.Mode = PolicyMode("bogus") },
			wantSubstring: []string{`invalid mode "bogus"`, "Auto, Recommend, Disabled"},
		},
		{
			name:          "missing selectors are rejected",
			mutate:        func(p *OptimizationPolicy) { p.Spec.Selector = WorkloadSelector{} },
			wantSubstring: []string{"selector is required", "namespaceSelector"},
		},
		{
			name: "invalid namespaceSelector operator is rejected with its index",
			mutate: func(p *OptimizationPolicy) {
				p.Spec.Selector = WorkloadSelector{
					NamespaceSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{Key: "team", Operator: metav1.LabelSelectorOperator("Bogus"), Values: []string{"platform"}},
						},
					},
				}
			},
			wantSubstring: []string{"namespaceSelector.matchExpressions[0]", `invalid operator "Bogus"`, "In, NotIn, Exists, DoesNotExist"},
		},
		{
			name: "In operator without values is rejected",
			mutate: func(p *OptimizationPolicy) {
				p.Spec.Selector = WorkloadSelector{
					WorkloadSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{Key: "app", Operator: metav1.LabelSelectorOpIn},
						},
					},
				}
			},
			wantSubstring: []string{"workloadSelector.matchExpressions[0]", "values are required for operator", `"In"`},
		},
		{
			name: "Exists operator with values is rejected",
			mutate: func(p *OptimizationPolicy) {
				p.Spec.Selector = WorkloadSelector{
					WorkloadSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{Key: "app", Operator: metav1.LabelSelectorOpExists, Values: []string{"web"}},
						},
					},
				}
			},
			wantSubstring: []string{"workloadSelector.matchExpressions[0]", "values must not be specified for operator", `"Exists"`},
		},
		{
			name: "unsupported workload type is rejected with its list index",
			mutate: func(p *OptimizationPolicy) {
				p.Spec.Selector.WorkloadTypes = &WorkloadTypeFilter{
					Include: []WorkloadType{WorkloadTypeDeployment, WorkloadType("Job")},
				}
			},
			wantSubstring: []string{"workloadTypes.include[1]", `invalid workload type "Job"`, "Deployment, StatefulSet, DaemonSet"},
		},
		{
			name:          "missing metrics provider is rejected",
			mutate:        func(p *OptimizationPolicy) { p.Spec.MetricsConfig.Provider = "" },
			wantSubstring: []string{"metricsConfig.provider is required"},
		},
		{
			name: "zero CPU min is rejected",
			mutate: func(p *OptimizationPolicy) {
				p.Spec.ResourceBounds.CPU.Min = resource.MustParse("0m")
			},
			wantSubstring: []string{"resourceBounds.cpu.min is required", "greater than zero"},
		},
		{
			name: "zero CPU max is rejected",
			mutate: func(p *OptimizationPolicy) {
				p.Spec.ResourceBounds.CPU.Max = resource.MustParse("0m")
			},
			wantSubstring: []string{"resourceBounds.cpu.max is required", "greater than zero"},
		},
		{
			name: "inverted CPU bounds are rejected with both values",
			mutate: func(p *OptimizationPolicy) {
				p.Spec.ResourceBounds.CPU.Min = resource.MustParse("2500m")
				p.Spec.ResourceBounds.CPU.Max = resource.MustParse("100m")
			},
			wantSubstring: []string{"CPU min (2500m) must be less than or equal to max (100m)"},
		},
		{
			name: "zero memory min is rejected",
			mutate: func(p *OptimizationPolicy) {
				p.Spec.ResourceBounds.Memory.Min = resource.MustParse("0")
			},
			wantSubstring: []string{"resourceBounds.memory.min is required", "greater than zero"},
		},
		{
			name: "inverted memory bounds are rejected with both values",
			mutate: func(p *OptimizationPolicy) {
				p.Spec.ResourceBounds.Memory.Min = resource.MustParse("8Gi")
				p.Spec.ResourceBounds.Memory.Max = resource.MustParse("128Mi")
			},
			wantSubstring: []string{"memory min (8Gi) must be less than or equal to max (128Mi)"},
		},
		{
			name: "safety factor below 1.0 is rejected with the observed value",
			mutate: func(p *OptimizationPolicy) {
				p.Spec.MetricsConfig.SafetyFactor = f(0.5)
			},
			wantSubstring: []string{"safety factor must be at least 1.0"},
		},
		{
			name: "metrics-server min samples below 1 is rejected",
			mutate: func(p *OptimizationPolicy) {
				p.Spec.MetricsConfig.MetricsServer = &MetricsServerConfig{MinSamplesRequired: i(0)}
			},
			wantSubstring: []string{"metricsConfig.metricsServer.minSamplesRequired must be >= 1"},
		},
		{
			name: "weight below the supported minimum is rejected",
			mutate: func(p *OptimizationPolicy) {
				p.Spec.Weight = i(0)
			},
			wantSubstring: []string{"weight must be between 1 and 1000"},
		},
		{
			name: "weight above the supported maximum is rejected",
			mutate: func(p *OptimizationPolicy) {
				p.Spec.Weight = i(1001)
			},
			wantSubstring: []string{"weight must be between 1 and 1000"},
		},
		{
			name: "unsupported strategy is rejected with the supported set",
			mutate: func(p *OptimizationPolicy) {
				p.Spec.UpdateStrategy.Strategy = str("blue-green")
			},
			wantSubstring: []string{`invalid strategy "blue-green"`, "ssa, webhook"},
		},
		{
			name: "unsupported rollout strategy is rejected with the supported set",
			mutate: func(p *OptimizationPolicy) {
				p.Spec.UpdateStrategy.RolloutStrategy = str("eventually")
			},
			wantSubstring: []string{`invalid rolloutStrategy "eventually"`, "immediate, onNextRestart"},
		},
		{
			name: "gradual decrease percentage below the supported range is rejected",
			mutate: func(p *OptimizationPolicy) {
				p.Spec.UpdateStrategy.GradualDecreaseConfig = &GradualDecreaseConfig{
					Enabled:                  true,
					MemoryDecreasePercentage: n(0),
				}
			},
			wantSubstring: []string{"gradualDecreaseConfig.memoryDecreasePercentage must be between 1 and 50"},
		},
		{
			name: "gradual decrease maximum total decrease above the range is rejected",
			mutate: func(p *OptimizationPolicy) {
				p.Spec.UpdateStrategy.GradualDecreaseConfig = &GradualDecreaseConfig{
					Enabled:              true,
					MaximumTotalDecrease: n(91),
				}
			},
			wantSubstring: []string{"gradualDecreaseConfig.maximumTotalDecrease must be between 1 and 90"},
		},
		{
			name: "non-positive gradual decrease threshold is rejected",
			mutate: func(p *OptimizationPolicy) {
				p.Spec.UpdateStrategy.GradualDecreaseConfig = &GradualDecreaseConfig{
					Enabled:                  true,
					MinimumDecreaseThreshold: ptrQuantity(resource.MustParse("0Mi")),
				}
			},
			wantSubstring: []string{"gradualDecreaseConfig.minimumDecreaseThreshold must be positive"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := validValidationPolicy()
			tt.mutate(policy)

			err := policy.ValidateCreate()
			if err == nil {
				t.Fatalf("ValidateCreate() succeeded, expected a field error")
			}
			for _, want := range tt.wantSubstring {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("ValidateCreate() error %q does not name the expected field detail %q", err.Error(), want)
				}
			}
		})
	}
}

// TestOptimizationPolicy_ValidateUpdate_FieldErrors proves the update path
// runs the same production validation, rejecting the same invalid inputs with
// field-level errors.
func TestOptimizationPolicy_ValidateUpdate_FieldErrors(t *testing.T) {
	str := func(v string) *string { return &v }

	tests := []struct {
		name          string
		mutate        func(*OptimizationPolicy)
		wantSubstring []string
	}{
		{
			name: "updating to inverted CPU bounds is rejected",
			mutate: func(p *OptimizationPolicy) {
				p.Spec.ResourceBounds.CPU.Min = resource.MustParse("2500m")
				p.Spec.ResourceBounds.CPU.Max = resource.MustParse("100m")
			},
			wantSubstring: []string{"CPU min (2500m) must be less than or equal to max (100m)"},
		},
		{
			name: "updating to an unsupported mode is rejected",
			mutate: func(p *OptimizationPolicy) {
				p.Spec.Mode = PolicyMode("Sometimes")
			},
			wantSubstring: []string{`invalid mode "Sometimes"`},
		},
		{
			name: "updating to an unsupported strategy is rejected",
			mutate: func(p *OptimizationPolicy) {
				p.Spec.UpdateStrategy.Strategy = str("blue-green")
			},
			wantSubstring: []string{`invalid strategy "blue-green"`},
		},
		{
			name: "updating to a safety factor below 1.0 is rejected",
			mutate: func(p *OptimizationPolicy) {
				f := 0.5
				p.Spec.MetricsConfig.SafetyFactor = &f
			},
			wantSubstring: []string{"safety factor must be at least 1.0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			old := validValidationPolicy()
			updated := validValidationPolicy()
			tt.mutate(updated)

			err := updated.ValidateUpdate(old)
			if err == nil {
				t.Fatalf("ValidateUpdate() succeeded, expected a field error")
			}
			for _, want := range tt.wantSubstring {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("ValidateUpdate() error %q does not name the expected field detail %q", err.Error(), want)
				}
			}
		})
	}
}

// TestOptimizationPolicy_ValidateDelete_NoError pins the production contract
// that deletion requires no validation.
func TestOptimizationPolicy_ValidateDelete_NoError(t *testing.T) {
	policy := validValidationPolicy()
	if err := policy.ValidateDelete(); err != nil {
		t.Fatalf("ValidateDelete() returned %v, expected nil", err)
	}
}

// ptrQuantity is a small helper for optional quantity fields.
func ptrQuantity(q resource.Quantity) *resource.Quantity {
	return &q
}
