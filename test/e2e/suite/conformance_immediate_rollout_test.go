package suite

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
)

// This scenario proves the installed controller's explicit immediate rollout
// for all three supported workload kinds (VAL-CONF-008). One Auto policy
// with strategy webhook and rollout immediate selects a Deployment, a
// StatefulSet and a DaemonSet, each created with the production
// optipod.io/webhook-enabled pod-template label before creation so every
// regenerated pod is naturally generated and admitted through the installed
// mutating webhook (never hand-labelled afterwards).
//
// For each kind the spec requires the concrete rollout evidence the
// contract demands: the supported template restart marker (an RFC3339
// timestamp at or after policy creation), the stored per-container
// recommendation and webhook metadata, a NEW ready naturally generated pod
// whose every sampled container carries the exact admitted recommended
// values, and NO unrequested template resource edits — the workload's pod
// template keeps its original requests; only the restart marker was
// requested. A stored annotation without the observed restart and admitted
// values cannot pass this spec.
var _ = Describe("Explicit immediate webhook rollout mutates newly generated pods per workload kind", Label("conformance", "rollout"), func() {
	It("triggers the template restart marker and admits every recommended container value on fresh ready Deployment, StatefulSet and DaemonSet pods", func(ctx context.Context) {
		ns := specNamespace(ctx, "conf-rollout-immediate")
		DeferCleanup(func(ctx context.Context) {
			Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed(),
				"clean up the immediate-rollout conformance spec namespace")
		})
		samples := smokeSamples()
		kinds := []optipodv1alpha1.WorkloadType{
			optipodv1alpha1.WorkloadTypeDeployment,
			optipodv1alpha1.WorkloadTypeStatefulSet,
			optipodv1alpha1.WorkloadTypeDaemonSet,
		}

		By("creating one webhook-eligible two-container workload per kind, each with a ready pod")
		names := make(map[optipodv1alpha1.WorkloadType]string, len(kinds))
		beforeUIDs := make(map[optipodv1alpha1.WorkloadType]map[types.UID]string, len(kinds))
		for _, kind := range kinds {
			names[kind] = createWebhookKindWorkload(ctx, kind, ns.Name, "conf-rollout", samples)
			beforeUIDs[kind] = podUIDs(ctx, ns.Name, "app="+names[kind])
		}

		By("registering deterministic metric samples for every kind's pod")
		var series []framework.FixtureSeries
		for _, kind := range kinds {
			series = append(series, buildWorkloadSeries(ctx, ns.Name, names[kind], samples)...)
		}
		Expect(env.Fixture.SetSeries(ctx, series)).To(Succeed(),
			"register the deterministic series for all three workloads")
		Expect(env.Fixture.ClearTrace(ctx)).To(Succeed(), "reset the fixture trace for this spec")

		By("creating one Auto policy with the explicit immediate rollout")
		policyName, err := framework.UniqueName("policy")
		Expect(err).NotTo(HaveOccurred(), "generate unique policy name")
		policy := framework.NewPolicyFixture(ns.Name, policyName, optipodv1alpha1.ModeAuto,
			framework.WithStrategy("webhook", "immediate"),
			framework.WithReconciliationInterval(30*time.Second))
		Expect(env.Clients.Ctrl.Create(ctx, policy)).To(Succeed(), "create the immediate-rollout Auto policy")
		createdAt := policy.CreationTimestamp.Time

		By("waiting for the restart marker and the stored metadata on every kind's workload")
		want := recommendationAnnotations(samples)
		for key, value := range webhookApplyAnnotations(samples) {
			want[key] = value
		}
		for _, kind := range kinds {
			marker := waitForTemplateRestartMarker(ctx, kind, ns.Name, names[kind])
			expectRestartMarkerRecent(marker, createdAt)
			view := waitForWorkloadAnnotations(ctx, kind, ns.Name, names[kind], want)
			expectManagementAnnotationsOn(view.annotations, policy, createdAt)
		}

		By("waiting for one new ready naturally generated pod per kind")
		for _, kind := range kinds {
			podSelector := "app=" + names[kind]
			fresh := waitForReplacementPod(ctx, ns.Name, podSelector, beforeUIDs[kind])
			waitForPodsToSettle(ctx, ns.Name, podSelector, beforeUIDs[kind])
			Expect(fresh).To(HaveLen(1),
				"the immediate rollout must have produced exactly one replacement %s pod", kind)
			expectAppliedContainerRequests(fresh[0].Spec.Containers, samples,
				fmt.Sprintf("the freshly admitted %s pod under immediate rollout", kind))
			settled, err := env.Clients.Kube.CoreV1().Pods(ns.Name).List(ctx,
				metav1.ListOptions{LabelSelector: podSelector})
			Expect(err).NotTo(HaveOccurred(), "list the settled %s pods", kind)
			Expect(settled.Items).To(HaveLen(1), "the %s must have settled at exactly one pod", kind)
			expectAppliedContainerRequests(settled.Items[0].Spec.Containers, samples,
				fmt.Sprintf("the settled %s pod after the immediate rollout", kind))
		}

		By("asserting the immediate rollout edited no template resource fields")
		for _, kind := range kinds {
			view, err := readWorkloadView(ctx, kind, ns.Name, names[kind])
			Expect(err).NotTo(HaveOccurred(), "read the %s %s/%s after the immediate rollout",
				kind, ns.Name, names[kind])
			expectUnappliedContainerRequests(view.containers, samples,
				fmt.Sprintf("the %s pod template after the immediate rollout (only the restart marker was requested)", kind))
		}

		By("waiting for the policy's reconciliation summary")
		waitForPolicyStatus(ctx, ns.Name, policyName, policyStatusWait{
			readyStatus:     "True",
			readyReason:     "PolicyValid",
			expectSummary:   true,
			discovered:      len(kinds),
			processed:       len(kinds),
			reconciledAfter: createdAt,
		})

		By("proving the deployed controller queried the fixture for every kind's original pod")
		expectFixtureQueried(ctx, series)
	})
})
