package suite

import (
	"context"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Parity: recommendation validity and stable rollout", Label("parity", "validity"), func() {
	It("admits expired and superseded plans without applying their resources", func(ctx context.Context) {
		ns := specNamespace(ctx, "parity-expiry")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		p := framework.NewPolicyFixture(ns.Name, "policy", api.ModeAuto)
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		for _, test := range []string{"fresh", "expired", "generation", "identity", "missing"} {
			a := probeRecommendationAnnotations(true, p)
			switch test {
			case "expired":
				a["optipod.io/recommendation-expires-at"] = time.Now().Add(-time.Minute).Format(time.RFC3339Nano)
			case "generation":
				a["optipod.io/applied-policy-generation"] = "99999"
			case "identity":
				a["optipod.io/applied-policy-uid"] = "another-policy"
			case "missing":
				delete(a, "optipod.io/recommendation-expires-at")
			}
			got, err := env.Clients.Kube.CoreV1().Pods(ns.Name).Create(ctx, newAdmissionProbePod(ns.Name, test, true, a), metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			if test == "fresh" {
				expectMutatedProbeRequests(got, test)
			} else {
				expectUnmutatedProbeRequests(got, test)
			}
		}
	})
	It("keeps pod identities and the template stable after an immediate plan is applied", func(ctx context.Context) {
		ns := specNamespace(ctx, "parity-noop")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		samples := smokeSamples()
		dep := framework.NewDeploymentFixture(ns.Name, "parity-noop", 1, smokeWebhookContainers(samples)...)
		Expect(env.Clients.Ctrl.Create(ctx, dep)).To(Succeed())
		Expect(framework.WaitForDeploymentAvailable(ctx, env.Clients, ns.Name, dep.Name, framework.WaitOptions{Timeout: rolloutWaitTimeout})).To(Succeed())
		selector := "app=" + dep.Name
		before := podUIDs(ctx, ns.Name, selector)
		registerContainerSeries(ctx, ns.Name, dep, samples)
		p := framework.NewPolicyFixture(ns.Name, "policy", api.ModeAuto, framework.WithStrategy("webhook", "immediate"), framework.WithReconciliationInterval(2*time.Second))
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		waitForTemplateRestartMarker(ctx, api.WorkloadTypeDeployment, ns.Name, dep.Name)
		pods := waitForReplacementPod(ctx, ns.Name, selector, before)
		expectAppliedContainerRequests(pods[0].Spec.Containers, samples, "immediate admission")
		waitForPodsToSettle(ctx, ns.Name, selector, before)
		registerContainerSeries(ctx, ns.Name, dep, samples)
		steady := captureSteadyState(ctx, ns.Name, dep.Name, selector)
		// An actual controller event proves the no-change apply path completed;
		// absence of restarts alone could otherwise hide a metrics failure.
		err := framework.WaitForCondition(ctx, framework.WaitOptions{Timeout: time.Minute, Description: "successful no-change application"}, func(ctx context.Context) (bool, error) {
			events, err := env.Clients.Kube.CoreV1().Events(ns.Name).List(ctx, metav1.ListOptions{FieldSelector: "involvedObject.uid=" + string(p.UID)})
			if err != nil {
				return false, err
			}
			for _, e := range events.Items {
				if strings.Contains(e.Message, "WebhookNoChange") {
					return true, nil
				}
			}
			return false, nil
		})
		Expect(err).NotTo(HaveOccurred())
		expectSteadyWorkload(ctx, steady, ns.Name, dep.Name, selector, "unchanged immediate plan")
	})
})
