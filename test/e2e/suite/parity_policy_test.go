package suite

import (
	"context"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
)

var _ = Describe("Parity: final admission policy controls", Label("parity", "policy-safety"), func() {
	It("stops previously enabled mutations on Recommend and on a higher-weight Disabled exclusion", func(ctx context.Context) {
		ns := specNamespace(ctx, "parity-stop")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		samples := smokeSamples()
		dep := createSmokeWorkload(ctx, ns.Name, "parity-stop", samples)
		registerContainerSeries(ctx, ns.Name, dep, samples)
		name, err := framework.UniqueName("auto")
		Expect(err).NotTo(HaveOccurred())
		p := framework.NewPolicyFixture(ns.Name, name, api.ModeAuto, framework.WithStrategy("webhook", "onNextRestart"), framework.WithWeight(10), framework.WithReconciliationInterval(10*time.Second))
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		waitForDeploymentAnnotation(ctx, ns.Name, dep.Name, map[string]string{api.AnnotationWebhookEnabled: "true"})
		pods, err := env.Clients.Kube.CoreV1().Pods(ns.Name).List(ctx, metav1.ListOptions{LabelSelector: "app=" + dep.Name})
		Expect(err).NotTo(HaveOccurred())
		Expect(pods.Items).NotTo(BeEmpty())
		// A real API CREATE with the workload's owner chain exercises the
		// installed TLS webhook and the controller-generated annotations.
		admit := func() *corev1.Pod {
			n, err := framework.UniqueName("admission")
			Expect(err).NotTo(HaveOccurred())
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: n, Namespace: ns.Name, Labels: dep.Spec.Template.Labels, OwnerReferences: pods.Items[0].OwnerReferences}, Spec: *dep.Spec.Template.Spec.DeepCopy()}
			pod.Labels[api.AnnotationWebhookEnabled] = "true"
			got, err := env.Clients.Kube.CoreV1().Pods(ns.Name).Create(ctx, pod, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			return got
		}
		expectAppliedContainerRequests(admit().Spec.Containers, samples, "Auto admission")
		updatePolicy(ctx, ns.Name, name, func(p *api.OptimizationPolicy) { p.Spec.Mode = api.ModeRecommend })
		expectUnappliedContainerRequests(admit().Spec.Containers, samples, "Recommend must stop existing annotations")
		resumed := updatePolicy(ctx, ns.Name, name, func(p *api.OptimizationPolicy) { p.Spec.Mode = api.ModeAuto })
		waitForDeploymentAnnotation(ctx, ns.Name, dep.Name, map[string]string{"optipod.io/applied-policy-generation": strconv.FormatInt(resumed.Generation, 10)})
		exclusion := framework.NewPolicyFixture(ns.Name, "exclude", api.ModeDisabled, framework.WithWeight(100))
		Expect(env.Clients.Ctrl.Create(ctx, exclusion)).To(Succeed())
		expectUnappliedContainerRequests(admit().Spec.Containers, samples, "Disabled exclusion must win")
		Expect(env.Clients.Ctrl.Delete(ctx, exclusion)).To(Succeed())
		Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(p), p)).To(Succeed())
		expectAppliedContainerRequests(admit().Spec.Containers, samples, "Auto resumes without exclusion")
	})
})
