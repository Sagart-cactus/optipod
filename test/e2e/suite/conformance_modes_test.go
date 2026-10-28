package suite

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
)

// This scenario proves the installed controller's Disabled/Recommend/Auto
// transition outcomes on a matching two-container workload (VAL-CONF-001). A
// matching Disabled policy must add no recommendations or applications after
// a completed reconcile; updating it to Recommend must write the exact
// per-container recommendation annotations without touching requests, limits,
// templates or pod identity; updating it to Auto with the explicit SSA
// strategy must apply the exact recommended values and roll the pod once; and
// returning it to Disabled must prevent any further effect after changed
// metric input and a new completed reconcile. Each stage is distinguished by
// the workload's observed annotations/spec and the pod UID sequence, never by
// a fixture field or a stale absence check: every stage's policy status
// records a completed reconcile (LastReconciliation) after the stage's
// change, and the fixture trace proves which stages actually queried metrics.
//
// Production timing notes that shape the bounded waits (no fixed sleeps):
//   - A policy object update (mode/strategy change) triggers the controller's
//     watch-driven reconcile, so stage transitions are observed in seconds.
//   - A Disabled policy's requeue interval is 4x the configured interval, and
//     the status summary only refreshes when counts change or the last
//     reconcile is older than a minute. The final stage deliberately changes
//     only fixture data, so the post-change reconcile is observed through the
//     requeue loop: with the 30s interval used here the summary refresh lands
//     within the explicit 5-minute bound below.
var _ = Describe("Disabled, Recommend and Auto transitions change actual workload outcomes", Label("conformance", "modes"), func() {
	It("moves a matching policy through Disabled, Recommend, Auto and back to Disabled with distinct observed outcomes", func(ctx context.Context) {
		ns := specNamespace(ctx, "conf-modes")
		DeferCleanup(func(ctx context.Context) {
			Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed(),
				"clean up the mode-transition spec namespace")
		})
		samples := smokeSamples()

		By("creating a two-container workload whose pod becomes ready")
		workload := createSmokeWorkload(ctx, ns.Name, "conf-modes", samples)
		podSelector := "app=" + workload.Name
		beforeUIDs := podUIDs(ctx, ns.Name, podSelector)
		before := &appsv1.Deployment{}
		Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(workload), before)).
			To(Succeed(), "read the workload before the policy exists")
		beforeReplicaSets := replicaSetNames(ctx, ns.Name, podSelector)

		By("registering the first deterministic metric input and clearing the trace")
		series := registerContainerSeries(ctx, ns.Name, workload, samples)
		traceClearedAt := time.Now()
		Expect(env.Fixture.ClearTrace(ctx)).To(Succeed(), "reset the fixture trace for this spec")

		By("stage 1: a matching Disabled policy adds no recommendations or applications")
		policyName, err := framework.UniqueName("policy")
		Expect(err).NotTo(HaveOccurred(), "generate unique policy name")
		policy := framework.NewPolicyFixture(ns.Name, policyName, optipodv1alpha1.ModeDisabled,
			framework.WithReconciliationInterval(30*time.Second))
		Expect(env.Clients.Ctrl.Create(ctx, policy)).To(Succeed(), "create the Disabled policy")
		createdAt := policy.CreationTimestamp.Time
		// The completed reconcile is observed through the policy summary:
		// the workload was discovered (1) but not processed (0), because
		// production winner selection excludes Disabled policies entirely.
		waitForPolicyStatus(ctx, ns.Name, policyName, policyStatusWait{
			readyStatus:     "True",
			readyReason:     "PolicyValid",
			expectSummary:   true,
			discovered:      1,
			processed:       0,
			reconciledAfter: createdAt,
		})
		disabled := &appsv1.Deployment{}
		Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(workload), disabled)).
			To(Succeed(), "read the workload after the completed Disabled reconcile")
		expectNoOptipodAnnotations(disabled.Annotations, "the workload under the Disabled policy")
		expectUnappliedContainerRequests(disabled.Spec.Template.Spec.Containers, samples,
			"the workload under the Disabled policy")
		for _, s := range samples {
			cpuLimit, memLimit := containerLimits(disabled.Spec.Template.Spec.Containers, s.name)
			Expect(cpuLimit).To(BeNil(), "the Disabled policy gave container %s a CPU limit", s.name)
			Expect(memLimit).To(BeNil(), "the Disabled policy gave container %s a memory limit", s.name)
		}
		Expect(apiequality.Semantic.DeepEqual(disabled.Spec, before.Spec)).To(BeTrue(),
			"the Disabled policy changed the workload spec")
		Expect(disabled.Annotations["deployment.kubernetes.io/revision"]).To(
			Equal(before.Annotations["deployment.kubernetes.io/revision"]),
			"the Disabled policy triggered a new Deployment revision")
		Expect(replicaSetNames(ctx, ns.Name, podSelector)).To(Equal(beforeReplicaSets),
			"the Disabled policy caused a new ReplicaSet")
		Expect(podUIDs(ctx, ns.Name, podSelector)).To(Equal(beforeUIDs),
			"the Disabled policy restarted or replaced workload pods")
		expectNoFixtureQuerySince(ctx, ns.Name, traceClearedAt,
			"the completed Disabled reconcile")

		By("stage 2: updating to Recommend writes exact recommendations without applying")
		recommendAt := time.Now()
		updated := updatePolicy(ctx, ns.Name, policyName, func(p *optipodv1alpha1.OptimizationPolicy) {
			p.Spec.Mode = optipodv1alpha1.ModeRecommend
		})
		annotated := waitForDeploymentAnnotation(ctx, ns.Name, workload.Name, recommendationAnnotations(samples))
		expectManagementAnnotations(annotated, updated, updated.CreationTimestamp.Time)
		expectUnappliedContainerRequests(annotated.Spec.Template.Spec.Containers, samples,
			"the workload under the Recommend policy")
		Expect(apiequality.Semantic.DeepEqual(annotated.Spec, before.Spec)).To(BeTrue(),
			"Recommend changed the workload spec")
		Expect(replicaSetNames(ctx, ns.Name, podSelector)).To(Equal(beforeReplicaSets),
			"Recommend caused a new ReplicaSet")
		Expect(podUIDs(ctx, ns.Name, podSelector)).To(Equal(beforeUIDs),
			"Recommend restarted or replaced workload pods")
		waitForPolicyStatus(ctx, ns.Name, policyName, policyStatusWait{
			readyStatus:     "True",
			readyReason:     "PolicyValid",
			expectSummary:   true,
			discovered:      1,
			processed:       1,
			reconciledAfter: recommendAt,
		})
		expectFixtureQueried(ctx, series)

		By("stage 3: updating to Auto with the explicit SSA strategy applies exact values and rolls the pod")
		autoAt := time.Now()
		updatePolicy(ctx, ns.Name, policyName, func(p *optipodv1alpha1.OptimizationPolicy) {
			p.Spec.Mode = optipodv1alpha1.ModeAuto
			ssa := string(optipodv1alpha1.StrategySSA)
			p.Spec.UpdateStrategy.Strategy = &ssa
		})
		waitForDeploymentRequests(ctx, ns.Name, workload.Name, smokeApp.name,
			smokeApp.expectedCPU, smokeApp.expectedMemory)
		waitForDeploymentRequests(ctx, ns.Name, workload.Name, smokeSidecar.name,
			smokeSidecar.expectedCPU, smokeSidecar.expectedMemory)
		applied := &appsv1.Deployment{}
		Expect(env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: workload.Name}, applied)).
			To(Succeed(), "read the applied workload")
		expectAppliedContainerRequests(applied.Spec.Template.Spec.Containers, samples, "the applied pod template")
		freshPods := waitForReplacementPod(ctx, ns.Name, podSelector, beforeUIDs)
		expectAppliedContainerRequests(freshPods[0].Spec.Containers, samples, "the replacement pod after Auto applied")
		waitForPodsToSettle(ctx, ns.Name, podSelector, beforeUIDs)
		appliedUIDs := podUIDs(ctx, ns.Name, podSelector)
		Expect(appliedUIDs).NotTo(Equal(beforeUIDs),
			"Auto did not distinguish the pod identity from the Recommend stages")
		waitForPolicyStatus(ctx, ns.Name, policyName, policyStatusWait{
			readyStatus:     "True",
			readyReason:     "PolicyValid",
			expectSummary:   true,
			discovered:      1,
			processed:       1,
			reconciledAfter: autoAt,
		})

		By("stage 4: returning to Disabled stops processing after a confirmed reconcile")
		disabledAgainAt := time.Now()
		updatePolicy(ctx, ns.Name, policyName, func(p *optipodv1alpha1.OptimizationPolicy) {
			p.Spec.Mode = optipodv1alpha1.ModeDisabled
		})
		// The processed count changes 1 -> 0, so the summary (and its fresh
		// LastReconciliation) is written by the watch-driven reconcile
		// triggered by the mode update itself.
		waitForPolicyStatus(ctx, ns.Name, policyName, policyStatusWait{
			readyStatus:     "True",
			readyReason:     "PolicyValid",
			expectSummary:   true,
			discovered:      1,
			processed:       0,
			reconciledAfter: disabledAgainAt,
		})

		By("registering the changed metric input for the current pod set")
		// recoverySamples() serves distinctly different values under the same
		// policy bounds; only a controller that queried the changed series
		// could ever write them.
		changed := recoverySamples()
		registerWorkloadSeries(ctx, ns.Name, workload.Name, changed)
		changedAt := time.Now()
		Expect(env.Fixture.ClearTrace(ctx)).To(Succeed(),
			"reset the trace so any query for the changed input is attributable")

		By("waiting for the completed post-change Disabled reconcile through the requeue loop")
		// A Disabled policy reconciles at 4x the 30s interval and the summary
		// only refreshes once the last recorded reconcile is older than a
		// minute, so the completed post-change reconcile is observed within
		// this explicit 5-minute bound by watching the policy status.
		const disabledRequeueTimeout = 5 * time.Minute
		reconciled := waitForReconcileAfter(ctx, ns.Name, policyName, changedAt, disabledRequeueTimeout)
		Expect(reconciled.Status.WorkloadsDiscovered).To(Equal(1),
			"the post-change Disabled reconcile lost the discovered workload")
		Expect(reconciled.Status.WorkloadsProcessed).To(Equal(0),
			"the post-change Disabled reconcile processed the workload")

		By("asserting the changed metric input produced no new workload effect")
		final := &appsv1.Deployment{}
		Expect(env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: workload.Name}, final)).
			To(Succeed(), "read the workload after the post-change Disabled reconcile")
		for key, value := range recommendationAnnotations(samples) {
			Expect(final.Annotations[key]).To(Equal(value),
				"the Disabled stage overwrote recommendation annotation %q; the changed metric input must not reach the workload", key)
		}
		for _, s := range changed {
			cpuKey := fmt.Sprintf("%s.%s.cpu-request", optipodv1alpha1.AnnotationRecommendationPrefix, s.name)
			Expect(final.Annotations[cpuKey]).NotTo(Equal(s.expectedCPU),
				"the Disabled stage wrote the changed CPU value for container %s", s.name)
		}
		expectAppliedContainerRequests(final.Spec.Template.Spec.Containers, samples,
			"the workload after the post-change Disabled reconcile")
		Expect(podUIDs(ctx, ns.Name, podSelector)).To(Equal(appliedUIDs),
			"the post-change Disabled reconcile rolled workload pods")
		expectNoFixtureQuerySince(ctx, ns.Name, changedAt,
			"the completed post-change Disabled reconcile")
	})
})
