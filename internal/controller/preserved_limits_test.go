package controller

import (
	"strings"
	"testing"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/capturedlimits"
	"github.com/optipod/optipod/internal/coexist"
	"github.com/optipod/optipod/internal/discovery"
	"github.com/optipod/optipod/internal/safety"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPreservedLimitCap(t *testing.T) {
	quantity := func(s string) *resource.Quantity {
		q := resource.MustParse(s)
		return &q
	}
	const capture = `{"workloadUID":"workload","containers":{"app":{"field":"containers","template":{"cpu":"200m"},"limits":{"cpu":"300m","memory":"256Mi"}}}}`
	const removal = `{"workloadUID":"workload","containers":{"app":{"field":"containers","template":{"cpu":"200m"},"limits":{"cpu":"300m"}}}}`
	for _, tc := range []struct {
		name, capture, templateMemory, liveMemory       string
		minCPU, minMemory, jvmFloor, oomFloor, hpaFloor string
		wantCPU, wantMemory, wantRawCPU, wantRawMemory  string
		restrictions                                    *coexist.Restrictions
		excluded, noLimit, wholePod, preserveDecrease   bool
	}{
		{
			name: "captured limits override matching declaration", capture: capture,
			wantCPU: "300m", wantMemory: "256Mi", wantRawCPU: "500m", wantRawMemory: "600Mi",
		},
		{
			name: "live admission default constrains recommendation", liveMemory: "192Mi",
			wantCPU: "200m", wantMemory: "192Mi", wantRawCPU: "500m", wantRawMemory: "600Mi",
		},
		{
			name: "capture explicitly removes a previous limit", capture: removal, liveMemory: "192Mi",
			wantCPU: "300m", wantMemory: "600Mi", wantRawCPU: "500m",
		},
		{
			name: "changed template applies independently", capture: capture, templateMemory: "384Mi",
			wantCPU: "300m", wantMemory: "384Mi", wantRawCPU: "500m", wantRawMemory: "600Mi",
		},
		{
			name: "no ceiling on an unset dimension", wantCPU: "200m", wantMemory: "600Mi", wantRawCPU: "500m",
		},
		{
			name: "CPU minimum blocks only CPU", capture: capture, minCPU: "350m",
			wantMemory: "256Mi", wantRawCPU: "500m", wantRawMemory: "600Mi",
		},
		{
			name: "memory minimum blocks only memory", capture: capture, minMemory: "300Mi",
			wantCPU: "300m", wantRawCPU: "500m", wantRawMemory: "600Mi",
		},
		{
			name: "JVM floor cannot be capped", capture: capture, jvmFloor: "300Mi",
			wantCPU: "300m", wantRawCPU: "500m", wantRawMemory: "600Mi",
		},
		{
			name: "HPA replay floor cannot be capped", capture: capture, hpaFloor: "350m",
			wantMemory: "256Mi", wantRawCPU: "500m", wantRawMemory: "600Mi",
		},
		{
			name: "OOM floor cannot be capped", capture: capture, oomFloor: "300Mi",
			wantCPU: "300m", wantRawCPU: "500m", wantRawMemory: "600Mi",
		},
		{
			name: "safe OOM target still caps above its floor", capture: capture, oomFloor: "192Mi",
			wantCPU: "300m", wantMemory: "256Mi", wantRawCPU: "500m", wantRawMemory: "600Mi",
		},
		{
			name: "memory decrease guard precedes cap", capture: capture, preserveDecrease: true,
			wantCPU: "300m", wantRawCPU: "500m", wantRawMemory: "600Mi",
		},
		{
			name: "whole Pod budget is separate", capture: capture, wholePod: true, wantCPU: "500m", wantMemory: "600Mi",
		},
		{
			name: "excluded container is untouched", capture: capture, excluded: true, wantCPU: "500m", wantMemory: "600Mi",
		},
		{
			name: "explicit CPU limit removal bypasses cap", capture: capture, noLimit: true,
			wantCPU: "500m", wantMemory: "256Mi", wantRawMemory: "600Mi",
		},
		{
			name: "horizontal scaler-owned resource is untouched", capture: capture,
			restrictions: &coexist.Restrictions{Resources: map[string]map[string]string{"app": {"cpu": "HPA"}}},
			wantCPU:      "500m", wantMemory: "256Mi", wantRawMemory: "600Mi",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deferred := string(api.RolloutOnNextRestart)
			p := &api.OptimizationPolicy{Spec: api.OptimizationPolicySpec{
				UpdateStrategy: api.UpdateStrategy{UpdateRequestsOnly: true, RolloutStrategy: &deferred},
			}}
			if tc.capture != "" {
				p.Annotations = map[string]string{capturedlimits.Annotation: tc.capture}
			}
			if tc.excluded {
				p.Spec.ResourcePolicy = &api.ResourcePolicy{ExcludedContainers: []string{"app"}}
			}
			if tc.noLimit {
				p.Spec.UpdateStrategy.LimitConfig = &api.LimitConfig{CPUMode: api.LimitNone}
			}
			if tc.minCPU != "" {
				p.Spec.ResourceBounds.CPU.Min = *quantity(tc.minCPU)
			}
			if tc.minMemory != "" {
				p.Spec.ResourceBounds.Memory.Min = *quantity(tc.minMemory)
			}
			floors := corev1.ResourceList{}
			if tc.jvmFloor != "" {
				floors[corev1.ResourceMemory] = *quantity(tc.jvmFloor)
			}
			if tc.hpaFloor != "" {
				floors[corev1.ResourceCPU] = *quantity(tc.hpaFloor)
			}
			dep := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{UID: "workload"},
				Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{Containers: []corev1.Container{{
						Name: "app", Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{corev1.ResourceCPU: *quantity("200m")}},
					}}},
				}},
			}
			if tc.templateMemory != "" {
				dep.Spec.Template.Spec.Containers[0].Resources.Limits[corev1.ResourceMemory] = *quantity(tc.templateMemory)
			}
			if tc.wholePod {
				dep.Spec.Template.Spec.Resources = &corev1.ResourceRequirements{}
			}
			state := &safety.State{}
			if tc.liveMemory != "" {
				pod := corev1.Pod{Spec: *dep.Spec.Template.Spec.DeepCopy()}
				pod.Spec.Containers[0].Resources.Limits[corev1.ResourceMemory] = *quantity(tc.liveMemory)
				state.Pods = []corev1.Pod{pod}
			}
			if tc.preserveDecrease {
				state.Containers = map[string]*safety.ContainerState{"app": {InitialMemory: *quantity("320Mi")}}
				state.Current = map[string]corev1.ResourceRequirements{"app": {Requests: corev1.ResourceList{corev1.ResourceMemory: *quantity("320Mi")}}}
			}
			recs := []api.ContainerRecommendation{{Container: "app", CPU: quantity("500m"), Memory: quantity("600Mi"), Explanation: "test"}}
			if tc.oomFloor != "" {
				recs[0].MinimumMemoryLimit, recs[0].Emergency = quantity(tc.oomFloor), true
			}
			wp := &WorkloadProcessor{}
			if err := wp.capPreservedLimits(t.Context(), &discovery.Workload{Kind: "Deployment", Object: dep}, p, state, tc.restrictions, recs, map[string]corev1.ResourceList{"app": floors}); err != nil {
				t.Fatal(err)
			}
			for _, result := range []struct {
				name string
				got  *resource.Quantity
				want string
			}{
				{"CPU", recs[0].CPU, tc.wantCPU}, {"memory", recs[0].Memory, tc.wantMemory},
				{"raw CPU", recs[0].RawCPU, tc.wantRawCPU}, {"raw memory", recs[0].RawMemory, tc.wantRawMemory},
			} {
				got := ""
				if result.got != nil {
					got = result.got.String()
				}
				if got != result.want {
					t.Errorf("%s = %q, want %q", result.name, got, result.want)
				}
			}
			if recs[0].Memory == nil && (!strings.Contains(recs[0].Explanation, "left unchanged") || recs[0].Emergency || recs[0].MinimumMemoryLimit != nil) {
				t.Errorf("blocked dimension lost its explanation or retained an emergency: %+v", recs[0])
			}
		})
	}
}
