package suite

import (
	"context"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
)

// These scenarios prove the installed cluster's API admission and the deployed
// controller's reconciliation status for OptimizationPolicy objects (VAL-SMOKE-001):
// a minimal valid policy is admitted through the real API and reconciled to a
// fresh Ready status; malformed schema values are rejected by the API server
// with actionable causes; and a value the CRD schema admits but the runtime
// validator rejects yields a clear invalid status without mutating a workload.
// None of this is inferred from calling the local validators.
var _ = Describe("Policy API admission and reconciliation readiness", Label("smoke", "install", "policy"), func() {
	const waitTimeout = 3 * time.Minute

	It("admits a minimal valid policy and reconciles it to a Ready status", func(ctx context.Context) {
		ns := specNamespace(ctx, "smoke-policy-valid")
		DeferCleanup(func(ctx context.Context) {
			Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed(),
				"clean up the valid-policy spec namespace")
		})

		name, err := framework.UniqueName("policy")
		Expect(err).NotTo(HaveOccurred(), "generate unique policy name")
		policy := framework.NewPolicyFixture(ns.Name, name, optipodv1alpha1.ModeRecommend)
		Expect(env.Clients.Ctrl.Create(ctx, policy)).To(Succeed(),
			"the API server must admit the minimal valid policy")

		By("reading the persisted policy back through the private-config API")
		created := &optipodv1alpha1.OptimizationPolicy{}
		Expect(env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: name}, created)).To(Succeed())
		Expect(created.UID).NotTo(BeEmpty(), "persisted policy has no API-assigned UID")
		Expect(created.ResourceVersion).NotTo(BeEmpty(), "persisted policy has no resourceVersion")
		Expect(created.Generation).To(Equal(int64(1)), "a freshly created policy must be generation 1")
		Expect(created.Spec.Mode).To(Equal(optipodv1alpha1.ModeRecommend), "spec.mode was not persisted")
		createdRV := created.ResourceVersion
		createdAt := created.CreationTimestamp.Time

		By("waiting for the deployed controller to reconcile the policy")
		observed := waitForPolicyStatus(ctx, ns.Name, name, policyStatusWait{
			readyStatus:     "True",
			readyReason:     "PolicyValid",
			messageContains: "Policy is active and processing workloads",
			expectSummary:   true,
			discovered:      0,
			processed:       0,
			reconciledAfter: createdAt,
		})

		ready := meta.FindStatusCondition(observed.Status.Conditions, "Ready")
		Expect(ready).NotTo(BeNil(), "the Ready condition vanished after it was observed")
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))
		Expect(ready.Reason).To(Equal("PolicyValid"))
		Expect(observed.Status.LastReconciliation).NotTo(BeNil(),
			"the reconciliation summary never appeared")
		Expect(observed.ResourceVersion).NotTo(Equal(createdRV),
			"the status update never advanced the resourceVersion")

		By("asserting the fresh namespace contains no workload the policy could act on")
		deps, err := env.Clients.Kube.AppsV1().Deployments(ns.Name).List(ctx, metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred(), "list deployments in the fresh policy namespace")
		Expect(deps.Items).To(BeEmpty(),
			"a policy-only namespace must not have workloads for the summary to be honest")
	})

	It("rejects malformed schema values with an actionable API cause", func(ctx context.Context) {
		ns := specNamespace(ctx, "smoke-policy-malformed")
		DeferCleanup(func(ctx context.Context) {
			Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed(),
				"clean up the malformed-policy spec namespace")
		})

		By("submitting a policy with an unsupported mode value")
		name, err := framework.UniqueName("policy")
		Expect(err).NotTo(HaveOccurred(), "generate unique policy name")
		badMode := framework.NewPolicyFixture(ns.Name, name, optipodv1alpha1.ModeRecommend)
		badMode.Spec.Mode = optipodv1alpha1.PolicyMode("Bogus")
		err = env.Clients.Ctrl.Create(ctx, badMode)
		Expect(err).To(HaveOccurred(), "the API server must reject an unsupported mode value")
		Expect(apierrors.IsInvalid(err)).To(BeTrue(),
			"an enum violation must be an Invalid (422) API error, got: %v", err)
		Expect(strings.Contains(err.Error(), "spec.mode")).To(BeTrue(),
			"the rejection must name the offending field, got: %v", err)
		Expect(strings.Contains(err.Error(), "Bogus")).To(BeTrue(),
			"the rejection must name the unsupported value, got: %v", err)

		By("submitting a policy with a weight below the schema minimum")
		badWeight := framework.NewPolicyFixture(ns.Name, name, optipodv1alpha1.ModeRecommend,
			framework.WithWeight(0))
		err = env.Clients.Ctrl.Create(ctx, badWeight)
		Expect(err).To(HaveOccurred(), "the API server must reject an out-of-range weight")
		Expect(apierrors.IsInvalid(err)).To(BeTrue(),
			"a weight below the schema minimum must be an Invalid (422) API error, got: %v", err)
		Expect(strings.Contains(err.Error(), "spec.weight")).To(BeTrue(),
			"the rejection must name the offending field, got: %v", err)

		By("asserting neither malformed policy was persisted")
		policies := &optipodv1alpha1.OptimizationPolicyList{}
		Expect(env.Clients.Ctrl.List(ctx, policies)).To(Succeed())
		for _, p := range policies.Items {
			Expect(p.Name).NotTo(Equal(name),
				"the API server persisted a policy it reported as invalid")
		}
	})

	It("reports a runtime-invalid policy clearly and never mutates its workloads", func(ctx context.Context) {
		ns := specNamespace(ctx, "smoke-policy-runtime-invalid")
		DeferCleanup(func(ctx context.Context) {
			Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed(),
				"clean up the runtime-invalid-policy spec namespace")
		})

		By("creating a workload the invalid policy would have selected")
		workloadName, err := framework.UniqueName("workload")
		Expect(err).NotTo(HaveOccurred(), "generate unique workload name")
		workload := framework.NewDeploymentFixture(ns.Name, workloadName, 1,
			framework.ContainerFixture{Name: "app", CPURequest: "100m", MemoryRequest: "64Mi"})
		Expect(env.Clients.Ctrl.Create(ctx, workload)).To(Succeed(), "create the guard workload")
		Expect(framework.WaitForDeploymentAvailable(ctx, env.Clients, ns.Name, workloadName,
			framework.WaitOptions{Timeout: waitTimeout})).To(Succeed(),
			"the guard workload never became Available")
		podSelector := "app=" + workloadName
		beforeUIDs := podUIDs(ctx, ns.Name, podSelector)
		Expect(beforeUIDs).NotTo(BeEmpty(), "the guard workload has no pods")

		By("creating a policy the CRD schema admits but the runtime validator rejects")
		policyName, err := framework.UniqueName("policy")
		Expect(err).NotTo(HaveOccurred(), "generate unique policy name")
		policy := framework.NewPolicyFixture(ns.Name, policyName, optipodv1alpha1.ModeAuto,
			// CPU min above max: both are schema-valid quantities, so the CRD
			// admits the object; only the deployed runtime validator rejects it.
			framework.WithResourceBounds("4", "1", "64Mi", "8Gi"),
			framework.WithReconciliationInterval(30*time.Second),
		)
		Expect(env.Clients.Ctrl.Create(ctx, policy)).To(Succeed(),
			"the CRD schema must admit the bounds-inverted policy")

		By("waiting for the deployed controller to mark the policy invalid")
		observed := waitForPolicyStatus(ctx, ns.Name, policyName, policyStatusWait{
			readyStatus:     "False",
			readyReason:     "ValidationFailed",
			messageContains: "CPU min",
			expectSummary:   false,
		})

		ready := meta.FindStatusCondition(observed.Status.Conditions, "Ready")
		Expect(ready).NotTo(BeNil(), "the Ready condition vanished after it was observed")
		Expect(ready.Message).To(ContainSubstring("must be less than or equal to"),
			"the invalid status must carry the actionable validation cause")
		Expect(observed.Status.LastReconciliation).To(BeNil(),
			"a validation-failed policy must never run a workload processing pass")

		By("asserting the selected workload was never touched")
		guard := &appsv1.Deployment{}
		Expect(env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: workloadName}, guard)).To(Succeed())
		for key := range guard.Annotations {
			Expect(strings.HasPrefix(key, "optipod.io/")).To(BeFalse(),
				"the invalid policy mutated the workload via annotation %q", key)
		}
		cpu, memory := containerRequests(guard.Spec.Template.Spec.Containers, "app")
		Expect(cpu).NotTo(BeNil(), "the guard workload lost its CPU request")
		Expect(cpu.String()).To(Equal("100m"), "the invalid policy changed the CPU request")
		Expect(memory.String()).To(Equal("64Mi"), "the invalid policy changed the memory request")
		cpuLimit, memLimit := containerLimits(guard.Spec.Template.Spec.Containers, "app")
		Expect(cpuLimit).To(BeNil(), "the invalid policy added a CPU limit")
		Expect(memLimit).To(BeNil(), "the invalid policy added a memory limit")
		Expect(guard.Generation).To(Equal(int64(1)),
			"a validation-failed policy must not bump the workload generation")

		By("asserting the guard workload never restarted")
		Expect(podUIDs(ctx, ns.Name, podSelector)).To(Equal(beforeUIDs),
			"the guard workload's pod identity changed under an invalid policy")
	})
})
