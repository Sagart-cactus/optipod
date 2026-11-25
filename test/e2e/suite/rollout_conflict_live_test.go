package suite

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/webhook"
	"github.com/optipod/optipod/test/e2e/framework"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type rolloutConflictLiveClient struct {
	client.Client
	policy      *api.OptimizationPolicy
	updates     int
	sawConflict bool
}

// Keep policy selection deterministic while the injected competing workload
// update and the restart itself go through the real API server.
func (c *rolloutConflictLiveClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if policies, ok := list.(*api.OptimizationPolicyList); ok {
		policies.Items = []api.OptimizationPolicy{*c.policy}
		return nil
	}
	return c.Client.List(ctx, list, opts...)
}

func (c *rolloutConflictLiveClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	if dep, ok := obj.(*appsv1.Deployment); ok {
		c.updates++
		if c.updates == 1 {
			concurrent := &appsv1.Deployment{}
			if err := c.Get(ctx, client.ObjectKeyFromObject(dep), concurrent); err != nil {
				return err
			}
			before := concurrent.DeepCopy()
			concurrent.Annotations["fixture.example/conflict"] = "retained"
			if err := c.Patch(ctx, concurrent, client.MergeFrom(before)); err != nil {
				return err
			}
		}
	}
	err := c.Client.Update(ctx, obj, opts...)
	c.sawConflict = c.sawConflict || apierrors.IsConflict(err)
	return err
}

var _ = Describe("Production restart writes after a real API conflict", Label("conformance", "rollout", "rollout-conflict-live"), func() {
	It("retains a concurrent write, applies one marker and gets a new Ready Pod", func(ctx context.Context) {
		ns := specNamespace(ctx, "restart-conflict")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		dep := framework.NewDeploymentFixture(ns.Name, "service", 1, smokeContainers(smokeSamples()[:1])...)
		Expect(env.Clients.Ctrl.Create(ctx, dep)).To(Succeed())
		initial, err := framework.WaitForPodCountReady(ctx, env.Clients, ns.Name, "app=service", 1, framework.WaitOptions{Timeout: rolloutWaitTimeout})
		Expect(err).NotTo(HaveOccurred())
		p := framework.NewPolicyFixture(ns.Name, "test-policy", api.ModeAuto)
		p.UID = "conflict-test-policy"
		p.Generation = 1
		rec := resource.MustParse("125m")
		recs := []webhook.ResourceRecommendation{{ContainerName: "app", CPU: &rec}}
		Expect(webhook.NewAnnotationManager(env.Clients.Ctrl).StoreRecommendations(ctx, dep, recs, p)).To(Succeed())
		ready := webhook.NewRolloutController(env.Clients.Ctrl)
		Eventually(func(g Gomega) {
			g.Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(dep), dep)).To(Succeed())
			g.Expect(ready.IsWorkloadReady(dep)).To(BeTrue())
		}).WithTimeout(time.Minute).Should(Succeed())
		// The workload processor records the ready spec before application.
		// Preserve that same proof when calling the rollout operation directly.
		Expect(webhook.MarkReadySpec(ctx, env.Clients.Ctrl, dep)).To(Succeed())

		hooked := &rolloutConflictLiveClient{Client: env.Clients.Ctrl, policy: p}
		restarted, err := webhook.NewRolloutController(hooked).TriggerRecommendationRollout(ctx, dep, p, recs)
		Expect(err).NotTo(HaveOccurred())
		Expect(restarted).To(BeTrue())
		Expect(hooked.sawConflict).To(BeTrue())
		Expect(hooked.updates).To(Equal(2))
		live := &appsv1.Deployment{}
		Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(dep), live)).To(Succeed())
		Expect(live.Annotations["fixture.example/conflict"]).To(Equal("retained"))
		Expect(live.Annotations[webhook.RolloutHash]).To(Equal(live.Annotations[webhook.RecommendationHash]))
		marker := live.Spec.Template.Annotations[ready.GetRestartAnnotationKey()]
		Expect(marker).NotTo(BeEmpty())
		restarted, err = webhook.NewRolloutController(hooked).TriggerRecommendationRollout(ctx, dep, p, recs)
		Expect(err).NotTo(HaveOccurred())
		Expect(restarted).To(BeFalse(), "the API already committed this rollout plan")
		Expect(hooked.updates).To(Equal(2), "replaying the accepted plan does not write another marker")
		Eventually(func(g Gomega) {
			pods, err := framework.WaitForPodCountReady(ctx, env.Clients, ns.Name, "app=service", 1, framework.WaitOptions{Timeout: 10 * time.Second})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(pods[0].UID).NotTo(Equal(initial[0].UID))
			g.Expect(pods[0].Annotations[ready.GetRestartAnnotationKey()]).To(Equal(marker))
		}).WithTimeout(time.Minute).Should(Succeed())
	})
})
