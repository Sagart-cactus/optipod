package suite

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/horizontal"
	"github.com/optipod/optipod/test/e2e/framework"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func cpuHPA() *api.HorizontalScalingConfig {
	target := int32(50)
	return &api.HorizontalScalingConfig{MinReplicas: 1, MaxReplicas: 3, Metrics: []autoscalingv2.MetricSpec{{Type: autoscalingv2.ResourceMetricSourceType, Resource: &autoscalingv2.ResourceMetricSource{Name: corev1.ResourceCPU, Target: autoscalingv2.MetricTarget{Type: autoscalingv2.UtilizationMetricType, AverageUtilization: &target}}}}}
}

var _ = Describe("Parity: opt-in native HPA", Label("parity", "horizontal"), Serial, func() {
	It("scales real load, preserves utilization requests, refuses drift, and cleans up on disable or deletion", func(ctx context.Context) {
		original := setControllerMetricArgs(ctx, []string{"--metrics-provider=metrics-server", "--metrics-server-sampling-interval=5s", "--metrics-server-min-samples-required=2"})
		DeferCleanup(func(ctx context.Context) { restoreControllerArgs(ctx, original) })
		ns := specNamespace(ctx, "horizontal")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		dep := framework.NewDeploymentFixture(ns.Name, "load", 1, smokeWebhookContainers(smokeSamples()[:1])...)
		co := &dep.Spec.Template.Spec.Containers[0]
		co.Image = "docker.io/optipod-e2e/load:dev"
		co.ImagePullPolicy = corev1.PullNever
		co.Command = nil
		co.Args = nil
		Expect(env.Clients.Ctrl.Create(ctx, dep)).To(Succeed())
		p := framework.NewPolicyFixture(ns.Name, "horizontal", api.ModeRecommend, framework.WithStrategy("webhook", "onNextRestart"), framework.WithReconciliationInterval(time.Second))
		p.Spec.Selector.WorkloadTypes = &api.WorkloadTypeFilter{Include: []api.WorkloadType{api.WorkloadTypeDeployment}}
		p.Spec.MetricsConfig.Provider = "metrics-server"
		p.Spec.MetricsConfig.RollingWindow.Duration = time.Minute
		p.Spec.MetricsConfig.MinimumHistory.Duration = 15 * time.Second
		p.Spec.HorizontalScaling = cpuHPA()
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		Consistently(func() int {
			hs, err := env.Clients.Kube.AutoscalingV2().HorizontalPodAutoscalers(ns.Name).List(ctx, metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			return len(hs.Items)
		}, 5*time.Second, time.Second).Should(BeZero())
		updatePolicy(ctx, ns.Name, p.Name, func(p *api.OptimizationPolicy) { p.Spec.Mode = api.ModeAuto })
		Eventually(func() int32 {
			d, err := env.Clients.Kube.AppsV1().Deployments(ns.Name).Get(ctx, dep.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			return d.Status.ReadyReplicas
		}, 3*time.Minute, 2*time.Second).Should(Equal(int32(3)))
		pods, err := env.Clients.Kube.CoreV1().Pods(ns.Name).List(ctx, metav1.ListOptions{LabelSelector: "app=load"})
		Expect(err).NotTo(HaveOccurred())
		for _, pod := range pods.Items {
			Expect(pod.Spec.Containers[0].Resources.Requests.Cpu().MilliValue()).To(Equal(int64(100)))
		}
		h, err := env.Clients.Kube.AutoscalingV2().HorizontalPodAutoscalers(ns.Name).Get(ctx, horizontal.Name(dep.UID), metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		h.Spec.MaxReplicas = 4
		_, err = env.Clients.Kube.AutoscalingV2().HorizontalPodAutoscalers(ns.Name).Update(ctx, h, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred())
		waitForWorkloadReason(ctx, p, "edited externally")
		updatePolicy(ctx, ns.Name, p.Name, func(p *api.OptimizationPolicy) { p.Spec.Mode = api.ModeDisabled })
		Eventually(func() int {
			hs, err := env.Clients.Kube.AutoscalingV2().HorizontalPodAutoscalers(ns.Name).List(ctx, metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			return len(hs.Items)
		}, time.Minute, time.Second).Should(BeZero())
		updatePolicy(ctx, ns.Name, p.Name, func(p *api.OptimizationPolicy) { p.Spec.Mode = api.ModeAuto })
		Eventually(func() bool {
			_, err := env.Clients.Kube.AutoscalingV2().HorizontalPodAutoscalers(ns.Name).Get(ctx, horizontal.Name(dep.UID), metav1.GetOptions{})
			return err == nil
		}, time.Minute, time.Second).Should(BeTrue())
		Expect(env.Clients.Ctrl.Delete(ctx, p)).To(Succeed())
		Eventually(func() int {
			hs, err := env.Clients.Kube.AutoscalingV2().HorizontalPodAutoscalers(ns.Name).List(ctx, metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			return len(hs.Items)
		}, time.Minute, time.Second).Should(BeZero())
	})
	It("never adopts or replaces a foreign HPA", func(ctx context.Context) {
		ns := specNamespace(ctx, "horizontal-foreign")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		dep := createResizeWorkload(ctx, ns.Name, "foreign", smokeSamples()[:1])
		one := int32(1)
		foreign := &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: "user-owned", Namespace: ns.Name}, Spec: autoscalingv2.HorizontalPodAutoscalerSpec{ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{APIVersion: "apps/v1", Kind: "Deployment", Name: dep.Name}, MinReplicas: &one, MaxReplicas: 1, Metrics: cpuHPA().Metrics}}
		Expect(env.Clients.Ctrl.Create(ctx, foreign)).To(Succeed())
		p := framework.NewPolicyFixture(ns.Name, "horizontal", api.ModeAuto, framework.WithReconciliationInterval(time.Second))
		p.Spec.HorizontalScaling = cpuHPA()
		p.Spec.Selector.WorkloadTypes = &api.WorkloadTypeFilter{Include: []api.WorkloadType{api.WorkloadTypeDeployment}}
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		waitForWorkloadReason(ctx, p, "blocked by existing")
		hs, err := env.Clients.Kube.AutoscalingV2().HorizontalPodAutoscalers(ns.Name).List(ctx, metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(hs.Items).To(HaveLen(1))
		Expect(hs.Items[0].UID).To(Equal(foreign.UID))
		Expect(hs.Items[0].Spec.MaxReplicas).To(Equal(int32(1)))
	})
})
