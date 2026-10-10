package suite

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/application"
	"github.com/optipod/optipod/test/e2e/framework"
)

// This scenario proves the SSA and webhook strategy boundaries on an
// installed workload (VAL-CONF-009) through one complete SSA -> webhook ->
// SSA sequence on the same two-container workload:
//
//   - Under the explicit SSA strategy the controller updates the intended
//     per-container request fields on the pod template while preserving
//     unrelated user-owned fields, and the live object's managedFields show
//     the production "optipod" Server-Side Apply owning exactly the request
//     leaves it applied.
//   - Switching the policy's strategy field to webhook is NOT a migration by
//     itself: across a completed reconcile the workload's SSA-applied
//     template values, revision, ReplicaSets and pods stay unchanged while
//     the webhook metadata (per-container webhook annotation family plus the
//     webhook-enabled and strategy markers) is recorded on the workload.
//   - With changed metric input under the webhook strategy, the newly
//     stored recommendations still edit no template resources, and a
//     deliberate restart admits a naturally generated pod carrying the
//     CHANGED values — proof of real admission mutation, since the template
//     still holds the older SSA-applied values at that moment.
//   - Returning the policy to SSA produces a new observable resource update:
//     the changed values are applied to the template under the same optipod
//     field-manager ownership and the workload rolls once to a replacement
//     pod carrying them.
//
// The pod template carries the production optipod.io/webhook-enabled label
// before creation, so the regenerated pods are admitted naturally through the
// installed mutating webhook; nothing is hand-labelled after generation.
var _ = Describe("SSA and webhook strategy boundaries on the installed workload", Label("conformance", "strategy"), func() {
	It("applies SSA fields, records webhook metadata without template migration, admits changed recommendations on fresh pods, and re-applies on the return to SSA", func(ctx context.Context) {
		ns := specNamespace(ctx, "conf-strategy")
		DeferCleanup(func(ctx context.Context) {
			Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed(),
				"clean up the strategy-boundary conformance spec namespace")
		})
		applied := smokeSamples()           // the first (SSA-applied) recommendation set
		changed := strategyChangedSamples() // the changed set stored under webhook and re-applied by SSA

		By("creating a two-container webhook-eligible workload carrying unrelated user-owned fields")
		workloadName, err := framework.UniqueName("workload")
		Expect(err).NotTo(HaveOccurred(), "generate unique strategy-boundary workload name")
		workload := framework.NewDeploymentFixture(ns.Name, workloadName, 1, smokeWebhookContainers(applied)...)
		enrichWithUnrelatedUserFields(workload)
		Expect(env.Clients.Ctrl.Create(ctx, workload)).To(Succeed(), "create the strategy-boundary workload")
		Expect(framework.WaitForDeploymentAvailable(ctx, env.Clients, ns.Name, workloadName,
			framework.WaitOptions{Timeout: conformanceWorkloadReadyTimeout})).To(Succeed(),
			"the strategy-boundary workload never became Available")
		podSelector := "app=" + workloadName
		beforeUIDs := podUIDs(ctx, ns.Name, podSelector)

		By("registering the first deterministic metric input")
		registerWorkloadSeries(ctx, ns.Name, workloadName, applied)
		Expect(env.Fixture.ClearTrace(ctx)).To(Succeed(), "reset the fixture trace for this spec")

		By("creating the Auto policy with the explicit SSA strategy and waiting for the first apply")
		policyName, err := framework.UniqueName("policy")
		Expect(err).NotTo(HaveOccurred(), "generate unique policy name")
		policy := framework.NewPolicyFixture(ns.Name, policyName, optipodv1alpha1.ModeAuto,
			framework.WithStrategy("ssa", ""),
			framework.WithReconciliationInterval(30*time.Second))
		Expect(env.Clients.Ctrl.Create(ctx, policy)).To(Succeed(), "create the SSA Auto policy")
		createdAt := policy.CreationTimestamp.Time
		waitForWorkloadTemplateRequests(ctx, optipodv1alpha1.WorkloadTypeDeployment, ns.Name, workloadName, applied)
		firstApplied := &appsv1.Deployment{}
		Expect(env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: workloadName}, firstApplied)).
			To(Succeed(), "read the SSA-applied workload")
		expectManagementAnnotations(firstApplied, policy, createdAt)
		for key, value := range recommendationAnnotations(applied) {
			Expect(firstApplied.Annotations[key]).To(Equal(value),
				"the SSA-applied workload recommendation annotation %q has the wrong value", key)
		}
		expectOptipodRequestLeafOwnership(firstApplied, applied)

		By("rolling to the replacement pod that carries the applied values")
		Expect(framework.WaitForDeploymentAvailable(ctx, env.Clients, ns.Name, workloadName,
			framework.WaitOptions{Timeout: conformanceWorkloadReadyTimeout})).To(Succeed(),
			"the applied workload never returned to Available")
		freshPods := waitForReplacementPod(ctx, ns.Name, podSelector, beforeUIDs)
		expectAppliedContainerRequests(freshPods[0].Spec.Containers, applied,
			"the replacement pod after the first SSA apply")
		waitForPodsToSettle(ctx, ns.Name, podSelector, beforeUIDs)
		registerWorkloadSeries(ctx, ns.Name, workloadName, applied)
		steady := captureSteadyState(ctx, ns.Name, workloadName, podSelector)

		By("switching the policy strategy to webhook without touching the workload")
		reference := fetchPolicy(ctx, ns.Name, policyName)
		Expect(reference.Status.LastReconciliation).NotTo(BeNil(),
			"the applied policy records no completed reconcile to reference")
		updatePolicy(ctx, ns.Name, policyName, func(p *optipodv1alpha1.OptimizationPolicy) {
			s := string(optipodv1alpha1.StrategyWebhook)
			p.Spec.UpdateStrategy.Strategy = &s
		})

		By("waiting for a completed reconcile and the webhook metadata under the new strategy")
		reconciled := waitForNextReconcile(ctx, ns.Name, policyName, reference.Status.LastReconciliation.Time)
		Expect(reconciled.Status.WorkloadsProcessed).To(Equal(1),
			"the completed webhook-strategy reconcile did not process the workload")
		waitForDeploymentAnnotation(ctx, ns.Name, workloadName, webhookApplyAnnotations(applied))

		By("asserting the strategy-field edit alone did not migrate or churn the workload")
		expectSteadyWorkload(ctx, steady, ns.Name, workloadName, podSelector,
			"the completed webhook-strategy reconcile")
		webhookView, err := readWorkloadView(ctx, optipodv1alpha1.WorkloadTypeDeployment, ns.Name, workloadName)
		Expect(err).NotTo(HaveOccurred(), "read the workload after the strategy switch")
		expectContainerRequestsEqual(webhookView.containers, applied,
			"the pod template after the strategy switch (webhook edits no template resources)")

		By("registering changed metrics and waiting for the new recommendations in the stored metadata")
		// The recommendation-annotation write is throttled for 30s after the
		// last successful write, so the window is waited out before the touch
		// that must store the changed values.
		waitForAnnotationThrottleWindow(ctx, ns.Name, workloadName)
		registerWorkloadSeries(ctx, ns.Name, workloadName, changed)
		Expect(env.Fixture.ClearTrace(ctx)).To(Succeed(),
			"reset the trace so the changed-data reconcile is attributable")
		touchPolicy(ctx, ns.Name, policyName)
		wantChanged := recommendationAnnotations(changed)
		for key, value := range webhookApplyAnnotations(changed) {
			wantChanged[key] = value
		}
		waitForDeploymentAnnotation(ctx, ns.Name, workloadName, wantChanged)

		By("asserting the changed recommendations still edited no template resources")
		changedView, err := readWorkloadView(ctx, optipodv1alpha1.WorkloadTypeDeployment, ns.Name, workloadName)
		Expect(err).NotTo(HaveOccurred(), "read the workload while the changed recommendations are stored")
		expectContainerRequestsEqual(changedView.containers, applied,
			"the pod template while the changed recommendations are stored (webhook edits no template resources)")
		expectNoTemplateRestartMarker(ctx, optipodv1alpha1.WorkloadTypeDeployment, ns.Name, workloadName,
			"the workload under the webhook strategy with the default rollout")

		By("deliberately restarting the pod so the naturally regenerated pod is admitted with the changed values")
		deleteWorkloadPod(ctx, ns.Name, podSelector)
		admittedPods := waitForReplacementPod(ctx, ns.Name, podSelector, steady.podUIDs)
		waitForPodsToSettle(ctx, ns.Name, podSelector, steady.podUIDs)
		Expect(admittedPods).To(HaveLen(1), "the deliberate restart must have produced exactly one replacement pod")
		expectAppliedContainerRequests(admittedPods[0].Spec.Containers, changed,
			"the naturally admitted pod carrying values the pod template never held (webhook admission mutation)")

		By("re-registering the changed series and switching the policy back to SSA")
		changedSeries := registerWorkloadSeries(ctx, ns.Name, workloadName, changed)
		admittedUIDs := podUIDs(ctx, ns.Name, podSelector)
		updatePolicy(ctx, ns.Name, policyName, func(p *optipodv1alpha1.OptimizationPolicy) {
			s := string(optipodv1alpha1.StrategySSA)
			p.Spec.UpdateStrategy.Strategy = &s
		})

		By("waiting for the new SSA resource update on the pod template")
		waitForWorkloadTemplateRequests(ctx, optipodv1alpha1.WorkloadTypeDeployment, ns.Name, workloadName, changed)
		final := &appsv1.Deployment{}
		Expect(env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: workloadName}, final)).
			To(Succeed(), "read the workload after the return to SSA")
		expectOptipodRequestLeafOwnership(final, changed)
		expectManagementAnnotations(final, policy, createdAt)

		By("rolling to the replacement pod that carries the re-applied values")
		Expect(framework.WaitForDeploymentAvailable(ctx, env.Clients, ns.Name, workloadName,
			framework.WaitOptions{Timeout: conformanceWorkloadReadyTimeout})).To(Succeed(),
			"the re-applied workload never returned to Available")
		finalPods := waitForReplacementPod(ctx, ns.Name, podSelector, admittedUIDs)
		expectAppliedContainerRequests(finalPods[0].Spec.Containers, changed,
			"the replacement pod after the return to SSA")
		waitForPodsToSettle(ctx, ns.Name, podSelector, admittedUIDs)

		By("asserting unrelated user-owned fields survived the whole strategy sequence")
		Expect(final.Annotations[unrelatedAnnotationKey]).To(Equal(unrelatedAnnotationValue),
			"the strategy sequence dropped the user-owned deployment annotation %q", unrelatedAnnotationKey)
		Expect(final.Labels[unrelatedLabelKey]).To(Equal(unrelatedLabelValue),
			"the strategy sequence dropped the user-owned deployment label %q", unrelatedLabelKey)
		Expect(final.Spec.Replicas).NotTo(BeNil(), "the workload lost its replica count")
		Expect(*final.Spec.Replicas).To(Equal(int32(1)), "the strategy sequence changed the workload replica count")
		template := final.Spec.Template
		Expect(template.Labels["app"]).To(Equal(workloadName),
			"the strategy sequence pruned the pod template selector labels")
		Expect(template.Labels["optipod.io/webhook-enabled"]).To(Equal("true"),
			"the strategy sequence pruned the production webhook-enabling pod-template label")
		Expect(template.Annotations[unrelatedTemplateAnnotationKey]).To(Equal(unrelatedTemplateAnnotationVal),
			"the strategy sequence dropped the user-owned pod template annotation %q", unrelatedTemplateAnnotationKey)
		Expect(template.Spec.TerminationGracePeriodSeconds).NotTo(BeNil(),
			"the strategy sequence pruned the pod template terminationGracePeriodSeconds")
		Expect(*template.Spec.TerminationGracePeriodSeconds).To(Equal(int64(unrelatedGraceSeconds)),
			"the strategy sequence changed the pod template terminationGracePeriodSeconds")
		appIdx := containerIndex(template.Spec.Containers, smokeApp.name)
		Expect(appIdx).NotTo(Equal(-1), "the pod template lost the app container")
		appContainer := template.Spec.Containers[appIdx]
		Expect(appContainer.Image).To(Equal(framework.WorkloadImage()),
			"the strategy sequence replaced the container image, an unrelated field")
		gotEnv, hasEnv := envValue(appContainer.Env, unrelatedEnvKey)
		Expect(hasEnv).To(BeTrue(), "the strategy sequence dropped the user-owned container env %q", unrelatedEnvKey)
		Expect(gotEnv).To(Equal(unrelatedEnvValue),
			"the user-owned container env %q changed", unrelatedEnvKey)

		By("waiting for the policy's reconciliation summary")
		waitForPolicyStatus(ctx, ns.Name, policyName, policyStatusWait{
			readyStatus:     "True",
			readyReason:     "PolicyValid",
			expectSummary:   true,
			discovered:      1,
			processed:       1,
			reconciledAfter: createdAt,
		})

		By("proving the deployed controller queried the fixture for the admitted pod's changed series")
		expectFixtureQueried(ctx, changedSeries)
	})
})

// strategyChangedSamples are the changed recommendation values used once the
// policy runs under the webhook strategy: distinct from the SSA-applied
// values, so a freshly admitted pod carrying them proves real webhook
// admission mutation (the pod template still held the SSA-applied values at
// that moment) rather than template inheritance.
func strategyChangedSamples() []containerSamples {
	return []containerSamples{
		{name: "app", cpuCores: 0.5, memoryBytes: 150 << 20,
			initialCPU: "100m", initialMemory: "64Mi",
			expectedCPU: "600m", expectedMemory: "180Mi"},
		{name: "sidecar", cpuCores: 0.25, memoryBytes: 100 << 20,
			initialCPU: "50m", initialMemory: "32Mi",
			expectedCPU: "300m", expectedMemory: "120Mi"},
	}
}

// expectOptipodRequestLeafOwnership asserts the live workload's managedFields
// show the production "optipod" field manager's Server-Side Apply entries
// owning exactly the per-container request leaves for every sampled
// container and nothing unrelated (no status subresource, no unrelated field
// paths).
func expectOptipodRequestLeafOwnership(dep *appsv1.Deployment, samples []containerSamples) {
	applyEntries := optipodApplyEntries(dep)
	Expect(applyEntries).NotTo(BeEmpty(),
		"the workload has no managedFields entry from field manager %q created by Server-Side Apply; "+
			"the values were not applied through the production SSA path",
		application.FieldManagerName)
	required := map[string]bool{}
	for _, s := range samples {
		required[ssaRequestLeaf(s.name, "cpu")] = true
		required[ssaRequestLeaf(s.name, "memory")] = true
	}
	for _, entry := range applyEntries {
		Expect(entry.Subresource).To(BeEmpty(),
			"the optipod Server-Side Apply entry owns status subresource fields")
		leaves, err := managedFieldLeaves(entry)
		Expect(err).NotTo(HaveOccurred(), "decode the optipod apply managed-fields set")
		for _, leaf := range leaves {
			delete(required, leaf)
			for _, fragment := range unrelatedLeafFragments {
				Expect(leaf).NotTo(ContainSubstring(fragment),
					"the optipod Server-Side Apply owns the unrelated field path %q", leaf)
			}
		}
	}
	Expect(required).To(BeEmpty(),
		"the optipod Server-Side Apply does not own the required request leaves %v", sortedKeys(required))
}
