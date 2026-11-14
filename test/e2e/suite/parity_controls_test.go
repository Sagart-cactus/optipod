package suite

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

var _ = Describe("Parity: resource and container controls", Label("parity", "resource-controls"), func() {
	DescribeTable("preserves excluded resources and containers while applying limit modes", func(ctx context.Context, strategy, controlled string) {
		ns := specNamespace(ctx, "parity-controls")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		samples := smokeSamples()
		dep := framework.NewDeploymentFixture(ns.Name, "controls", 1, smokeWebhookContainers(samples)...)
		for i := range dep.Spec.Template.Spec.Containers {
			dep.Spec.Template.Spec.Containers[i].Resources.Limits = corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("256Mi")}
		}
		Expect(env.Clients.Ctrl.Create(ctx, dep)).To(Succeed())
		_, err := framework.WaitForPodCountReady(ctx, env.Clients, ns.Name, "app=controls", 1, framework.WaitOptions{Timeout: rolloutWaitTimeout})
		Expect(err).NotTo(HaveOccurred())
		series := registerContainerSeries(ctx, ns.Name, dep, samples[:1])
		if controlled == "cpu" {
			series[0].MemoryBytes = nil
		} else {
			series[0].CPUCores = nil
		}
		Expect(env.Fixture.SetSeries(ctx, series)).To(Succeed())
		p := framework.NewPolicyFixture(ns.Name, "controls", api.ModeAuto, framework.WithStrategy(strategy, "immediate"), framework.WithReconciliationInterval(time.Second))
		p.Spec.ResourcePolicy = &api.ResourcePolicy{ControlledResources: []api.ControlledResource{api.ControlledResource(controlled)}, ExcludedContainers: []string{"sidecar"}}
		factor := 2.0
		p.Spec.UpdateStrategy.LimitConfig = &api.LimitConfig{CPUMode: "None", MemoryMode: "Multiplier", MemoryLimitMultiplier: &factor}
		before := podUIDs(ctx, ns.Name, "app=controls")
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		pods := waitForReplacementPod(ctx, ns.Name, "app=controls", before)
		app, sidecar := pods[0].Spec.Containers[0], pods[0].Spec.Containers[1]
		if controlled == "cpu" {
			Expect(app.Resources.Requests.Cpu().MilliValue()).To(Equal(int64(300)))
			Expect(app.Resources.Limits).NotTo(HaveKey(corev1.ResourceCPU))
			Expect(app.Resources.Requests.Memory().String()).To(Equal("64Mi"))
			Expect(app.Resources.Limits.Memory().String()).To(Equal("256Mi"))
		} else {
			Expect(app.Resources.Requests.Memory().String()).To(Equal("120Mi"))
			// The multiplier is subject to the default memory-decrease guard.
			Expect(app.Resources.Limits.Memory().String()).To(Equal("256Mi"))
			Expect(app.Resources.Requests.Cpu().MilliValue()).To(Equal(int64(100)))
			Expect(app.Resources.Limits.Cpu().MilliValue()).To(Equal(int64(500)))
		}
		Expect(sidecar.Resources.Requests.Cpu().MilliValue()).To(Equal(int64(50)))
		Expect(sidecar.Resources.Requests.Memory().String()).To(Equal("32Mi"))
	}, Entry("webhook CPU only", "webhook", "cpu"), Entry("webhook memory only", "webhook", "memory"), Entry("SSA CPU only", "ssa", "cpu"), Entry("SSA memory only", "ssa", "memory"))
})
