package capturedlimits

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNodeCaptureChecksIdentity(t *testing.T) {
	const raw = `{"workloadUID":"daemon","nodes":{"node-a":{"uid":"first","containers":{"main":{"field":"containers","template":{},"limits":{},"runningRequests":{}}}}}}`
	s, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithObjects(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a", UID: "first"}}).Build()
	p := &corev1.Pod{Spec: corev1.PodSpec{NodeName: "node-a"}}
	got, err := s.ForPod(context.Background(), c, "DaemonSet", "daemon", p)
	if err != nil || got == nil || got.Containers["main"].RunningRequests == nil {
		t.Fatalf("missing original capture: %v %v", got, err)
	}
	s.Nodes["node-a"] = NodeCapture{UID: "replaced", Containers: got.Containers}
	got, err = s.ForPod(context.Background(), c, "DaemonSet", "daemon", p)
	if err != nil || got != nil {
		t.Fatalf("reused name inherited capture: %v %v", got, err)
	}
	p.Spec.NodeName = ""
	if _, err = s.ForPod(context.Background(), c, "DaemonSet", "daemon", p); err == nil {
		t.Fatal("ambiguous node was accepted")
	}
	if got, err = s.ForPod(context.Background(), c, "DaemonSet", "another", p); err != nil || got != nil {
		t.Fatal("recreated workload inherited capture")
	}
	if _, err = Parse(`{"workloadUID":"daemon","nodes":{"n":{"uid":"one","containers":{"main":{"field":"containers","limits":{}}}}}}`); err == nil {
		t.Fatal("missing running requests accepted as an explicit empty capture")
	}
}
