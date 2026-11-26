package v1alpha1

// WorkloadSettings preserves workload-specific exceptions to a shared policy.
// UID activation supports staged handoffs. Policy activation follows the elected selector.
// +kubebuilder:validation:XValidation:rule="!(has(self.policyActivation) && self.policyActivation && has(self.requireActivation) && self.requireActivation)",message="choose one activation mode"
// +kubebuilder:validation:XValidation:rule="!has(self.byWorkload) || (has(self.policyActivation) && self.policyActivation)",message="named exceptions require policy activation"
// +kubebuilder:validation:XValidation:rule="!(has(self.policyActivation) && self.policyActivation && has(self.byUID) && size(self.byUID) > 0)",message="UID settings cannot be combined with policy activation"
type WorkloadSettings struct {
	// PolicyActivation opts elected Auto workloads into webhook admission.
	PolicyActivation bool `json:"policyActivation,omitempty"`
	// ByWorkload holds exceptions keyed by namespace/kind/name.
	ByWorkload map[string]NamedWorkloadSetting `json:"byWorkload,omitempty"`
	// RequireActivation keeps matched workloads in Recommend until explicitly active.
	RequireActivation bool                       `json:"requireActivation,omitempty"`
	ByUID             map[string]WorkloadSetting `json:"byUID,omitempty"`
}

type NamedWorkloadSetting struct {
	// RecommendOnly prevents this workload from receiving automatic changes.
	RecommendOnly bool `json:"recommendOnly,omitempty"`
	// OperatorOwned allows deferred request-only admission for this operator-owned workload.
	OperatorOwned bool `json:"operatorOwned,omitempty"`
	// +kubebuilder:validation:Enum=immediate;onNextRestart
	RolloutStrategy         RolloutStrategyType `json:"rolloutStrategy,omitempty"`
	IncludeInjectedSidecars *bool               `json:"includeInjectedSidecars,omitempty"`
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
	return p.ForWorkloadRef(uid, "", "", "")
}

// ForWorkloadRef applies workload exceptions after a policy wins selector precedence.
func (p *OptimizationPolicy) ForWorkloadRef(uid, namespace, kind, name string) *OptimizationPolicy {
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
	sidecars := setting.IncludeInjectedSidecars
	if settings.PolicyActivation && !settings.RequireActivation && namespace != "" && kind != "" && name != "" && uid != "" {
		override := settings.ByWorkload[namespace+"/"+kind+"/"+name]
		if override.RecommendOnly && effective.Spec.Mode == ModeAuto {
			effective.Spec.Mode = ModeRecommend
		}
		effective.policyWebhookEnabled = effective.Spec.Mode == ModeAuto
		effective.operatorOwnedAllowed = effective.policyWebhookEnabled && override.OperatorOwned
		if override.RolloutStrategy != "" {
			rollout := string(override.RolloutStrategy)
			effective.Spec.UpdateStrategy.RolloutStrategy = &rollout
		}
		if effective.GetRolloutStrategy() == RolloutOnNextRestart {
			effective.Spec.UpdateStrategy.AllowRecreate = false
		}
		sidecars = override.IncludeInjectedSidecars
	} else if setting.Deferred {
		rollout := string(RolloutOnNextRestart)
		effective.Spec.UpdateStrategy.RolloutStrategy = &rollout
		effective.Spec.UpdateStrategy.AllowRecreate = false
	}
	if sidecars != nil {
		resourcePolicy := p.Spec.ResourcePolicy.DeepCopy()
		if resourcePolicy == nil {
			resourcePolicy = &ResourcePolicy{}
		}
		resourcePolicy.IncludeInjectedSidecars = *sidecars
		effective.Spec.ResourcePolicy = resourcePolicy
	}
	return &effective
}

// WorkloadActivated reports an explicit opt-in on this resolved policy only.
func (p *OptimizationPolicy) WorkloadActivated() bool { return p.workloadActivated }

// AllowsPolicyWebhook reports whether the elected policy opts the workload into admission.
func (p *OptimizationPolicy) AllowsPolicyWebhook() bool {
	return p.workloadActivated || p.policyWebhookEnabled
}

// AllowsOperatorOwnedAdmission authorizes only deferred, request-only webhook sizing.
func (p *OptimizationPolicy) AllowsOperatorOwnedAdmission() bool {
	return (p.workloadActivated || p.operatorOwnedAllowed) && p.IsWebhookStrategy() && p.GetRolloutStrategy() == RolloutOnNextRestart && p.Spec.UpdateStrategy.UpdateRequestsOnly
}
