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
	"fmt"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Annotation keys for workload recommendations
const (
	// AnnotationManaged indicates the workload is managed by OptiPod
	AnnotationManaged = "optipod.io/managed"

	// AnnotationPolicy indicates which policy manages this workload
	AnnotationPolicy = "optipod.io/policy"

	// AnnotationLastRecommendation is the timestamp of the last recommendation
	AnnotationLastRecommendation = "optipod.io/last-recommendation"

	// AnnotationLastApplied is the timestamp of the last applied change
	AnnotationLastApplied = "optipod.io/last-applied"

	// AnnotationRecommendationPrefix is the prefix for per-container recommendations
	// Format: optipod.io/recommendation.<container-name>.cpu
	//         optipod.io/recommendation.<container-name>.memory
	AnnotationRecommendationPrefix = "optipod.io/recommendation"
)

// PolicyMode defines the operational mode of the optimization policy
// +kubebuilder:validation:Enum=Auto;Recommend;Disabled
type PolicyMode string

const (
	// ModeAuto automatically applies resource recommendations to workloads
	ModeAuto PolicyMode = "Auto"
	// ModeRecommend computes recommendations but does not apply them
	ModeRecommend PolicyMode = "Recommend"
	// ModeDisabled stops processing workloads under this policy
	ModeDisabled PolicyMode = "Disabled"
)

// OptimizationPolicySpec defines the desired state of OptimizationPolicy
type OptimizationPolicySpec struct {
	// Mode defines the operational behavior of the policy
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=Auto;Recommend;Disabled
	Mode PolicyMode `json:"mode"`

	// Weight defines the priority of this policy when multiple policies match the same workload
	// Higher weight policies take precedence. Default weight is 100.
	// +kubebuilder:default=100
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1000
	// +optional
	Weight *int32 `json:"weight,omitempty"`

	// Selector defines which workloads this policy applies to
	// +kubebuilder:validation:Required
	Selector WorkloadSelector `json:"selector"`

	// MetricsConfig defines how metrics are collected and processed
	// +kubebuilder:validation:Required
	MetricsConfig MetricsConfig `json:"metricsConfig"`

	// ResourceBounds defines min/max constraints for resource recommendations
	// +kubebuilder:validation:Required
	ResourceBounds ResourceBounds `json:"resourceBounds"`

	// UpdateStrategy defines how resource updates are applied
	// +kubebuilder:validation:Required
	UpdateStrategy UpdateStrategy `json:"updateStrategy"`

	// ReconciliationInterval defines how often the policy is evaluated
	// +kubebuilder:default="5m"
	// +optional
	ReconciliationInterval metav1.Duration `json:"reconciliationInterval,omitempty"`
}

// WorkloadSelector defines which workloads a policy applies to
type WorkloadSelector struct {
	// NamespaceSelector selects namespaces by labels
	// +optional
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`

	// WorkloadSelector selects workloads by labels
	// +optional
	WorkloadSelector *metav1.LabelSelector `json:"workloadSelector,omitempty"`

	// Namespaces defines allow/deny lists for namespace filtering
	// +optional
	Namespaces *NamespaceFilter `json:"namespaces,omitempty"`
}

// NamespaceFilter defines allow and deny lists for namespaces
type NamespaceFilter struct {
	// Allow is a list of namespaces to include
	// +optional
	Allow []string `json:"allow,omitempty"`

	// Deny is a list of namespaces to exclude (takes precedence over Allow)
	// +optional
	Deny []string `json:"deny,omitempty"`
}

// MetricsConfig defines metrics collection and processing configuration
type MetricsConfig struct {
	// Provider specifies the metrics backend (e.g., "prometheus", "metrics-server")
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=prometheus;metrics-server;custom
	Provider string `json:"provider"`

	// RollingWindow defines the time period over which metrics are aggregated
	// +kubebuilder:default="24h"
	// +optional
	RollingWindow metav1.Duration `json:"rollingWindow,omitempty"`

	// Percentile defines which percentile to use for recommendations
	// +kubebuilder:validation:Enum=P50;P90;P99
	// +kubebuilder:default="P90"
	// +optional
	Percentile string `json:"percentile,omitempty"`

	// SafetyFactor is a multiplier applied to the selected percentile
	// Must be >= 1.0. Defaults to 1.2 if not specified.
	// +kubebuilder:default=1.2
	// +optional
	SafetyFactor *float64 `json:"safetyFactor,omitempty"`
}

// ResourceBounds defines min/max constraints for CPU and memory
type ResourceBounds struct {
	// CPU defines CPU resource bounds
	// +kubebuilder:validation:Required
	CPU ResourceBound `json:"cpu"`

	// Memory defines memory resource bounds
	// +kubebuilder:validation:Required
	Memory ResourceBound `json:"memory"`
}

// ResourceBound defines min/max for a single resource type
type ResourceBound struct {
	// Min is the minimum allowed value
	// +kubebuilder:validation:Required
	Min resource.Quantity `json:"min"`

	// Max is the maximum allowed value
	// +kubebuilder:validation:Required
	Max resource.Quantity `json:"max"`
}

// UpdateStrategy defines how resource updates are applied to workloads
type UpdateStrategy struct {
	// AllowInPlaceResize enables in-place pod resize when supported
	// +kubebuilder:default=true
	// +optional
	AllowInPlaceResize bool `json:"allowInPlaceResize,omitempty"`

	// AllowRecreate enables pod recreation when in-place resize is not available
	// +kubebuilder:default=false
	// +optional
	AllowRecreate bool `json:"allowRecreate,omitempty"`

	// UpdateRequestsOnly controls whether to update only requests or both requests and limits
	// +kubebuilder:default=true
	// +optional
	UpdateRequestsOnly bool `json:"updateRequestsOnly,omitempty"`

	// UseServerSideApply enables Server-Side Apply for field-level ownership
	// +kubebuilder:default=true
	// +optional
	UseServerSideApply *bool `json:"useServerSideApply,omitempty"`

	// LimitConfig defines how resource limits are calculated from recommendations
	// +optional
	LimitConfig *LimitConfig `json:"limitConfig,omitempty"`
}

// LimitConfig defines how resource limits are calculated from recommendations
type LimitConfig struct {
	// CPULimitMultiplier is the multiplier applied to CPU recommendation to calculate limit
	// Default: 1.0 (limit equals recommendation)
	// Example: 1.5 means limit = recommendation * 1.5
	// +kubebuilder:default=1.0
	// +kubebuilder:validation:Minimum=1.0
	// +kubebuilder:validation:Maximum=10.0
	// +optional
	CPULimitMultiplier *float64 `json:"cpuLimitMultiplier,omitempty"`

	// MemoryLimitMultiplier is the multiplier applied to memory recommendation to calculate limit
	// Default: 1.1 (limit is 10% higher than recommendation)
	// Example: 1.2 means limit = recommendation * 1.2
	// +kubebuilder:default=1.1
	// +kubebuilder:validation:Minimum=1.0
	// +kubebuilder:validation:Maximum=10.0
	// +optional
	MemoryLimitMultiplier *float64 `json:"memoryLimitMultiplier,omitempty"`
}

// OptimizationPolicyStatus defines the observed state of OptimizationPolicy.
type OptimizationPolicyStatus struct {
	// Conditions represent the current state of the OptimizationPolicy resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// WorkloadsDiscovered is the count of workloads matching this policy
	// +optional
	WorkloadsDiscovered int `json:"workloadsDiscovered,omitempty"`

	// WorkloadsProcessed is the count of workloads successfully processed
	// +optional
	WorkloadsProcessed int `json:"workloadsProcessed,omitempty"`

	// LastReconciliation is the timestamp of the last reconciliation
	// +optional
	LastReconciliation *metav1.Time `json:"lastReconciliation,omitempty"`
}

// WorkloadStatus represents the optimization status for a single workload
type WorkloadStatus struct {
	// Name is the workload name
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Namespace is the workload namespace
	// +kubebuilder:validation:Required
	Namespace string `json:"namespace"`

	// Kind is the workload kind (Deployment, StatefulSet, DaemonSet)
	// +kubebuilder:validation:Required
	Kind string `json:"kind"`

	// LastRecommendation is the timestamp of the last recommendation
	// +optional
	LastRecommendation *metav1.Time `json:"lastRecommendation,omitempty"`

	// LastApplied is the timestamp of the last applied change
	// +optional
	LastApplied *metav1.Time `json:"lastApplied,omitempty"`

	// Recommendations contains per-container resource recommendations
	// +optional
	Recommendations []ContainerRecommendation `json:"recommendations,omitempty"`

	// Status describes the current state (e.g., "Applied", "Skipped", "Error")
	// +optional
	Status string `json:"status,omitempty"`

	// Reason provides additional context for the status
	// +optional
	Reason string `json:"reason,omitempty"`

	// LastApplyMethod indicates the patch method used for the last update
	// +optional
	LastApplyMethod string `json:"lastApplyMethod,omitempty"`

	// FieldOwnership indicates if OptipPod owns resource fields via SSA
	// +optional
	FieldOwnership bool `json:"fieldOwnership,omitempty"`
}

// ContainerRecommendation represents resource recommendations for a single container
type ContainerRecommendation struct {
	// Container is the container name
	// +kubebuilder:validation:Required
	Container string `json:"container"`

	// CPU is the recommended CPU request
	// +optional
	CPU *resource.Quantity `json:"cpu,omitempty"`

	// Memory is the recommended memory request
	// +optional
	Memory *resource.Quantity `json:"memory,omitempty"`

	// Explanation describes how the recommendation was computed
	// +optional
	Explanation string `json:"explanation,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=optpol
// +kubebuilder:printcolumn:name="Mode",type=string,JSONPath=`.spec.mode`
// +kubebuilder:printcolumn:name="Provider",type=string,JSONPath=`.spec.metricsConfig.provider`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// OptimizationPolicy is the Schema for the optimizationpolicies API
type OptimizationPolicy struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of OptimizationPolicy
	// +required
	Spec OptimizationPolicySpec `json:"spec"`

	// status defines the observed state of OptimizationPolicy
	// +optional
	Status OptimizationPolicyStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// OptimizationPolicyList contains a list of OptimizationPolicy
type OptimizationPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []OptimizationPolicy `json:"items"`
}

// GetWeight returns the policy weight, defaulting to 100 if not specified
func (r *OptimizationPolicy) GetWeight() int32 {
	if r.Spec.Weight != nil {
		return *r.Spec.Weight
	}
	return 100 // Default weight
}

func init() {
	SchemeBuilder.Register(&OptimizationPolicy{}, &OptimizationPolicyList{})
}

// ValidateCreate validates the OptimizationPolicy on creation
func (r *OptimizationPolicy) ValidateCreate() error {
	return r.validateOptimizationPolicy()
}

// ValidateUpdate validates the OptimizationPolicy on update
func (r *OptimizationPolicy) ValidateUpdate(old *OptimizationPolicy) error {
	return r.validateOptimizationPolicy()
}

// ValidateDelete validates the OptimizationPolicy on deletion
func (r *OptimizationPolicy) ValidateDelete() error {
	// No validation needed on delete
	return nil
}

// validateOptimizationPolicy performs validation of the OptimizationPolicy
func (r *OptimizationPolicy) validateOptimizationPolicy() error {
	// Validate required fields - mode
	if r.Spec.Mode == "" {
		return fmt.Errorf("mode is required and must be one of: Auto, Recommend, Disabled")
	}

	// Validate mode value
	if r.Spec.Mode != ModeAuto && r.Spec.Mode != ModeRecommend && r.Spec.Mode != ModeDisabled {
		return fmt.Errorf("invalid mode %q, must be one of: Auto, Recommend, Disabled", r.Spec.Mode)
	}

	// Validate selector - at least one selector must be specified
	if r.Spec.Selector.NamespaceSelector == nil &&
		r.Spec.Selector.WorkloadSelector == nil &&
		r.Spec.Selector.Namespaces == nil {
		return fmt.Errorf("selector is required: at least one of namespaceSelector, workloadSelector, or namespaces must be specified")
	}

	// Validate selector syntax if provided
	if r.Spec.Selector.NamespaceSelector != nil {
		if err := validateLabelSelector(r.Spec.Selector.NamespaceSelector, "namespaceSelector"); err != nil {
			return err
		}
	}

	if r.Spec.Selector.WorkloadSelector != nil {
		if err := validateLabelSelector(r.Spec.Selector.WorkloadSelector, "workloadSelector"); err != nil {
			return err
		}
	}

	// Validate metrics provider
	if r.Spec.MetricsConfig.Provider == "" {
		return fmt.Errorf("metricsConfig.provider is required")
	}

	// Validate CPU bounds
	if r.Spec.ResourceBounds.CPU.Min.IsZero() {
		return fmt.Errorf("resourceBounds.cpu.min is required and must be greater than zero")
	}

	if r.Spec.ResourceBounds.CPU.Max.IsZero() {
		return fmt.Errorf("resourceBounds.cpu.max is required and must be greater than zero")
	}

	if r.Spec.ResourceBounds.CPU.Min.Cmp(r.Spec.ResourceBounds.CPU.Max) > 0 {
		return fmt.Errorf("CPU min (%s) must be less than or equal to max (%s)",
			r.Spec.ResourceBounds.CPU.Min.String(),
			r.Spec.ResourceBounds.CPU.Max.String())
	}

	// Validate memory bounds
	if r.Spec.ResourceBounds.Memory.Min.IsZero() {
		return fmt.Errorf("resourceBounds.memory.min is required and must be greater than zero")
	}

	if r.Spec.ResourceBounds.Memory.Max.IsZero() {
		return fmt.Errorf("resourceBounds.memory.max is required and must be greater than zero")
	}

	if r.Spec.ResourceBounds.Memory.Min.Cmp(r.Spec.ResourceBounds.Memory.Max) > 0 {
		return fmt.Errorf("memory min (%s) must be less than or equal to max (%s)",
			r.Spec.ResourceBounds.Memory.Min.String(),
			r.Spec.ResourceBounds.Memory.Max.String())
	}

	// Validate safety factor
	if r.Spec.MetricsConfig.SafetyFactor != nil && *r.Spec.MetricsConfig.SafetyFactor < 1.0 {
		return fmt.Errorf("safety factor must be at least 1.0, got %f", *r.Spec.MetricsConfig.SafetyFactor)
	}

	// Validate weight
	if r.Spec.Weight != nil && (*r.Spec.Weight < 1 || *r.Spec.Weight > 1000) {
		return fmt.Errorf("weight must be between 1 and 1000, got %d", *r.Spec.Weight)
	}

	return nil
}

// validateLabelSelector validates a label selector's syntax
func validateLabelSelector(selector *metav1.LabelSelector, fieldName string) error {
	if selector == nil {
		return nil
	}

	// Validate match expressions if present
	for i, expr := range selector.MatchExpressions {
		if expr.Key == "" {
			return fmt.Errorf("%s.matchExpressions[%d]: key is required", fieldName, i)
		}

		// Validate operator
		validOperators := map[string]bool{
			"In":           true,
			"NotIn":        true,
			"Exists":       true,
			"DoesNotExist": true,
		}

		if !validOperators[string(expr.Operator)] {
			return fmt.Errorf("%s.matchExpressions[%d]: invalid operator %q, must be one of: In, NotIn, Exists, DoesNotExist",
				fieldName, i, expr.Operator)
		}

		// Validate values for In/NotIn operators
		if (expr.Operator == "In" || expr.Operator == "NotIn") && len(expr.Values) == 0 {
			return fmt.Errorf("%s.matchExpressions[%d]: values are required for operator %q",
				fieldName, i, expr.Operator)
		}

		// Validate no values for Exists/DoesNotExist operators
		if (expr.Operator == "Exists" || expr.Operator == "DoesNotExist") && len(expr.Values) > 0 {
			return fmt.Errorf("%s.matchExpressions[%d]: values must not be specified for operator %q",
				fieldName, i, expr.Operator)
		}
	}

	return nil
}
