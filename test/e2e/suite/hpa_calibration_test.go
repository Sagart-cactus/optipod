package suite

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("HPA CPU calibration", Label("conformance", "hpa-calibration"), func() {
	DescribeTable("replays HPA replica history for the selected container and preserves the workload and HPA specs in Recommend mode", func(ctx context.Context, external bool) {
		ns := specNamespace(ctx, "hpa-cpu")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		dep := framework.NewDeploymentFixture(ns.Name, "scaled", 1, smokeContainers(smokeSamples()[:2])...)
		dep.Annotations = map[string]string{"optipod.io/hpa-cpu-calibration": "app"}
		Expect(env.Clients.Ctrl.Create(ctx, dep)).To(Succeed())
		before := dep.Spec.Template.DeepCopy()
		one := int32(1)
		target := resource.MustParse("10")
		h := &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: "queue", Namespace: ns.Name}, Spec: autoscalingv2.HorizontalPodAutoscalerSpec{ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{APIVersion: "apps/v1", Kind: "Deployment", Name: dep.Name}, MinReplicas: &one, MaxReplicas: 3, Metrics: []autoscalingv2.MetricSpec{{Type: autoscalingv2.ExternalMetricSourceType, External: &autoscalingv2.ExternalMetricSource{Metric: autoscalingv2.MetricIdentifier{Name: "queue_depth"}, Target: autoscalingv2.MetricTarget{Type: autoscalingv2.AverageValueMetricType, AverageValue: &target}}}}}}
		if !external {
			cpu := resource.MustParse("250m")
			h.Spec.Metrics = []autoscalingv2.MetricSpec{{Type: autoscalingv2.ResourceMetricSourceType, Resource: &autoscalingv2.ResourceMetricSource{Name: "cpu", Target: autoscalingv2.MetricTarget{Type: autoscalingv2.AverageValueMetricType, AverageValue: &cpu}}}}
		}
		Expect(env.Clients.Ctrl.Create(ctx, h)).To(Succeed())
		hpaSpec := h.Spec.DeepCopy()
		pods, err := framework.WaitForPodCountReady(ctx, env.Clients, ns.Name, "app="+dep.Name, 1, framework.WaitOptions{Timeout: rolloutWaitTimeout})
		Expect(err).NotTo(HaveOccurred())
		series := []framework.FixtureSeries{}
		for _, c := range dep.Spec.Template.Spec.Containers {
			series = append(series, framework.FixtureSeries{Namespace: ns.Name, Pod: pods[0].Name, Container: c.Name, SpacingSeconds: 30, CPUCores: []float64{.1, .1, .1, .1}, MemoryBytes: []float64{100 << 20, 100 << 20, 100 << 20, 100 << 20}})
		}
		Expect(env.Fixture.SetSeries(ctx, series)).To(Succeed())
		p := framework.NewPolicyFixture(ns.Name, "cpu", api.ModeRecommend, framework.WithReconciliationInterval(time.Second))
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		waitForWorkloadReason(ctx, p, "HPA CPU calibration")
		series = append(series, framework.FixtureSeries{Namespace: ns.Name, HorizontalPodAutoscaler: h.Name, SpacingSeconds: 30, HPAReplicaRatio: []float64{2}})
		Expect(env.Fixture.SetSeries(ctx, series)).To(Succeed())
		waitForWorkloadReason(ctx, p, "Missing metrics: container app: insufficient overlapping HPA and CPU history")
		series[len(series)-1].HPAReplicaRatio = []float64{2, 2, 2, 2}
		Expect(env.Fixture.SetSeries(ctx, series)).To(Succeed())
		Eventually(func() []string {
			Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(dep), dep)).To(Succeed())
			return []string{dep.Annotations[api.RecommendationAnnotationKey("app", "cpu-request")], dep.Annotations[api.RecommendationAnnotationKey(dep.Spec.Template.Spec.Containers[1].Name, "cpu-request")], dep.Annotations[api.RecommendationAnnotationKey("app", "memory-request")]}
		}, 3*time.Minute, time.Second).Should(Equal([]string{"240m", "120m", "120Mi"}))
		Expect(dep.Spec.Template).To(Equal(*before))
		Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(h), h)).To(Succeed())
		Expect(h.Spec).To(Equal(*hpaSpec))
	}, Entry("external queue target", true), Entry("absolute pod CPU target", false))
})
