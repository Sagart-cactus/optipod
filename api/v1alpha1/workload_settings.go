package v1alpha1

// WorkloadSettings preserves workload-specific exceptions to a shared policy.
// Keys are Kubernetes workload UIDs, so replacing a workload requires a new opt-in.
type WorkloadSettings struct {
	// RequireActivation keeps matched workloads in Recommend until explicitly active.
	RequireActivation bool                       `json:"requireActivation,omitempty"`
	ByUID             map[string]WorkloadSetting `json:"byUID,omitempty"`
}

type WorkloadSetting struct {
	Active bool `json:"active,omitempty"`
	// Deferred prevents this workload from being restarted to apply recommendations.
	Deferred                bool  `json:"deferred,omitempty"`
	IncludeInjectedSidecars *bool `json:"includeInjectedSidecars,omitempty"`
}

// ForWorkload resolves policy-writer settings after normal selector precedence.
// No workload setting enables writes on a Recommend or Disabled policy.
func (p *OptimizationPolicy) ForWorkload(uid string) *OptimizationPolicy {
	settings := p.Spec.WorkloadSettings
	if settings == nil {
		return p
	}
	// Copy only the effective settings. Copying the entire roster, status and
	// all other workloads' captures for every admission would scale quadratically.
	effective := *p
	effective.Spec.WorkloadSettings = nil
	setting := settings.ByUID[uid]
	effective.workloadActivated = uid != "" && settings.RequireActivation && setting.Active && effective.Spec.Mode == ModeAuto
	if effective.Spec.Mode == ModeAuto && settings.RequireActivation && !effective.workloadActivated {
		effective.Spec.Mode = ModeRecommend
	}
	if setting.Deferred {
		rollout := string(RolloutOnNextRestart)
		effective.Spec.UpdateStrategy.RolloutStrategy = &rollout
		effective.Spec.UpdateStrategy.AllowRecreate = false
	}
	if setting.IncludeInjectedSidecars != nil {
		resourcePolicy := p.Spec.ResourcePolicy.DeepCopy()
		if resourcePolicy == nil {
			resourcePolicy = &ResourcePolicy{}
		}
		resourcePolicy.IncludeInjectedSidecars = *setting.IncludeInjectedSidecars
		effective.Spec.ResourcePolicy = resourcePolicy
	}
	return &effective
}

// WorkloadActivated reports an explicit opt-in on this resolved policy only.
func (p *OptimizationPolicy) WorkloadActivated() bool { return p.workloadActivated }

// AllowsOperatorOwnedAdmission authorizes replacement-only requests for a UID
// that a policy writer explicitly activated.
func (p *OptimizationPolicy) AllowsOperatorOwnedAdmission() bool {
	return p.WorkloadActivated() && p.IsWebhookStrategy() && p.GetRolloutStrategy() == RolloutOnNextRestart && p.Spec.UpdateStrategy.UpdateRequestsOnly
}
