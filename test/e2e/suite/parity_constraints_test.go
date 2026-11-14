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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Parity: resource constraints", Label("parity", "constraints"), func() {
	DescribeTable("blocks unsafe plans before storing or applying them", func(ctx context.Context, kind, reason string) {
		ns := specNamespace(ctx, "constraints")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		samples := smokeSamples()[:1]
		dep := framework.NewDeploymentFixture(ns.Name, "bounded", 1, smokeWebhookContainers(samples)...)
		if kind == "preserved-limit" {
			dep.Spec.Template.Spec.Containers[0].Resources.Limits = corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("200m")}
		}
		if kind == "qos" {
			dep.Spec.Template.Spec.Containers[0].Resources = corev1.ResourceRequirements{}
		}
		Expect(env.Clients.Ctrl.Create(ctx, dep)).To(Succeed())
		_, err := framework.WaitForPodCountReady(ctx, env.Clients, ns.Name, "app=bounded", 1, framework.WaitOptions{Timeout: time.Minute})
		Expect(err).NotTo(HaveOccurred())
		registerContainerSeries(ctx, ns.Name, dep, samples)
		if kind == "quota" {
			q := &corev1.ResourceQuota{ObjectMeta: metav1.ObjectMeta{Name: "budget", Namespace: ns.Name}, Spec: corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{corev1.ResourceRequestsCPU: resource.MustParse("200m")}}}
			Expect(env.Clients.Ctrl.Create(ctx, q)).To(Succeed())
			Eventually(func() bool {
				Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(q), q)).To(Succeed())
				_, ok := q.Status.Used[corev1.ResourceRequestsCPU]
				return ok
			}, time.Minute, time.Second).Should(BeTrue())
		}
		if kind == "limit-range" {
			lr := &corev1.LimitRange{ObjectMeta: metav1.ObjectMeta{Name: "bounded", Namespace: ns.Name}, Spec: corev1.LimitRangeSpec{Limits: []corev1.LimitRangeItem{{Type: corev1.LimitTypeContainer, Max: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("400m")}}}}}
			Expect(env.Clients.Ctrl.Create(ctx, lr)).To(Succeed())
		}
		p := framework.NewPolicyFixture(ns.Name, "bounded", api.ModeAuto, framework.WithStrategy("webhook", "onNextRestart"), framework.WithReconciliationInterval(time.Second))
		if kind == "limit-range" {
			p.Spec.UpdateStrategy.LimitConfig = &api.LimitConfig{CPUMode: "Multiplier", MemoryMode: "Preserve"}
			factor := 2.0
			p.Spec.UpdateStrategy.LimitConfig.CPULimitMultiplier = &factor
		}
		if kind == "qos" {
			p.Spec.UpdateStrategy.PreserveQoS = true
		}
		before := podUIDs(ctx, ns.Name, "app=bounded")
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		waitForWorkloadReason(ctx, p, reason)
		Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(dep), dep)).To(Succeed())
		Expect(dep.Annotations[api.AnnotationCPURequestPrefix+".app"]).To(BeEmpty())
		Expect(podUIDs(ctx, ns.Name, "app=bounded")).To(Equal(before))
		if kind == "quota" {
			q := &corev1.ResourceQuota{ObjectMeta: metav1.ObjectMeta{Name: "budget", Namespace: ns.Name}}
			Expect(env.Clients.Ctrl.Delete(ctx, q)).To(Succeed())
			Eventually(func() string {
				Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(dep), dep)).To(Succeed())
				return dep.Annotations[api.AnnotationCPURequestPrefix+".app"]
			}, time.Minute, time.Second).Should(Equal("300m"))
		}
	}, Entry("namespace quota", "quota", "ResourceQuota"), Entry("namespace LimitRange", "limit-range", "LimitRange"), Entry("preserved container limit", "preserved-limit", "request exceeds preserved limit"), Entry("QoS preservation", "qos", "QoS preservation"))

	DescribeTable("requires explicit field ownership takeover for every template patch path", func(ctx context.Context, removeLimit bool) {
		ns := specNamespace(ctx, "ownership")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		samples := smokeSamples()[:1]
		dep := createResizeWorkload(ctx, ns.Name, "owned", samples)
		registerContainerSeries(ctx, ns.Name, dep, samples)
		p := framework.NewPolicyFixture(ns.Name, "owned", api.ModeAuto, framework.WithStrategy("ssa", ""), framework.WithReconciliationInterval(time.Second))
		p.Spec.UpdateStrategy.AllowFieldOwnershipTakeover = false
		if removeLimit {
			p.Spec.UpdateStrategy.LimitConfig = &api.LimitConfig{CPUMode: "None", MemoryMode: "Preserve"}
		}
		before := podUIDs(ctx, ns.Name, "app=owned")
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		waitForWorkloadReason(ctx, p, "SSA conflict")
		Expect(podUIDs(ctx, ns.Name, "app=owned")).To(Equal(before))
		updatePolicy(ctx, ns.Name, p.Name, func(p *api.OptimizationPolicy) { p.Spec.UpdateStrategy.AllowFieldOwnershipTakeover = true })
		pods := waitForReplacementPod(ctx, ns.Name, "app=owned", before)
		expectAppliedContainerRequests(pods[0].Spec.Containers, samples, "explicit ownership takeover")
	}, Entry("SSA", false), Entry("limit-removal strategic merge", true))

	It("rechecks a LimitRange created after a valid admission plan", func(ctx context.Context) {
		ns := specNamespace(ctx, "late-limits")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		samples := smokeSamples()[:1]
		dep := createResizeWorkload(ctx, ns.Name, "late", samples)
		registerContainerSeries(ctx, ns.Name, dep, samples)
		p := framework.NewPolicyFixture(ns.Name, "late", api.ModeAuto, framework.WithStrategy("webhook", "onNextRestart"), framework.WithReconciliationInterval(time.Hour))
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		Eventually(func() string {
			Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(dep), dep)).To(Succeed())
			return dep.Annotations[api.AnnotationCPURequestPrefix+".app"]
		}, time.Minute, time.Second).Should(Equal("300m"))
		lr := &corev1.LimitRange{ObjectMeta: metav1.ObjectMeta{Name: "late", Namespace: ns.Name}, Spec: corev1.LimitRangeSpec{Limits: []corev1.LimitRangeItem{{Type: corev1.LimitTypeContainer, Min: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("50m")}, Max: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("200m")}, Default: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("200m")}}}}}
		Expect(env.Clients.Ctrl.Create(ctx, lr)).To(Succeed())
		pods, err := env.Clients.Kube.CoreV1().Pods(ns.Name).List(ctx, metav1.ListOptions{LabelSelector: "app=late"})
		Expect(err).NotTo(HaveOccurred())
		probe := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "late-probe", Namespace: ns.Name, Labels: dep.Spec.Template.Labels, OwnerReferences: pods.Items[0].OwnerReferences}, Spec: *dep.Spec.Template.Spec.DeepCopy()}
		created, err := env.Clients.Kube.CoreV1().Pods(ns.Name).Create(ctx, probe, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
		Expect(err).NotTo(HaveOccurred())
		Expect(created.Spec.Containers[0].Resources.Requests.Cpu().MilliValue()).To(Equal(int64(100)))
	})
})
