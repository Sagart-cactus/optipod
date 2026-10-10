package suite

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
)

// This scenario proves the deployed controller's default-strategy Auto path on
// naturally generated pods (VAL-SMOKE-005): with the update strategy left
// unset, the production default (webhook strategy, onNextRestart rollout)
// stores every container's recommendations as workload annotations and the
// installed mutating webhook then mutates EVERY eligible container on each
// pod the Deployment controller admits through the real API — existing pods
// keep their previous values until naturally recreated. The pod template
// carries the production optipod.io/webhook-enabled=true label before
// creation, so this is a natural rollout, not a hand-labelled admission
// probe.
//
// The mutator's parent-annotation lookup (pod -> ReplicaSet -> Deployment,
// internal/webhook/mutator.go getParentDeploymentAnnotations, for ArgoCD
// compatibility) needs the controller's service account to read ReplicaSets.
// The shipped Helm chart grants that (charts/optipod/templates/rbac.yaml) and
// config/rbac/role.yaml now mirrors it (VAL-FIX-003), so a config/-installed
// controller mutates every eligible container on newly admitted Deployment
// pods. This scenario asserts both the installed ClusterRole grant and the
// live mutation, so a drift between the two install paths cannot pass
// silently.
var _ = Describe("Default webhook strategy mutates every container on naturally admitted pods", Label("smoke", "modes"), func() {
	const waitTimeout = 3 * time.Minute

	It("mutates every eligible container on newly admitted pods under the default webhook strategy", Label("auto", "webhook"), func(ctx context.Context) {
		ns := specNamespace(ctx, "smoke-auto-webhook")
		DeferCleanup(func(ctx context.Context) {
			Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed(),
				"clean up the Auto webhook-strategy spec namespace")
		})
		samples := smokeSamples()

		By("proving the installed controller ClusterRole grants the mutator's parent-workload lookup")
		// The mutator runs in-process under the manager's service account, so
		// the shipped manager ClusterRole is what authorizes the ReplicaSet
		// read. Without the apps replicasets grant a config/-installed
		// controller resolves no parent annotations and admits new Deployment
		// pods unmutated (VAL-FIX-003).
		expectInstalledClusterRoleGrant(ctx)

		By("creating a two-container webhook-eligible workload whose pod becomes ready")
		// smokeWebhookContainers sets the production optipod.io/webhook-enabled=true
		// pod-template label BEFORE creation, so every pod the Deployment
		// controller generates is routed to the installed mutating webhook: a
		// natural rollout, not a hand-labelled probe.
		workloadName, err := framework.UniqueName("workload")
		Expect(err).NotTo(HaveOccurred(), "generate unique webhook workload name")
		workload := framework.NewDeploymentFixture(ns.Name, workloadName, 1, smokeWebhookContainers(samples)...)
		Expect(env.Clients.Ctrl.Create(ctx, workload)).To(Succeed(), "create the webhook-strategy workload")
		Expect(framework.WaitForDeploymentAvailable(ctx, env.Clients, ns.Name, workloadName,
			framework.WaitOptions{Timeout: waitTimeout})).To(Succeed(),
			"the webhook-strategy workload never became Available")
		podSelector := "app=" + workloadName
		beforeUIDs := podUIDs(ctx, ns.Name, podSelector)

		By("registering deterministic metric samples for the workload's pod")
		series := registerContainerSeries(ctx, ns.Name, workload, samples)
		Expect(env.Fixture.ClearTrace(ctx)).To(Succeed(), "reset the fixture trace for this spec")

		By("creating an Auto policy with the update strategy left unset (production default)")
		// WithDefaultStrategy clears the fixture's strategy pointer, so the
		// deployed controller's own defaults (webhook strategy, onNextRestart
		// rollout) are what actually execute.
		policyName, err := framework.UniqueName("policy")
		Expect(err).NotTo(HaveOccurred(), "generate unique policy name")
		policy := framework.NewPolicyFixture(ns.Name, policyName, optipodv1alpha1.ModeAuto,
			framework.WithDefaultStrategy(),
			framework.WithReconciliationInterval(30*time.Second))
		Expect(env.Clients.Ctrl.Create(ctx, policy)).To(Succeed(), "create the default-strategy Auto policy")
		createdAt := policy.CreationTimestamp.Time

		By("waiting for every container's recommendation and webhook annotations on the workload")
		want := recommendationAnnotations(samples)
		for key, value := range webhookApplyAnnotations(samples) {
			want[key] = value
		}
		annotated := waitForDeploymentAnnotation(ctx, ns.Name, workloadName, want)
		expectManagementAnnotations(annotated, policy, createdAt)

		By("asserting the default onNextRestart rollout left the pod template and existing pod untouched")
		expectUnappliedContainerRequests(annotated.Spec.Template.Spec.Containers, samples,
			"the pod template before any restart")
		beforePods, err := env.Clients.Kube.CoreV1().Pods(ns.Name).List(ctx,
			metav1.ListOptions{LabelSelector: podSelector})
		Expect(err).NotTo(HaveOccurred(), "list the workload's pods before scaling")
		Expect(beforePods.Items).To(HaveLen(1), "the workload must still have exactly its original pod")
		expectUnappliedContainerRequests(beforePods.Items[0].Spec.Containers, samples,
			"the original pod before any restart")

		By("scaling the workload so the Deployment controller admits a fresh pod naturally")
		scaleDeployment(ctx, ns.Name, workloadName, 2)
		Expect(framework.WaitForDeploymentAvailable(ctx, env.Clients, ns.Name, workloadName,
			framework.WaitOptions{Timeout: waitTimeout})).To(Succeed(),
			"the scaled workload never returned to Available")
		scaledPods, err := framework.WaitForPodCountReady(ctx, env.Clients, ns.Name, podSelector, 2,
			framework.WaitOptions{Timeout: waitTimeout})
		Expect(err).NotTo(HaveOccurred(), "the scaled workload never had two ready pods")

		By("asserting the newly admitted pod carries every eligible container's applied values")
		var fresh *corev1.Pod
		var kept []corev1.Pod
		for i := range scaledPods {
			if _, existed := beforeUIDs[scaledPods[i].UID]; existed {
				kept = append(kept, scaledPods[i])
				continue
			}
			fresh = &scaledPods[i]
		}
		Expect(fresh).NotTo(BeNil(), "no newly admitted pod appeared after scaling")
		Expect(kept).To(HaveLen(1), "scaling must not have replaced the original pod")
		expectAppliedContainerRequests(fresh.Spec.Containers, samples, "the newly admitted pod")
		expectUnappliedContainerRequests(kept[0].Spec.Containers, samples,
			"the pre-existing pod under onNextRestart rollout")

		By("proving the parent-workload lookup succeeded with no forbidden ReplicaSets access")
		// The fresh pod was mutated, so the mutator's pod -> ReplicaSet ->
		// Deployment lookup ran and was authorized; the controller must not
		// have logged the missing-access failure the drift used to produce.
		expectNoForbiddenParentLookup(ctx)

		By("registering deterministic samples for the new pod too, so later reconciles stay deterministic")
		current := &appsv1.Deployment{}
		Expect(env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: workloadName}, current)).
			To(Succeed(), "read the scaled workload for series registration")
		registerContainerSeries(ctx, ns.Name, current, samples)

		By("capturing the steady state and waiting for one more completed reconcile")
		steady := captureSteadyState(ctx, ns.Name, workloadName, podSelector)
		reference := &optipodv1alpha1.OptimizationPolicy{}
		Expect(env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: policyName}, reference)).
			To(Succeed(), "read the policy for the reference reconcile")
		Expect(reference.Status.LastReconciliation).NotTo(BeNil(),
			"the applied policy records no completed reconcile to reference")
		reconciled := waitForNextReconcile(ctx, ns.Name, policyName,
			reference.Status.LastReconciliation.Time)
		Expect(reconciled.Status.WorkloadsProcessed).To(Equal(1),
			"the completed webhook-strategy reconcile did not process the workload again")

		By("asserting the completed reconcile kept every container's stored annotations with no churn")
		expectSteadyWorkload(ctx, steady, ns.Name, workloadName, podSelector,
			"the completed webhook-strategy reconcile")
		reannotated := &appsv1.Deployment{}
		Expect(env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: workloadName}, reannotated)).
			To(Succeed(), "read the re-annotated workload")
		for key, value := range want {
			Expect(reannotated.Annotations[key]).To(Equal(value),
				"after the completed reconcile, stored annotation %q no longer carries the exact value", key)
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

		By("proving the deployed controller queried the fixture for these targets")
		expectFixtureQueried(ctx, series)
	})
})

// expectInstalledClusterRoleGrant proves the installed (rendered config/default)
// controller ClusterRole grants the parent-workload read the production pod
// mutator needs and the shipped Helm chart ships: the mutator resolves a
// Deployment-owned pod to its parent Deployment through pod -> ReplicaSet ->
// Deployment (internal/webhook/mutator.go) using the manager's cached client,
// so its first ReplicaSet Get needs list/watch on replicasets.apps. Without it
// a config/-installed controller admits new Deployment pods unmutated
// (VAL-FIX-003).
func expectInstalledClusterRoleGrant(ctx context.Context) {
	role, err := env.Clients.Kube.RbacV1().ClusterRoles().
		Get(ctx, framework.ControllerClusterRoleName, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred(), "read the installed controller ClusterRole %s",
		framework.ControllerClusterRoleName)
	var verbs []string
	for _, rule := range role.Rules {
		if slices.Contains(rule.APIGroups, "apps") && slices.Contains(rule.Resources, "replicasets") {
			verbs = rule.Verbs
		}
	}
	Expect(verbs).NotTo(BeEmpty(),
		"the installed ClusterRole %s apps rule does not grant replicasets; a config/-installed controller cannot resolve a Deployment pod to its parent workload and admits new pods unmutated",
		framework.ControllerClusterRoleName)
	Expect(verbs).To(ContainElements("get", "list", "watch"),
		"the installed ClusterRole %s grants replicasets %v, want at least get/list/watch to mirror the shipped chart",
		framework.ControllerClusterRoleName, verbs)
}

// expectNoForbiddenParentLookup proves the deployed controller never logged the
// missing-access failure the parent lookup produces without the ClusterRole
// grant. The newly admitted pod was already observed mutated, so the lookup
// ran; any forbidden ReplicaSets record here would contradict that grant.
func expectNoForbiddenParentLookup(ctx context.Context) {
	logs, err := controllerLogs(ctx)
	Expect(err).NotTo(HaveOccurred(), "read the deployed controller logs for parent-lookup evidence")
	for _, line := range strings.Split(logs, "\n") {
		if strings.Contains(line, "forbidden") && strings.Contains(line, "replicasets") {
			Fail(fmt.Sprintf("the deployed controller logged a forbidden ReplicaSets request while resolving the parent workload: %s", line))
		}
	}
}

// smokeWebhookContainers builds the workload container fixtures matching
// samples with the production optipod.io/webhook-enabled=true pod-template
// label set, so pods the workload controller generates naturally are routed
// to the installed mutating webhook (natural rollout, not hand-labelled
// probes).
func smokeWebhookContainers(samples []containerSamples) []framework.ContainerFixture {
	containers := smokeContainers(samples)
	for i := range containers {
		containers[i].WebhookEnabled = true
	}
	return containers
}

// webhookApplyAnnotations builds the exact per-container webhook annotation
// family the default webhook strategy must store on the workload: the
// production optipod.io/{cpu,memory}-request.<container> keys the deployed pod
// mutator reads, plus the webhook-enabled and strategy markers that gate
// mutation, for every sampled container.
func webhookApplyAnnotations(samples []containerSamples) map[string]string {
	annotations := map[string]string{
		optipodv1alpha1.AnnotationWebhookEnabled: "true",
		optipodv1alpha1.AnnotationStrategy:       string(optipodv1alpha1.StrategyWebhook),
	}
	for _, s := range samples {
		annotations[fmt.Sprintf("%s.%s", optipodv1alpha1.AnnotationCPURequestPrefix, s.name)] = s.expectedCPU
		annotations[fmt.Sprintf("%s.%s", optipodv1alpha1.AnnotationMemoryRequestPrefix, s.name)] = s.expectedMemory
	}
	return annotations
}

// scaleDeployment applies a user-style replica-count update through the typed
// client, so the Deployment controller creates the new pods naturally (real
// API admission, no hand-built pods).
func scaleDeployment(ctx context.Context, namespace, name string, replicas int32) {
	dep := &appsv1.Deployment{}
	Expect(env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, dep)).
		To(Succeed(), "read workload %s/%s before scaling", namespace, name)
	dep.Spec.Replicas = &replicas
	_, err := env.Clients.Kube.AppsV1().Deployments(namespace).Update(ctx, dep, metav1.UpdateOptions{})
	Expect(err).NotTo(HaveOccurred(), "scale workload %s/%s to %d replicas", namespace, name, replicas)
}

// expectUnappliedContainerRequests asserts every sampled container of a live
// pod still carries exactly its original requests: an onNextRestart workload
// must leave existing pods untouched until they are naturally recreated.
func expectUnappliedContainerRequests(containers []corev1.Container, samples []containerSamples, what string) {
	for _, s := range samples {
		cpu, memory := containerRequests(containers, s.name)
		Expect(cpu).NotTo(BeNil(), "%s lost container %s", what, s.name)
		Expect(memory).NotTo(BeNil(), "%s lost container %s memory request", what, s.name)
		Expect(cpu.String()).To(Equal(s.initialCPU),
			"%s: container %s CPU request changed from %s", what, s.name, s.initialCPU)
		Expect(memory.String()).To(Equal(s.initialMemory),
			"%s: container %s memory request changed from %s", what, s.name, s.initialMemory)
	}
}
