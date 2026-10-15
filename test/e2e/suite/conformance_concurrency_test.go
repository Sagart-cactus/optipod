package suite

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
)

// This scenario proves how overlapping current-policy and workload edits settle
// on an installed cluster (VAL-CONF-010). It exercises two shapes on one
// two-container Auto SSA workload:
//
//   - A deliberately stale workload write (read object, let the API advance its
//     resourceVersion, then write the stale object) surfaces an explicit
//     bounded conflict from the real API server; the rejected value never
//     lands, and a bounded retry on the fresh object preserves the newer
//     unrelated user field.
//   - A live overlap: concurrent user workload writes (an unrelated
//     annotation) race a concurrent current-policy edit (a resource-bound
//     change plus observe touches) through the real API. Every overlapping
//     write either succeeds or reports a resourceVersion conflict; the final
//     workload then converges on the CURRENT policy outcome (the changed
//     bound's clamped recommendation) while the newest user field and every
//     other unrelated user-owned field survive.
//
// The applied values are asserted on the installed workload object after the
// reconciles complete, the resourceVersion/generation history is recorded in
// the JUnit report entry, and the conflict is observed as a real
// apierrors.IsConflict. Merely launching concurrent operations is not
// sufficient: the final API state is what is asserted.
var _ = Describe("Concurrent policy and workload edits settle on current policy behavior", Label("conformance", "concurrency"), func() {
	It("surfaces an explicit conflict, preserves the newest unrelated user field and applies the current policy outcome", func(ctx context.Context) {
		ns := specNamespace(ctx, "conf-concurrency")
		DeferCleanup(func(ctx context.Context) {
			Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed(),
				"clean up the concurrency conformance spec namespace")
		})
		baseline := smokeSamples()
		clamped := concurrencyClampedSamples()

		By("creating a two-container workload carrying unrelated user-owned fields")
		workloadName, err := framework.UniqueName("workload")
		Expect(err).NotTo(HaveOccurred(), "generate unique concurrency workload name")
		workload := framework.NewDeploymentFixture(ns.Name, workloadName, 1, smokeContainers(baseline)...)
		enrichWithUnrelatedUserFields(workload)
		Expect(env.Clients.Ctrl.Create(ctx, workload)).To(Succeed(), "create the concurrency workload")
		Expect(framework.WaitForDeploymentAvailable(ctx, env.Clients, ns.Name, workloadName,
			framework.WaitOptions{Timeout: conformanceWorkloadReadyTimeout})).To(Succeed(),
			"the concurrency workload never became Available")
		podSelector := "app=" + workloadName
		beforeUIDs := podUIDs(ctx, ns.Name, podSelector)

		By("registering the baseline deterministic metric input")
		registerWorkloadSeries(ctx, ns.Name, workloadName, baseline)
		Expect(env.Fixture.ClearTrace(ctx)).To(Succeed(), "reset the fixture trace for this spec")

		By("creating the Auto SSA policy and observing the baseline outcome")
		policyName, err := framework.UniqueName("policy")
		Expect(err).NotTo(HaveOccurred(), "generate unique policy name")
		policy := framework.NewPolicyFixture(ns.Name, policyName, optipodv1alpha1.ModeAuto,
			framework.WithStrategy("ssa", ""),
			framework.WithReconciliationInterval(30*time.Second))
		Expect(env.Clients.Ctrl.Create(ctx, policy)).To(Succeed(), "create the concurrency Auto policy")
		createdAt := policy.CreationTimestamp.Time
		waitForWorkloadTemplateRequests(ctx, optipodv1alpha1.WorkloadTypeDeployment, ns.Name, workloadName, baseline)

		By("settling the baseline rollout and re-registering the series for the current pod")
		// The SSA apply edits the pod template, so the workload rolls; metrics
		// are per pod, so the current pod's series must be registered before
		// later reconciles can consume deterministic data.
		freshPods := waitForReplacementPod(ctx, ns.Name, podSelector, beforeUIDs)
		expectAppliedContainerRequests(freshPods[0].Spec.Containers, baseline,
			"the baseline replacement pod")
		waitForPodsToSettle(ctx, ns.Name, podSelector, beforeUIDs)
		registerWorkloadSeries(ctx, ns.Name, workloadName, baseline)

		baselineDep := readDeployment(ctx, ns.Name, workloadName)
		baselineRV, baselineGen := baselineDep.ResourceVersion, baselineDep.Generation

		By("attempting a stale workload write and observing an explicit bounded conflict")
		stale := readDeployment(ctx, ns.Name, workloadName)
		staleRV := stale.ResourceVersion
		// Advance the object's resourceVersion with a genuine newer user write.
		newer := writeDeploymentUserNote(ctx, ns.Name, workloadName, concurrencyUserNoteKey, concurrencyUserNoteNewer)
		Expect(newer.err).NotTo(HaveOccurred(),
			"the newer user write failed: %v (conflicts so far: %d)", newer.err, newer.conflicts)
		advanced := readDeployment(ctx, ns.Name, workloadName)
		expectResourceVersionAdvanced(advanced.ResourceVersion, staleRV,
			"the newer user write must advance the workload resourceVersion")
		// Now write the STALE object (its resourceVersion is the pre-write one).
		stale.Annotations[concurrencyUserNoteKey] = concurrencyUserNoteStale
		_, staleErr := env.Clients.Kube.AppsV1().Deployments(ns.Name).Update(ctx, stale, metav1.UpdateOptions{})
		Expect(staleErr).To(HaveOccurred(), "the stale workload write must be rejected by the API server")
		Expect(apierrors.IsConflict(staleErr)).To(BeTrue(),
			"the stale workload write returned %v; want an explicit resourceVersion conflict", staleErr)
		conflictSignal := fmt.Sprintf("stale workload write rejected with %T: %v", staleErr, staleErr)

		By("recovering with the fresh object and preserving the newer user field")
		recovered := writeDeploymentUserNote(ctx, ns.Name, workloadName, concurrencyUserNoteKey, concurrencyUserNoteRecovered)
		Expect(recovered.err).NotTo(HaveOccurred(), "recovering from the conflict failed: %v", recovered.err)
		afterConflict := readDeployment(ctx, ns.Name, workloadName)
		Expect(afterConflict.Annotations[concurrencyUserNoteKey]).To(Equal(concurrencyUserNoteRecovered),
			"the recovered user write did not persist")
		Expect(afterConflict.Annotations[concurrencyUserNoteKey]).NotTo(Equal(concurrencyUserNoteStale),
			"the rejected stale user value persisted anyway")
		expectUnappliedRequestsAfterConflict(afterConflict, baseline,
			"the workload after the conflict recovery must still carry the applied recommendation")

		By("recording the pre-overlap resourceVersion/generation history")
		prePhase := readDeployment(ctx, ns.Name, workloadName)
		prePhaseRV, prePhaseGen := prePhase.ResourceVersion, prePhase.Generation

		By("overlapping concurrent user workload writes with a concurrent current-policy edit")
		const workloadWrites = 6
		outcomes := make([]writeOutcome, workloadWrites)
		values := make([]string, workloadWrites)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			defer GinkgoRecover()
			for i := 0; i < workloadWrites; i++ {
				values[i] = fmt.Sprintf("concurrent-%d", i)
				outcomes[i] = writeDeploymentUserNote(ctx, ns.Name, workloadName,
					concurrencyUserNoteKey, values[i])
			}
		}()
		go func() {
			defer wg.Done()
			defer GinkgoRecover()
			// The current-policy edit: tighten the CPU upper bound so the
			// controller's recommendation for the app container is clamped,
			// then drive reconciles through the production watch path.
			updatePolicy(ctx, ns.Name, policyName, func(p *optipodv1alpha1.OptimizationPolicy) {
				p.Spec.ResourceBounds.CPU.Max = resource.MustParse("200m")
			})
			for i := 0; i < 4; i++ {
				touchPolicy(ctx, ns.Name, policyName)
			}
		}()
		wg.Wait()

		By("asserting every overlapping workload write either succeeded or surfaced a conflict")
		conflicts := 0
		for i := range outcomes {
			Expect(outcomes[i].err).NotTo(HaveOccurred(),
				"overlapping workload write %q failed with a non-conflict error: %v", values[i], outcomes[i].err)
			conflicts += outcomes[i].conflicts
		}

		By("waiting for the CURRENT policy outcome (the changed bound) to be applied")
		// One further touch guarantees the controller re-reconciles promptly
		// even if a concurrent user write momentarily reverted the template;
		// the bounded wait then observes the production re-apply.
		touchPolicy(ctx, ns.Name, policyName)
		waitForWorkloadTemplateRequests(ctx, optipodv1alpha1.WorkloadTypeDeployment, ns.Name, workloadName, clamped)

		By("asserting the newest user change, the current policy outcome and unrelated user fields all survive")
		final := readDeployment(ctx, ns.Name, workloadName)
		Expect(final.Annotations[concurrencyUserNoteKey]).To(Equal(values[workloadWrites-1]),
			"the newest unrelated user change was not preserved across the overlapping edits")
		expectContainerRequestsMatch(final.Spec.Template.Spec.Containers, clamped,
			"the workload pod template after the overlapping edits")
		Expect(final.Annotations[unrelatedAnnotationKey]).To(Equal(unrelatedAnnotationValue),
			"the overlapping edits dropped the user-owned deployment annotation %q", unrelatedAnnotationKey)
		Expect(final.Labels[unrelatedLabelKey]).To(Equal(unrelatedLabelValue),
			"the overlapping edits dropped the user-owned deployment label %q", unrelatedLabelKey)
		Expect(final.Spec.Template.Annotations[unrelatedTemplateAnnotationKey]).To(Equal(unrelatedTemplateAnnotationVal),
			"the overlapping edits dropped the user-owned pod template annotation %q", unrelatedTemplateAnnotationKey)
		Expect(final.Spec.Template.Spec.TerminationGracePeriodSeconds).NotTo(BeNil(),
			"the overlapping edits pruned the user-owned terminationGracePeriodSeconds")
		Expect(*final.Spec.Template.Spec.TerminationGracePeriodSeconds).To(Equal(int64(unrelatedGraceSeconds)),
			"the overlapping edits changed the user-owned terminationGracePeriodSeconds")

		By("asserting the resourceVersion and generation history advanced")
		expectResourceVersionAdvanced(prePhase.ResourceVersion, baselineRV,
			"the conflict phase must advance the workload resourceVersion from the baseline")
		expectResourceVersionAdvanced(final.ResourceVersion, prePhaseRV,
			"the overlap phase must advance the workload resourceVersion")
		Expect(final.Generation).To(BeNumerically(">", prePhaseGen),
			"the current-policy bound change must advance the workload generation (before %d, after %d)",
			prePhaseGen, final.Generation)
		Expect(final.Generation).To(BeNumerically(">", baselineGen),
			"the workload generation must advance from the baseline (%d) to the final state (%d)",
			baselineGen, final.Generation)
		AddReportEntry("concurrency-history", fmt.Sprintf(
			"baseline rv=%s gen=%d; after-conflict rv=%s gen=%d; final rv=%s gen=%d; %s; overlap conflicts=%d",
			baselineRV, baselineGen, prePhaseRV, prePhaseGen, final.ResourceVersion, final.Generation,
			conflictSignal, conflicts))

		By("waiting for the policy's reconciliation summary under the changed bound")
		waitForPolicyStatus(ctx, ns.Name, policyName, policyStatusWait{
			readyStatus:     "True",
			readyReason:     "PolicyValid",
			expectSummary:   true,
			discovered:      1,
			processed:       1,
			reconciledAfter: createdAt,
		})
	})
})

// Concurrency-only annotation keys and values. The user note key is the
// unrelated field the overlapping writes touch; the rejected value is written
// only through the deliberately stale object.
const (
	concurrencyUserNoteKey       = "team.example.com/notes"
	concurrencyUserNoteNewer     = "user-note-newer"
	concurrencyUserNoteStale     = "user-note-stale-should-not-persist"
	concurrencyUserNoteRecovered = "user-note-recovered"
)

// concurrencyClampedSamples derives the expected current-policy outcome after
// the CPU upper bound is tightened to 200m: the app container's raw 300m
// recommendation is clamped to the new maximum while the sidecar (150m) stays
// below it and every memory value stays inside its bound.
func concurrencyClampedSamples() []containerSamples {
	app := smokeApp
	app.expectedCPU = "200m"
	return []containerSamples{app, smokeSidecar}
}

// writeOutcome records how a bounded read-modify-write on the workload settled:
// how many attempts it needed, how many resourceVersion conflicts it absorbed,
// and any non-conflict failure.
type writeOutcome struct {
	attempts  int
	conflicts int
	err       error
}

// writeDeploymentUserNote performs a bounded read-modify-write of one unrelated
// user annotation through the real API, retrying only on resourceVersion
// conflicts (ordinary optimistic-concurrency behavior, not a functional wait).
// Any other error is surfaced.
func writeDeploymentUserNote(ctx context.Context, namespace, name, key, value string) writeOutcome {
	const maxAttempts = 10
	outcome := writeOutcome{}
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		outcome.attempts = attempt
		dep := &appsv1.Deployment{}
		if err := env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, dep); err != nil {
			outcome.err = fmt.Errorf("read workload %s/%s: %w", namespace, name, err)
			return outcome
		}
		if dep.Annotations == nil {
			dep.Annotations = map[string]string{}
		}
		dep.Annotations[key] = value
		if err := env.Clients.Ctrl.Update(ctx, dep); err != nil {
			if apierrors.IsConflict(err) {
				outcome.conflicts++
				time.Sleep(time.Duration(attempt) * 20 * time.Millisecond)
				continue
			}
			outcome.err = fmt.Errorf("update workload %s/%s annotation %q: %w", namespace, name, key, err)
			return outcome
		}
		return outcome
	}
	outcome.err = fmt.Errorf("update workload %s/%s annotation %q kept conflicting after %d attempts",
		namespace, name, key, maxAttempts)
	return outcome
}

// readDeployment reads the named Deployment through the private typed client.
func readDeployment(ctx context.Context, namespace, name string) *appsv1.Deployment {
	dep := &appsv1.Deployment{}
	Expect(env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, dep)).
		To(Succeed(), "read workload %s/%s", namespace, name)
	return dep
}

// expectResourceVersionAdvanced asserts the later resourceVersion is
// numerically greater than the earlier one, so the history is a real
// advancement rather than an unrelated re-read.
func expectResourceVersionAdvanced(later, earlier, what string) {
	l, errLater := strconv.ParseUint(later, 10, 64)
	e, errEarlier := strconv.ParseUint(earlier, 10, 64)
	Expect(errEarlier).NotTo(HaveOccurred(), "%s: resourceVersion %q is not numeric", what, earlier)
	Expect(errLater).NotTo(HaveOccurred(), "%s: resourceVersion %q is not numeric", what, later)
	Expect(l).To(BeNumerically(">", e), "%s: resourceVersion did not advance (%s -> %s)", what, earlier, later)
}

// expectContainerRequestsMatch asserts every sampled container of a container
// list carries exactly the sample's expected applied requests. It is the
// concurrency spec's local restatement of the exact-value check (the shared
// expectAppliedContainerRequests helper also asserts the original values were
// left, which does not hold once the policy outcome itself changed).
func expectContainerRequestsMatch(containers []corev1.Container, samples []containerSamples, what string) {
	for _, s := range samples {
		cpu, memory := containerRequests(containers, s.name)
		Expect(cpu).NotTo(BeNil(), "%s lost container %s", what, s.name)
		Expect(memory).NotTo(BeNil(), "%s lost container %s memory request", what, s.name)
		Expect(cpu.String()).To(Equal(s.expectedCPU),
			"%s: container %s CPU request is not the current policy outcome %s", what, s.name, s.expectedCPU)
		Expect(memory.String()).To(Equal(s.expectedMemory),
			"%s: container %s memory request is not the current policy outcome %s", what, s.name, s.expectedMemory)
	}
}

// expectUnappliedRequestsAfterConflict asserts the workload still carries its
// applied requests after the rejected stale write: the conflict must not have
// damaged the controller's applied outcome.
func expectUnappliedRequestsAfterConflict(dep *appsv1.Deployment, samples []containerSamples, what string) {
	for _, s := range samples {
		cpu, memory := containerRequests(dep.Spec.Template.Spec.Containers, s.name)
		Expect(cpu).NotTo(BeNil(), "%s lost container %s", what, s.name)
		Expect(memory).NotTo(BeNil(), "%s lost container %s memory request", what, s.name)
		Expect(cpu.String()).To(Equal(s.expectedCPU),
			"%s: container %s CPU request is not the applied recommendation %s", what, s.name, s.expectedCPU)
		Expect(memory.String()).To(Equal(s.expectedMemory),
			"%s: container %s memory request is not the applied recommendation %s", what, s.name, s.expectedMemory)
	}
}
