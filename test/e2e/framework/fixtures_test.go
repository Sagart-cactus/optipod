package framework

import (
	"strings"
	"testing"
	"time"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
)

func TestUniqueNameGeneratesDistinctDNSSafeNames(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		name, err := UniqueName("spec")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(name, "spec-") || !dns1123Prefix.MatchString(name) {
			t.Fatalf("name %q not DNS-safe with prefix", name)
		}
		if seen[name] {
			t.Fatalf("duplicate name %q", name)
		}
		seen[name] = true
	}
}

func TestUniqueNameRejectsBadPrefixes(t *testing.T) {
	for _, prefix := range []string{"", "-bad", "Bad", strings.Repeat("a", 41), "bad_prefix"} {
		if _, err := UniqueName(prefix); err == nil {
			t.Errorf("expected error for prefix %q", prefix)
		}
	}
}

func TestNormalizePrefix(t *testing.T) {
	cases := map[string]string{
		"Recommend emits annotations": "recommend-emits-annotations",
		"SSA/apply (exact)":           "ssa-apply-exact",
		"":                            "spec",
		strings.Repeat("Long", 30):    strings.Repeat("long", 10),
	}
	for in, want := range cases {
		if got := NormalizePrefix(in); got != want {
			t.Errorf("NormalizePrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWorkloadFixturesUsePinnedImageAndUniqueLabels(t *testing.T) {
	containers := []ContainerFixture{{Name: "app", CPURequest: "100m", MemoryRequest: "128Mi"}}

	dep := NewDeploymentFixture("ns-x", "web-abcd1234", 2, containers...)
	if dep.Spec.Template.Spec.Containers[0].Image != WorkloadImage() {
		t.Errorf("deployment image = %q, want pinned %q",
			dep.Spec.Template.Spec.Containers[0].Image, WorkloadImage())
	}
	if !strings.Contains(WorkloadImage(), "@sha256:") {
		t.Error("workload image is not digest-pinned")
	}
	if *dep.Spec.Replicas != 2 {
		t.Errorf("replicas = %d", *dep.Spec.Replicas)
	}
	if dep.Spec.Selector.MatchLabels["app"] != "web-abcd1234" ||
		dep.Spec.Template.Labels["app"] != "web-abcd1234" {
		t.Error("selector/template labels inconsistent")
	}
	if got := dep.Spec.Template.Spec.Containers[0].Resources.Requests.Cpu().String(); got != "100m" {
		t.Errorf("cpu request = %q", got)
	}

	sts := NewStatefulSetFixture("ns-x", "db-abcd1234", 1, containers...)
	if sts.Spec.ServiceName != "db-abcd1234" {
		t.Errorf("statefulset serviceName = %q", sts.Spec.ServiceName)
	}

	ds := NewDaemonSetFixture("ns-x", "agent-abcd1234", containers...)
	if ds.Spec.Selector.MatchLabels["app"] != "agent-abcd1234" {
		t.Error("daemonset selector mismatch")
	}
}

func TestWebhookEnabledContainerLabelsPodTemplate(t *testing.T) {
	dep := NewDeploymentFixture("ns-x", "web-x", 1,
		ContainerFixture{Name: "app", CPURequest: "100m", MemoryRequest: "128Mi", WebhookEnabled: true})
	if dep.Spec.Template.Labels["optipod.io/webhook-enabled"] != "true" {
		t.Error("eligible pod template lacks the production webhook-enabling label")
	}
	plain := NewDeploymentFixture("ns-x", "web-y", 1,
		ContainerFixture{Name: "app", CPURequest: "100m", MemoryRequest: "128Mi"})
	if _, ok := plain.Spec.Template.Labels["optipod.io/webhook-enabled"]; ok {
		t.Error("ineligible pod template unexpectedly carries the webhook label")
	}
}

func TestPolicyFixtureDefaultsAndOptions(t *testing.T) {
	p := NewPolicyFixture("ns-x", "policy-1", optipodv1alpha1.ModeRecommend)
	if p.Spec.MetricsConfig.Provider != "prometheus" {
		t.Errorf("provider = %q", p.Spec.MetricsConfig.Provider)
	}
	if p.Spec.Selector.Namespaces.Allow[0] != "ns-x" {
		t.Errorf("namespace allow = %v", p.Spec.Selector.Namespaces.Allow)
	}
	if *p.Spec.UpdateStrategy.Strategy != "webhook" {
		t.Errorf("default strategy = %q", *p.Spec.UpdateStrategy.Strategy)
	}
	if p.Spec.ResourceBounds.CPU.Min.String() != "50m" {
		t.Errorf("cpu min = %s", p.Spec.ResourceBounds.CPU.Min.String())
	}

	custom := NewPolicyFixture("ns-x", "policy-2", optipodv1alpha1.ModeAuto,
		WithStrategy("ssa", ""),
		WithWeight(250),
		WithResourceBounds("100m", "2", "128Mi", "4Gi"),
		WithWorkloadSelector(map[string]string{"app": "web"}),
		WithReconciliationInterval(30*time.Second),
	)
	if *custom.Spec.UpdateStrategy.Strategy != "ssa" {
		t.Errorf("strategy = %q", *custom.Spec.UpdateStrategy.Strategy)
	}
	if *custom.Spec.Weight != 250 {
		t.Errorf("weight = %d", *custom.Spec.Weight)
	}
	if custom.Spec.ResourceBounds.Memory.Max.String() != "4Gi" {
		t.Errorf("mem max = %s", custom.Spec.ResourceBounds.Memory.Max.String())
	}
	if custom.Spec.Selector.WorkloadSelector.MatchLabels["app"] != "web" {
		t.Error("workload selector not applied")
	}
	if custom.Spec.ReconciliationInterval.Duration != 30*time.Second {
		t.Errorf("reconciliation interval = %v, want 30s", custom.Spec.ReconciliationInterval.Duration)
	}
}
