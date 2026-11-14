package suite

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var _ = Describe("Parity: real pod resize", Label("parity", "resize"), func() {
	It("changes kubelet allocated resources without replacing or restarting containers", func(ctx context.Context) {
		ns := specNamespace(ctx, "resize")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		samples := smokeSamples()[:1]
		dep := createResizeWorkload(ctx, ns.Name, "resize", samples)
		registerContainerSeries(ctx, ns.Name, dep, samples)
		before, err := env.Clients.Kube.CoreV1().Pods(ns.Name).List(ctx, metav1.ListOptions{LabelSelector: "app=" + dep.Name})
		Expect(err).NotTo(HaveOccurred())
		Expect(before.Items).To(HaveLen(1))
		uid := before.Items[0].UID
		p := framework.NewPolicyFixture(ns.Name, "resize", api.ModeAuto, framework.WithStrategy("webhook", "immediate"), framework.WithReconciliationInterval(time.Second))
		p.Spec.UpdateStrategy.AllowInPlaceResize = true
		p.Spec.UpdateStrategy.AllowRecreate = false
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		waitForWorkloadReason(ctx, p, "InPlace")
		pods, err := env.Clients.Kube.CoreV1().Pods(ns.Name).List(ctx, metav1.ListOptions{LabelSelector: "app=" + dep.Name})
		Expect(err).NotTo(HaveOccurred())
		Expect(pods.Items).To(HaveLen(1))
		Expect(pods.Items[0].UID).To(Equal(uid))
		expectAppliedContainerRequests(pods.Items[0].Spec.Containers, samples, "in-place pod spec")
		status := pods.Items[0].Status.ContainerStatuses[0]
		Expect(status.RestartCount).To(Equal(int32(0)))
		Expect(status.Resources.Requests.Cpu().MilliValue()).To(Equal(int64(300)))
		got, err := env.Clients.Kube.AppsV1().Deployments(ns.Name).Get(ctx, dep.Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Spec.Template.Spec.Containers[0].Resources.Requests.Cpu().MilliValue()).To(Equal(int64(100)))
	})
	It("requires explicit recreate permission when a QoS change prevents resize", func(ctx context.Context) {
		ns := specNamespace(ctx, "resize-fallback")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		samples := smokeSamples()[:1]
		dep := framework.NewDeploymentFixture(ns.Name, "fallback", 1, smokeWebhookContainers(samples)...)
		dep.Spec.Template.Spec.Containers[0].Resources = corev1.ResourceRequirements{}
		Expect(env.Clients.Ctrl.Create(ctx, dep)).To(Succeed())
		_, err := framework.WaitForPodCountReady(ctx, env.Clients, ns.Name, "app=fallback", 1, framework.WaitOptions{Timeout: rolloutWaitTimeout})
		Expect(err).NotTo(HaveOccurred())
		registerContainerSeries(ctx, ns.Name, dep, samples)
		before := podUIDs(ctx, ns.Name, "app=fallback")
		p := framework.NewPolicyFixture(ns.Name, "fallback", api.ModeAuto, framework.WithStrategy("webhook", "immediate"), framework.WithReconciliationInterval(time.Second))
		p.Spec.UpdateStrategy.AllowInPlaceResize = true
		p.Spec.UpdateStrategy.AllowRecreate = false
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		waitForWorkloadReason(ctx, p, "resize preflight failed")
		Expect(podUIDs(ctx, ns.Name, "app=fallback")).To(Equal(before))
		updatePolicy(ctx, ns.Name, p.Name, func(p *api.OptimizationPolicy) { p.Spec.UpdateStrategy.AllowRecreate = true })
		pods := waitForReplacementPod(ctx, ns.Name, "app=fallback", before)
		expectAppliedContainerRequests(pods[0].Spec.Containers, samples, "explicit fallback")
	})
	It("blocks rolling restarts while a PDB has no disruption allowance", func(ctx context.Context) {
		ns := specNamespace(ctx, "pdb")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		samples := smokeSamples()[:1]
		dep := createResizeWorkload(ctx, ns.Name, "pdb", samples)
		registerContainerSeries(ctx, ns.Name, dep, samples)
		before := podUIDs(ctx, ns.Name, "app="+dep.Name)
		one := intstr.FromInt32(1)
		b := &policyv1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Name: "protect", Namespace: ns.Name}, Spec: policyv1.PodDisruptionBudgetSpec{MinAvailable: &one, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": dep.Name}}}}
		Expect(env.Clients.Ctrl.Create(ctx, b)).To(Succeed())
		p := framework.NewPolicyFixture(ns.Name, "pdb", api.ModeAuto, framework.WithStrategy("webhook", "immediate"), framework.WithReconciliationInterval(time.Second))
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		waitForWorkloadReason(ctx, p, "PodDisruptionBudget")
		Expect(podUIDs(ctx, ns.Name, "app="+dep.Name)).To(Equal(before))
		Expect(env.Clients.Ctrl.Delete(ctx, b)).To(Succeed())
		pods := waitForReplacementPod(ctx, ns.Name, "app="+dep.Name, before)
		expectAppliedContainerRequests(pods[0].Spec.Containers, samples, "restart after PDB release")
	})
})

func createResizeWorkload(ctx context.Context, namespace, name string, samples []containerSamples) *appsv1.Deployment {
	d := framework.NewDeploymentFixture(namespace, name, 1, smokeWebhookContainers(samples)...)
	Expect(env.Clients.Ctrl.Create(ctx, d)).To(Succeed())
	_, err := framework.WaitForPodCountReady(ctx, env.Clients, namespace, "app="+d.Name, 1, framework.WaitOptions{Timeout: rolloutWaitTimeout})
	Expect(err).NotTo(HaveOccurred())
	return d
}
