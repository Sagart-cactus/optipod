package nodesizing

import (
	"testing"

	api "github.com/optipod/optipod/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestAmbiguousNodeIsRejected(t *testing.T) {
	pod := &corev1.Pod{Spec: corev1.PodSpec{Affinity: &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{{MatchFields: []corev1.NodeSelectorRequirement{{Key: "metadata.name", Operator: corev1.NodeSelectorOpIn, Values: []string{"one"}}}}, {}}}}}}}
	if _, err := NodeName(pod); err == nil {
		t.Fatal("unconstrained alternative must not select a node")
	}
	pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms = pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[:1]
	if name, err := NodeName(pod); err != nil || name != "one" {
		t.Fatalf("exact node: %s %v", name, err)
	}
}
func TestBoundsExclusionsAndNoRequests(t *testing.T) {
	cpu := 50.0
	p := &api.OptimizationPolicy{Spec: api.OptimizationPolicySpec{DaemonSetSizing: &api.DaemonSetSizing{Default: &api.NodePercentages{CPUPercent: &cpu}, Containers: map[string]api.NodePercentages{"explicit": {CPUPercent: &cpu}}}, ResourcePolicy: &api.ResourcePolicy{ExcludedContainers: []string{"excluded"}}, ResourceBounds: api.ResourceBounds{CPU: api.ResourceBound{Min: resource.MustParse("50m"), Max: resource.MustParse("1")}}, UpdateStrategy: api.UpdateStrategy{LimitConfig: &api.LimitConfig{CPUMode: "None"}}}}
	pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")}}}, {Name: "absent"}, {Name: "explicit"}, {Name: "excluded"}}}}
	node := &corev1.Node{Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("8")}}}
	plan, err := ForNode(pod, p, node)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 2 || plan["app"]["requests"].(map[string]any)["cpu"] != "1" || plan["explicit"]["limits"].(map[string]any)["cpu"] != nil {
		t.Fatalf("unexpected plan: %#v", plan)
	}
}
