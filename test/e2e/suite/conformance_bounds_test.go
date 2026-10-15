package suite

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
)

// This scenario proves the installed controller's CPU/memory bounds and
// decrease behavior on a real cluster (VAL-CONF-004). One four-container
// workload serves four fixture containers whose metrics land below, within
// and above the policy's custom bounds, plus one container whose recommended
// memory is far below its current request. Under an explicit Auto SSA policy
// with limit updates enabled (CPU 1.5x, memory 1.25x), the deployed
// controller must:
//   - clamp the below-bounds container to the exact CPU/memory minima,
//   - keep the within-bounds container at the exact P90*1.2 arithmetic,
//   - clamp the above-bounds container to the exact CPU/memory maxima,
//   - apply the recommended memory DECREASE in full: production has no
//     memory-decrease guard (the allowUnsafeMemoryDecrease/gradualDecrease
//     fields are schema-validated but read by no production code), so the
//     decreased value reaches the workload like any other recommendation,
//   - write limits consistent with the policy's configured multipliers next
//     to the applied requests, in both the recommendation annotations and
//     the applied pod template and pods.
//
// An inverted CPU bound (min > max) is admitted by the CRD schema (it has no
// ordering rule) but rejected by the production runtime validator, so the
// reconciler must report an actionable ValidationFailed condition and event
// and never process or clamp the workload. The persisted inverted bounds, the
// untouched workload and the absence of any fixture query prove the
// schema-admitted-but-runtime-invalid boundary live.
var _ = Describe("Resource bounds and memory-decrease application", Label("conformance", "safety"), func() {
	const waitTimeout = 3 * time.Minute

	It("clamps below and above the bounds, keeps within-bounds math exact, derives limits consistently and applies a memory decrease in full", func(ctx context.Context) {
		ns := specNamespace(ctx, "conf-bounds")
		DeferCleanup(func(ctx context.Context) {
			Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed(),
				"clean up the bounds conformance spec namespace")
		})
		rows := boundsRows()
		samples := boundsSamples(rows)

		By("creating the four-container workload whose pod becomes ready")
		workload := createKindWorkload(ctx, optipodv1alpha1.WorkloadTypeDeployment,
			ns.Name, "conf-bounds", samples, nil)
		podSelector := "app=" + workload
		beforeUIDs := podUIDs(ctx, ns.Name, podSelector)

		By("registering the deterministic below/within/above/decrease metric inputs")
		series := registerWorkloadSeries(ctx, ns.Name, workload, samples)
		Expect(env.Fixture.ClearTrace(ctx)).To(Succeed(), "reset the fixture trace for this spec")

		By("creating an Auto SSA policy with custom bounds and enabled limit updates")
		policyName, err := framework.UniqueName("policy")
		Expect(err).NotTo(HaveOccurred(), "generate unique policy name")
		policy := framework.NewPolicyFixture(ns.Name, policyName, optipodv1alpha1.ModeAuto,
			framework.WithStrategy("ssa", ""),
			framework.WithResourceBounds("200m", "1", "128Mi", "512Mi"),
			framework.WithLimitsConfig(1.5, 1.25),
			framework.WithReconciliationInterval(30*time.Second))
		// updateRequestsOnly=false cannot be sent through the typed client
		// (omitempty drops the explicit false and the API server re-applies
		// the schema default true), so the policy is created through the raw
		// API path — the exact wire format a user's YAML apply sends for a
		// limits-enabled policy.
		Expect(framework.CreatePolicyPreservingExplicitBools(ctx, env.Clients, policy)).
			To(Succeed(), "create the bounds policy with explicit updateRequestsOnly=false")
		created := fetchPolicy(ctx, ns.Name, policyName)
		Expect(created.Spec.UpdateStrategy.UpdateRequestsOnly).To(BeFalse(),
			"the API must persist the explicit updateRequestsOnly=false a user's YAML apply sends")
		Expect(created.Spec.ResourceBounds.CPU.Min.String()).To(Equal("200m"),
			"the custom CPU bounds did not persist")
		createdAt := created.CreationTimestamp.Time

		By("waiting for every container's exact clamped or unclamped request on the pod template")
		waitForWorkloadTemplateRequests(ctx, optipodv1alpha1.WorkloadTypeDeployment, ns.Name, workload, samples)

		By("asserting the applied requests, the recommendation metadata and the policy-derived limits")
		applied, err := readWorkloadView(ctx, optipodv1alpha1.WorkloadTypeDeployment, ns.Name, workload)
		Expect(err).NotTo(HaveOccurred(), "read the applied workload")
		expectManagementAnnotationsOn(applied.annotations, created, createdAt)
		for _, row := range rows {
			expectAnnotationQuantity(applied.annotations,
				fmt.Sprintf("%s.%s.cpu-request", optipodv1alpha1.AnnotationRecommendationPrefix, row.samples.name),
				row.samples.expectedCPU)
			expectAnnotationQuantity(applied.annotations,
				fmt.Sprintf("%s.%s.memory-request", optipodv1alpha1.AnnotationRecommendationPrefix, row.samples.name),
				row.samples.expectedMemory)
			expectAnnotationQuantity(applied.annotations,
				fmt.Sprintf("%s.%s.cpu-limit", optipodv1alpha1.AnnotationRecommendationPrefix, row.samples.name),
				row.cpuLimit)
			expectAnnotationQuantity(applied.annotations,
				fmt.Sprintf("%s.%s.memory-limit", optipodv1alpha1.AnnotationRecommendationPrefix, row.samples.name),
				row.memoryLimit)
			expectBoundsRowApplied(applied.containers, row, "the applied pod template")
		}

		By("asserting the memory decrease reached the workload in full")
		decrease := boundsDecreaseRow(rows)
		initialMemory := resource.MustParse(decrease.samples.initialMemory)
		gotCPU, gotMemory := containerRequests(applied.containers, decrease.samples.name)
		Expect(gotMemory.Cmp(initialMemory)).To(BeNumerically("<", 0),
			"the recommended memory %s never decreased below the original request %s; production has no decrease guard so the recommendation must reach the workload in full",
			gotMemory.String(), decrease.samples.initialMemory)
		initialCPU := resource.MustParse(decrease.samples.initialCPU)
		Expect(gotCPU.Cmp(initialCPU)).To(BeNumerically("<", 0),
			"the recommended CPU %s never decreased below the original request %s", gotCPU.String(), decrease.samples.initialCPU)
		Expect(gotMemory.String()).To(Equal(decrease.samples.expectedMemory),
			"a gradual or intermediate decrease was applied; the full recommendation must land in one reconcile")

		By("rolling the workload and asserting requests plus limits on the replacement pod")
		Expect(framework.WaitForDeploymentAvailable(ctx, env.Clients, ns.Name, workload,
			framework.WaitOptions{Timeout: waitTimeout})).To(Succeed(),
			"the applied workload never returned to Available")
		freshPods := waitForReplacementPod(ctx, ns.Name, podSelector, beforeUIDs)
		for _, row := range rows {
			expectBoundsRowApplied(freshPods[0].Spec.Containers, row, "the replacement pod")
		}
		waitForPodsToSettle(ctx, ns.Name, podSelector, beforeUIDs)

		By("proving the fixture was queried and the policy processed the workload")
		expectFixtureQueried(ctx, series)
		waitForPolicyStatus(ctx, ns.Name, policyName, policyStatusWait{
			readyStatus:     "True",
			readyReason:     "PolicyValid",
			expectSummary:   true,
			discovered:      1,
			processed:       1,
			reconciledAfter: createdAt,
		})
	})

	It("denies an inverted CPU bound with an actionable invalid status and never clamps the workload", func(ctx context.Context) {
		ns := specNamespace(ctx, "conf-bounds-invalid")
		DeferCleanup(func(ctx context.Context) {
			Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed(),
				"clean up the invalid-bounds conformance spec namespace")
		})
		samples := smokeSamples()

		By("creating a workload whose metrics would yield recommendations if the policy were valid")
		workload := createKindWorkload(ctx, optipodv1alpha1.WorkloadTypeDeployment,
			ns.Name, "conf-bounds-invalid", samples, nil)
		series := registerWorkloadSeries(ctx, ns.Name, workload, samples)
		Expect(env.Fixture.ClearTrace(ctx)).To(Succeed(), "reset the fixture trace for this spec")

		By("creating an Auto policy with an inverted CPU bound (min > max)")
		policyName, err := framework.UniqueName("policy")
		Expect(err).NotTo(HaveOccurred(), "generate unique policy name")
		policy := framework.NewPolicyFixture(ns.Name, policyName, optipodv1alpha1.ModeAuto,
			framework.WithStrategy("ssa", ""),
			// CPU min 4 > max 50m: valid quantities, so the CRD schema admits
			// them, but the production runtime validator must reject them.
			framework.WithResourceBounds("4", "50m", "64Mi", "8Gi"),
			framework.WithReconciliationInterval(30*time.Second))
		Expect(env.Clients.Ctrl.Create(ctx, policy)).To(Succeed(), "create the inverted-bounds policy")
		createdAt := policy.CreationTimestamp.Time

		By("waiting for the actionable validation-failure status from the runtime validator")
		waitForPolicyStatus(ctx, ns.Name, policyName, policyStatusWait{
			readyStatus:     metav1.ConditionFalse,
			readyReason:     "ValidationFailed",
			messageContains: "must be less than or equal to max",
		})
		persisted := fetchPolicy(ctx, ns.Name, policyName)
		Expect(persisted.Spec.ResourceBounds.CPU.Min.String()).To(Equal("4"),
			"the API must admit the inverted bound (schema has no ordering rule) so the runtime rejection is live")
		Expect(persisted.Spec.ResourceBounds.CPU.Max.String()).To(Equal("50m"),
			"the API must persist the inverted bound the runtime validator rejected")

		By("asserting the explicit validation-failure event on the policy")
		waitForPolicyEvent(ctx, ns.Name, policyName, "ValidationFailed", createdAt,
			"Policy validation failed", "CPU min")

		By("asserting the workload was never processed or clamped")
		untouched, err := readWorkloadView(ctx, optipodv1alpha1.WorkloadTypeDeployment, ns.Name, workload)
		Expect(err).NotTo(HaveOccurred(), "read the untouched workload")
		expectNoOptipodAnnotations(untouched.annotations, "the workload under the inverted-bounds policy")
		expectUnappliedContainerRequests(untouched.containers, samples, "the workload under the inverted-bounds policy")
		for _, s := range samples {
			cpuLimit, memLimit := containerLimits(untouched.containers, s.name)
			Expect(cpuLimit).To(BeNil(), "the inverted-bounds policy gave container %s a CPU limit", s.name)
			Expect(memLimit).To(BeNil(), "the inverted-bounds policy gave container %s a memory limit", s.name)
		}
		expectNoFixtureQueryFor(ctx, series,
			"the completed inverted-bounds reconcile")
	})
})

// boundsRow pairs one container's deterministic samples with the exact limits
// the policy's configured multipliers must derive from its applied requests.
type boundsRow struct {
	samples containerSamples
	// cpuLimit/memoryLimit are the exact expected limits (requests x the
	// policy's 1.5x CPU and 1.25x memory multipliers).
	cpuLimit    string
	memoryLimit string
}

// boundsRows builds the below/within/above/decrease table against the policy
// bounds CPU 200m..1 and memory 128Mi..512Mi. Every expected request value is
// raw P90 x 1.2 arithmetic (the eleven identical samples land the P90 rank
// exactly on the sample) clamped to those bounds; the expected values are
// derived from the fixture input independently of the code under test.
func boundsRows() []boundsRow {
	return []boundsRow{
		// below: CPU 50m x 1.2 = 60m -> clamped to the 200m min; memory
		// 50Mi x 1.2 = 60Mi -> clamped to the 128Mi min. Limits are derived
		// from the CLAMPED requests: 200m x 1.5 = 300m, 128Mi x 1.25 = 160Mi.
		{samples: containerSamples{
			name: "below", cpuCores: 0.05, memoryBytes: 50 << 20,
			initialCPU: "100m", initialMemory: "64Mi",
			expectedCPU: "200m", expectedMemory: "128Mi",
		}, cpuLimit: "300m", memoryLimit: "160Mi"},
		// within: CPU 250m x 1.2 = 300m; memory 200Mi x 1.2 = 240Mi. No
		// clamping. Limits: 300m x 1.5 = 450m, 240Mi x 1.25 = 300Mi.
		{samples: containerSamples{
			name: "within", cpuCores: 0.25, memoryBytes: 200 << 20,
			initialCPU: "100m", initialMemory: "64Mi",
			expectedCPU: "300m", expectedMemory: "240Mi",
		}, cpuLimit: "450m", memoryLimit: "300Mi"},
		// above: CPU 2000m x 1.2 = 2400m -> clamped to the 1 max; memory
		// 600Mi x 1.2 = 720Mi -> clamped to the 512Mi max. Limits are derived
		// from the CLAMPED requests: 1 x 1.5 = 1500m, 512Mi x 1.25 = 640Mi.
		{samples: containerSamples{
			name: "above", cpuCores: 2.0, memoryBytes: 600 << 20,
			initialCPU: "100m", initialMemory: "64Mi",
			expectedCPU: "1", expectedMemory: "512Mi",
		}, cpuLimit: "1500m", memoryLimit: "640Mi"},
		// decrease: the container starts far above the recommendation (CPU
		// 500m, memory 1Gi) and the metrics recommend 300m/240Mi, so the
		// applied outcome is a decrease that must land in full.
		{samples: containerSamples{
			name: "decrease", cpuCores: 0.25, memoryBytes: 200 << 20,
			initialCPU: "500m", initialMemory: "1Gi",
			expectedCPU: "300m", expectedMemory: "240Mi",
		}, cpuLimit: "450m", memoryLimit: "300Mi"},
	}
}

// boundsSamples projects the table's container samples.
func boundsSamples(rows []boundsRow) []containerSamples {
	samples := make([]containerSamples, 0, len(rows))
	for _, row := range rows {
		samples = append(samples, row.samples)
	}
	return samples
}

// boundsDecreaseRow returns the decrease row from the table.
func boundsDecreaseRow(rows []boundsRow) boundsRow {
	for _, row := range rows {
		if row.samples.name == "decrease" {
			return row
		}
	}
	Fail("the bounds table has no decrease row")
	return boundsRow{}
}

// expectBoundsRowApplied asserts one row's exact CPU/memory requests AND the
// policy-multiplier limits on a container list (workload template or live
// pod), comparing quantities semantically so canonical string forms cannot
// mask a value mismatch.
func expectBoundsRowApplied(containers []corev1.Container, row boundsRow, what string) {
	cpu, memory := containerRequests(containers, row.samples.name)
	Expect(cpu).NotTo(BeNil(), "%s lost container %s CPU request", what, row.samples.name)
	Expect(memory).NotTo(BeNil(), "%s lost container %s memory request", what, row.samples.name)
	expectQuantity(cpu, row.samples.expectedCPU,
		fmt.Sprintf("%s: container %s CPU request", what, row.samples.name))
	expectQuantity(memory, row.samples.expectedMemory,
		fmt.Sprintf("%s: container %s memory request", what, row.samples.name))
	cpuLimit, memLimit := containerLimits(containers, row.samples.name)
	Expect(cpuLimit).NotTo(BeNil(), "%s: container %s has no CPU limit; the policy enables limit updates", what, row.samples.name)
	Expect(memLimit).NotTo(BeNil(), "%s: container %s has no memory limit; the policy enables limit updates", what, row.samples.name)
	expectQuantity(cpuLimit, row.cpuLimit,
		fmt.Sprintf("%s: container %s CPU limit (requests x the policy CPU multiplier)", what, row.samples.name))
	expectQuantity(memLimit, row.memoryLimit,
		fmt.Sprintf("%s: container %s memory limit (requests x the policy memory multiplier)", what, row.samples.name))
}

// expectQuantity asserts a live quantity equals the expected value
// semantically, so the assertion survives canonical string forms.
func expectQuantity(got *resource.Quantity, want, what string) {
	Expect(got.Cmp(resource.MustParse(want))).To(BeZero(), "%s = %s, want %s", what, got.String(), want)
}

// expectAnnotationQuantity asserts an annotation carries a quantity equal to
// the expected value, parsing the stored string so canonicalization cannot
// mask a mismatch.
func expectAnnotationQuantity(annotations map[string]string, key, want string) {
	value, ok := annotations[key]
	Expect(ok).To(BeTrue(), "annotation %q is missing", key)
	parsed, err := resource.ParseQuantity(value)
	Expect(err).NotTo(HaveOccurred(), "annotation %q value %q is not a parseable quantity", key, value)
	Expect(parsed.Cmp(resource.MustParse(want))).To(BeZero(),
		"annotation %q = %s, want %s", key, value, want)
}
