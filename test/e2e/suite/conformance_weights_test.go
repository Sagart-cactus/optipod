package suite

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
)

// These scenarios prove the installed controller's winner selection between
// overlapping policies (VAL-CONF-003). Every row observes the winner through
// the workload itself — the exact recommendation annotation values the winner
// derives under its own bounds plus the optipod.io/policy and policy-uid
// annotations naming it — never through a stored weight alone. The loser is
// proven to lose through its own policy status (the workload discovered but
// not processed). Distinguishable outcomes come from distinguishable bounds:
// the wide-bounds winner clamps nothing (app 300m/120Mi) while the
// clamp-bounds policy caps the recommendation (app 200m/90Mi), so the
// annotated values identify the winner. Rows that change a supported
// selection input (a weight edit, deleting the winner) prove the observed
// winner and resulting effect flip consistently. No unsupported
// selector-specificity precedence is asserted; equal weights resolving by
// name IS supported production behavior.
var _ = Describe("Overlapping policies choose a consistent observed winner", Label("conformance", "policy"), func() {
	// weightPolicyBuilder builds one of a row's overlapping policies.
	type weightPolicyBuilder func(namespace, name string) *optipodv1alpha1.OptimizationPolicy

	// weightFlipFunc proves a supported selection-input change flips the
	// observed winner and the resulting workload effect.
	type weightFlipFunc func(ctx context.Context, namespace, workload string,
		beforeUIDs map[types.UID]string, winner, loser *optipodv1alpha1.OptimizationPolicy,
		loserValues []containerSamples)

	type weightRow struct {
		description  string
		winner       weightPolicyBuilder
		winnerValues []containerSamples
		loser        weightPolicyBuilder
		loserValues  []containerSamples
		// flip is optional; observation-only rows leave it nil.
		flip weightFlipFunc
		// onWinner is an optional extra assertion on the persisted winner
		// after the winner's effect is observed (the omitted-weight row pins
		// the API-persisted default).
		onWinner func(ctx context.Context, namespace, name string, created *optipodv1alpha1.OptimizationPolicy)
		// winnerNamePrefix/loserNamePrefix generate the unique policy names;
		// the name-tie row uses orderable prefixes.
		winnerNamePrefix, loserNamePrefix string
	}

	DescribeTable("selects a winner by supported inputs and observes it through the workload",
		func(ctx context.Context, row weightRow) {
			ns := specNamespace(ctx, "conf-weights")
			DeferCleanup(func(ctx context.Context) {
				Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed(),
					"clean up the %s spec namespace", row.description)
			})
			samples := smokeSamples()

			By("creating the two-container workload and registering the fixture input")
			workload := createKindWorkload(ctx, optipodv1alpha1.WorkloadTypeDeployment,
				ns.Name, "conf-weights", samples, nil)
			podSelector := "app=" + workload
			beforeUIDs := podUIDs(ctx, ns.Name, podSelector)
			series := registerWorkloadSeries(ctx, ns.Name, workload, samples)
			Expect(env.Fixture.ClearTrace(ctx)).To(Succeed(), "reset the fixture trace for this spec")

			By("creating the winner policy first and the overlapping loser policy second")
			winnerPrefix := row.winnerNamePrefix
			if winnerPrefix == "" {
				winnerPrefix = "policy"
			}
			winnerName, err := framework.UniqueName(winnerPrefix)
			Expect(err).NotTo(HaveOccurred(), "generate unique winner policy name")
			winner := row.winner(ns.Name, winnerName)
			Expect(env.Clients.Ctrl.Create(ctx, winner)).To(Succeed(), "create the winner policy")
			winnerCreated := winner.CreationTimestamp.Time

			loserPrefix := row.loserNamePrefix
			if loserPrefix == "" {
				loserPrefix = "policy"
			}
			loserName, err := framework.UniqueName(loserPrefix)
			Expect(err).NotTo(HaveOccurred(), "generate unique loser policy name")
			loser := row.loser(ns.Name, loserName)
			Expect(env.Clients.Ctrl.Create(ctx, loser)).To(Succeed(), "create the loser policy")
			loserCreated := loser.CreationTimestamp.Time

			By("observing the winner through the workload's exact values and policy identity")
			annotated := waitForDeploymentAnnotation(ctx, ns.Name, workload,
				winnerAnnotations(row.winnerValues, winnerName, winner.UID))
			expectManagementAnnotations(annotated, winner, winnerCreated)
			expectUnappliedContainerRequests(annotated.Spec.Template.Spec.Containers, samples,
				"the workload while the winning policy holds it")
			Expect(podUIDs(ctx, ns.Name, podSelector)).To(Equal(beforeUIDs),
				"a winning Recommend policy rolled workload pods")

			if row.onWinner != nil {
				row.onWinner(ctx, ns.Name, winnerName, winner)
			}

			By("observing both policies' summaries: only the winner processed the workload")
			waitForPolicyStatus(ctx, ns.Name, winnerName, policyStatusWait{
				readyStatus: "True", readyReason: "PolicyValid", expectSummary: true,
				discovered: 1, processed: 1, reconciledAfter: winnerCreated,
			})
			waitForPolicyStatus(ctx, ns.Name, loserName, policyStatusWait{
				readyStatus: "True", readyReason: "PolicyValid", expectSummary: true,
				discovered: 1, processed: 0, reconciledAfter: loserCreated,
			})

			By("proving the deployed controller queried the fixture for the winner's values")
			expectFixtureQueried(ctx, series)

			if row.flip != nil {
				By("changing a supported selection input and observing the consistent flip")
				row.flip(ctx, ns.Name, workload, beforeUIDs, winner, loser, row.loserValues)
			}
		},
		Entry("the higher weight wins and deleting the winner flips the effect to the runner-up",
			weightRow{
				description: "weight winner and deletion flip",
				winner: func(namespace, name string) *optipodv1alpha1.OptimizationPolicy {
					return framework.NewPolicyFixture(namespace, name, optipodv1alpha1.ModeRecommend,
						framework.WithWeight(200),
						framework.WithReconciliationInterval(30*time.Second))
				},
				winnerValues: smokeSamples(),
				loser: func(namespace, name string) *optipodv1alpha1.OptimizationPolicy {
					return framework.NewPolicyFixture(namespace, name, optipodv1alpha1.ModeRecommend,
						framework.WithWeight(10),
						clampPolicyBounds,
						framework.WithReconciliationInterval(30*time.Second))
				},
				loserValues: clampedSamples(),
				flip:        runDeletionFlip,
			}),
		Entry("an omitted weight uses the API default and beats a lower explicit weight",
			weightRow{
				description: "omitted-weight default wins",
				winner: func(namespace, name string) *optipodv1alpha1.OptimizationPolicy {
					return framework.NewPolicyFixture(namespace, name, optipodv1alpha1.ModeRecommend,
						framework.WithReconciliationInterval(30*time.Second))
				},
				winnerValues: smokeSamples(),
				loser: func(namespace, name string) *optipodv1alpha1.OptimizationPolicy {
					return framework.NewPolicyFixture(namespace, name, optipodv1alpha1.ModeRecommend,
						framework.WithWeight(10),
						clampPolicyBounds,
						framework.WithReconciliationInterval(30*time.Second))
				},
				loserValues: clampedSamples(),
				onWinner: func(ctx context.Context, namespace, name string, created *optipodv1alpha1.OptimizationPolicy) {
					persisted := fetchPolicy(ctx, namespace, name)
					Expect(persisted.Spec.Weight).NotTo(BeNil(),
						"the API did not persist the schema default weight")
					Expect(*persisted.Spec.Weight).To(Equal(int32(100)),
						"the API-persisted default weight must be 100")
				},
			}),
		Entry("equal weights resolve deterministically by name",
			weightRow{
				description: "equal-weight name tie",
				// "tie-a-..." always sorts before "tie-z-..." regardless of
				// the random unique suffixes, so the deterministic name
				// tie-break is observable.
				winnerNamePrefix: "tie-a",
				loserNamePrefix:  "tie-z",
				winner: func(namespace, name string) *optipodv1alpha1.OptimizationPolicy {
					return framework.NewPolicyFixture(namespace, name, optipodv1alpha1.ModeRecommend,
						framework.WithWeight(100),
						framework.WithReconciliationInterval(30*time.Second))
				},
				winnerValues: smokeSamples(),
				loser: func(namespace, name string) *optipodv1alpha1.OptimizationPolicy {
					return framework.NewPolicyFixture(namespace, name, optipodv1alpha1.ModeRecommend,
						framework.WithWeight(100),
						clampPolicyBounds,
						framework.WithReconciliationInterval(30*time.Second))
				},
				loserValues: clampedSamples(),
			}),
		Entry("a higher-weight Recommend winner blocks Auto until a weight change flips the application",
			weightRow{
				description: "mode conflict and weight flip",
				winner: func(namespace, name string) *optipodv1alpha1.OptimizationPolicy {
					return framework.NewPolicyFixture(namespace, name, optipodv1alpha1.ModeRecommend,
						framework.WithWeight(300),
						framework.WithReconciliationInterval(30*time.Second))
				},
				winnerValues: smokeSamples(),
				loser: func(namespace, name string) *optipodv1alpha1.OptimizationPolicy {
					return framework.NewPolicyFixture(namespace, name, optipodv1alpha1.ModeAuto,
						framework.WithWeight(100),
						clampPolicyBounds,
						framework.WithStrategy(string(optipodv1alpha1.StrategySSA), ""),
						framework.WithReconciliationInterval(30*time.Second))
				},
				loserValues: clampedSamples(),
				flip:        runRecommendToAutoWeightFlip,
			}),
	)
})

// clampPolicyBounds cap a policy's recommendation (CPU max 200m, memory max
// 90Mi) so a clamp-bounds policy produces values distinct from the default
// wide bounds on the same fixture input.
var clampPolicyBounds = framework.WithResourceBounds("50m", "200m", "64Mi", "90Mi")

// runDeletionFlip deletes the winner and proves the runner-up takes over:
// the workload's exact values and policy identity flip to the runner-up's,
// the runner-up's summary records the processing, and Recommend policies
// still never roll pods.
func runDeletionFlip(ctx context.Context, namespace, workload string,
	beforeUIDs map[types.UID]string, winner, loser *optipodv1alpha1.OptimizationPolicy,
	loserValues []containerSamples) {
	By("deleting the winner and touching the runner-up to observe its takeover reconcile")
	// A policy object update triggers the production watch-driven reconcile,
	// so the takeover is observed promptly instead of waiting out the requeue
	// timer; the workload's flipped annotations below are what prove the flip.
	Expect(env.Clients.Ctrl.Delete(ctx, &optipodv1alpha1.OptimizationPolicy{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: winner.Name},
	})).To(Succeed(), "delete the winning policy")
	deletedAt := time.Now()
	touchPolicy(ctx, namespace, loser.Name)

	By("observing the runner-up's exact values and identity on the workload")
	flipped := waitForDeploymentAnnotation(ctx, namespace, workload,
		winnerAnnotations(loserValues, loser.Name, loser.UID))
	expectManagementAnnotations(flipped, loser, loser.CreationTimestamp.Time)
	expectUnappliedContainerRequests(flipped.Spec.Template.Spec.Containers, smokeSamples(),
		"the workload after the runner-up took over")
	Expect(podUIDs(ctx, namespace, "app="+workload)).To(Equal(beforeUIDs),
		"the winner flip between Recommend policies rolled workload pods")
	waitForPolicyStatus(ctx, namespace, loser.Name, policyStatusWait{
		readyStatus: "True", readyReason: "PolicyValid", expectSummary: true,
		discovered: 1, processed: 1, reconciledAfter: deletedAt,
	})
}

// runRecommendToAutoWeightFlip raises the blocked Auto policy's weight above
// the Recommend winner and proves the consistent flip: the Auto policy's own
// distinct values are applied to the pod template, the pod rolls, the
// workload's policy identity flips, and the flipped summaries show the Auto
// winner processing while the Recommend loser now skips.
//
// Two production behaviors shape the observation order: the flip reconcile
// applies immediately, but its annotation write is throttled while the
// previous winner's recommendation annotation is under 30 seconds old; and
// the apply rolls the pod while metrics are per pod, so the flipped
// recommendation annotations land on the winner's first requeue reconcile
// that finds re-registered data for the replacement pod. The bounded waits
// observe exactly that requeue-driven flip.
func runRecommendToAutoWeightFlip(ctx context.Context, namespace, workload string,
	beforeUIDs map[types.UID]string, winner, loser *optipodv1alpha1.OptimizationPolicy,
	loserValues []containerSamples) {
	By("raising the Auto policy's weight above the Recommend winner")
	flippedAt := time.Now()
	updatePolicy(ctx, namespace, loser.Name, func(p *optipodv1alpha1.OptimizationPolicy) {
		weight := int32(500)
		p.Spec.Weight = &weight
	})

	By("observing the Auto winner's exact clamp values applied to the template")
	waitForWorkloadTemplateRequests(ctx, optipodv1alpha1.WorkloadTypeDeployment,
		namespace, workload, loserValues)

	By("waiting for the rollout to produce a new ready pod with the applied values")
	freshPods := waitForReplacementPod(ctx, namespace, "app="+workload, beforeUIDs)
	expectAppliedContainerRequests(freshPods[0].Spec.Containers, loserValues,
		"the replacement pod after the Auto policy won")
	waitForPodsToSettle(ctx, namespace, "app="+workload, beforeUIDs)

	By("re-registering the fixture series for the replacement pod the Auto winner rolled")
	registerWorkloadSeries(ctx, namespace, workload, smokeSamples())

	By("observing the Auto winner's identity and clamp values flip onto the workload")
	applied := waitForDeploymentAnnotation(ctx, namespace, workload,
		winnerAnnotations(loserValues, loser.Name, loser.UID))
	expectManagementAnnotations(applied, loser, loser.CreationTimestamp.Time)
	expectAppliedContainerRequests(applied.Spec.Template.Spec.Containers, loserValues,
		"the workload after the Auto policy won")

	By("observing the flipped summaries: the Auto winner processes, the Recommend loser skips")
	waitForPolicyStatus(ctx, namespace, loser.Name, policyStatusWait{
		readyStatus: "True", readyReason: "PolicyValid", expectSummary: true,
		discovered: 1, processed: 1, reconciledAfter: flippedAt,
	})
	touchPolicy(ctx, namespace, winner.Name)
	waitForPolicyStatus(ctx, namespace, winner.Name, policyStatusWait{
		readyStatus: "True", readyReason: "PolicyValid", expectSummary: true,
		discovered: 1, processed: 0, reconciledAfter: flippedAt,
	})
}

// clampedSamples returns the same container identities and fixture inputs as
// smokeSamples() but with the exact recommendation values a policy whose
// bounds clamp the recommendation (CPU max 200m, memory max 90Mi) must
// derive from that input: the app CPU/memory recommendations hit the bounds,
// the sidecar stays within them.
func clampedSamples() []containerSamples {
	return []containerSamples{
		// app: P90 250m * 1.2 = 300m clamped to 200m; P90 100Mi * 1.2 =
		// 120Mi clamped to 90Mi.
		{name: "app", cpuCores: 0.25, memoryBytes: 100 << 20,
			initialCPU: "100m", initialMemory: "64Mi",
			expectedCPU: "200m", expectedMemory: "90Mi"},
		// sidecar: P90 125m * 1.2 = 150m (within 200m); P90 60Mi * 1.2 =
		// 72Mi (within 90Mi).
		{name: "sidecar", cpuCores: 0.125, memoryBytes: 60 << 20,
			initialCPU: "50m", initialMemory: "32Mi",
			expectedCPU: "150m", expectedMemory: "72Mi"},
	}
}

// winnerAnnotations builds the annotation map proving which policy won: the
// winner's exact recommendation values plus the managing-policy identity.
func winnerAnnotations(samples []containerSamples, policyName string, policyUID types.UID) map[string]string {
	want := recommendationAnnotations(samples)
	want[optipodv1alpha1.AnnotationPolicy] = policyName
	want[optipodv1alpha1.AnnotationPolicyUID] = string(policyUID)
	return want
}
