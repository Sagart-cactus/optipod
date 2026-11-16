package console

import (
	"testing"

	api "github.com/optipod/optipod/api/v1alpha1"
	"sigs.k8s.io/yaml"
)

func TestFirstPolicyIsScopedRecommend(t *testing.T) {
	r := Registration{ID: "qa", Namespaces: []string{"app"}, MetricsSource: "prometheus"}
	input := FirstPolicy{ClusterID: "qa", Name: "first-policy", Namespace: "app", LabelKey: "app", LabelValue: "test", CPUMin: "50m", CPUMax: "2", MemoryMin: "64Mi", MemoryMax: "2Gi"}
	output, err := RecommendPolicy(input, r)
	if err != nil {
		t.Fatal(err)
	}
	var p api.OptimizationPolicy
	if err = yaml.Unmarshal([]byte(output), &p); err != nil {
		t.Fatal(err)
	}
	if err = p.ValidateCreate(); err != nil {
		t.Fatal(err)
	}
	if p.Spec.Mode != api.ModeRecommend || p.Spec.UpdateStrategy.AllowRecreate {
		t.Fatal("unsafe first policy")
	}
	input.Namespace = "other"
	if _, err = RecommendPolicy(input, r); err == nil {
		t.Fatal("policy escaped enrollment scope")
	}
	input.Namespace = "app"
	input.CPUMax = "1m"
	if _, err = RecommendPolicy(input, r); err == nil {
		t.Fatal("invalid resource bounds accepted")
	}
}
