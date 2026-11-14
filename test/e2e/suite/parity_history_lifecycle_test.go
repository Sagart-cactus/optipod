package suite

import (
	"context"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Parity: workload metrics lifecycle", Label("parity", "metrics-lifecycle"), Serial, func() {
	It("retains a retired pod peak across replacement and controller restart", func(ctx context.Context) {
		ns := specNamespace(ctx, "history")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		samples := smokeSamples()[:1]
		dep := createResizeWorkload(ctx, ns.Name, "history", samples)
		series := registerContainerSeries(ctx, ns.Name, dep, samples)
		series[0].CPUCores = []float64{0.9, 0.9, 0.9}
		series[0].FixedEnd = float64(time.Now().UnixNano()) / 1e9
		oldName := series[0].Pod
		Expect(env.Fixture.SetSeries(ctx, series)).To(Succeed())
		p := framework.NewPolicyFixture(ns.Name, "history", api.ModeRecommend, framework.WithReconciliationInterval(time.Second))
		one := 1.0
		p.Spec.MetricsConfig.SafetyFactor = &one
		p.Spec.MetricsConfig.Percentile = "P100"
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		waitForDeploymentAnnotation(ctx, ns.Name, dep.Name, map[string]string{"optipod.io/recommendation.app.cpu-request": "900m"})
		before := podUIDs(ctx, ns.Name, "app=history")
		Expect(env.Clients.Kube.CoreV1().Pods(ns.Name).Delete(ctx, oldName, metav1.DeleteOptions{})).To(Succeed())
		newPods := waitForReplacementPod(ctx, ns.Name, "app=history", before)
		fresh := series[0]
		fresh.Pod = newPods[0].Name
		fresh.FixedEnd = 0
		fresh.CPUCores = []float64{0.1, 0.1, 0.1}
		series = append(series, fresh)
		Expect(env.Fixture.SetSeries(ctx, series)).To(Succeed())
		Expect(env.Fixture.ClearTrace(ctx)).To(Succeed())
		restartController(ctx)
		// Force an observable recomputation of display data. Failure to query
		// the retired pod would now yield 100m; stale display data cannot pass.
		_, err := env.Clients.Kube.AppsV1().Deployments(ns.Name).Patch(ctx, dep.Name, "application/merge-patch+json", []byte(`{"metadata":{"annotations":{"optipod.io/recommendation.app.cpu-request":"0m","optipod.io/last-recommendation":"2000-01-01T00:00:00Z"}}}`), metav1.PatchOptions{})
		Expect(err).NotTo(HaveOccurred())
		waitForDeploymentAnnotation(ctx, ns.Name, dep.Name, map[string]string{"optipod.io/recommendation.app.cpu-request": "900m"})
		trace, err := env.Fixture.Trace(ctx)
		Expect(err).NotTo(HaveOccurred())
		for _, name := range []string{oldName, fresh.Pod} {
			found := false
			for _, q := range trace {
				if strings.Contains(q.Query, `pod="`+name+`"`) {
					found = true
				}
			}
			Expect(found).To(BeTrue(), "historical query for "+name)
		}
	})
	It("rejects incomplete replica coverage and recovers when both replicas have samples", func(ctx context.Context) {
		ns := specNamespace(ctx, "coverage")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		dep := framework.NewDeploymentFixture(ns.Name, "coverage", 2, smokeWebhookContainers(smokeSamples()[:1])...)
		Expect(env.Clients.Ctrl.Create(ctx, dep)).To(Succeed())
		pods, err := framework.WaitForPodCountReady(ctx, env.Clients, ns.Name, "app=coverage", 2, framework.WaitOptions{Timeout: rolloutWaitTimeout})
		Expect(err).NotTo(HaveOccurred())
		series := []framework.FixtureSeries{{Namespace: ns.Name, Pod: pods[0].Name, Container: "app", CPUCores: []float64{0.25, 0.25, 0.25}, MemoryBytes: []float64{100 * 1024 * 1024, 100 * 1024 * 1024, 100 * 1024 * 1024}}}
		Expect(env.Fixture.SetSeries(ctx, series)).To(Succeed())
		p := framework.NewPolicyFixture(ns.Name, "coverage", api.ModeAuto, framework.WithReconciliationInterval(time.Second))
		p.Spec.MetricsConfig.MinCoveragePercent = 90
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		waitForWorkloadReason(ctx, p, "insufficient coverage")
		second := series[0]
		second.Pod = pods[1].Name
		series = append(series, second)
		Expect(env.Fixture.SetSeries(ctx, series)).To(Succeed())
		waitForDeploymentAnnotation(ctx, ns.Name, dep.Name, map[string]string{api.AnnotationCPURequestPrefix + ".app": "300m"})
	})
})
