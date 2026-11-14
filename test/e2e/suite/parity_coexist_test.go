package suite

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var _ = Describe("Parity: horizontal scaler coexistence", Label("parity", "coexist"), func() {
	DescribeTable("protects utilization denominators and still optimizes other resources", func(ctx context.Context, mode string) {
		ns := specNamespace(ctx, "coexist")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		samples := smokeSamples()
		dep := createResizeWorkload(ctx, ns.Name, "scaled", samples)
		series := registerContainerSeries(ctx, ns.Name, dep, samples)
		one, target := int32(1), int32(70)
		if mode == "keda" {
			scaled := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "keda.sh/v1alpha1", "kind": "ScaledObject",
				"metadata": map[string]any{"name": "scaled", "namespace": ns.Name},
				"spec":     map[string]any{"scaleTargetRef": map[string]any{"name": dep.Name}, "minReplicaCount": int64(1), "maxReplicaCount": int64(1), "triggers": []any{map[string]any{"type": "cpu", "metricType": "Utilization", "metadata": map[string]any{"value": "70"}}}},
			}}
			Expect(env.Clients.Ctrl.Create(ctx, scaled)).To(Succeed())
			Eventually(func() bool {
				hs, err := env.Clients.Kube.AutoscalingV2().HorizontalPodAutoscalers(ns.Name).List(ctx, metav1.ListOptions{})
				return err == nil && len(hs.Items) == 1
			}, time.Minute, time.Second).Should(BeTrue())
		} else {
			metric := autoscalingv2.MetricSpec{Type: autoscalingv2.ResourceMetricSourceType, Resource: &autoscalingv2.ResourceMetricSource{Name: corev1.ResourceCPU, Target: autoscalingv2.MetricTarget{Type: autoscalingv2.UtilizationMetricType, AverageUtilization: &target}}}
			if mode == "container" {
				metric = autoscalingv2.MetricSpec{Type: autoscalingv2.ContainerResourceMetricSourceType, ContainerResource: &autoscalingv2.ContainerResourceMetricSource{Name: corev1.ResourceCPU, Container: "app", Target: autoscalingv2.MetricTarget{Type: autoscalingv2.UtilizationMetricType, AverageUtilization: &target}}}
			}
			if mode == "absolute" {
				q := resource.MustParse("200m")
				metric.Resource.Target = autoscalingv2.MetricTarget{Type: autoscalingv2.AverageValueMetricType, AverageValue: &q}
			}
			h := &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: "scaled", Namespace: ns.Name}, Spec: autoscalingv2.HorizontalPodAutoscalerSpec{ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{APIVersion: "apps/v1", Kind: "Deployment", Name: dep.Name}, MinReplicas: &one, MaxReplicas: 1, Metrics: []autoscalingv2.MetricSpec{metric}}}
			Expect(env.Clients.Ctrl.Create(ctx, h)).To(Succeed())
		}
		if mode != "absolute" {
			series[0].CPUCores = nil
		}
		if mode == "resource" || mode == "keda" {
			series[1].CPUCores = nil
		}
		Expect(env.Fixture.SetSeries(ctx, series)).To(Succeed())
		p := framework.NewPolicyFixture(ns.Name, "scaled", api.ModeAuto, framework.WithStrategy("webhook", "immediate"), framework.WithReconciliationInterval(time.Second))
		p.Spec.UpdateStrategy.AllowInPlaceResize = true
		p.Spec.UpdateStrategy.AllowRecreate = false
		before := podUIDs(ctx, ns.Name, "app=scaled")
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		waitForWorkloadReason(ctx, p, "InPlace")
		pods, err := env.Clients.Kube.CoreV1().Pods(ns.Name).List(ctx, metav1.ListOptions{LabelSelector: "app=scaled"})
		Expect(err).NotTo(HaveOccurred())
		Expect(podUIDs(ctx, ns.Name, "app=scaled")).To(Equal(before))
		app, sidecar := pods.Items[0].Spec.Containers[0], pods.Items[0].Spec.Containers[1]
		Expect(app.Resources.Requests.Memory().String()).To(Equal("120Mi"))
		if mode == "absolute" {
			Expect(app.Resources.Requests.Cpu().MilliValue()).To(Equal(int64(300)))
		} else {
			Expect(app.Resources.Requests.Cpu().MilliValue()).To(Equal(int64(100)))
		}
		if mode == "container" || mode == "absolute" {
			Expect(sidecar.Resources.Requests.Cpu().MilliValue()).To(Equal(int64(150)))
		} else {
			Expect(sidecar.Resources.Requests.Cpu().MilliValue()).To(Equal(int64(50)))
		}
	}, Entry("HPA resource utilization", "resource"), Entry("HPA container utilization", "container"), Entry("HPA absolute usage", "absolute"), Entry("real KEDA CPU scaler", "keda"))

	It("rechecks a newly created HPA at admission before applying a stored plan", func(ctx context.Context) {
		ns := specNamespace(ctx, "coexist-admission")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		samples := smokeSamples()[:1]
		dep := createResizeWorkload(ctx, ns.Name, "admission", samples)
		registerContainerSeries(ctx, ns.Name, dep, samples)
		p := framework.NewPolicyFixture(ns.Name, "admission", api.ModeAuto, framework.WithStrategy("webhook", "onNextRestart"), framework.WithReconciliationInterval(time.Hour))
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		Eventually(func() string {
			d, err := env.Clients.Kube.AppsV1().Deployments(ns.Name).Get(ctx, dep.Name, metav1.GetOptions{})
			if err != nil {
				return ""
			}
			return d.Annotations["optipod.io/cpu-request.app"]
		}, time.Minute, time.Second).Should(Equal("300m"))
		one, target := int32(1), int32(80)
		h := &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: "late-hpa", Namespace: ns.Name}, Spec: autoscalingv2.HorizontalPodAutoscalerSpec{ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{APIVersion: "apps/v1", Kind: "Deployment", Name: dep.Name}, MinReplicas: &one, MaxReplicas: 1, Metrics: []autoscalingv2.MetricSpec{{Type: autoscalingv2.ResourceMetricSourceType, Resource: &autoscalingv2.ResourceMetricSource{Name: corev1.ResourceCPU, Target: autoscalingv2.MetricTarget{Type: autoscalingv2.UtilizationMetricType, AverageUtilization: &target}}}}}}
		Expect(env.Clients.Ctrl.Create(ctx, h)).To(Succeed())
		pods, err := env.Clients.Kube.CoreV1().Pods(ns.Name).List(ctx, metav1.ListOptions{LabelSelector: "app=admission"})
		Expect(err).NotTo(HaveOccurred())
		probe := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "admission-probe", Namespace: ns.Name, Labels: dep.Spec.Template.Labels, OwnerReferences: pods.Items[0].OwnerReferences}, Spec: *dep.Spec.Template.Spec.DeepCopy()}
		created, err := env.Clients.Kube.CoreV1().Pods(ns.Name).Create(ctx, probe, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
		Expect(err).NotTo(HaveOccurred())
		Expect(created.Spec.Containers[0].Resources.Requests.Cpu().MilliValue()).To(Equal(int64(100)))
		Expect(created.Spec.Containers[0].Resources.Requests.Memory().String()).To(Equal("120Mi"))
	})
})
