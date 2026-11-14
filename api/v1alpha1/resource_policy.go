package v1alpha1

import (
	"fmt"

	"gopkg.in/inf.v0"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	LimitPreserve   = "Preserve"
	LimitNone       = "None"
	LimitMultiplier = "Multiplier"
	LimitAutomatic  = "Automatic"
)

// AutomaticMemoryLimit never lowers a current limit. Decimal arithmetic avoids
// truncating fractional bytes or overflowing a large Kubernetes quantity.
func AutomaticMemoryLimit(request resource.Quantity, existing *resource.Quantity) resource.Quantity {
	d := new(inf.Dec).Mul(request.AsDec(), inf.NewDec(15, 1))
	d.Round(d, 0, inf.RoundCeil)
	limit := resource.NewDecimalQuantity(*d, resource.BinarySI)
	minimum := resource.MustParse("128Mi")
	if limit.Cmp(minimum) < 0 {
		*limit = minimum
	}
	if existing != nil && limit.Cmp(*existing) < 0 {
		*limit = existing.DeepCopy()
	}
	return *limit
}

// +kubebuilder:validation:Enum=cpu;memory
type ControlledResource string

type ResourcePolicy struct {
	// Include restartable init containers; one-shot init containers are never optimized.
	// +optional
	IncludeNativeSidecars bool `json:"includeNativeSidecars,omitempty"`
	// Discover containers injected into owned pods. Requires webhook strategy.
	// +optional
	IncludeInjectedSidecars bool `json:"includeInjectedSidecars,omitempty"`
	// An omitted list controls both CPU and memory.
	// +optional
	ControlledResources []ControlledResource `json:"controlledResources,omitempty"`
	// ExcludedContainers contains exact names, including injected sidecars.
	// +optional
	ExcludedContainers []string `json:"excludedContainers,omitempty"`
}

func (p *OptimizationPolicy) ControlsResource(name string) bool {
	if p.Spec.ResourcePolicy == nil || len(p.Spec.ResourcePolicy.ControlledResources) == 0 {
		return true
	}
	for _, r := range p.Spec.ResourcePolicy.ControlledResources {
		if string(r) == name {
			return true
		}
	}
	return false
}
func (p *OptimizationPolicy) ExcludesContainer(name string) bool {
	if p.Spec.ResourcePolicy != nil {
		for _, excluded := range p.Spec.ResourcePolicy.ExcludedContainers {
			if excluded == name {
				return true
			}
		}
	}
	return false
}
func (p *OptimizationPolicy) LimitMode(cpu bool) string {
	mode := ""
	if c := p.Spec.UpdateStrategy.LimitConfig; c != nil {
		if cpu {
			mode = c.CPUMode
		} else {
			mode = c.MemoryMode
		}
	}
	if mode != "" {
		return mode
	}
	if p.Spec.UpdateStrategy.UpdateRequestsOnly {
		return LimitPreserve
	}
	return LimitMultiplier
}
func (p *OptimizationPolicy) validateResourcePolicy() error {
	if r := p.Spec.ResourcePolicy; r != nil {
		for _, name := range r.ControlledResources {
			if name != "cpu" && name != "memory" {
				return fmt.Errorf("invalid controlled resource %q", name)
			}
		}
	}
	for _, cpu := range []bool{true, false} {
		switch p.LimitMode(cpu) {
		case LimitPreserve, LimitNone, LimitMultiplier:
		case LimitAutomatic:
			if cpu {
				return fmt.Errorf("automatic limits are supported only for memory")
			}
		default:
			return fmt.Errorf("invalid limit mode")
		}
	}
	return nil
}

func (p *OptimizationPolicy) IncludesNativeSidecars() bool {
	return p.Spec.ResourcePolicy != nil && p.Spec.ResourcePolicy.IncludeNativeSidecars
}
func (p *OptimizationPolicy) IncludesInjectedSidecars() bool {
	return p.Spec.ResourcePolicy != nil && p.Spec.ResourcePolicy.IncludeInjectedSidecars
}
