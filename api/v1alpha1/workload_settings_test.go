package v1alpha1

import "testing"

func TestPolicyActivation(t *testing.T) {
	const key = "app/Deployment/api"
	p := &OptimizationPolicy{Spec: OptimizationPolicySpec{
		Mode: ModeAuto, UpdateStrategy: UpdateStrategy{UpdateRequestsOnly: true},
		WorkloadSettings: &WorkloadSettings{PolicyActivation: true, ByWorkload: map[string]NamedWorkloadSetting{
			key: {OperatorOwned: true}, "app/Deployment/hold": {RecommendOnly: true},
		}},
	}}
	for _, uid := range []string{"original", "replacement"} {
		for _, tc := range []struct {
			ns, kind, name    string
			webhook, operator bool
			mode              PolicyMode
		}{
			{"app", "Deployment", "api", true, true, ModeAuto},
			{"different", "Deployment", "api", true, false, ModeAuto},
			{"app", "StatefulSet", "api", true, false, ModeAuto},
			{"app", "Deployment", "new", true, false, ModeAuto},
			{"app", "Deployment", "hold", false, false, ModeRecommend},
		} {
			got := p.ForWorkloadRef(uid, tc.ns, tc.kind, tc.name)
			if got.Spec.Mode != tc.mode || got.AllowsPolicyWebhook() != tc.webhook || got.AllowsOperatorOwnedAdmission() != tc.operator || got.WorkloadActivated() {
				t.Fatalf("%s/%s/%s: unexpected resolved privileges", tc.ns, tc.kind, tc.name)
			}
		}
	}
	for _, mode := range []PolicyMode{ModeRecommend, ModeDisabled} {
		p.Spec.Mode = mode
		got := p.ForWorkloadRef("original", "app", "Deployment", "api")
		if got.AllowsPolicyWebhook() || got.AllowsOperatorOwnedAdmission() || got.Spec.Mode != mode {
			t.Fatalf("elevated %s policy", mode)
		}
	}
	p.Spec.Mode = ModeAuto
	p.Spec.WorkloadSettings = &WorkloadSettings{RequireActivation: true, ByUID: map[string]WorkloadSetting{"original": {Active: true, Deferred: true}}}
	if !p.ForWorkload("original").AllowsOperatorOwnedAdmission() || p.ForWorkload("replacement").Spec.Mode != ModeRecommend {
		t.Fatal("staged handoff lost UID isolation")
	}
}
