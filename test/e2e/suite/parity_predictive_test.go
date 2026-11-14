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

var _ = Describe("Parity: bounded CPU forecasting", Label("parity", "predictive"), func() {
	It("raises a real pod within the forecast cap and respects policy maximums", func(ctx context.Context) {
		ns := specNamespace(ctx, "predictive")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		samples := smokeSamples()[:1]
		createResizeWorkload(ctx, ns.Name, "trend", samples)
		pods, err := env.Clients.Kube.CoreV1().Pods(ns.Name).List(ctx, metav1.ListOptions{LabelSelector: "app=trend"})
		Expect(err).NotTo(HaveOccurred())
		Expect(pods.Items).To(HaveLen(1))
		started := pods.Items[0].Status.StartTime.Time
		Eventually(func() time.Duration { return time.Since(started) }, 30*time.Second, time.Second).Should(BeNumerically(">=", 22*time.Second))
		Expect(env.Fixture.SetSeries(ctx, []framework.FixtureSeries{{Namespace: ns.Name, Pod: pods.Items[0].Name, Container: "app", SpacingSeconds: 3, CPUCores: []float64{.10, .12, .14, .16, .18, .20, .22}, MemoryBytes: []float64{100 * 1024 * 1024, 100 * 1024 * 1024}}})).To(Succeed())
		p := framework.NewPolicyFixture(ns.Name, "trend", api.ModeAuto, framework.WithStrategy("webhook", "immediate"), framework.WithReconciliationInterval(time.Second))
		p.Spec.UpdateStrategy.AllowInPlaceResize = true
		p.Spec.UpdateStrategy.AllowRecreate = false
		one := 1.0
		samplesRequired, cap := int32(6), int32(25)
		p.Spec.MetricsConfig.CPU = &api.ResourceMetricsConfig{Percentile: "P50", SafetyFactor: &one, RollingWindow: metav1.Duration{Duration: time.Minute}}
		p.Spec.MetricsConfig.PredictiveCPU = &api.PredictiveCPUConfig{TrainingWindow: metav1.Duration{Duration: time.Minute}, Horizon: metav1.Duration{Duration: 30 * time.Second}, MinimumHistory: metav1.Duration{Duration: 10 * time.Second}, MinSamples: &samplesRequired, MaxIncreasePercent: &cap}
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		waitForWorkloadReason(ctx, p, "InPlace")
		Eventually(func() int64 {
			pod, err := env.Clients.Kube.CoreV1().Pods(ns.Name).Get(ctx, pods.Items[0].Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			return pod.Spec.Containers[0].Resources.Requests.Cpu().MilliValue()
		}, time.Minute, time.Second).Should(Equal(int64(200)))
		updatePolicy(ctx, ns.Name, p.Name, func(p *api.OptimizationPolicy) { p.Spec.ResourceBounds.CPU.Max = resource.MustParse("180m") })
		Eventually(func() int64 {
			pod, err := env.Clients.Kube.CoreV1().Pods(ns.Name).Get(ctx, pods.Items[0].Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			return pod.Spec.Containers[0].Resources.Requests.Cpu().MilliValue()
		}, time.Minute, time.Second).Should(Equal(int64(180)))
	})
})
