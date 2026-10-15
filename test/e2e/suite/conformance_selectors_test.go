package suite

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
)

// These scenarios prove the installed controller's selection behavior on real
// Deployment, StatefulSet and DaemonSet objects (VAL-CONF-002): every eligible
// kind receives the exact fixture-derived recommendations and, under an
// explicit Auto SSA policy, the exact applied template resources and a rolled
// pod; workload-type include/exclude filters (exclude with precedence) and an
// omitted filter (compatibility) discover and process exactly the eligible
// kinds; and namespace allow/deny lists, namespace labels and workload labels
// each leave every non-matching object untouched after a completed reconcile.
// Every positive outcome asserts exact per-container values and management
// annotations read through the private typed API, and every negative outcome
// is asserted only after the policy status records a completed reconcile
// (fresh LastReconciliation), so an excluded object is proven unchanged by a
// reconcile that actually ran rather than by an instantaneous absence check.
// Non-matching workloads receive fixture series too, so a wrongly-selected
// object would be annotated and no negative can pass merely for lack of data.

var _ = Describe("Each eligible workload kind receives exact recommendations and applied resources", Label("conformance", "selectors"), func() {
	type kindRow struct {
		kind optipodv1alpha1.WorkloadType
	}

	DescribeTable("applies the fixture-derived values to the eligible kind and rolls its pod",
		func(ctx context.Context, row kindRow) {
			ns := specNamespace(ctx, framework.NormalizePrefix("conf-kind-"+string(row.kind)))
			DeferCleanup(func(ctx context.Context) {
				Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed(),
					"clean up the %s kind spec namespace", row.kind)
			})
			samples := smokeSamples()

			By("creating a two-container " + string(row.kind) + " whose pod becomes ready")
			name := createKindWorkload(ctx, row.kind, ns.Name, "conf-kind", samples, nil)
			podSelector := "app=" + name
			beforeUIDs := podUIDs(ctx, ns.Name, podSelector)

			By("registering deterministic metric samples for the workload's pod")
			series := registerWorkloadSeries(ctx, ns.Name, name, samples)
			Expect(env.Fixture.ClearTrace(ctx)).To(Succeed(), "reset the fixture trace for this spec")

			By("creating an Auto policy with the explicit SSA strategy selecting the namespace")
			policyName, err := framework.UniqueName("policy")
			Expect(err).NotTo(HaveOccurred(), "generate unique policy name")
			policy := framework.NewPolicyFixture(ns.Name, policyName, optipodv1alpha1.ModeAuto,
				framework.WithStrategy(string(optipodv1alpha1.StrategySSA), ""),
				framework.WithReconciliationInterval(30*time.Second))
			Expect(env.Clients.Ctrl.Create(ctx, policy)).To(Succeed(), "create the Auto SSA policy")
			createdAt := policy.CreationTimestamp.Time

			By("waiting for the exact applied requests on the " + string(row.kind) + " pod template")
			waitForWorkloadTemplateRequests(ctx, row.kind, ns.Name, name, samples)

			By("asserting the exact recommendation annotations and management metadata")
			view := waitForWorkloadAnnotations(ctx, row.kind, ns.Name, name, recommendationAnnotations(samples))
			expectManagementAnnotationsOn(view.annotations, policy, createdAt)

			By("waiting for the rollout to produce a new ready pod with the applied values")
			freshPods := waitForReplacementPod(ctx, ns.Name, podSelector, beforeUIDs)
			expectAppliedContainerRequests(freshPods[0].Spec.Containers, samples,
				"the "+string(row.kind)+" replacement pod")
			waitForPodsToSettle(ctx, ns.Name, podSelector, beforeUIDs)

			By("waiting for the policy's reconciliation summary and per-kind counts")
			observed := waitForPolicyStatus(ctx, ns.Name, policyName, policyStatusWait{
				readyStatus:     "True",
				readyReason:     "PolicyValid",
				expectSummary:   true,
				discovered:      1,
				processed:       1,
				reconciledAfter: createdAt,
			})
			expectWorkloadTypeCounts(observed,
				countForKind(row.kind, optipodv1alpha1.WorkloadTypeDeployment),
				countForKind(row.kind, optipodv1alpha1.WorkloadTypeStatefulSet),
				countForKind(row.kind, optipodv1alpha1.WorkloadTypeDaemonSet))

			By("proving the deployed controller queried the fixture for this kind's pod")
			expectFixtureQueried(ctx, series)
		},
		Entry("Deployment", kindRow{optipodv1alpha1.WorkloadTypeDeployment}),
		Entry("StatefulSet", kindRow{optipodv1alpha1.WorkloadTypeStatefulSet}),
		Entry("DaemonSet", kindRow{optipodv1alpha1.WorkloadTypeDaemonSet}),
	)
})

var _ = Describe("Workload-type include/exclude filters select only eligible kinds", Label("conformance", "selectors"), func() {
	type typeFilterRow struct {
		description string
		include     []optipodv1alpha1.WorkloadType
		exclude     []optipodv1alpha1.WorkloadType
		// wantAnnotated lists the kinds that must receive exact recommendations.
		wantAnnotated []optipodv1alpha1.WorkloadType
		// wantDiscovered is the discovery count the filter must produce.
		wantDiscovered int
	}

	DescribeTable("processes exactly the eligible kinds and leaves excluded kinds untouched",
		func(ctx context.Context, row typeFilterRow) {
			ns := specNamespace(ctx, "conf-types")
			DeferCleanup(func(ctx context.Context) {
				Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed(),
					"clean up the workload-type filter spec namespace")
			})
			samples := smokeSamples()

			By("creating one workload of each kind whose pods become ready")
			names := map[optipodv1alpha1.WorkloadType]string{}
			for _, kind := range allWorkloadKinds {
				names[kind] = createKindWorkload(ctx, kind, ns.Name, "conf-types", samples, nil)
			}

			By("registering one wholesale fixture series set covering every kind's pods")
			// SetSeries replaces the fixture's entire series set, so every
			// workload's series must be built and written together; each kind
			// gets series so a wrongly-included kind would be processed and
			// annotated, and an excluded-kind assertion can never pass merely
			// for lack of data.
			seriesByKind := map[optipodv1alpha1.WorkloadType][]framework.FixtureSeries{}
			var all []framework.FixtureSeries
			for _, kind := range allWorkloadKinds {
				seriesByKind[kind] = buildWorkloadSeries(ctx, ns.Name, names[kind], samples)
				all = append(all, seriesByKind[kind]...)
			}
			Expect(env.Fixture.SetSeries(ctx, all)).To(Succeed(),
				"register deterministic series for every kind's pods")
			Expect(env.Fixture.ClearTrace(ctx)).To(Succeed(), "reset the fixture trace for this spec")

			By("creating a Recommend policy with the row's workload-type filter")
			policyName, err := framework.UniqueName("policy")
			Expect(err).NotTo(HaveOccurred(), "generate unique policy name")
			policy := framework.NewPolicyFixture(ns.Name, policyName, optipodv1alpha1.ModeRecommend,
				framework.WithWorkloadTypes(row.include, row.exclude),
				framework.WithReconciliationInterval(30*time.Second))
			Expect(env.Clients.Ctrl.Create(ctx, policy)).To(Succeed(), "create the type-filter policy")
			createdAt := policy.CreationTimestamp.Time

			By("waiting for the completed reconcile with the filtered discovery count")
			observed := waitForPolicyStatus(ctx, ns.Name, policyName, policyStatusWait{
				readyStatus:     "True",
				readyReason:     "PolicyValid",
				expectSummary:   true,
				discovered:      row.wantDiscovered,
				processed:       row.wantDiscovered,
				reconciledAfter: createdAt,
			})
			expectWorkloadTypeCounts(observed,
				memberCount(row.wantAnnotated, optipodv1alpha1.WorkloadTypeDeployment),
				memberCount(row.wantAnnotated, optipodv1alpha1.WorkloadTypeStatefulSet),
				memberCount(row.wantAnnotated, optipodv1alpha1.WorkloadTypeDaemonSet))

			By("asserting exact recommendations on each eligible kind")
			for _, kind := range row.wantAnnotated {
				view := waitForWorkloadAnnotations(ctx, kind, ns.Name, names[kind], recommendationAnnotations(samples))
				expectManagementAnnotationsOn(view.annotations, policy, createdAt)
			}

			By("asserting every excluded kind remains untouched after the confirmed reconcile")
			for _, kind := range allWorkloadKinds {
				if kindInList(row.wantAnnotated, kind) {
					continue
				}
				view, err := readWorkloadView(ctx, kind, ns.Name, names[kind])
				Expect(err).NotTo(HaveOccurred(), "read the excluded %s after the reconcile", kind)
				expectUntouchedWorkload(view, samples, "the excluded "+string(kind))
			}

			By("proving the controller queried the fixture only for the eligible kinds' pods")
			var eligibleSeries, excludedSeries []framework.FixtureSeries
			for _, kind := range allWorkloadKinds {
				if kindInList(row.wantAnnotated, kind) {
					eligibleSeries = append(eligibleSeries, seriesByKind[kind]...)
				} else {
					excludedSeries = append(excludedSeries, seriesByKind[kind]...)
				}
			}
			expectFixtureQueried(ctx, eligibleSeries)
			expectNoFixtureQueryFor(ctx, excludedSeries, "the excluded kinds")
		},
		Entry("an include list processes only the Deployment",
			typeFilterRow{
				description:    "include Deployments only",
				include:        []optipodv1alpha1.WorkloadType{optipodv1alpha1.WorkloadTypeDeployment},
				wantAnnotated:  []optipodv1alpha1.WorkloadType{optipodv1alpha1.WorkloadTypeDeployment},
				wantDiscovered: 1,
			}),
		Entry("an exclude list processes the StatefulSet and DaemonSet and skips the Deployment",
			typeFilterRow{
				description:    "exclude Deployments",
				exclude:        []optipodv1alpha1.WorkloadType{optipodv1alpha1.WorkloadTypeDeployment},
				wantAnnotated:  []optipodv1alpha1.WorkloadType{optipodv1alpha1.WorkloadTypeStatefulSet, optipodv1alpha1.WorkloadTypeDaemonSet},
				wantDiscovered: 2,
			}),
		Entry("exclude takes precedence over include for the same kind",
			typeFilterRow{
				description:    "exclude beats include",
				include:        []optipodv1alpha1.WorkloadType{optipodv1alpha1.WorkloadTypeDeployment, optipodv1alpha1.WorkloadTypeStatefulSet},
				exclude:        []optipodv1alpha1.WorkloadType{optipodv1alpha1.WorkloadTypeDeployment},
				wantAnnotated:  []optipodv1alpha1.WorkloadType{optipodv1alpha1.WorkloadTypeStatefulSet},
				wantDiscovered: 1,
			}),
		Entry("an omitted type filter processes all three kinds (compatibility)",
			typeFilterRow{
				description:    "no workload-type filter",
				wantAnnotated:  []optipodv1alpha1.WorkloadType{optipodv1alpha1.WorkloadTypeDeployment, optipodv1alpha1.WorkloadTypeStatefulSet, optipodv1alpha1.WorkloadTypeDaemonSet},
				wantDiscovered: 3,
			}),
	)
})

var _ = Describe("Namespace and workload label selectors affect only eligible objects", Label("conformance", "selectors"), func() {
	type selectionRow struct {
		description string
		// ineligibleOtherNamespace places the ineligible workload in a second
		// namespace (namespace-level negatives).
		ineligibleOtherNamespace bool
		// eligibleNamespaceLabels are labels on the eligible workload's
		// namespace (namespace label-selector rows).
		eligibleNamespaceLabels map[string]string
		// eligibleWorkloadLabels are labels on the eligible workload object
		// (workload label-selector rows).
		eligibleWorkloadLabels map[string]string
		// policyOptions builds the policy's selector options from the two
		// namespace names (the deny row must name both dynamically created
		// namespaces).
		policyOptions func(eligibleNS, ineligibleNS string) []framework.PolicyOption
	}

	DescribeTable("processes the matching object and leaves the non-matching one untouched",
		func(ctx context.Context, row selectionRow) {
			var eligibleNS *corev1.Namespace
			if row.eligibleNamespaceLabels != nil {
				eligibleNS = labeledNamespace(ctx, "conf-select", row.eligibleNamespaceLabels)
			} else {
				eligibleNS = specNamespace(ctx, "conf-select")
			}
			DeferCleanup(func(ctx context.Context) {
				Expect(framework.DeleteNamespace(ctx, env.Clients, eligibleNS.Name)).To(Succeed(),
					"clean up the eligible namespace of the %s row", row.description)
			})
			samples := smokeSamples()

			By("creating the eligible workload in the selected namespace")
			eligible := createKindWorkload(ctx, optipodv1alpha1.WorkloadTypeDeployment,
				eligibleNS.Name, "conf-select", samples, row.eligibleWorkloadLabels)

			var ineligibleNS *corev1.Namespace
			if row.ineligibleOtherNamespace {
				By("creating the ineligible workload in a second namespace")
				ineligibleNS = specNamespace(ctx, "conf-other")
				DeferCleanup(func(ctx context.Context) {
					Expect(framework.DeleteNamespace(ctx, env.Clients, ineligibleNS.Name)).To(Succeed(),
						"clean up the ineligible namespace of the %s row", row.description)
				})
			} else {
				By("creating the ineligible workload in the same namespace without the selector label")
				ineligibleNS = eligibleNS
			}
			ineligible := createKindWorkload(ctx, optipodv1alpha1.WorkloadTypeDeployment,
				ineligibleNS.Name, "conf-select", samples, nil)

			By("registering one wholesale fixture series set covering both workloads' pods")
			// Both workloads receive series in one wholesale write: a
			// wrongly-selected ineligible workload would then be annotated,
			// so the untouched assertion cannot pass merely for lack of data.
			eligibleSeries := buildWorkloadSeries(ctx, eligibleNS.Name, eligible, samples)
			ineligibleSeries := buildWorkloadSeries(ctx, ineligibleNS.Name, ineligible, samples)
			all := make([]framework.FixtureSeries, 0, len(eligibleSeries)+len(ineligibleSeries))
			all = append(all, eligibleSeries...)
			all = append(all, ineligibleSeries...)
			Expect(env.Fixture.SetSeries(ctx, all)).To(Succeed(),
				"register deterministic series for both workloads' pods")
			Expect(env.Fixture.ClearTrace(ctx)).To(Succeed(), "reset the fixture trace for this spec")

			By("creating a Recommend policy with the row's selectors")
			policyName, err := framework.UniqueName("policy")
			Expect(err).NotTo(HaveOccurred(), "generate unique policy name")
			policy := framework.NewPolicyFixture(eligibleNS.Name, policyName, optipodv1alpha1.ModeRecommend,
				append(row.policyOptions(eligibleNS.Name, ineligibleNS.Name),
					framework.WithReconciliationInterval(30*time.Second))...)
			Expect(env.Clients.Ctrl.Create(ctx, policy)).To(Succeed(), "create the selector policy")
			createdAt := policy.CreationTimestamp.Time

			By("waiting for the completed reconcile that discovered only the eligible workload")
			waitForPolicyStatus(ctx, eligibleNS.Name, policyName, policyStatusWait{
				readyStatus:     "True",
				readyReason:     "PolicyValid",
				expectSummary:   true,
				discovered:      1,
				processed:       1,
				reconciledAfter: createdAt,
			})

			By("asserting exact recommendations on the eligible workload")
			view := waitForWorkloadAnnotations(ctx, optipodv1alpha1.WorkloadTypeDeployment,
				eligibleNS.Name, eligible, recommendationAnnotations(samples))
			expectManagementAnnotationsOn(view.annotations, policy, createdAt)

			By("asserting the non-matching workload remains untouched after the confirmed reconcile")
			untouched, err := readWorkloadView(ctx, optipodv1alpha1.WorkloadTypeDeployment, ineligibleNS.Name, ineligible)
			Expect(err).NotTo(HaveOccurred(), "read the non-matching workload after the reconcile")
			expectUntouchedWorkload(untouched, samples, "the non-matching workload")

			By("proving the controller queried the eligible workload and never queried the ineligible one")
			expectFixtureQueried(ctx, eligibleSeries)
			expectNoFixtureQueryFor(ctx, ineligibleSeries, "the non-matching workload")
		},
		Entry("a workload label selector processes only the labeled Deployment",
			selectionRow{
				description:            "workload label selector",
				eligibleWorkloadLabels: map[string]string{"optimize-tier": "gold"},
				policyOptions: func(_, _ string) []framework.PolicyOption {
					return []framework.PolicyOption{
						framework.WithWorkloadSelector(map[string]string{"optimize-tier": "gold"}),
					}
				},
			}),
		Entry("a deny list takes precedence over the allow list for a namespace",
			selectionRow{
				description:              "denied namespace",
				ineligibleOtherNamespace: true,
				policyOptions: func(eligibleNS, ineligibleNS string) []framework.PolicyOption {
					return []framework.PolicyOption{
						framework.WithNamespaceFilter([]string{eligibleNS, ineligibleNS}, []string{ineligibleNS}),
					}
				},
			}),
		Entry("a namespace label selector processes only the labeled namespace",
			selectionRow{
				description:              "namespace label selector",
				ineligibleOtherNamespace: true,
				eligibleNamespaceLabels:  map[string]string{"optimize-env": "true"},
				policyOptions: func(_, _ string) []framework.PolicyOption {
					return []framework.PolicyOption{
						// Clear the fixture's default allow list so the label
						// selector alone governs namespace matching.
						framework.WithNamespaceFilter(nil, nil),
						framework.WithNamespaceSelector(map[string]string{"optimize-env": "true"}),
					}
				},
			}),
	)
})

// allWorkloadKinds is the fixed per-row order of the three supported kinds.
var allWorkloadKinds = []optipodv1alpha1.WorkloadType{
	optipodv1alpha1.WorkloadTypeDeployment,
	optipodv1alpha1.WorkloadTypeStatefulSet,
	optipodv1alpha1.WorkloadTypeDaemonSet,
}

// kindInList reports whether kind appears in list.
func kindInList(list []optipodv1alpha1.WorkloadType, kind optipodv1alpha1.WorkloadType) bool {
	for _, k := range list {
		if k == kind {
			return true
		}
	}
	return false
}

// memberCount returns 1 when kind is in the annotated list, else 0: each
// filter row creates at most one workload per kind, so membership is the
// expected per-kind discovered count.
func memberCount(list []optipodv1alpha1.WorkloadType, kind optipodv1alpha1.WorkloadType) int {
	if kindInList(list, kind) {
		return 1
	}
	return 0
}

// countForKind returns the expected per-kind discovered count for a
// single-workload kind row.
func countForKind(rowKind, kind optipodv1alpha1.WorkloadType) int {
	if rowKind == kind {
		return 1
	}
	return 0
}
