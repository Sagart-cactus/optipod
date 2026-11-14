package v1alpha1

import (
	"fmt"
	"math"
)

// NodePercentages sizes each configured resource from the destination node's
// allocatable capacity. An omitted resource keeps its submitted Pod resources.
type NodePercentages struct {
	// +kubebuilder:validation:Minimum=0.0001
	// +kubebuilder:validation:Maximum=100
	// +optional
	CPUPercent *float64 `json:"cpuPercent,omitempty"`
	// +kubebuilder:validation:Minimum=0.0001
	// +kubebuilder:validation:Maximum=100
	// +optional
	MemoryPercent *float64 `json:"memoryPercent,omitempty"`
}

// DaemonSetSizing uses fresh node capacity at Pod admission. It never restarts
// existing Pods. Defaults apply to containers with requests; named overrides can
// also size containers without requests. Exclusions/sidecar opt-ins still apply.
type DaemonSetSizing struct {
	// +optional
	Default *NodePercentages `json:"default,omitempty"`
	// Exact container names; replaces Default for that container.
	// +optional
	Containers map[string]NodePercentages `json:"containers,omitempty"`
}

func (p *OptimizationPolicy) validateDaemonSetSizing() error {
	d := p.Spec.DaemonSetSizing
	if d == nil {
		return nil
	}
	types := GetActiveWorkloadTypes(p.Spec.Selector.WorkloadTypes)
	if len(types) != 1 || !types.Contains(WorkloadTypeDaemonSet) {
		return fmt.Errorf("daemonSetSizing requires only DaemonSet workload type")
	}
	if !p.IsWebhookStrategy() || p.GetRolloutStrategy() != "onNextRestart" {
		return fmt.Errorf("daemonSetSizing requires webhook onNextRestart admission strategy")
	}
	if p.Spec.MetricsConfig.PredictiveCPU != nil || p.Spec.MetricsConfig.JVM != nil || (p.Spec.MetricsConfig.CPU != nil && p.Spec.MetricsConfig.CPU.Source != nil) || (p.Spec.MetricsConfig.Memory != nil && p.Spec.MetricsConfig.Memory.Source != nil) {
		return fmt.Errorf("daemonSetSizing cannot combine metric recommendation sources")
	}
	all := []NodePercentages{}
	if d.Default != nil {
		all = append(all, *d.Default)
	}
	for name, v := range d.Containers {
		if name == "" {
			return fmt.Errorf("empty daemonSetSizing container name")
		}
		all = append(all, v)
	}
	if len(all) == 0 {
		return fmt.Errorf("daemonSetSizing requires default or container percentages")
	}
	for _, v := range all {
		if v.CPUPercent == nil && v.MemoryPercent == nil {
			return fmt.Errorf("daemonSetSizing requires a resource percentage")
		}
		for i, n := range []*float64{v.CPUPercent, v.MemoryPercent} {
			if n == nil {
				continue
			}
			if math.IsNaN(*n) || math.IsInf(*n, 0) || *n <= 0 || *n > 100 {
				return fmt.Errorf("node percentage must be in (0,100]")
			}
			if p.LimitMode(i == 0) == LimitPreserve {
				return fmt.Errorf("node percentages cannot preserve limits for the configured resource")
			}
		}
	}
	return nil
}
