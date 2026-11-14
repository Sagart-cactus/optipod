package application

import api "github.com/optipod/optipod/api/v1alpha1"

// Emergency admission plans must affect the running container. They still
// require allowInPlaceResize or allowRecreate and pass all final policy checks.
func emergencyPolicy(p *api.OptimizationPolicy) *api.OptimizationPolicy {
	result := p.DeepCopy()
	immediate := string(api.RolloutImmediate)
	result.Spec.UpdateStrategy.RolloutStrategy = &immediate
	return result
}
