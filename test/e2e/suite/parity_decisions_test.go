package suite

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/history"
	"github.com/optipod/optipod/internal/report"
	"github.com/optipod/optipod/test/e2e/framework"
)

var _ = Describe("Parity: decision and settings history", Label("parity", "decisions", "audit"), func() {
	It("retains skipped reasons and exact policy settings across edits, restart and deletion", func(ctx context.Context) {
		ns := specNamespace(ctx, "decisions")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		samples := smokeSamples()[:1]
		dep := createResizeWorkload(ctx, ns.Name, "decisions", samples)
		p := framework.NewPolicyFixture(ns.Name, "decisions", api.ModeDisabled, framework.WithReconciliationInterval(time.Second))
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		collector := &report.Collector{Client: env.Clients.Ctrl, Namespace: ns.Name}
		Eventually(func() bool {
			journal, err := collector.History(ctx, ns.Name, string(dep.UID))
			Expect(err).NotTo(HaveOccurred())
			for _, r := range journal.Records {
				if r.Decision != nil && r.Decision.Reason == "Policy is disabled" && r.PolicySpec.Mode == api.ModeDisabled {
					return true
				}
			}
			return false
		}, time.Minute, time.Second).Should(BeTrue())
		p = updatePolicy(ctx, ns.Name, p.Name, func(p *api.OptimizationPolicy) { p.Spec.Mode = api.ModeRecommend })
		waitForWorkloadReason(ctx, p, "Missing metrics")
		var skipped *history.Record
		Eventually(func() bool {
			journal, err := collector.History(ctx, ns.Name, string(dep.UID))
			Expect(err).NotTo(HaveOccurred())
			for _, r := range journal.Records {
				if r.Decision != nil && r.Decision.Outcome == "Skipped" && r.Mode == api.ModeRecommend {
					skipped = r
					return true
				}
			}
			return false
		}, time.Minute, time.Second).Should(BeTrue())
		Expect(skipped.Decision.Reason).To(ContainSubstring("Missing metrics"))
		Expect(skipped.PolicySpec).NotTo(BeNil())
		Expect(*skipped.PolicySpec.MetricsConfig.ResourceSettings(true).SafetyFactor).To(Equal(1.2))
		registerContainerSeries(ctx, ns.Name, dep, samples)
		updated := updatePolicy(ctx, ns.Name, p.Name, func(p *api.OptimizationPolicy) { factor := 1.5; p.Spec.MetricsConfig.SafetyFactor = &factor })
		Eventually(func() bool {
			journal, err := collector.History(ctx, ns.Name, string(dep.UID))
			Expect(err).NotTo(HaveOccurred())
			for _, r := range journal.Records {
				if r.Decision != nil && r.Decision.Outcome == "Recommended" && r.Policy.Generation == updated.Generation && len(r.Decision.Recommendations) == 1 {
					Expect(*r.PolicySpec.MetricsConfig.ResourceSettings(true).SafetyFactor).To(Equal(1.5))
					return r.Decision.Recommendations[0].CPU.MilliValue() == 375
				}
			}
			return false
		}, time.Minute, time.Second).Should(BeTrue())
		restartController(ctx)
		Expect(env.Clients.Ctrl.Delete(ctx, p)).To(Succeed())
		Expect(env.Clients.Ctrl.Delete(ctx, dep)).To(Succeed())
		retained, err := history.Read(ctx, env.Clients.Ctrl, ns.Name, skipped.Name)
		Expect(err).NotTo(HaveOccurred())
		Expect(retained.Decision.Reason).To(Equal(skipped.Decision.Reason))
		Expect(*retained.PolicySpec.MetricsConfig.ResourceSettings(true).SafetyFactor).To(Equal(1.2))
	})
})
