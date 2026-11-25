package webhook

import (
	"testing"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/capturedlimits"
	"github.com/optipod/optipod/internal/coexist"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestCapturedLimitsRespectTemplateChangesAndResourceOwnership(t *testing.T) {
	snapshot, err := capturedlimits.Parse(`{"workloadUID":"original","containers":{"app":{"field":"containers","template":{"cpu":"1","memory":"100Mi"},"limits":{"memory":"200Mi"}},"agent":{"field":"containers","template":null,"limits":{}}}}`)
	if err != nil {
		t.Fatal(err)
	}
	declared := corev1.Container{Name: "app", Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1000m"), corev1.ResourceMemory: resource.MustParse("110Mi")}}}
	template := corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{declared}}}
	pod := &corev1.Pod{Spec: *template.Spec.DeepCopy()}
	pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{Name: "agent"})
	p := &api.OptimizationPolicy{}
	p.Spec.UpdateStrategy.UpdateRequestsOnly = true
	r := restoreCapturedLimits(pod, &template, nil, snapshot, p, nil)
	if len(r) != 1 || !r[0].RemoveCPULimit || r[0].MemoryLimit != nil || r[0].RemoveMemoryLimit {
		t.Fatalf("changed template or unselected injector affected: %+v", r)
	}
	p.Spec.ResourcePolicy = &api.ResourcePolicy{IncludeInjectedSidecars: true}
	restricted := &coexist.Restrictions{Resources: map[string]map[string]string{"app": {"cpu": "HPA"}}}
	r = restoreCapturedLimits(pod, &template, nil, snapshot, p, restricted)
	if len(r) != 1 || r[0].ContainerName != "agent" || !r[0].RemoveCPULimit || !r[0].RemoveMemoryLimit {
		t.Fatalf("unexpected controlled dimensions: %+v", r)
	}
}

func TestCapturedUtilizationRequest(t *testing.T) {
	snapshot, err := capturedlimits.Parse(`{"workloadUID":"owner","containers":{"app":{"field":"containers","template":{"cpu":"1"},"limits":{},"templateRequests":{"cpu":"100m"},"hpaRequests":{"cpu":"35m"}}}}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name        string
		templateCPU string
		restricted  bool
		rollout     api.RolloutStrategyType
		restored    bool
	}{
		{"matching utilization denominator", "0.1", true, api.RolloutOnNextRestart, true},
		{"user changed template", "120m", true, api.RolloutOnNextRestart, false},
		{"utilization target removed", "100m", false, api.RolloutOnNextRestart, false},
		{"immediate rollout", "100m", true, api.RolloutImmediate, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := corev1.Container{Name: "app", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(tc.templateCPU)}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")}}}
			template := corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{app}}}
			p := &api.OptimizationPolicy{}
			p.Spec.UpdateStrategy.UpdateRequestsOnly = true
			rollout := string(tc.rollout)
			p.Spec.UpdateStrategy.RolloutStrategy = &rollout
			var ownership *coexist.Restrictions
			if tc.restricted {
				ownership = &coexist.Restrictions{Resources: map[string]map[string]string{"app": {"cpu": "HPA"}}}
			}
			recs := restoreCapturedLimits(&corev1.Pod{Spec: *template.Spec.DeepCopy()}, &template, nil, snapshot, p, ownership)
			restored := len(recs) > 0 && recs[0].CPU != nil
			if restored != tc.restored {
				t.Fatalf("unexpected request restoration %+v", recs)
			}
			if restored && (recs[0].CPU.MilliValue() != 35 || !recs[0].RemoveCPULimit) {
				t.Fatalf("incorrect protected resources %+v", recs)
			}
		})
	}
}
