package suite

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
)

// This scenario proves older optional-field policies still work against the
// installed controller (VAL-CONF-014): the installed CRD accepts an otherwise
// valid policy that omits the newer optional weight, workload-type, strategy,
// rollout, limit and gradual-decrease settings, the defaults it persists are
// read back through the API (never inferred from an uninitialized Go struct),
// and the observed workload behavior follows those persisted production
// defaults:
//
//   - Recommend on the legacy policy writes the exact per-container
//     recommendation annotations with no template/pod disturbance, leaving
//     the workload's pre-existing user annotations and requests untouched.
//   - Flipping the same legacy policy to Auto applies through the persisted
//     default route (webhook strategy, onNextRestart rollout): the pod template
//     and original pod stay untouched with no restart marker, a naturally
//     admitted new pod carries every container's recommended requests with no
//     limits (the persisted updateRequestsOnly=true default), and the
//     pre-existing recommendation annotations and user annotations survive.
//
// The omitted workload-type filter defaults to all three kinds, proven by the
// persisted policy selecting the Deployment (discovered/processed through the
// policy status with per-kind counts) rather than by a stored field alone.
var _ = Describe("Older optional-field policies still work", Label("conformance", "compatibility"), func() {
	const waitTimeout = 3 * time.Minute

	It("accepts a policy omitting newer optional fields and follows persisted production defaults on workloads", func(ctx context.Context) {
		ns := specNamespace(ctx, "conf-compat")
		DeferCleanup(func(ctx context.Context) {
			Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed(),
				"clean up the compatibility spec namespace")
		})
		samples := smokeSamples()

		By("creating a two-container webhook-eligible workload carrying pre-existing user annotations")
		workloadName, err := framework.UniqueName("workload")
		Expect(err).NotTo(HaveOccurred(), "generate unique compatibility workload name")
		workload := framework.NewDeploymentFixture(ns.Name, workloadName, 1, smokeWebhookContainers(samples)...)
		userAnnotations := map[string]string{
			"docs.example.com/owner":   "platform-team",
			"docs.example.com/runbook": "https://example.com/runbooks/optipod-compat",
		}
		workload.Annotations = userAnnotations
		Expect(env.Clients.Ctrl.Create(ctx, workload)).To(Succeed(), "create the compatibility workload")
		Expect(framework.WaitForDeploymentAvailable(ctx, env.Clients, ns.Name, workloadName,
			framework.WaitOptions{Timeout: waitTimeout})).To(Succeed(),
			"the compatibility workload never became Available")
		podSelector := "app=" + workloadName
		beforeUIDs := podUIDs(ctx, ns.Name, podSelector)

		By("registering deterministic metric samples for the workload's pod")
		registerContainerSeries(ctx, ns.Name, workload, samples)

		By("creating a policy that omits every newer optional field")
		policyName, err := framework.UniqueName("policy")
		Expect(err).NotTo(HaveOccurred(), "generate unique compatibility policy name")
		// The legacy CR is created from its raw manifest shape, not the typed
		// Go struct: encoding/json never omits a zero struct field, so a typed
		// create would serialize rollingWindow as an explicit "0s" and
		// server-side CRD defaulting would never apply. A legacy user manifest
		// simply lacks those keys.
		policy := legacyPolicyManifest(ns.Name, policyName, optipodv1alpha1.ModeRecommend)
		Expect(env.Clients.Ctrl.Create(ctx, policy)).To(Succeed(),
			"the installed CRD must accept a valid policy omitting newer optional fields")
		createdAt := time.Now()

		By("reading the persisted defaults back through the API")
		persisted := &optipodv1alpha1.OptimizationPolicy{}
		Expect(env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: policyName}, persisted)).
			To(Succeed(), "read the persisted legacy policy")
		Expect(persisted.Spec.Weight).NotTo(BeNil(),
			"the omitted weight was not defaulted in the persisted policy")
		Expect(*persisted.Spec.Weight).To(Equal(int32(100)),
			"the persisted default weight is %d, want 100", *persisted.Spec.Weight)
		Expect(persisted.Spec.UpdateStrategy.Strategy).NotTo(BeNil(),
			"the omitted strategy was not defaulted in the persisted policy")
		Expect(*persisted.Spec.UpdateStrategy.Strategy).To(Equal(string(optipodv1alpha1.StrategyWebhook)),
			"the persisted default strategy is %q, want webhook", *persisted.Spec.UpdateStrategy.Strategy)
		Expect(persisted.Spec.UpdateStrategy.RolloutStrategy).NotTo(BeNil(),
			"the omitted rollout strategy was not defaulted in the persisted policy")
		Expect(*persisted.Spec.UpdateStrategy.RolloutStrategy).To(Equal(string(optipodv1alpha1.RolloutOnNextRestart)),
			"the persisted default rollout strategy is %q, want onNextRestart", *persisted.Spec.UpdateStrategy.RolloutStrategy)
		Expect(persisted.Spec.UpdateStrategy.UpdateRequestsOnly).To(BeTrue(),
			"the persisted updateRequestsOnly default is %v, want true", persisted.Spec.UpdateStrategy.UpdateRequestsOnly)
		Expect(persisted.Spec.MetricsConfig.Percentile).To(Equal("P90"),
			"the persisted default percentile is %q, want P90", persisted.Spec.MetricsConfig.Percentile)
		Expect(persisted.Spec.MetricsConfig.SafetyFactor).NotTo(BeNil(),
			"the omitted safety factor was not defaulted in the persisted policy")
		Expect(*persisted.Spec.MetricsConfig.SafetyFactor).To(Equal(1.2),
			"the persisted default safety factor is %v, want 1.2", *persisted.Spec.MetricsConfig.SafetyFactor)
		Expect(persisted.Spec.MetricsConfig.RollingWindow.Duration).To(Equal(24*time.Hour),
			"the persisted default rolling window is %s, want 24h", persisted.Spec.MetricsConfig.RollingWindow.Duration)
		Expect(persisted.Spec.Selector.WorkloadTypes).To(BeNil(),
			"the omitted workload-type filter must remain absent so all kinds stay eligible")
		Expect(persisted.Spec.UpdateStrategy.LimitConfig).To(BeNil(),
			"the omitted limit configuration must remain absent (requests-only defaults)")
		Expect(persisted.Spec.UpdateStrategy.GradualDecreaseConfig).To(BeNil(),
			"the omitted gradual-decrease configuration must remain absent (not implemented in production)")
		if persisted.Spec.UpdateStrategy.AllowUnsafeMemoryDecrease != nil {
			Expect(*persisted.Spec.UpdateStrategy.AllowUnsafeMemoryDecrease).To(BeFalse(),
				"the persisted allowUnsafeMemoryDecrease default must not enable anything (no production guard reads it)")
		}

		By("waiting for the legacy Recommend policy to write exact recommendations")
		annotated := waitForDeploymentAnnotation(ctx, ns.Name, workloadName, recommendationAnnotations(samples))
		expectManagementAnnotations(annotated, persisted, persisted.CreationTimestamp.Time)
		expectUnappliedContainerRequests(annotated.Spec.Template.Spec.Containers, samples,
			"the workload under the legacy Recommend policy")
		for _, s := range samples {
			cpuLimit, memLimit := containerLimits(annotated.Spec.Template.Spec.Containers, s.name)
			Expect(cpuLimit).To(BeNil(), "the legacy Recommend policy added a CPU limit to container %s", s.name)
			Expect(memLimit).To(BeNil(), "the legacy Recommend policy added a memory limit to container %s", s.name)
		}
		for key, value := range userAnnotations {
			Expect(annotated.Annotations[key]).To(Equal(value),
				"the legacy policy disturbed the pre-existing user annotation %q", key)
		}
		expectNoTemplateRestartMarker(ctx, ns.Name, workloadName,
			"the legacy Recommend policy")
		Expect(podUIDs(ctx, ns.Name, podSelector)).To(Equal(beforeUIDs),
			"the legacy Recommend policy replaced workload pods")

		By("observing the persisted defaults selecting and processing the workload")
		waitForPolicyStatus(ctx, ns.Name, policyName, policyStatusWait{
			readyStatus:     "True",
			readyReason:     "PolicyValid",
			expectSummary:   true,
			discovered:      1,
			processed:       1,
			reconciledAfter: createdAt,
		})
		reconciled := fetchPolicy(ctx, ns.Name, policyName)
		expectWorkloadTypeCounts(reconciled, 1, 0, 0)

		By("waiting out the recommendation-annotation throttle window before the mode flip")
		waitForAnnotationThrottleWindow(ctx, ns.Name, workloadName)

		By("flipping the same legacy policy to Auto with every optional field still omitted")
		updatePolicy(ctx, ns.Name, policyName, func(p *optipodv1alpha1.OptimizationPolicy) {
			p.Spec.Mode = optipodv1alpha1.ModeAuto
		})

		By("waiting for the persisted webhook-strategy apply annotations")
		want := recommendationAnnotations(samples)
		for key, value := range webhookApplyAnnotations(samples) {
			want[key] = value
		}
		applied := waitForDeploymentAnnotation(ctx, ns.Name, workloadName, want)

		By("asserting the persisted onNextRestart default left template and existing pod untouched")
		expectUnappliedContainerRequests(applied.Spec.Template.Spec.Containers, samples,
			"the pod template after the legacy Auto policy")
		expectNoTemplateRestartMarker(ctx, ns.Name, workloadName,
			"the legacy Auto policy under the persisted onNextRestart default")
		for key, value := range userAnnotations {
			Expect(applied.Annotations[key]).To(Equal(value),
				"the legacy Auto policy disturbed the pre-existing user annotation %q", key)
		}
		beforePods, err := env.Clients.Kube.CoreV1().Pods(ns.Name).List(ctx,
			metav1.ListOptions{LabelSelector: podSelector})
		Expect(err).NotTo(HaveOccurred(), "list the workload's pods before scaling")
		Expect(beforePods.Items).To(HaveLen(1), "the workload must still have exactly its original pod")
		expectUnappliedContainerRequests(beforePods.Items[0].Spec.Containers, samples,
			"the original pod under the persisted onNextRestart default")
		Expect(applied.Annotations["deployment.kubernetes.io/revision"]).
			To(Equal(annotated.Annotations["deployment.kubernetes.io/revision"]),
				"the legacy Auto policy under the webhook strategy changed the Deployment revision")

		By("scaling the workload so the Deployment controller admits a fresh pod naturally")
		scaleDeployment(ctx, ns.Name, workloadName, 2)
		Expect(framework.WaitForDeploymentAvailable(ctx, env.Clients, ns.Name, workloadName,
			framework.WaitOptions{Timeout: waitTimeout})).To(Succeed(),
			"the scaled workload never returned to Available")
		scaledPods, err := framework.WaitForPodCountReady(ctx, env.Clients, ns.Name, podSelector, 2,
			framework.WaitOptions{Timeout: waitTimeout})
		Expect(err).NotTo(HaveOccurred(), "the scaled workload never had two ready pods")

		By("asserting the newly admitted pod follows the persisted defaults exactly")
		var fresh, keptPod *corev1.Pod
		for i := range scaledPods {
			if _, existed := beforeUIDs[scaledPods[i].UID]; existed {
				keptPod = &scaledPods[i]
				continue
			}
			fresh = &scaledPods[i]
		}
		Expect(fresh).NotTo(BeNil(), "no newly admitted pod appeared after scaling")
		Expect(keptPod).NotTo(BeNil(), "scaling must not have replaced the original pod")
		expectAppliedContainerRequests(fresh.Spec.Containers, samples, "the pod admitted under the legacy policy's persisted defaults")
		expectUnappliedContainerRequests(keptPod.Spec.Containers, samples,
			"the pre-existing pod under the persisted onNextRestart default")

		By("registering deterministic samples for the new pod and proving a steady repeat reconcile")
		current := &appsv1.Deployment{}
		Expect(env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: workloadName}, current)).
			To(Succeed(), "read the scaled workload for series registration")
		registerContainerSeries(ctx, ns.Name, current, samples)
		steady := captureSteadyState(ctx, ns.Name, workloadName, podSelector)
		reference := fetchPolicy(ctx, ns.Name, policyName)
		Expect(reference.Status.LastReconciliation).NotTo(BeNil(),
			"the legacy Auto policy records no completed reconcile to reference")
		repeat := waitForNextReconcile(ctx, ns.Name, policyName, reference.Status.LastReconciliation.Time)
		Expect(repeat.Status.WorkloadsProcessed).To(Equal(1),
			"the completed legacy-policy reconcile did not process the workload again")
		expectSteadyWorkload(ctx, steady, ns.Name, workloadName, podSelector,
			"the completed legacy-policy reconcile")

		By("re-asserting the pre-existing annotations survive the completed reconcile")
		final := &appsv1.Deployment{}
		Expect(env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: workloadName}, final)).
			To(Succeed(), "read the workload after the repeat reconcile")
		for key, value := range want {
			Expect(final.Annotations[key]).To(Equal(value),
				"after the completed reconcile, stored annotation %q no longer carries the exact value", key)
		}
		for key, value := range userAnnotations {
			Expect(final.Annotations[key]).To(Equal(value),
				"the completed legacy-policy reconcile disturbed the pre-existing user annotation %q", key)
		}
	})
})

// legacyPolicyManifest builds a legacy policy as its raw manifest shape — the
// wire form of a CR written before the newer optional fields existed: no
// weight, no workload-type filter, no strategy, no rollout strategy, no limit
// configuration, no safety-factor, percentile or rolling-window keys at all,
// and no gradual-decrease configuration. Only the schema-required fields plus
// the harness's bounded reconciliation interval are present, so the installed
// CRD's server-side defaults are what persist and execute. It must stay
// unstructured: a typed Go struct carries zero-value struct fields (notably
// rollingWindow) onto the wire explicitly, which suppresses CRD defaulting.
func legacyPolicyManifest(namespace, name string, mode optipodv1alpha1.PolicyMode) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion("optipod.optipod.io/v1alpha1")
	u.SetKind("OptimizationPolicy")
	u.SetNamespace(namespace)
	u.SetName(name)
	u.Object["spec"] = map[string]interface{}{
		"mode": string(mode),
		"selector": map[string]interface{}{
			"namespaces": map[string]interface{}{
				"allow": []interface{}{namespace},
			},
		},
		"metricsConfig": map[string]interface{}{
			"provider": "prometheus",
		},
		"resourceBounds": map[string]interface{}{
			"cpu":    map[string]interface{}{"min": "50m", "max": "4"},
			"memory": map[string]interface{}{"min": "64Mi", "max": "8Gi"},
		},
		// updateStrategy is schema-required as a field, but every setting
		// inside it is optional and omitted here.
		"updateStrategy": map[string]interface{}{},
		// A bounded interval keeps the spec's reconciles observable; every
		// field under test stays omitted.
		"reconciliationInterval": "30s",
	}
	return u
}
