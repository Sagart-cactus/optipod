package suite

import (
	"context"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/migration"
	"github.com/optipod/optipod/test/e2e/framework"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Parity: migration", Label("parity", "migration"), func() {
	It("validates both translated QA profiles and applies an explicitly promoted canary", func(ctx context.Context) {
		ns := specNamespace(ctx, "migration")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		for _, name := range []string{"qa-stability-1", "qa-resiliency-1"} {
			data, err := os.ReadFile("../../../examples/migration/" + name + ".yaml")
			Expect(err).NotTo(HaveOccurred())
			result, err := migration.Translate(data, migration.Options{Namespace: ns.Name, Name: name, StartupMode: "ignore", CoveragePercent: 90, MinimumHistory: 24 * time.Hour})
			Expect(err).NotTo(HaveOccurred())
			p := result.Policy
			Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
			Expect(p.Spec.Mode).To(Equal(api.ModeRecommend))
			Expect(p.Spec.MetricsConfig.MinCoveragePercent).To(Equal(int32(90)))
			Expect(env.Clients.Ctrl.Delete(ctx, p)).To(Succeed())
		}
		samples := smokeSamples()[:1]
		dep := createResizeWorkload(ctx, ns.Name, "canary", samples)
		registerContainerSeries(ctx, ns.Name, dep, samples)
		data, err := os.ReadFile("../../../examples/migration/qa-stability-1.yaml")
		Expect(err).NotTo(HaveOccurred())
		result, err := migration.Translate(data, migration.Options{Namespace: ns.Name, Name: "canary", StartupMode: "include"})
		Expect(err).NotTo(HaveOccurred())
		p := result.Policy
		p.Spec.ReconciliationInterval.Duration = time.Second
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		waitForWorkloadReason(ctx, p, "Generated resource recommendations")
		before := podUIDs(ctx, ns.Name, "app="+dep.Name)
		updatePolicy(ctx, ns.Name, p.Name, func(p *api.OptimizationPolicy) { p.Spec.Mode = api.ModeAuto })
		waitForWorkloadReason(ctx, p, "InPlace")
		Expect(podUIDs(ctx, ns.Name, "app="+dep.Name)).To(Equal(before))
		pods, err := env.Clients.Kube.CoreV1().Pods(ns.Name).List(ctx, metav1.ListOptions{LabelSelector: "app=" + dep.Name})
		Expect(err).NotTo(HaveOccurred())
		Expect(pods.Items).To(HaveLen(1))
		Expect(pods.Items[0].Spec.Containers[0].Resources.Requests.Cpu().MilliValue()).To(Equal(int64(275)))
		Expect(pods.Items[0].Spec.Containers[0].Resources.Limits.Cpu().IsZero()).To(BeTrue())
		Expect(pods.Items[0].Spec.Containers[0].Resources.Requests.Memory().Value()).To(Equal(int64(110 * 1024 * 1024)))
	})
})
