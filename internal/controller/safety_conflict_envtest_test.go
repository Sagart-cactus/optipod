package controller

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/discovery"
	"github.com/optipod/optipod/internal/safety"
)

// concurrentSafetyWriter commits a second writer's checkpoint between the
// processor's read and its optimistic patch, producing a real API conflict.
type concurrentSafetyWriter struct {
	client.Client
	raced bool
}

func (c *concurrentSafetyWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	if !c.raced {
		c.raced = true
		competing := obj.DeepCopyObject().(client.Object)
		checkpoint, err := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{safety.StateAnnotation: `{"containers":{},"planHash":"concurrent-observation"}`}}})
		if err != nil {
			return err
		}
		if err := c.Client.Patch(ctx, competing, client.RawPatch(types.MergePatchType, checkpoint)); err != nil {
			return err
		}
	}
	return c.Client.Patch(ctx, obj, patch, opts...)
}

var _ = Describe("Concurrent safety checkpoints", func() {
	It("recomputes the checkpoint after a workload resourceVersion conflict", func(ctx context.Context) {
		const namespace = "checkpoint-conflict"
		DeferCleanup(createModesWorkloadFixture(ctx, namespace, "app"))
		d := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "app"}, d)).To(Succeed())
		c := &concurrentSafetyWriter{Client: k8sClient}
		wp := WorkloadProcessor{client: c}
		state, err := wp.observeSafety(ctx, &discovery.Workload{Object: d, Namespace: namespace, Kind: "Deployment", Name: "app"}, &api.OptimizationPolicy{}, d.Spec.Template.Spec.Containers)
		Expect(err).NotTo(HaveOccurred())
		Expect(c.raced).To(BeTrue())
		Expect(state.PlanHash).To(Equal("concurrent-observation"))
	})
})
