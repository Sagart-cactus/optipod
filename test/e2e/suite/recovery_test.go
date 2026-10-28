package suite

import (
	"context"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
)

// These scenarios prove the installed controller recovers processing across a
// full pod restart (VAL-SMOKE-004). After an observed recommendation the owned
// controller pod is deleted; the replacement pod — ready, on the same image
// and shipped service account — must then consume changed deterministic
// fixture input through a fresh reconcile and rewrite the workload's
// recommendation annotations. A ready replacement pod, a Ready condition or a
// stale pre-restart annotation alone can never pass: the changed annotation
// values could only have been written by a controller that queried the series
// registered after the restart, and the fixture trace proves those queries.
var _ = Describe("Controller restart recovery", Label("smoke", "failure-recovery"), func() {
	const waitTimeout = 3 * time.Minute

	It("restarts into a fresh ready pod and reconciles changed metrics into a new recommendation", func(ctx context.Context) {
		ns := specNamespace(ctx, "smoke-recovery")
		DeferCleanup(func(ctx context.Context) {
			Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed(),
				"clean up the restart-recovery spec namespace")
		})
		samples := smokeSamples()

		By("creating a two-container workload whose pods become ready")
		workload := createSmokeWorkload(ctx, ns.Name, "smoke-recovery", samples)
		podSelector := "app=" + workload.Name
		workloadPodsBefore := podUIDs(ctx, ns.Name, podSelector)
		registerContainerSeries(ctx, ns.Name, workload, samples)

		By("observing the first recommendation before any restart")
		policyName, err := framework.UniqueName("policy")
		Expect(err).NotTo(HaveOccurred(), "generate unique policy name")
		policy := framework.NewPolicyFixture(ns.Name, policyName, optipodv1alpha1.ModeRecommend,
			framework.WithReconciliationInterval(30*time.Second))
		Expect(env.Clients.Ctrl.Create(ctx, policy)).To(Succeed(), "create the Recommend policy")
		createdAt := policy.CreationTimestamp.Time

		first := waitForDeploymentAnnotation(ctx, ns.Name, workload.Name, recommendationAnnotations(samples))
		expectManagementAnnotations(first, policy, createdAt)
		waitForPolicyStatus(ctx, ns.Name, policyName, policyStatusWait{
			readyStatus:     "True",
			readyReason:     "PolicyValid",
			expectSummary:   true,
			discovered:      1,
			processed:       1,
			reconciledAfter: createdAt,
		})
		// The reference reconcile is the last one the pre-restart controller
		// completed; the post-restart status must be newer than this.
		reference := &optipodv1alpha1.OptimizationPolicy{}
		Expect(env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: policyName}, reference)).
			To(Succeed(), "read the policy for the pre-restart reference reconcile")
		Expect(reference.Status.LastReconciliation).NotTo(BeNil(),
			"the pre-restart controller recorded no completed reconcile to reference")

		By("capturing the installed controller pod identity before the restart")
		beforePods := controllerPods(ctx)
		Expect(beforePods).NotTo(BeEmpty(), "no installed controller pods found to restart")
		beforeUIDs := make(map[types.UID]string, len(beforePods))
		beforeImages := make(map[string]string, len(beforePods))
		for i := range beforePods {
			pod := &beforePods[i]
			beforeUIDs[pod.UID] = pod.Name
			beforeImages[pod.Name] = controllerImage(pod)
			Expect(pod.Spec.ServiceAccountName).To(Equal(framework.ControllerServiceAccountName),
				"the pre-restart controller pod does not use the shipped service account")
		}

		By("restarting the controller by deleting its pod")
		restarted := beforePods[0]
		Expect(env.Clients.Kube.CoreV1().Pods(framework.SystemNamespace).Delete(ctx,
			restarted.Name, metav1.DeleteOptions{})).To(Succeed(),
			"delete the controller pod %s to restart it", restarted.Name)

		By("waiting for a new ready controller pod on the same image and service account")
		fresh := waitForReplacementPod(ctx, framework.SystemNamespace, framework.ControllerPodSelector, beforeUIDs)
		Expect(fresh).NotTo(BeEmpty(), "no replacement controller pod became ready")
		newPod := fresh[0]
		Expect(newPod.Spec.ServiceAccountName).To(Equal(framework.ControllerServiceAccountName),
			"the restarted controller pod does not use the shipped service account")
		Expect(controllerImage(&newPod)).To(Equal(beforeImages[restarted.Name]),
			"the restarted controller pod is not running the same installed image")
		Expect(isReady(&newPod)).To(BeTrue(), "the restarted controller pod is not ready")
		// On an ordinary run the freshly built image is known, so prove the
		// replacement runs exactly this run's build and loaded digest. A
		// focused reuse rerun attaches to the retained install by design and
		// has no fresh build to compare against.
		if env.Installation != nil && env.Installation.ControllerImage != nil {
			Expect(framework.VerifyControllerIdentity(ctx, env.Clients, env.Installation.ControllerImage)).
				To(Succeed(),
					"the restarted controller pod identity does not match the image this run built")
		}
		Expect(framework.WaitForDeploymentAvailable(ctx, env.Clients, framework.SystemNamespace,
			framework.ControllerDeploymentName, framework.WaitOptions{Timeout: waitTimeout})).To(Succeed(),
			"the controller deployment never returned to Available after the restart")

		By("changing the deterministic metric input served to the restarted controller")
		// The trace is cleared before the changed series are registered, so
		// any query that could have consumed the changed values is provably a
		// post-restart, post-change query.
		Expect(env.Fixture.ClearTrace(ctx)).To(Succeed(),
			"reset the fixture trace before registering the changed metric input")
		recovery := recoverySamples()
		recoverySeries := registerContainerSeries(ctx, ns.Name, workload, recovery)

		By("waiting for the changed recommendation from a fresh post-restart reconcile")
		changed := waitForDeploymentAnnotation(ctx, ns.Name, workload.Name, recommendationAnnotations(recovery))
		for key, value := range recommendationAnnotations(samples) {
			Expect(changed.Annotations[key]).NotTo(Equal(value),
				"recommendation annotation %q still carries the stale pre-restart value after changed metric input", key)
		}
		for key, value := range recommendationAnnotations(recovery) {
			Expect(changed.Annotations[key]).To(Equal(value),
				"recommendation annotation %q was not updated to the changed metric value", key)
		}
		beforeRV, err := strconv.ParseInt(first.ResourceVersion, 10, 64)
		Expect(err).NotTo(HaveOccurred(), "parse the pre-restart workload resourceVersion")
		afterRV, err := strconv.ParseInt(changed.ResourceVersion, 10, 64)
		Expect(err).NotTo(HaveOccurred(), "parse the post-restart workload resourceVersion")
		Expect(afterRV).To(BeNumerically(">", beforeRV),
			"the workload resourceVersion did not advance across the recovery reconcile")

		By("proving the restarted controller queried the fixture for the changed input")
		expectFixtureQueried(ctx, recoverySeries)

		By("asserting the policy status shows the fresh post-restart reconciliation")
		waitForPolicyStatus(ctx, ns.Name, policyName, policyStatusWait{
			readyStatus:     "True",
			readyReason:     "PolicyValid",
			expectSummary:   true,
			discovered:      1,
			processed:       1,
			reconciledAfter: reference.Status.LastReconciliation.Time,
		})

		By("asserting Recommend semantics held across the restart: no application, no pod churn")
		for _, s := range samples {
			gotCPU, gotMemory := containerRequests(changed.Spec.Template.Spec.Containers, s.name)
			Expect(gotCPU).NotTo(BeNil(), "container %s lost its CPU request across the restart", s.name)
			Expect(gotCPU.String()).To(Equal(s.initialCPU),
				"the recovery reconcile changed container %s CPU request under Recommend", s.name)
			Expect(gotMemory.String()).To(Equal(s.initialMemory),
				"the recovery reconcile changed container %s memory request under Recommend", s.name)
		}
		Expect(podUIDs(ctx, ns.Name, podSelector)).To(Equal(workloadPodsBefore),
			"the restart and recovery reconciles replaced workload pods under Recommend")
	})
})

// recoverySamples are the changed deterministic inputs registered after the
// controller restart: same container names (the workload is unchanged) but
// values distinct from smokeSamples(), so only a controller that queried the
// changed series can produce them. Like the initial pair they stay inside the
// policy bounds (CPU 50m..4, memory 64Mi..8Gi) and are exact P90 * 1.2
// multiples of the served sample, with no clamping involved.
func recoverySamples() []containerSamples {
	return []containerSamples{
		// app: CPU 0.5 cores -> 500m * 1.2 = 600m; memory 150Mi -> 180Mi.
		{name: "app", cpuCores: 0.5, memoryBytes: 150 << 20,
			initialCPU: "100m", initialMemory: "64Mi",
			expectedCPU: "600m", expectedMemory: "180Mi"},
		// sidecar: CPU 0.25 cores -> 250m * 1.2 = 300m; memory 100Mi -> 120Mi.
		{name: "sidecar", cpuCores: 0.25, memoryBytes: 100 << 20,
			initialCPU: "50m", initialMemory: "32Mi",
			expectedCPU: "300m", expectedMemory: "120Mi"},
	}
}

// controllerPods lists the installed controller pods through the private
// kubeconfig.
func controllerPods(ctx context.Context) []corev1.Pod {
	pods, err := env.Clients.Kube.CoreV1().Pods(framework.SystemNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: framework.ControllerPodSelector,
	})
	Expect(err).NotTo(HaveOccurred(), "list the installed controller pods")
	return pods.Items
}

// controllerImage returns the manager container's image reference of a
// controller pod.
func controllerImage(pod *corev1.Pod) string {
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == framework.ControllerContainerName {
			return pod.Spec.Containers[i].Image
		}
	}
	return ""
}
