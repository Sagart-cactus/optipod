package v1alpha1

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// StabilityConfig limits routine changes; emergency OOM increases bypass it.
type StabilityConfig struct {
	// MinimumChangePercent ignores smaller request changes (0 disables).
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	// +optional
	MinimumChangePercent int32 `json:"minimumChangePercent,omitempty"`
	// +optional
	MinimumCPUChange *resource.Quantity `json:"minimumCPUChange,omitempty"`
	// +optional
	MinimumMemoryChange *resource.Quantity `json:"minimumMemoryChange,omitempty"`
	// Cooldown is the minimum time between distinct accepted plans.
	// +optional
	Cooldown metav1.Duration `json:"cooldown,omitempty"`
}

func (p *OptimizationPolicy) validateStability() error {
	if s := p.Spec.UpdateStrategy.Stability; s != nil {
		if s.MinimumChangePercent < 0 || s.MinimumChangePercent > 100 {
			return fmt.Errorf("minimumChangePercent must be between 0 and 100")
		}
		if s.Cooldown.Duration < 0 || s.Cooldown.Duration > 30*24*time.Hour {
			return fmt.Errorf("cooldown must be between zero and 720h")
		}
		for _, q := range []*resource.Quantity{s.MinimumCPUChange, s.MinimumMemoryChange} {
			if q != nil && q.Sign() < 0 {
				return fmt.Errorf("minimum change cannot be negative")
			}
		}
	}
	return nil
}
