package suite

import (
	"context"
	"fmt"
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

// These scenarios prove the installed controller's multi-container outcomes
// and the whole-workload partial-data skip on a real cluster (VAL-CONF-005).
//
// The default-webhook spec observes every container's own outcome through
// natural admission: a two-container workload with complete distinct metrics
// stores both containers' recommendations and the installed mutator mutates
// BOTH on a naturally admitted pod. A user then adds an unrelated sidecar
// container to the pod template without registering metrics for it: the
// sidecar has no stored recommendation, so freshly admitted pods carry
// app/worker's applied values while the sidecar stays untouched. With one
// required container's metrics missing, the deployed processor skips the WHOLE
// workload (an explicit WorkloadSkipped event naming the missing container)
// even though the two containers with data have changed metric input, so no
// partial application of the changed values can occur. Registering the
// sidecar's own metrics restores complete data: every container gains its own
// independently named recommendation and a further natural rollout admits a
// pod where all three containers carry their own values.
//
// The explicit-SSA spec proves the workload-wide apply for a three-container
// workload: one apply lands every container's exact requests, the replacement
// pod carries all three, removing ONE container's series makes the next
// completed reconcile skip the whole workload with no churn and no partial
// template update, and restoring complete data re-applies the full
// per-container set in one update.
var _ = Describe("Multi-container outcomes and partial metrics on installed strategies", Label("conformance", "multi-container"), func() {
	const waitTimeout = 3 * time.Minute

	It("mutates every container with complete metrics, leaves an unrelated metrics-less sidecar untouched, and skips the whole workload on partial data", func(ctx context.Context) {
		ns := specNamespace(ctx, "conf-multi")
		DeferCleanup(func(ctx context.Context) {
			Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed(),
				"clean up the multi-container webhook conformance spec namespace")
		})
		samples := multiContainerSamples()

		By("creating the two-container webhook-eligible workload whose pod becomes ready")
		// The pod template carries the production optipod.io/webhook-enabled
		// label before creation, so every pod the Deployment controller admits
		// is routed to the installed mutating webhook: natural admission.
		workloadName, err := framework.UniqueName("workload")
		Expect(err).NotTo(HaveOccurred(), "generate unique webhook workload name")
		workload := framework.NewDeploymentFixture(ns.Name, workloadName, 1, smokeWebhookContainers(samples)...)
		Expect(env.Clients.Ctrl.Create(ctx, workload)).To(Succeed(), "create the webhook workload")
		Expect(framework.WaitForDeploymentAvailable(ctx, env.Clients, ns.Name, workloadName,
			framework.WaitOptions{Timeout: waitTimeout})).To(Succeed(),
			"the webhook workload never became Available")
		podSelector := "app=" + workloadName
		beforeUIDs := podUIDs(ctx, ns.Name, podSelector)

		By("registering complete distinct metrics for both containers")
		registerWorkloadSeries(ctx, ns.Name, workloadName, samples)
		Expect(env.Fixture.ClearTrace(ctx)).To(Succeed(), "reset the fixture trace for this spec")

		By("creating the Auto policy with the update strategy left unset (production default webhook)")
		policyName, err := framework.UniqueName("policy")
		Expect(err).NotTo(HaveOccurred(), "generate unique policy name")
		policy := framework.NewPolicyFixture(ns.Name, policyName, optipodv1alpha1.ModeAuto,
			framework.WithDefaultStrategy(),
			framework.WithReconciliationInterval(30*time.Second))
		Expect(env.Clients.Ctrl.Create(ctx, policy)).To(Succeed(), "create the default-strategy Auto policy")
		createdAt := policy.CreationTimestamp.Time

		By("waiting for both containers' recommendation and webhook annotations")
		want := recommendationAnnotations(samples)
		for key, value := range webhookApplyAnnotations(samples) {
			want[key] = value
		}
		annotated := waitForDeploymentAnnotation(ctx, ns.Name, workloadName, want)
		expectManagementAnnotations(annotated, policy, createdAt)
		expectUnappliedContainerRequests(annotated.Spec.Template.Spec.Containers, samples,
			"the pod template before any restart (onNextRestart)")

		By("scaling so the Deployment controller admits a fresh pod naturally")
		scaleDeployment(ctx, ns.Name, workloadName, 2)
		Expect(framework.WaitForDeploymentAvailable(ctx, env.Clients, ns.Name, workloadName,
			framework.WaitOptions{Timeout: waitTimeout})).To(Succeed(),
			"the scaled workload never returned to Available")
		scaledPods, err := framework.WaitForPodCountReady(ctx, env.Clients, ns.Name, podSelector, 2,
			framework.WaitOptions{Timeout: waitTimeout})
		Expect(err).NotTo(HaveOccurred(), "the scaled workload never had two ready pods")
		var fresh, kept []corev1.Pod
		for i := range scaledPods {
			if _, existed := beforeUIDs[scaledPods[i].UID]; existed {
				kept = append(kept, scaledPods[i])
				continue
			}
			fresh = append(fresh, scaledPods[i])
		}
		Expect(fresh).To(HaveLen(1), "scaling must have admitted exactly one fresh pod")
		expectAppliedContainerRequests(fresh[0].Spec.Containers, samples, "the naturally admitted pod")
		expectUnappliedContainerRequests(kept[0].Spec.Containers, samples,
			"the pre-existing pod under onNextRestart rollout")
		// Register series for the new pod too: the processor samples one
		// arbitrary live pod per container (its cache list order is
		// unspecified), so all pods need series for determinism.
		registerWorkloadSeries(ctx, ns.Name, workloadName, samples)
		beforeEditUIDs := podUIDs(ctx, ns.Name, podSelector)

		By("adding an unrelated sidecar container to the template without registering metrics for it")
		sidecar := sidecarSamples()
		addContainer(ctx, ns.Name, workloadName, sidecar)
		Expect(framework.WaitForDeploymentAvailable(ctx, env.Clients, ns.Name, workloadName,
			framework.WaitOptions{Timeout: waitTimeout})).To(Succeed(),
			"the sidecar-extended workload never returned to Available")
		replacedPods := waitForReplacedReadyPods(ctx, ns.Name, podSelector, beforeEditUIDs)
		for i := range replacedPods {
			what := fmt.Sprintf("the fresh pod after the sidecar edit (%d)", i)
			// The mutator patches containers with stored recommendations, so
			// app/worker keep their applied values while the unrelated
			// sidecar (no stored keys, no metrics) stays untouched.
			expectAppliedContainerRequests(replacedPods[i].Spec.Containers, samples, what)
			sidecarCPU, sidecarMemory := containerRequests(replacedPods[i].Spec.Containers, sidecar.name)
			Expect(sidecarCPU).NotTo(BeNil(), "%s lost the sidecar container", what)
			Expect(sidecarMemory).NotTo(BeNil(), "%s lost the sidecar memory request", what)
			Expect(sidecarCPU.String()).To(Equal(sidecar.initialCPU),
				"%s: the unrelated sidecar's CPU request was altered despite having no stored recommendation", what)
			Expect(sidecarMemory.String()).To(Equal(sidecar.initialMemory),
				"%s: the unrelated sidecar's memory request was altered despite having no stored recommendation", what)
		}
		extended := &appsv1.Deployment{}
		Expect(env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: workloadName}, extended)).
			To(Succeed(), "read the sidecar-extended workload")
		sidecarCPUKey := fmt.Sprintf("%s.%s.cpu-request", optipodv1alpha1.AnnotationRecommendationPrefix, sidecar.name)
		Expect(extended.Annotations).NotTo(HaveKey(sidecarCPUKey),
			"the workload gained a sidecar recommendation before the sidecar had metrics")
		for key, value := range want {
			Expect(extended.Annotations[key]).To(Equal(value),
				"the sidecar edit changed the stored annotation %q", key)
		}
		afterEditUIDs := podUIDs(ctx, ns.Name, podSelector)

		By("registering changed metrics for the two measured containers, still leaving the sidecar without metrics")
		changed := multiChangedSamples()
		partialSeries := buildWorkloadSeries(ctx, ns.Name, workloadName, changed)
		Expect(env.Fixture.SetSeries(ctx, partialSeries)).To(Succeed(),
			"register the partial (app/worker only, changed values) series set")
		skipPhase := time.Now()
		Expect(env.Fixture.ClearTrace(ctx)).To(Succeed(),
			"reset the trace so the partial-data reconcile is attributable")
		touchPolicy(ctx, ns.Name, policyName)

		By("waiting for the whole-workload skip signal naming the missing sidecar metrics")
		waitForPolicyEvent(ctx, ns.Name, policyName, "WorkloadSkipped", skipPhase,
			"Missing metrics:", "sidecar")

		By("asserting the partial data produced no partial application")
		skipped := &appsv1.Deployment{}
		Expect(env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: workloadName}, skipped)).
			To(Succeed(), "read the workload after the partial-data reconcile")
		for key, value := range want {
			Expect(skipped.Annotations[key]).To(Equal(value),
				"the whole-workload skip overwrote stored annotation %q with new values", key)
		}
		// The changed values for the two containers with data must NOT have
		// partially applied: the stored recommendations keep their baseline
		// values exactly.
		for _, s := range changed {
			cpuKey := fmt.Sprintf("%s.%s.cpu-request", optipodv1alpha1.AnnotationRecommendationPrefix, s.name)
			baselineValue, existed := want[cpuKey]
			Expect(existed).To(BeTrue(),
				"the baseline annotation %q must exist so a partial overwrite would be observable", cpuKey)
			Expect(skipped.Annotations[cpuKey]).To(Equal(baselineValue),
				"the partial-data reconcile partially applied container %s despite the missing sidecar metrics", s.name)
		}
		Expect(skipped.Annotations).NotTo(HaveKey(sidecarCPUKey),
			"the partial-data reconcile stored a sidecar recommendation")
		expectUnappliedContainerRequests(skipped.Spec.Template.Spec.Containers, samples,
			"the pod template after the partial-data reconcile (webhook never edits it)")
		sidecarTemplateCPU, sidecarTemplateMemory := containerRequests(skipped.Spec.Template.Spec.Containers, sidecar.name)
		Expect(sidecarTemplateCPU).NotTo(BeNil(), "the sidecar container vanished from the template")
		Expect(sidecarTemplateCPU.String()).To(Equal(sidecar.initialCPU),
			"the partial-data reconcile edited the sidecar's template request")
		Expect(sidecarTemplateMemory.String()).To(Equal(sidecar.initialMemory),
			"the partial-data reconcile edited the sidecar's template memory request")
		Expect(podUIDs(ctx, ns.Name, podSelector)).To(Equal(afterEditUIDs),
			"the partial-data reconcile rolled workload pods")
		// The controller queried the two containers that DO have data (it
		// samples one live pod per container; the registered series cover
		// every live pod), so the skip is the production decision, not an
		// absence of collection.
		expectFixtureQueriedAfter(ctx, partialSeries, skipPhase)

		By("registering the sidecar's own metrics so the workload has complete data again")
		complete := buildWorkloadSeries(ctx, ns.Name, workloadName, changed)
		complete = append(complete, buildWorkloadSeries(ctx, ns.Name, workloadName,
			[]containerSamples{sidecar})...)
		Expect(env.Fixture.SetSeries(ctx, complete)).To(Succeed(),
			"register the complete three-container series set")
		completePhase := time.Now()
		Expect(env.Fixture.ClearTrace(ctx)).To(Succeed(),
			"reset the trace so the complete-data reconcile is attributable")
		// Reconciles are watch-driven, so the new series alone would never be
		// consumed: the policy touch triggers the complete-data reconcile.
		// The recommendation-annotation write is throttled for 30s after the
		// last successful write, and the skip reconcile stored nothing, so
		// the window from the baseline write is waited out first — otherwise
		// the trigger would leave the changed recommendations unstored.
		waitForAnnotationThrottleWindow(ctx, ns.Name, workloadName)
		touchPolicy(ctx, ns.Name, policyName)

		By("waiting for every container's own recommendation and webhook annotations")
		completeSamples := append(append([]containerSamples{}, changed...), sidecar)
		wantComplete := recommendationAnnotations(completeSamples)
		for key, value := range webhookApplyAnnotations(completeSamples) {
			wantComplete[key] = value
		}
		// Sanity: the complete-data recommendations for the two measured
		// containers must differ from their baseline values, so the changed
		// input is observable in every stored family. The strategy markers
		// (webhook-enabled, strategy) are invariant by design and new
		// sidecar keys have no baseline to compare, so only the overlapping
		// per-container recommendation keys are checked.
		for key, value := range recommendationAnnotations(changed) {
			if original, existed := want[key]; existed {
				Expect(value).NotTo(Equal(original),
					"the changed fixture input for %q matches the original value; the test would be vacuous", key)
			}
		}
		completeAnnotated := waitForDeploymentAnnotation(ctx, ns.Name, workloadName, wantComplete)
		for _, s := range completeSamples {
			cpu, memory := containerRequests(completeAnnotated.Spec.Template.Spec.Containers, s.name)
			Expect(cpu).NotTo(BeNil(), "the workload template lost container %s", s.name)
			Expect(memory).NotTo(BeNil(), "the workload template lost container %s memory", s.name)
		}
		sidecarAnnotationCPU := completeAnnotated.Annotations[sidecarCPUKey]
		Expect(sidecarAnnotationCPU).To(Equal(sidecar.expectedCPU),
			"the sidecar's own recommendation must be stored once its metrics are complete")
		Expect(sidecarAnnotationCPU).NotTo(Equal(samples[0].expectedCPU),
			"the sidecar must not inherit the app container's recommendation")
		expectFixtureQueriedAfter(ctx, complete, completePhase)

		By("scaling again so a naturally admitted pod proves every container's outcome")
		scaleDeployment(ctx, ns.Name, workloadName, 3)
		Expect(framework.WaitForDeploymentAvailable(ctx, env.Clients, ns.Name, workloadName,
			framework.WaitOptions{Timeout: waitTimeout})).To(Succeed(),
			"the scaled complete-data workload never returned to Available")
		finalPods, err := framework.WaitForPodCountReady(ctx, env.Clients, ns.Name, podSelector, 3,
			framework.WaitOptions{Timeout: waitTimeout})
		Expect(err).NotTo(HaveOccurred(), "the complete-data workload never had three ready pods")
		for i := range finalPods {
			if _, existed := afterEditUIDs[finalPods[i].UID]; existed {
				// The pre-existing pods keep the values they were admitted
				// with: app/worker applied, sidecar untouched.
				expectAppliedContainerRequests(finalPods[i].Spec.Containers, samples,
					"the pre-existing pod after the complete-data reconcile")
				continue
			}
			// The freshly admitted pod carries every container's complete-
			// data outcome, including the sidecar's own values.
			expectAppliedContainerRequests(finalPods[i].Spec.Containers, completeSamples,
				"the freshly admitted pod with complete data")
		}

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

	It("applies a three-container workload-wide SSA set, skips whole-workload on partial data and re-applies the full set after restoration", func(ctx context.Context) {
		ns := specNamespace(ctx, "conf-multi-ssa")
		DeferCleanup(func(ctx context.Context) {
			Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed(),
				"clean up the multi-container SSA conformance spec namespace")
		})
		samples := ssaSamples()

		By("creating the three-container workload whose pod becomes ready")
		workload := createKindWorkload(ctx, optipodv1alpha1.WorkloadTypeDeployment,
			ns.Name, "conf-multi-ssa", samples, nil)
		podSelector := "app=" + workload
		beforeUIDs := podUIDs(ctx, ns.Name, podSelector)

		By("registering complete distinct metrics for all three containers")
		registerWorkloadSeries(ctx, ns.Name, workload, samples)
		Expect(env.Fixture.ClearTrace(ctx)).To(Succeed(), "reset the fixture trace for this spec")

		By("creating the Auto policy with the explicit SSA strategy")
		policyName, err := framework.UniqueName("policy")
		Expect(err).NotTo(HaveOccurred(), "generate unique policy name")
		policy := framework.NewPolicyFixture(ns.Name, policyName, optipodv1alpha1.ModeAuto,
			framework.WithStrategy("ssa", ""),
			framework.WithReconciliationInterval(30*time.Second))
		Expect(env.Clients.Ctrl.Create(ctx, policy)).To(Succeed(), "create the SSA Auto policy")
		createdAt := policy.CreationTimestamp.Time

		By("waiting for all three containers' exact requests to coexist after the workload-wide apply")
		waitForWorkloadTemplateRequests(ctx, optipodv1alpha1.WorkloadTypeDeployment, ns.Name, workload, samples)
		applied, err := readWorkloadView(ctx, optipodv1alpha1.WorkloadTypeDeployment, ns.Name, workload)
		Expect(err).NotTo(HaveOccurred(), "read the applied workload")
		expectManagementAnnotationsOn(applied.annotations, policy, createdAt)
		for key, value := range recommendationAnnotations(samples) {
			Expect(applied.annotations[key]).To(Equal(value),
				"applied workload annotation %q has the wrong value", key)
		}

		By("rolling to a replacement pod that carries every container's applied outcome")
		Expect(framework.WaitForDeploymentAvailable(ctx, env.Clients, ns.Name, workload,
			framework.WaitOptions{Timeout: waitTimeout})).To(Succeed(),
			"the applied workload never returned to Available")
		freshPods := waitForReplacementPod(ctx, ns.Name, podSelector, beforeUIDs)
		expectAppliedContainerRequests(freshPods[0].Spec.Containers, samples, "the replacement pod")
		waitForPodsToSettle(ctx, ns.Name, podSelector, beforeUIDs)
		registerWorkloadSeries(ctx, ns.Name, workload, samples)
		steady := captureSteadyState(ctx, ns.Name, workload, podSelector)

		By("removing one container's series so the next reconcile sees partial data")
		partialSamples := ssaChangedSamples()[:2] // app and worker only; sidecar missing
		partialSeries := buildWorkloadSeries(ctx, ns.Name, workload, partialSamples)
		Expect(env.Fixture.SetSeries(ctx, partialSeries)).To(Succeed(),
			"register the partial (app/worker only, changed values) series set")
		skipPhase := time.Now()
		Expect(env.Fixture.ClearTrace(ctx)).To(Succeed(),
			"reset the trace so the partial-data reconcile is attributable")
		touchPolicy(ctx, ns.Name, policyName)

		By("waiting for the whole-workload skip signal naming the missing sidecar metrics")
		waitForPolicyEvent(ctx, ns.Name, policyName, "WorkloadSkipped", skipPhase,
			"Missing metrics:", "sidecar")

		By("asserting the skip left every container's applied values with no churn")
		expectSteadyWorkload(ctx, steady, ns.Name, workload, podSelector, "the partial-metrics skip reconcile")
		skipped, err := readWorkloadView(ctx, optipodv1alpha1.WorkloadTypeDeployment, ns.Name, workload)
		Expect(err).NotTo(HaveOccurred(), "read the workload after the partial-data reconcile")
		expectAppliedContainerRequests(skipped.containers, samples,
			"the pod template after the partial-data skip")
		// The stored recommendations keep their original values exactly: the
		// changed values for the two containers with data must NOT have
		// partially applied while the sidecar's metrics were missing.
		for key, value := range recommendationAnnotations(samples) {
			Expect(skipped.annotations[key]).To(Equal(value),
				"the partial-data skip changed stored recommendation %q", key)
		}
		// The controller did query the containers with data; the whole-
		// workload skip is the production decision, not missing collection.
		expectFixtureQueriedAfter(ctx, partialSeries, skipPhase)

		By("restoring complete data with changed values for every container")
		changed := ssaChangedSamples()
		restoreSeries := buildWorkloadSeries(ctx, ns.Name, workload, changed)
		Expect(env.Fixture.SetSeries(ctx, restoreSeries)).To(Succeed(),
			"register the restored complete series set")
		restorePhase := time.Now()
		Expect(env.Fixture.ClearTrace(ctx)).To(Succeed(),
			"reset the trace so the restoration reconcile is attributable")
		// The recommendation-annotation write is throttled for 30s after the
		// last successful write (the skip reconcile stored nothing, so the
		// baseline write still anchors the window). Waiting the window out
		// before triggering keeps the restored reconcile from applying the
		// changed values while silently leaving the stored recommendations
		// at their baseline values.
		waitForAnnotationThrottleWindow(ctx, ns.Name, workload)
		touchPolicy(ctx, ns.Name, policyName)

		By("waiting for the full per-container set to be re-applied in one workload-wide update")
		waitForWorkloadTemplateRequests(ctx, optipodv1alpha1.WorkloadTypeDeployment, ns.Name, workload, changed)
		// The recommendation-annotation write is throttled to once per 30s by
		// production while the apply itself is not, so the metadata catch-up
		// is awaited bounded rather than assumed immediate.
		restoredAnnotated := waitForDeploymentAnnotation(ctx, ns.Name, workload,
			recommendationAnnotations(changed))
		expectManagementAnnotations(restoredAnnotated, policy, createdAt)
		expectAppliedContainerRequests(restoredAnnotated.Spec.Template.Spec.Containers, changed,
			"the pod template after the restored complete data")

		By("rolling to a replacement pod that carries the restored set")
		Expect(framework.WaitForDeploymentAvailable(ctx, env.Clients, ns.Name, workload,
			framework.WaitOptions{Timeout: waitTimeout})).To(Succeed(),
			"the restored workload never returned to Available")
		restoredPods := waitForReplacementPod(ctx, ns.Name, podSelector, steady.podUIDs)
		expectAppliedContainerRequests(restoredPods[0].Spec.Containers, changed,
			"the replacement pod after the restored complete data")
		expectFixtureQueriedAfter(ctx, restoreSeries, restorePhase)
	})
})

// multiContainerSamples are the two measured containers of the webhook spec:
// complete distinct metrics (within the fixture policy's default bounds) that
// must produce independently named recommendations for each container.
func multiContainerSamples() []containerSamples {
	return []containerSamples{
		{name: "app", cpuCores: 0.25, memoryBytes: 100 << 20,
			initialCPU: "100m", initialMemory: "64Mi",
			expectedCPU: "300m", expectedMemory: "120Mi"},
		{name: "worker", cpuCores: 0.125, memoryBytes: 60 << 20,
			initialCPU: "50m", initialMemory: "32Mi",
			expectedCPU: "150m", expectedMemory: "72Mi"},
	}
}

// multiChangedSamples are the changed app/worker values used for the
// partial-data phase: distinct from the originals so a partial application of
// them is observable, and still inside the default policy bounds.
func multiChangedSamples() []containerSamples {
	return []containerSamples{
		{name: "app", cpuCores: 0.5, memoryBytes: 150 << 20,
			initialCPU: "100m", initialMemory: "64Mi",
			expectedCPU: "600m", expectedMemory: "180Mi"},
		{name: "worker", cpuCores: 0.25, memoryBytes: 100 << 20,
			initialCPU: "50m", initialMemory: "32Mi",
			expectedCPU: "300m", expectedMemory: "120Mi"},
	}
}

// sidecarSamples is the unrelated sidecar added by the user after creation: it
// deliberately starts with no metrics, so no stored recommendation exists for
// it and the production pod mutator leaves it untouched on newly admitted
// pods; once its own metrics are registered it must gain its OWN distinct
// recommendation (not the app's values).
func sidecarSamples() containerSamples {
	return containerSamples{name: "sidecar", cpuCores: 0.2, memoryBytes: 80 << 20,
		initialCPU: "50m", initialMemory: "32Mi",
		expectedCPU: "240m", expectedMemory: "96Mi"}
}

// ssaSamples are the three complete distinct containers of the SSA spec.
func ssaSamples() []containerSamples {
	return []containerSamples{
		{name: "app", cpuCores: 0.25, memoryBytes: 100 << 20,
			initialCPU: "100m", initialMemory: "64Mi",
			expectedCPU: "300m", expectedMemory: "120Mi"},
		{name: "worker", cpuCores: 0.125, memoryBytes: 60 << 20,
			initialCPU: "50m", initialMemory: "32Mi",
			expectedCPU: "150m", expectedMemory: "72Mi"},
		{name: "sidecar", cpuCores: 0.2, memoryBytes: 80 << 20,
			initialCPU: "30m", initialMemory: "16Mi",
			expectedCPU: "240m", expectedMemory: "96Mi"},
	}
}

// ssaChangedSamples are the restored complete values for the SSA spec's
// three containers: distinct from the originals so the workload-wide re-apply
// is observable on every container.
func ssaChangedSamples() []containerSamples {
	return []containerSamples{
		{name: "app", cpuCores: 0.5, memoryBytes: 150 << 20,
			initialCPU: "100m", initialMemory: "64Mi",
			expectedCPU: "600m", expectedMemory: "180Mi"},
		{name: "worker", cpuCores: 0.25, memoryBytes: 100 << 20,
			initialCPU: "50m", initialMemory: "32Mi",
			expectedCPU: "300m", expectedMemory: "120Mi"},
		{name: "sidecar", cpuCores: 0.3, memoryBytes: 120 << 20,
			initialCPU: "30m", initialMemory: "16Mi",
			expectedCPU: "360m", expectedMemory: "144Mi"},
	}
}

// addContainer performs a user-style pod-template edit appending one container
// (using the pinned inert workload image), with a bounded conflict retry for
// concurrent workload writes by the controller.
func addContainer(ctx context.Context, namespace, name string, s containerSamples) {
	const maxAttempts = 5
	for attempt := 1; ; attempt++ {
		dep := &appsv1.Deployment{}
		Expect(env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, dep)).
			To(Succeed(), "read workload %s/%s before adding the container", namespace, name)
		dep.Spec.Template.Spec.Containers = append(dep.Spec.Template.Spec.Containers, corev1.Container{
			Name:  s.name,
			Image: framework.WorkloadImage(),
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse(s.initialCPU),
					corev1.ResourceMemory: resource.MustParse(s.initialMemory),
				},
			},
		})
		_, err := env.Clients.Kube.AppsV1().Deployments(namespace).Update(ctx, dep, metav1.UpdateOptions{})
		if err == nil {
			return
		}
		if !apierrors.IsConflict(err) || attempt == maxAttempts {
			Fail(fmt.Sprintf("add container %s to workload %s/%s: %v (after %d attempts)",
				s.name, namespace, name, err, attempt))
		}
		time.Sleep(time.Duration(attempt) * 100 * time.Millisecond)
	}
}
