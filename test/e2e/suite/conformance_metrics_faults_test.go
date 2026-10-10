package suite

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
)

// This scenario proves the installed controller's behavior against provider
// faults and its recovery on a real cluster (VAL-CONF-006). The deployed
// controller is pointed at the in-cluster deterministic fixture; after it has
// applied a known baseline to a two-container Auto SSA workload, the fixture
// is switched — independently per phase — to an empty (successful but
// result-less) response, a well-formed response with the wrong result type,
// and an HTTP 500 unavailable response.
//
// For each fault the spec triggers a fresh reconcile (a benign policy touch
// drives the production watch path immediately; no sleeps), then requires:
//   - the explicit whole-workload skip signal: a WorkloadSkipped event on the
//     policy whose message carries the production "Missing metrics:" reason
//     and the phase-specific provider error,
//   - a fixture trace query at or after the fault took effect, proving the
//     controller attempted collection,
//   - NO new application: the applied template values, the recommendation
//     annotation values, the revision/ReplicaSet/pod identities are all
//     exactly the pre-fault state,
//   - the policy's Ready=True status and processed=1 counts persist — which is
//     precisely why the contract says policy Ready by itself is not evidence
//     of successful metrics processing.
//
// Restoring valid metrics with changed values must resume processing: the new
// recommendations are stored and applied, the workload rolls once, and the
// replacement pod carries the recovered values.
var _ = Describe("Provider faults skip without false application and processing resumes after restoration", Label("conformance", "metrics-fault"), func() {
	It("reports independent empty, invalid and unavailable faults without new application and recovers after valid data returns", func(ctx context.Context) {
		ns := specNamespace(ctx, "conf-faults")
		DeferCleanup(func(ctx context.Context) {
			Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed(),
				"clean up the metrics-fault conformance spec namespace")
		})
		samples := smokeSamples()

		By("creating the two-container workload whose pod becomes ready")
		workload := createKindWorkload(ctx, optipodv1alpha1.WorkloadTypeDeployment,
			ns.Name, "conf-faults", samples, nil)
		podSelector := "app=" + workload
		beforeUIDs := podUIDs(ctx, ns.Name, podSelector)

		By("registering the baseline metric inputs")
		registerWorkloadSeries(ctx, ns.Name, workload, samples)
		Expect(env.Fixture.ClearTrace(ctx)).To(Succeed(), "reset the fixture trace for this spec")

		By("creating the Auto SSA policy and waiting for the baseline application")
		policyName, err := framework.UniqueName("policy")
		Expect(err).NotTo(HaveOccurred(), "generate unique policy name")
		policy := framework.NewPolicyFixture(ns.Name, policyName, optipodv1alpha1.ModeAuto,
			framework.WithStrategy("ssa", ""),
			framework.WithReconciliationInterval(30*time.Second))
		Expect(env.Clients.Ctrl.Create(ctx, policy)).To(Succeed(), "create the fault-recovery Auto policy")
		createdAt := policy.CreationTimestamp.Time
		waitForWorkloadTemplateRequests(ctx, optipodv1alpha1.WorkloadTypeDeployment, ns.Name, workload, samples)
		freshPods := waitForReplacementPod(ctx, ns.Name, podSelector, beforeUIDs)
		expectAppliedContainerRequests(freshPods[0].Spec.Containers, samples, "the baseline replacement pod")
		waitForPodsToSettle(ctx, ns.Name, podSelector, beforeUIDs)

		By("capturing the baseline steady state and applied annotation values")
		// Re-register the series for the replacement pod so every later
		// reconcile consumes deterministic data; then snapshot the steady
		// observables the fault phases must not disturb.
		registerWorkloadSeries(ctx, ns.Name, workload, samples)
		steady := captureSteadyState(ctx, ns.Name, workload, podSelector)
		baselineAnnotated := waitForDeploymentAnnotation(ctx, ns.Name, workload,
			recommendationAnnotations(samples))
		for key, value := range recommendationAnnotations(samples) {
			Expect(baselineAnnotated.Annotations[key]).To(Equal(value),
				"baseline annotation %q has the wrong value", key)
		}

		// The policy summary stays Ready=True with the workload counted as
		// processed even while metrics processing later fails, so it must
		// never be treated as processing evidence.
		baselinePolicy := fetchPolicy(ctx, ns.Name, policyName)
		ready := meta.FindStatusCondition(baselinePolicy.Status.Conditions, "Ready")
		Expect(ready).NotTo(BeNil(), "the policy has no Ready condition")
		Expect(ready.Status).To(Equal(metav1.ConditionTrue), "the baseline policy is not Ready")
		Expect(baselinePolicy.Status.WorkloadsProcessed).To(Equal(1),
			"the baseline policy did not process the workload")

		// faultPhases: independently served fixture fault modes with the
		// provider-error fragment each one produces inside the processor's
		// "Missing metrics:" skip reason.
		faultPhases := []struct {
			description string
			mode        string
			fragment    string
		}{
			{"an empty (successful, result-less) metrics response", framework.FixtureModeMissing, "no data returned from Prometheus"},
			{"an invalid (wrong result type) metrics response", framework.FixtureModeInvalid, "unexpected result type"},
			{"an unavailable metrics provider response", framework.FixtureModeUnavailable, "query failed"},
		}
		for _, phase := range faultPhases {
			By(fmt.Sprintf("serving %s and observing a completed skip without new application", phase.description))
			fault := faultSeries(ctx, ns.Name, workload, samples, phase.mode)
			Expect(env.Fixture.SetSeries(ctx, fault)).To(Succeed(),
				"register the %s fixture series", phase.description)
			phaseStart := time.Now()
			Expect(env.Fixture.ClearTrace(ctx)).To(Succeed(),
				"reset the trace so the %s reconcile is attributable", phase.description)
			touchPolicy(ctx, ns.Name, policyName)

			waitForPolicyEvent(ctx, ns.Name, policyName, "WorkloadSkipped", phaseStart,
				"Missing metrics:", phase.fragment)
			expectFixtureQueriedAfter(ctx, fault, phaseStart)

			expectSteadyWorkload(ctx, steady, ns.Name, workload, podSelector,
				fmt.Sprintf("the %s reconcile", phase.description))
			duringFault := fetchPolicy(ctx, ns.Name, policyName)
			faultReady := meta.FindStatusCondition(duringFault.Status.Conditions, "Ready")
			Expect(faultReady).NotTo(BeNil(), "the policy lost its Ready condition during %s", phase.description)
			Expect(faultReady.Status).To(Equal(metav1.ConditionTrue),
				"the policy must stay Ready during %s; the skip event, not policy status, is the processing signal", phase.description)
			Expect(duringFault.Status.WorkloadsProcessed).To(Equal(1),
				"the workload's skip still counts as processed during %s", phase.description)
		}

		By("asserting the recommendation annotations kept the baseline values through every fault")
		current, err := readWorkloadView(ctx, optipodv1alpha1.WorkloadTypeDeployment, ns.Name, workload)
		Expect(err).NotTo(HaveOccurred(), "read the workload after the fault phases")
		for key, value := range recommendationAnnotations(samples) {
			Expect(current.annotations[key]).To(Equal(value),
				"a fault phase changed recommendation annotation %q; faults must not produce new values", key)
		}
		expectAppliedContainerRequests(current.containers, samples,
			"the pod template after every fault phase")

		By("restoring valid metrics with changed values and observing processing resume")
		recovery := registerWorkloadSeries(ctx, ns.Name, workload, recoverySamples())
		recoveryStart := time.Now()
		Expect(env.Fixture.ClearTrace(ctx)).To(Succeed(),
			"reset the trace so the recovery reconcile is attributable")
		// The recommendation-annotation write is throttled for 30s after the
		// last successful write, and every fault-phase skip stored nothing, so
		// the baseline write still anchors the window: wait it out before
		// triggering, or the recovery reconcile would apply the changed
		// values while leaving the stored recommendations at their baseline.
		waitForAnnotationThrottleWindow(ctx, ns.Name, workload)
		touchPolicy(ctx, ns.Name, policyName)

		waitForWorkloadTemplateRequests(ctx, optipodv1alpha1.WorkloadTypeDeployment, ns.Name, workload, recoverySamples())
		recoveredPods := waitForReplacementPod(ctx, ns.Name, podSelector, steady.podUIDs)
		expectAppliedContainerRequests(recoveredPods[0].Spec.Containers, recoverySamples(),
			"the replacement pod after provider recovery")
		waitForPodsToSettle(ctx, ns.Name, podSelector, steady.podUIDs)
		recovered := waitForDeploymentAnnotation(ctx, ns.Name, workload, recommendationAnnotations(recoverySamples()))
		for key, baselineValue := range recommendationAnnotations(samples) {
			Expect(recovered.Annotations[key]).NotTo(Equal(baselineValue),
				"the recovered recommendation annotation %q kept its baseline value; recovery must consume the changed data", key)
		}
		expectFixtureQueriedAfter(ctx, recovery, recoveryStart)

		By("waiting for the policy's reconciliation summary")
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

// faultSeries builds the wholesale series set that serves the given fault mode
// for every container of every live pod of the named workload: the fixture
// responds with the fault for these targets regardless of sample values.
func faultSeries(ctx context.Context, namespace, workloadName string, samples []containerSamples, mode string) []framework.FixtureSeries {
	pods, err := env.Clients.Kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=" + workloadName,
	})
	Expect(err).NotTo(HaveOccurred(), "list pods of workload %s for fault series", workloadName)
	Expect(pods.Items).NotTo(BeEmpty(), "workload %s has no pods to serve faults for", workloadName)
	series := make([]framework.FixtureSeries, 0, len(pods.Items)*len(samples))
	for _, pod := range pods.Items {
		for _, s := range samples {
			series = append(series, framework.FixtureSeries{
				Namespace: namespace,
				Pod:       pod.Name,
				Container: s.name,
				Mode:      mode,
			})
		}
	}
	return series
}
