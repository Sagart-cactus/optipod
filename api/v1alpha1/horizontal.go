package v1alpha1

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// HorizontalScalingConfig opts into one native autoscaling/v2 HPA per selected
// scalable workload. Existing user or KEDA scalers are never adopted.
type HorizontalScalingConfig struct {
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=1
	MinReplicas int32 `json:"minReplicas"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1000
	MaxReplicas int32 `json:"maxReplicas"`
	// Native HPA metric specifications. External/custom metrics require an adapter.
	// +kubebuilder:validation:MinItems=1
	Metrics []autoscalingv2.MetricSpec `json:"metrics"`
	// Native HPA behavior. Kubernetes defaults include scale-down stabilization.
	// +optional
	Behavior *autoscalingv2.HorizontalPodAutoscalerBehavior `json:"behavior,omitempty"`
}

func (p *OptimizationPolicy) validateHorizontal() error {
	h := p.Spec.HorizontalScaling
	if h == nil {
		return nil
	}
	if h.MinReplicas < 1 || h.MaxReplicas < h.MinReplicas || h.MaxReplicas > 1000 || len(h.Metrics) == 0 {
		return fmt.Errorf("horizontalScaling requires 1 <= minReplicas <= maxReplicas <= 1000 and metrics")
	}
	active := GetActiveWorkloadTypes(p.Spec.Selector.WorkloadTypes)
	for _, kind := range []WorkloadType{WorkloadTypeDaemonSet, WorkloadTypeJob, WorkloadTypeCronJob} {
		if active.Contains(kind) {
			return fmt.Errorf("horizontalScaling requires explicit scalable workload types; %s has no supported replica scale target", kind)
		}
	}
	return nil
}
