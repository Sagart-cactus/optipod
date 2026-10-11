package suite

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
)

// This scenario proves the installed controller's default webhook rollout
// timing on a naturally admitted workload (VAL-CONF-007). A policy omitting
// the rollout strategy selects the production default (onNextRestart): new
// recommendation metadata — the per-container recommendation and webhook
// annotation families — must NOT itself restart running pods or edit the
// workload's resource template during a bounded completed reconcile. Only a
// deliberate workload restart produces naturally generated, newly admitted
// pods, and those pods carry every container's recommended resources while
// the template and the original pod keep the baseline requests throughout.
//
// The pod template carries the production optipod.io/webhook-enabled label
// before creation, so the regenerated pod is admitted through the installed
// mutating webhook naturally; nothing is hand-labelled after generation.
var _ = Describe("Webhook onNextRestart rollout timing does not restart running pods", Label("conformance", "webhook"), func() {
	It("stores recommendation metadata without restarting pods and a deliberate restart admits the recommended values", func(ctx context.Context) {
		ns := specNamespace(ctx, "conf-rollout-onnext")
		DeferCleanup(func(ctx context.Context) {
			Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed(),
				"clean up the rollout-timing conformance spec namespace")
		})
		samples := smokeSamples()

		By("creating the two-container webhook-eligible workload whose pod becomes ready")
		workloadName, err := framework.UniqueName("workload")
		Expect(err).NotTo(HaveOccurred(), "generate unique rollout-timing workload name")
		workload := framework.NewDeploymentFixture(ns.Name, workloadName, 1, smokeWebhookContainers(samples)...)
		Expect(env.Clients.Ctrl.Create(ctx, workload)).To(Succeed(), "create the rollout-timing workload")
		Expect(framework.WaitForDeploymentAvailable(ctx, env.Clients, ns.Name, workloadName,
			framework.WaitOptions{Timeout: conformanceWorkloadReadyTimeout})).To(Succeed(),
			"the rollout-timing workload never became Available")
		podSelector := "app=" + workloadName
		beforeUIDs := podUIDs(ctx, ns.Name, podSelector)

		By("registering deterministic metric samples for the workload's pod")
		series := registerWorkloadSeries(ctx, ns.Name, workloadName, samples)
		Expect(env.Fixture.ClearTrace(ctx)).To(Succeed(), "reset the fixture trace for this spec")

		By("creating an Auto policy that omits the rollout strategy (production default onNextRestart)")
		policyName, err := framework.UniqueName("policy")
		Expect(err).NotTo(HaveOccurred(), "generate unique policy name")
		policy := framework.NewPolicyFixture(ns.Name, policyName, optipodv1alpha1.ModeAuto,
			framework.WithDefaultStrategy(),
			framework.WithReconciliationInterval(30*time.Second))
		Expect(env.Clients.Ctrl.Create(ctx, policy)).To(Succeed(), "create the default-rollout Auto policy")
		createdAt := policy.CreationTimestamp.Time

		By("waiting for every container's recommendation and webhook metadata on the workload")
		want := recommendationAnnotations(samples)
		for key, value := range webhookApplyAnnotations(samples) {
			want[key] = value
		}
		annotated := waitForDeploymentAnnotation(ctx, ns.Name, workloadName, want)
		expectManagementAnnotations(annotated, policy, createdAt)
		expectUnappliedContainerRequests(annotated.Spec.Template.Spec.Containers, samples,
			"the pod template after the metadata write (onNextRestart edits no resource template)")

		By("capturing the steady state and waiting for a further completed reconcile")
		// The completed reconcile is the controller's genuine opportunity to
		// restart pods if the default rollout were immediate; every stable
		// observable must survive it unchanged.
		steady := captureSteadyState(ctx, ns.Name, workloadName, podSelector)
		reference := fetchPolicy(ctx, ns.Name, policyName)
		Expect(reference.Status.LastReconciliation).NotTo(BeNil(),
			"the applied policy records no completed reconcile to reference")
		reconciled := waitForNextReconcile(ctx, ns.Name, policyName, reference.Status.LastReconciliation.Time)
		Expect(reconciled.Status.WorkloadsProcessed).To(Equal(1),
			"the completed default-rollout reconcile did not process the workload again")

		By("asserting the completed reconcile restarted no pods and edited no template")
		expectSteadyWorkload(ctx, steady, ns.Name, workloadName, podSelector,
			"the completed onNextRestart reconcile")
		expectNoTemplateRestartMarker(ctx, ns.Name, workloadName,
			"the workload under the default onNextRestart rollout")
		livePods, err := env.Clients.Kube.CoreV1().Pods(ns.Name).List(ctx,
			metav1.ListOptions{LabelSelector: podSelector})
		Expect(err).NotTo(HaveOccurred(), "list the workload's pods after the completed reconcile")
		Expect(livePods.Items).To(HaveLen(1), "the workload must still have exactly its original pod")
		expectUnappliedContainerRequests(livePods.Items[0].Spec.Containers, samples,
			"the running pod after the completed onNextRestart reconcile")

		By("deliberately restarting the workload so the Deployment controller regenerates the pod naturally")
		deleteWorkloadPod(ctx, ns.Name, podSelector)
		freshPods := waitForReplacementPod(ctx, ns.Name, podSelector, beforeUIDs)
		waitForPodsToSettle(ctx, ns.Name, podSelector, beforeUIDs)

		By("asserting the naturally regenerated pod was admitted with every container's recommended values")
		Expect(freshPods).To(HaveLen(1), "the deliberate restart must have produced exactly one replacement pod")
		expectAppliedContainerRequests(freshPods[0].Spec.Containers, samples,
			"the deliberately restarted, naturally admitted pod")
		restarted := &appsv1.Deployment{}
		Expect(env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: workloadName}, restarted)).
			To(Succeed(), "read the workload after the deliberate restart")
		expectUnappliedContainerRequests(restarted.Spec.Template.Spec.Containers, samples,
			"the pod template after the deliberate restart (the restart was the user's, not a controller template edit)")
		expectNoTemplateRestartMarker(ctx, ns.Name, workloadName,
			"the workload after the deliberate restart")

		By("waiting for the policy's reconciliation summary")
		waitForPolicyStatus(ctx, ns.Name, policyName, policyStatusWait{
			readyStatus:     "True",
			readyReason:     "PolicyValid",
			expectSummary:   true,
			discovered:      1,
			processed:       1,
			reconciledAfter: createdAt,
		})

		By("proving the deployed controller queried the fixture for the original pod")
		expectFixtureQueried(ctx, series)
	})
})
