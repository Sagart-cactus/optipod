package constraints

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestBudgetUsesConcurrentDemandAndRespectsMissingDimensions(t *testing.T) {
	requests := func(cpu, mem string) v1.ResourceList {
		return v1.ResourceList{v1.ResourceCPU: resource.MustParse(cpu), v1.ResourceMemory: resource.MustParse(mem)}
	}
	p := &v1.Pod{Spec: v1.PodSpec{Containers: []v1.Container{{Name: "a", Resources: v1.ResourceRequirements{Requests: requests("10m", "4Mi")}}, {Name: "b"}}, Resources: &v1.ResourceRequirements{Requests: requests("30m", "40Mi"), Limits: requests("50m", "64Mi")}}}
	updates := map[string]map[string]any{"a": {"requests": map[string]any{"cpu": "1m", "memory": "12Mi"}}, "b": {"requests": map[string]any{"cpu": "26m"}}}
	next, err := BudgetCandidate(p, updates)
	if err != nil {
		t.Fatal(err)
	}
	if next.Spec.Resources.Requests.Cpu().MilliValue() != 36 || next.Spec.Resources.Requests.Memory().Cmp(resource.MustParse("40Mi")) != 0 {
		t.Fatal(next.Spec.Resources)
	}
	updates["b"]["requests"].(map[string]any)["memory"] = "80Mi"
	next, err = BudgetCandidate(p, updates)
	if err != nil || next.Spec.Resources.Requests.Memory().Cmp(resource.MustParse("64Mi")) != 0 {
		t.Fatal(next, err)
	}
	if p.Spec.Resources.Requests.Cpu().MilliValue() != 30 || next.Spec.Containers[1].Resources.Requests != nil {
		t.Fatal("input or container reservations changed")
	}
}
