package suite

import (
	"context"
	"fmt"
	"sort"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
)

// These scenarios prove the deployed controller's Recommend and Auto behavior
// against deterministic metric samples served by the in-cluster fixture
// (VAL-SMOKE-002). Recommend must write the exact per-container recommendation
// annotations derived from the queried samples while leaving workload
// requests/limits, the pod template and pod identities untouched; explicit
// Auto under an SSA strategy must apply the corresponding distinct CPU and
// memory values and roll the workload. The fixture's existence, a Ready policy
// or an unchanged request alone can never pass these specs: each one observes
// the controller's queries, the recommendation values and the workload state.
//
// The Auto spec uses a single-container workload. Applying to more than one
// container under SSA is currently destructive in production (each
// per-container apply shares the "optipod" field manager and omits the other
// containers, so the API server prunes previously applied requests); that
// behavior belongs to multi-container conformance, not to this smoke tier.
var _ = Describe("Recommend and Auto modes with deterministic deployed metrics", Label("smoke", "modes"), func() {
	const waitTimeout = 3 * time.Minute

	// recommendationAnnotations builds the exact per-container annotation map a
	// policy with the default requests-only behavior must write.
	recommendationAnnotations := func(samples []containerSamples) map[string]string {
		prefix := optipodv1alpha1.AnnotationRecommendationPrefix
		annotations := map[string]string{}
		for _, s := range samples {
			annotations[fmt.Sprintf("%s.%s.cpu-request", prefix, s.name)] = s.expectedCPU
			annotations[fmt.Sprintf("%s.%s.memory-request", prefix, s.name)] = s.expectedMemory
		}
		return annotations
	}

	It("writes exact per-container recommendations without changing requests, limits or pods", Label("recommend"), func(ctx context.Context) {
		ns := specNamespace(ctx, "smoke-recommend")
		DeferCleanup(func(ctx context.Context) {
			Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed(),
				"clean up the Recommend-mode spec namespace")
		})

		By("creating a two-container workload whose pods become ready")
		workload := createSmokeWorkload(ctx, ns.Name, "smoke-recommend", smokeSamples())
		podSelector := "app=" + workload.Name
		beforeUIDs := podUIDs(ctx, ns.Name, podSelector)
		before := &appsv1.Deployment{}
		Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(workload), before)).To(Succeed(),
			"read the workload before the policy exists")
		beforeReplicaSets := replicaSetNames(ctx, ns.Name, podSelector)

		By("registering deterministic metric samples for the workload's pods")
		series := registerContainerSeries(ctx, ns.Name, workload, smokeSamples())
		Expect(env.Fixture.ClearTrace(ctx)).To(Succeed(), "reset the fixture trace for this spec")

		By("creating a Recommend policy that selects the workload")
		policyName, err := framework.UniqueName("policy")
		Expect(err).NotTo(HaveOccurred(), "generate unique policy name")
		policy := framework.NewPolicyFixture(ns.Name, policyName, optipodv1alpha1.ModeRecommend,
			framework.WithReconciliationInterval(30*time.Second))
		Expect(env.Clients.Ctrl.Create(ctx, policy)).To(Succeed(), "create the Recommend policy")
		createdAt := policy.CreationTimestamp.Time

		By("waiting for the exact per-container recommendation annotations")
		annotated := waitForDeploymentAnnotation(ctx, ns.Name, workload.Name,
			recommendationAnnotations(smokeSamples()), waitTimeout)
		expectManagementAnnotations(annotated, policy, createdAt)

		By("asserting requests, limits, the pod template and pod identity were left untouched")
		for _, s := range smokeSamples() {
			gotCPU, gotMemory := containerRequests(annotated.Spec.Template.Spec.Containers, s.name)
			Expect(gotCPU).NotTo(BeNil(), "container %s lost its CPU request", s.name)
			Expect(gotCPU.String()).To(Equal(s.initialCPU),
				"Recommend changed container %s CPU request", s.name)
			Expect(gotMemory.String()).To(Equal(s.initialMemory),
				"Recommend changed container %s memory request", s.name)
			gotCPULimit, gotMemLimit := containerLimits(annotated.Spec.Template.Spec.Containers, s.name)
			Expect(gotCPULimit).To(BeNil(), "Recommend added a CPU limit to container %s", s.name)
			Expect(gotMemLimit).To(BeNil(), "Recommend added a memory limit to container %s", s.name)
		}
		// metadata.generation is not a usable invariant here: the Deployment
		// registry strategy increments it on annotation changes as well as
		// spec changes, so the controller's annotation-only write bumps it.
		Expect(apiequality.Semantic.DeepEqual(annotated.Spec, before.Spec)).To(BeTrue(),
			"Recommend changed the workload spec")
		Expect(annotated.Annotations["deployment.kubernetes.io/revision"]).To(
			Equal(before.Annotations["deployment.kubernetes.io/revision"]),
			"Recommend triggered a new Deployment revision")
		Expect(replicaSetNames(ctx, ns.Name, podSelector)).To(Equal(beforeReplicaSets),
			"Recommend caused a new ReplicaSet")
		Expect(podUIDs(ctx, ns.Name, podSelector)).To(Equal(beforeUIDs),
			"Recommend restarted or replaced workload pods")

		By("asserting the default requests-only policy wrote no limit annotations")
		limitKey := fmt.Sprintf("%s.%s.cpu-limit", optipodv1alpha1.AnnotationRecommendationPrefix, smokeApp.name)
		Expect(annotated.Annotations).NotTo(HaveKey(limitKey),
			"the default updateRequestsOnly=true behavior must not write limit annotations")

		By("waiting for the policy's reconciliation summary")
		waitForPolicyStatus(ctx, ns.Name, policyName, policyStatusWait{
			readyStatus:     "True",
			readyReason:     "PolicyValid",
			expectSummary:   true,
			discovered:      1,
			processed:       1,
			reconciledAfter: createdAt,
		})

		By("proving the deployed controller queried the fixture for these targets")
		expectFixtureQueried(ctx, series)
	})

	It("applies the exact resource values under an explicit Auto SSA policy", Label("auto", "ssa"), func(ctx context.Context) {
		ns := specNamespace(ctx, "smoke-auto")
		DeferCleanup(func(ctx context.Context) {
			Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed(),
				"clean up the Auto-mode spec namespace")
		})
		samples := []containerSamples{smokeApp}

		By("creating a single-container workload whose pod becomes ready")
		workload := createSmokeWorkload(ctx, ns.Name, "smoke-auto", samples)
		podSelector := "app=" + workload.Name
		beforeUIDs := podUIDs(ctx, ns.Name, podSelector)

		By("registering deterministic metric samples for the workload's pod")
		series := registerContainerSeries(ctx, ns.Name, workload, samples)
		Expect(env.Fixture.ClearTrace(ctx)).To(Succeed(), "reset the fixture trace for this spec")

		By("creating an Auto policy with the explicit SSA strategy")
		policyName, err := framework.UniqueName("policy")
		Expect(err).NotTo(HaveOccurred(), "generate unique policy name")
		policy := framework.NewPolicyFixture(ns.Name, policyName, optipodv1alpha1.ModeAuto,
			framework.WithStrategy("ssa", ""),
			framework.WithReconciliationInterval(30*time.Second))
		Expect(env.Clients.Ctrl.Create(ctx, policy)).To(Succeed(), "create the Auto policy")
		createdAt := policy.CreationTimestamp.Time

		By("waiting for the exact request values to be applied")
		applied := waitForDeploymentRequests(ctx, ns.Name, workload.Name, smokeApp.name,
			smokeApp.expectedCPU, smokeApp.expectedMemory, waitTimeout)

		By("asserting the recommendation annotations and untouched limits on the applied workload")
		expectManagementAnnotations(applied, policy, createdAt)
		for key, value := range recommendationAnnotations(samples) {
			Expect(applied.Annotations[key]).To(Equal(value),
				"applied workload annotation %q has the wrong value", key)
		}
		gotCPULimit, gotMemLimit := containerLimits(applied.Spec.Template.Spec.Containers, smokeApp.name)
		Expect(gotCPULimit).To(BeNil(),
			"the default updateRequestsOnly=true behavior added a CPU limit to %s", smokeApp.name)
		Expect(gotMemLimit).To(BeNil(),
			"the default updateRequestsOnly=true behavior added a memory limit to %s", smokeApp.name)

		By("waiting for the rollout to produce a new ready pod with the applied values")
		Expect(framework.WaitForDeploymentAvailable(ctx, env.Clients, ns.Name, workload.Name,
			framework.WaitOptions{Timeout: waitTimeout})).To(Succeed(),
			"the applied workload never returned to Available")
		freshPods := waitForReplacementPod(ctx, ns.Name, podSelector, beforeUIDs, waitTimeout)
		podCPU, podMemory := containerRequests(freshPods[0].Spec.Containers, smokeApp.name)
		Expect(podCPU).NotTo(BeNil(), "replacement pod lost container %s", smokeApp.name)
		Expect(podCPU.String()).To(Equal(smokeApp.expectedCPU),
			"replacement pod container %s does not carry the applied CPU request", smokeApp.name)
		Expect(podMemory.String()).To(Equal(smokeApp.expectedMemory),
			"replacement pod container %s does not carry the applied memory request", smokeApp.name)

		By("waiting for the policy's reconciliation summary")
		waitForPolicyStatus(ctx, ns.Name, policyName, policyStatusWait{
			readyStatus:     "True",
			readyReason:     "PolicyValid",
			expectSummary:   true,
			discovered:      1,
			processed:       1,
			reconciledAfter: createdAt,
		})

		By("proving the deployed controller queried the fixture for this target")
		expectFixtureQueried(ctx, series)
	})
})

// createSmokeWorkload creates a Deployment with one container per sample and
// waits until it is Available with a ready pod, so the deterministic fixture
// series can be registered for an actually running pod.
func createSmokeWorkload(ctx context.Context, namespace, prefix string, samples []containerSamples) *appsv1.Deployment {
	name, err := framework.UniqueName("workload")
	Expect(err).NotTo(HaveOccurred(), "generate unique workload name from prefix %q", prefix)
	workload := framework.NewDeploymentFixture(namespace, name, 1, smokeContainers(samples)...)
	Expect(env.Clients.Ctrl.Create(ctx, workload)).To(Succeed(), "create the smoke workload deployment")
	Expect(framework.WaitForDeploymentAvailable(ctx, env.Clients, namespace, name,
		framework.WaitOptions{Timeout: 3 * time.Minute})).To(Succeed(),
		"the smoke workload never became Available")
	pods, err := framework.WaitForPodCountReady(ctx, env.Clients, namespace, "app="+name, 1,
		framework.WaitOptions{Timeout: 3 * time.Minute})
	Expect(err).NotTo(HaveOccurred(), "the smoke workload never had a ready pod")
	Expect(pods).NotTo(BeEmpty(), "no ready pods for the smoke workload")
	return workload
}

// replicaSetNames returns the sorted names of the ReplicaSets matching the
// workload's pod selector; a rollout always adds one.
func replicaSetNames(ctx context.Context, namespace, selector string) []string {
	list, err := env.Clients.Kube.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	Expect(err).NotTo(HaveOccurred(), "list ReplicaSets for selector %s", selector)
	names := make([]string, 0, len(list.Items))
	for _, rs := range list.Items {
		names = append(names, rs.Name)
	}
	sort.Strings(names)
	return names
}
