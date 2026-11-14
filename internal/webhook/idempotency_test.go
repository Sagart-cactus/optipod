package webhook

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestAdmissionResourcePlanIsIdempotent(t *testing.T) {
	cpu, mem := resource.MustParse("250m"), resource.MustParse("128Mi")
	pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: cpu, corev1.ResourceMemory: mem}, Limits: corev1.ResourceList{corev1.ResourceMemory: mem}}}}}}
	recs := []ResourceRecommendation{{ContainerName: "app", CPU: &cpu, Memory: &mem, MemoryLimit: &mem, RemoveCPULimit: true}}
	if patches := (&Mutator{}).generateResourcePatches(pod, recs, "p"); len(patches) != 0 {
		t.Fatalf("reinvocation must not rewrite identical resources: %+v", patches)
	}
}
