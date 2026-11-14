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
)

var _ = Describe("Parity: automatic memory limits", Label("parity", "automatic-limits"), func() {
	DescribeTable("preserves higher limits while enforcing the minimum and request ratio", func(ctx context.Context, strategy string, resize bool) {
		ns := specNamespace(ctx, "automatic")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		samples := smokeSamples()[:1]
		base := smokeWebhookContainers(samples)[0]
		dep := framework.NewDeploymentFixture(ns.Name, "automatic", 1, base, base, base)
		containers := dep.Spec.Template.Spec.Containers
		for i, name := range []string{"minimum", "ratio", "preserved"} {
			containers[i].Name = name
			containers[i].Resources.Limits = corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("64Mi")}
		}
		containers[2].Resources.Requests[corev1.ResourceMemory] = resource.MustParse("512Mi")
		containers[2].Resources.Limits[corev1.ResourceMemory] = resource.MustParse("1Gi")
		Expect(env.Clients.Ctrl.Create(ctx, dep)).To(Succeed())
		pods, err := framework.WaitForPodCountReady(ctx, env.Clients, ns.Name, "app="+dep.Name, 1, framework.WaitOptions{Timeout: rolloutWaitTimeout})
		Expect(err).NotTo(HaveOccurred())
		series := []framework.FixtureSeries{}
		for i, co := range containers {
			memory := 100.0 * 1024 * 1024
			if i == 0 {
				memory = 40 * 1024 * 1024
			}
			series = append(series, framework.FixtureSeries{Namespace: ns.Name, Pod: pods[0].Name, Container: co.Name, Mode: "present", CPUCores: []float64{0.25}, MemoryBytes: []float64{memory}})
		}
		Expect(env.Fixture.SetSeries(ctx, series)).To(Succeed())
		before := podUIDs(ctx, ns.Name, "app="+dep.Name)
		p := framework.NewPolicyFixture(ns.Name, "automatic", api.ModeAuto, framework.WithStrategy(strategy, "immediate"), framework.WithReconciliationInterval(time.Second))
		p.Spec.UpdateStrategy.AllowInPlaceResize = resize
		p.Spec.UpdateStrategy.AllowRecreate = !resize
		unsafe := true
		p.Spec.UpdateStrategy.AllowUnsafeMemoryDecrease = &unsafe
		p.Spec.UpdateStrategy.LimitConfig = &api.LimitConfig{CPUMode: api.LimitPreserve, MemoryMode: api.LimitAutomatic}
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		if resize {
			waitForWorkloadReason(ctx, p, "InPlace")
			Expect(podUIDs(ctx, ns.Name, "app="+dep.Name)).To(Equal(before))
			list, err := env.Clients.Kube.CoreV1().Pods(ns.Name).List(ctx, metav1.ListOptions{LabelSelector: "app=" + dep.Name})
			Expect(err).NotTo(HaveOccurred())
			pods = list.Items
		} else {
			pods = waitForReplacementPod(ctx, ns.Name, "app="+dep.Name, before)
		}
		for i, want := range []string{"128Mi", "180Mi", "1Gi"} {
			Expect(pods[0].Spec.Containers[i].Resources.Limits.Memory().Cmp(resource.MustParse(want))).To(BeZero())
			Expect(pods[0].Status.ContainerStatuses[i].RestartCount).To(BeZero())
		}
		Expect(pods[0].Spec.Containers[2].Resources.Requests.Memory().String()).To(Equal("120Mi"))
	}, Entry("in-place", "webhook", true), Entry("admission", "webhook", false), Entry("SSA", "ssa", false))
})
