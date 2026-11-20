package suite

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Policy work queue", Label("policy-queue"), Serial, func() {
	It("reconciles pre-existing quiet policies while other policy generations change", func(ctx context.Context) {
		ns := specNamespace(ctx, "policy-queue")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		deployments := env.Clients.Kube.AppsV1().Deployments(framework.SystemNamespace)
		deployment, err := deployments.Get(ctx, framework.ControllerDeploymentName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		originalReplicas := *deployment.Spec.Replicas
		scale := func(ctx context.Context, replicas int32) {
			patch := []byte(fmt.Sprintf(`{"spec":{"replicas":%d}}`, replicas))
			_, err := deployments.Patch(ctx, deployment.Name, types.MergePatchType, patch, metav1.PatchOptions{})
			Expect(err).NotTo(HaveOccurred())
		}
		DeferCleanup(func(ctx context.Context) { scale(ctx, originalReplicas) })
		scale(ctx, 0)
		Eventually(func() bool {
			current, err := deployments.Get(ctx, deployment.Name, metav1.GetOptions{})
			return err == nil && current.Status.ObservedGeneration == current.Generation && current.Status.Replicas == 0
		}, time.Minute, time.Second).Should(BeTrue())

		const policyCount = 8
		policies := make([]*api.OptimizationPolicy, policyCount)
		for i := range policies {
			policies[i] = framework.NewPolicyFixture(ns.Name, fmt.Sprintf("policy-%d", i), api.ModeRecommend)
			Expect(env.Clients.Ctrl.Create(ctx, policies[i])).To(Succeed())
		}
		scale(ctx, originalReplicas)
		updates := 0
		Eventually(func() bool {
			// Keep the first two keys changing. The remaining six only enter the
			// worker's queue through the initial list when leadership starts.
			percentile := "P90"
			if (updates/2)%2 == 0 {
				percentile = "P95"
			}
			patch := client.RawPatch(types.MergePatchType, []byte(fmt.Sprintf(`{"spec":{"metricsConfig":{"percentile":%q}}}`, percentile)))
			Expect(env.Clients.Ctrl.Patch(ctx, policies[updates%2], patch)).To(Succeed())
			updates++
			current := &api.OptimizationPolicyList{}
			Expect(env.Clients.Ctrl.List(ctx, current, client.InNamespace(ns.Name))).To(Succeed())
			for _, p := range current.Items {
				if p.Status.LastReconciliation == nil {
					return false
				}
			}
			return len(current.Items) == policyCount && updates >= 10
		}, 2*time.Minute, 150*time.Millisecond).Should(BeTrue())
	})
})
