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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Parity: stale plan revocation", Label("parity", "freshness"), func() {
	It("caps admission validity at the source deadline and revokes on lost metrics", func(ctx context.Context) {
		ns := specNamespace(ctx, "parity-freshness")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		samples := smokeSamples()[:1]
		dep := createSmokeWorkload(ctx, ns.Name, "freshness", samples)
		series := registerContainerSeries(ctx, ns.Name, dep, samples)
		p := framework.NewPolicyFixture(ns.Name, "freshness", api.ModeAuto, framework.WithReconciliationInterval(time.Second))
		p.Spec.MetricsConfig.MaxSampleAge.Duration = 10 * time.Second
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		got := waitForDeploymentAnnotation(ctx, ns.Name, dep.Name, webhookApplyAnnotations(samples))
		expires, err := time.Parse(time.RFC3339Nano, got.Annotations[webhook.RecommendationExpiry])
		Expect(err).NotTo(HaveOccurred())
		Expect(time.Until(expires)).To(BeNumerically("<=", 10*time.Second))
		pods, err := env.Clients.Kube.CoreV1().Pods(ns.Name).List(ctx, metav1.ListOptions{LabelSelector: "app=" + dep.Name})
		Expect(err).NotTo(HaveOccurred())
		Expect(env.Fixture.SetSeries(ctx, nil)).To(Succeed())
		Expect(framework.WaitForCondition(ctx, framework.WaitOptions{Timeout: time.Minute, Description: "revoked source plan"}, func(ctx context.Context) (bool, error) {
			d, err := env.Clients.Kube.AppsV1().Deployments(ns.Name).Get(ctx, dep.Name, metav1.GetOptions{})
			return err == nil && d.Annotations[webhook.RecommendationExpiry] == "", err
		})).To(Succeed())
		probe := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "revoked-probe", Namespace: ns.Name, Labels: map[string]string{api.AnnotationWebhookEnabled: "true"}, OwnerReferences: pods.Items[0].OwnerReferences}, Spec: *dep.Spec.Template.Spec.DeepCopy()}
		admitted, err := env.Clients.Kube.CoreV1().Pods(ns.Name).Create(ctx, probe, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		expectUnappliedContainerRequests(admitted.Spec.Containers, samples, "revoked metrics")
		Expect(env.Clients.Kube.CoreV1().Pods(ns.Name).Delete(ctx, probe.Name, metav1.DeleteOptions{})).To(Succeed())
		Expect(env.Fixture.SetSeries(ctx, series)).To(Succeed())
		Expect(framework.WaitForCondition(ctx, framework.WaitOptions{Timeout: time.Minute, Description: "renewed source plan"}, func(ctx context.Context) (bool, error) {
			d, err := env.Clients.Kube.AppsV1().Deployments(ns.Name).Get(ctx, dep.Name, metav1.GetOptions{})
			return err == nil && webhook.ValidRecommendations(d.Annotations, p, time.Now()), err
		})).To(Succeed())
		updatePolicy(ctx, ns.Name, p.Name, func(p *api.OptimizationPolicy) { p.Spec.MetricsConfig.Provider = "metrics-server" })
		waitForWorkloadReason(ctx, p, "differs from configured provider")
	})
})
