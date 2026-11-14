package suite

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/webhook"
	"github.com/optipod/optipod/test/e2e/framework"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Parity: JVM and custom Prometheus gauges", Label("parity", "jvm", "custom-metrics"), Serial, func() {
	It("uses a real JVM heap floor and custom CPU gauge, and fails closed on missing data or impossible bounds", func(ctx context.Context) {
		original := setControllerMetricArgs(ctx, []string{"--metrics-provider=prometheus", "--prometheus-url=http://prometheus.optipod-e2e-backends.svc:9090"})
		DeferCleanup(func(ctx context.Context) { restoreControllerArgs(ctx, original) })
		ns := specNamespace(ctx, "jvm")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		dep := framework.NewDeploymentFixture(ns.Name, "java", 1, smokeWebhookContainers(smokeSamples()[:1])...)
		dep.Spec.Template.Labels["optipod-e2e/app-metrics"] = "true"
		co := &dep.Spec.Template.Spec.Containers[0]
		co.Image = "docker.io/optipod-e2e/jvm:dev"
		co.ImagePullPolicy = corev1.PullNever
		co.Command = nil
		co.Args = nil
		co.Ports = []corev1.ContainerPort{{Name: "metrics", ContainerPort: 8080}}
		co.Resources.Limits = corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("768Mi")}
		co.ReadinessProbe = &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/ready", Port: intstr.FromInt32(8080)}}, PeriodSeconds: 1}
		Expect(env.Clients.Ctrl.Create(ctx, dep)).To(Succeed())
		pods, err := framework.WaitForPodCountReady(ctx, env.Clients, ns.Name, "app=java", 1, framework.WaitOptions{Timeout: rolloutWaitTimeout})
		Expect(err).NotTo(HaveOccurred())
		uid := pods[0].UID
		p := framework.NewPolicyFixture(ns.Name, "jvm", api.ModeRecommend, framework.WithStrategy("webhook", "immediate"), framework.WithReconciliationInterval(3*time.Second))
		p.Spec.UpdateStrategy.AllowInPlaceResize = true
		p.Spec.UpdateStrategy.AllowRecreate = false
		p.Spec.MetricsConfig.RollingWindow.Duration = 2 * time.Minute
		p.Spec.MetricsConfig.MinimumHistory.Duration = 30 * time.Second
		p.Spec.MetricsConfig.CPU = &api.ResourceMetricsConfig{Source: &api.MetricSource{Name: "app_cpu_demand_cores"}}
		p.Spec.MetricsConfig.JVM = &api.JVMConfig{HeapUsed: api.MetricSource{Name: "optipod_jvm_heap_bytes"}, HeapMax: api.MetricSource{Name: "optipod_jvm_max_bytes"}, NonHeapUsed: api.MetricSource{Name: "optipod_jvm_nonheap_bytes"}, NativeReserve: resource.MustParse("64Mi")}
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		Eventually(func() string {
			Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(dep), dep)).To(Succeed())
			return dep.Annotations["optipod.io/recommendation.app.cpu-request"]
		}, 3*time.Minute, 2*time.Second).Should(Equal("420m"))
		recommendation, err := resource.ParseQuantity(dep.Annotations["optipod.io/recommendation.app.memory-request"])
		Expect(err).NotTo(HaveOccurred())
		Expect(recommendation.Value()).To(BeNumerically(">=", 320*1024*1024))
		Expect(recommendation.Value()).To(BeNumerically("<", 768*1024*1024))
		updatePolicy(ctx, ns.Name, p.Name, func(p *api.OptimizationPolicy) { p.Spec.Mode = api.ModeAuto })
		waitForWorkloadReason(ctx, p, "InPlace")
		pod, err := env.Clients.Kube.CoreV1().Pods(ns.Name).Get(ctx, pods[0].Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(pod.UID).To(Equal(uid))
		Expect(pod.Spec.Containers[0].Resources.Requests.Cpu().MilliValue()).To(Equal(int64(420)))
		Expect(pod.Status.ContainerStatuses[0].RestartCount).To(BeZero())
		updatePolicy(ctx, ns.Name, p.Name, func(p *api.OptimizationPolicy) { p.Spec.MetricsConfig.CPU.Source.Name = "absent_cpu_demand" })
		waitForWorkloadReason(ctx, p, "insufficient samples")
		Eventually(func() string {
			Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(dep), dep)).To(Succeed())
			return dep.Annotations[webhook.RecommendationExpiry]
		}, time.Minute, time.Second).Should(BeEmpty())
		updatePolicy(ctx, ns.Name, p.Name, func(p *api.OptimizationPolicy) {
			p.Spec.MetricsConfig.CPU.Source.Name = "app_cpu_demand_cores"
			p.Spec.MetricsConfig.JVM.NativeReserve = resource.MustParse("512Mi")
			p.Spec.ResourceBounds.Memory.Max = resource.MustParse("512Mi")
		})
		waitForWorkloadReason(ctx, p, "JVM memory floor exceeds")
	})
})
