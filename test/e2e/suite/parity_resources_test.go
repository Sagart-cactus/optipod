package suite

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Parity: independent resource recommendations", Label("parity", "resources"), func() {
	DescribeTable("applies the configured CPU percentile and memory maximum", func(ctx context.Context, percentile string, expectedMilli int64) {
		ns := specNamespace(ctx, "parity-resources")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		samples := smokeSamples()[:1]
		dep := framework.NewDeploymentFixture(ns.Name, "resources", 1, smokeWebhookContainers(samples)...)
		dep.Labels = map[string]string{"app": dep.Name}
		Expect(env.Clients.Ctrl.Create(ctx, dep)).To(Succeed())
		pods, err := framework.WaitForPodCountReady(ctx, env.Clients, ns.Name, "app="+dep.Name, 1, framework.WaitOptions{Timeout: rolloutWaitTimeout})
		Expect(err).NotTo(HaveOccurred())
		Expect(env.Fixture.SetSeries(ctx, []framework.FixtureSeries{{Namespace: ns.Name, Pod: pods[0].Name, Container: "app", Mode: "present",
			CPUCores: []float64{0.1, 0.2, 0.3, 0.4, 0.5}, MemoryBytes: []float64{64 * 1024 * 1024, 128 * 1024 * 1024, 256 * 1024 * 1024}}})).To(Succeed())
		p := framework.NewPolicyFixture(ns.Name, "resources", api.ModeAuto, framework.WithStrategy("webhook", "immediate"))
		cpuMargin, memoryMargin := 1.5, 1.25
		p.Spec.MetricsConfig.CPU = &api.ResourceMetricsConfig{Percentile: percentile, SafetyFactor: &cpuMargin, RollingWindow: metav1.Duration{Duration: time.Hour}}
		p.Spec.MetricsConfig.Memory = &api.ResourceMetricsConfig{Percentile: "P100", SafetyFactor: &memoryMargin, RollingWindow: metav1.Duration{Duration: 7 * 24 * time.Hour}}
		if percentile == "P80" {
			p.Spec.Selector = api.WorkloadSelector{}
			p.Spec.Selector.AnyOf = []api.AssignmentRule{{NamespacePattern: "^never-matches-this-fixture$", Kinds: []string{"DaemonSet"}}, {NamespacePattern: "^" + ns.Name + "$", LabelValueContains: []string{"esource"}}}

			p.Spec.ResourceBounds.CPU.Max = resource.Quantity{}
			p.Spec.ResourceBounds.CPU.NoMaximum = true
		}
		before := podUIDs(ctx, ns.Name, "app="+dep.Name)
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		replaced := waitForReplacementPod(ctx, ns.Name, "app="+dep.Name, before)
		Expect(replaced[0].Spec.Containers[0].Resources.Requests.Cpu().MilliValue()).To(Equal(expectedMilli))
		Expect(replaced[0].Spec.Containers[0].Resources.Requests.Memory().Value()).To(Equal(int64(320 * 1024 * 1024)))
	}, Entry("P95", "P95", int64(720)), Entry("P80", "P80", int64(630)))
})
