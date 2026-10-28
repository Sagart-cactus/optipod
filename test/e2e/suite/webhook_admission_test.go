package suite

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionregv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
)

// These scenarios prove live TLS admission through the installed components
// (VAL-SMOKE-003): an eligible pod created through the real API is actually
// mutated at creation to its expected per-container resources, while ineligible
// pods are admitted unchanged. Mutations are only observable if the API server
// reached the deployed webhook over its TLS service with the harness CA, so a
// simulator, a direct mutator call or asserting stored annotations alone cannot
// pass.
//
// This is the M2 hand-labelled admission probe: it proves admission
// reachability ONLY, not natural rollout. The probe pods are created directly
// with the supported optipod.io/webhook-enabled label and production-format
// recommendation annotations set by hand; they carry no controller owner.
// Natural rollout — where an eligible workload's pod template is configured
// with the supported webhook-enabling label before controller-generated pods
// are created — belongs to the conformance tier (VAL-CONF-007/VAL-CONF-008).
var _ = Describe("Live TLS webhook admission through the installed API", Label("smoke", "webhook"), func() {
	const waitTimeout = 3 * time.Minute

	It("mutates an eligible pod at API creation and admits ineligible pods unchanged", Label("admission"), func(ctx context.Context) {
		ns := specNamespace(ctx, "smoke-webhook-admission")
		DeferCleanup(func(ctx context.Context) {
			Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed(),
				"clean up the webhook admission spec namespace")
		})

		By("reading the installed MutatingWebhookConfiguration the probes run against")
		mwc, err := env.Clients.Kube.AdmissionregistrationV1().MutatingWebhookConfigurations().
			Get(ctx, framework.WebhookConfigurationName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "read the installed MutatingWebhookConfiguration")
		var webhook *admissionregv1.MutatingWebhook
		for i := range mwc.Webhooks {
			if mwc.Webhooks[i].Name == framework.PodMutatorWebhookName {
				webhook = &mwc.Webhooks[i]
			}
		}
		Expect(webhook).NotTo(BeNil(),
			"the installed configuration lacks the %s mutating webhook", framework.PodMutatorWebhookName)
		Expect(webhook.ObjectSelector).NotTo(BeNil(),
			"the installed mutating webhook has no object selector")
		enablingLabelRequired := false
		for _, req := range webhook.ObjectSelector.MatchExpressions {
			if req.Key == optipodv1alpha1.AnnotationWebhookEnabled &&
				req.Operator == metav1.LabelSelectorOpIn &&
				len(req.Values) == 1 && req.Values[0] == "true" {
				enablingLabelRequired = true
			}
		}
		Expect(enablingLabelRequired).To(BeTrue(),
			"the installed webhook object selector does not require the supported optipod.io/webhook-enabled=true pod label")
		Expect(webhook.ClientConfig.Service).NotTo(BeNil(),
			"the installed webhook does not target an in-cluster service")
		Expect(webhook.ClientConfig.Service.Path).NotTo(BeNil(),
			"the installed webhook targets no admission path")
		Expect(*webhook.ClientConfig.Service.Path).To(Equal(framework.WebhookPath),
			"the installed webhook does not target the production %q admission path", framework.WebhookPath)
		Expect(webhook.FailurePolicy).NotTo(BeNil(), "the installed webhook has no failure policy")
		Expect(*webhook.FailurePolicy).To(Equal(admissionregv1.Ignore),
			"the installed webhook must keep the shipped failurePolicy=Ignore")

		By("creating a webhook-strategy Auto policy that selects the probe namespace")
		policyName, err := framework.UniqueName("policy")
		Expect(err).NotTo(HaveOccurred(), "generate unique probe policy name")
		policy := framework.NewPolicyFixture(ns.Name, policyName, optipodv1alpha1.ModeAuto,
			framework.WithStrategy("webhook", ""),
			framework.WithReconciliationInterval(30*time.Second))
		Expect(env.Clients.Ctrl.Create(ctx, policy)).To(Succeed(),
			"create the webhook-strategy Auto policy")

		By("waiting for the deployed controller to reconcile the policy to Ready")
		// The deployed mutator lists policies through the controller's shared
		// cache, so waiting for the observed reconciliation guarantees the
		// probe policy is visible to the webhook before any probe is admitted.
		waitForPolicyStatus(ctx, ns.Name, policyName, policyStatusWait{
			readyStatus:     "True",
			readyReason:     "PolicyValid",
			expectSummary:   true,
			discovered:      0,
			processed:       0,
			reconciledAfter: policy.CreationTimestamp.Time,
		})

		By("creating the hand-labelled eligible probe pod through the real API")
		eligibleName, err := framework.UniqueName("probe-eligible")
		Expect(err).NotTo(HaveOccurred(), "generate unique eligible probe name")
		eligiblePod := newAdmissionProbePod(ns.Name, eligibleName, true, probeRecommendationAnnotations(true))
		admitted, err := env.Clients.Kube.CoreV1().Pods(ns.Name).Create(ctx, eligiblePod, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), "the real API must admit the eligible probe pod")

		By("asserting the admission response itself carries the mutated per-container requests")
		expectMutatedProbeRequests(admitted, "the admission response")

		By("asserting the persisted pod, its hand-set metadata and its webhook-attributed ownership")
		persisted := &corev1.Pod{}
		Expect(env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: eligibleName}, persisted)).
			To(Succeed(), "read the persisted eligible probe pod")
		expectMutatedProbeRequests(persisted, "the persisted pod")
		Expect(persisted.OwnerReferences).To(BeEmpty(),
			"the admission probe must stay a bare hand-labelled pod with no controller owner; "+
				"this spec proves admission reachability only, not natural rollout")
		Expect(persisted.Annotations[optipodv1alpha1.AnnotationWebhookEnabled]).To(Equal("true"),
			"admission dropped the pod's optipod.io/webhook-enabled annotation")
		for _, p := range probeRecommendations {
			Expect(persisted.Annotations[fmt.Sprintf("%s.%s", optipodv1alpha1.AnnotationCPURequestPrefix, p.name)]).
				To(Equal(p.cpuRequest), "admission dropped the %s CPU recommendation annotation", p.name)
			Expect(persisted.Annotations[fmt.Sprintf("%s.%s", optipodv1alpha1.AnnotationMemoryRequestPrefix, p.name)]).
				To(Equal(p.memoryRequest), "admission dropped the %s memory recommendation annotation", p.name)
			cpuLimit, memLimit := containerLimits(persisted.Spec.Containers, p.name)
			Expect(cpuLimit).To(BeNil(), "admission added a CPU limit to %s; the probe stores requests only", p.name)
			Expect(memLimit).To(BeNil(), "admission added a memory limit to %s; the probe stores requests only", p.name)
		}

		By("waiting for the admitted eligible pod to run Ready")
		_, err = framework.WaitForPodCountReady(ctx, env.Clients, ns.Name, "app="+eligibleName, 1,
			framework.WaitOptions{Timeout: waitTimeout})
		Expect(err).NotTo(HaveOccurred(),
			"the mutated eligible probe pod never became Ready on the installed cluster")

		By("creating an ineligible pod without the enabling label")
		noLabelName, err := framework.UniqueName("probe-nolabel")
		Expect(err).NotTo(HaveOccurred(), "generate unique no-label probe name")
		// Carries every enabling annotation and recommendation, so the only
		// thing that can keep it unchanged is the missing object-selector label.
		expectIneligibleProbe(ctx, ns.Name,
			newAdmissionProbePod(ns.Name, noLabelName, false, probeRecommendationAnnotations(true)),
			"the pod without the enabling label")

		By("creating an ineligible pod with the enabling label but without the webhook-enabled annotation")
		gateName, err := framework.UniqueName("probe-gate")
		Expect(err).NotTo(HaveOccurred(), "generate unique gate probe name")
		// Carries the enabling label and every recommendation annotation, so
		// the only thing that can keep it unchanged is the deployed mutator's
		// own optipod.io/webhook-enabled eligibility gate.
		expectIneligibleProbe(ctx, ns.Name,
			newAdmissionProbePod(ns.Name, gateName, true, probeRecommendationAnnotations(false)),
			"the pod with the enabling label but no webhook-enabled annotation")

		By("proving the deployed webhook logged the eligible mutation and no ineligible one")
		logs, err := controllerLogs(ctx)
		Expect(err).NotTo(HaveOccurred(), "read the deployed controller logs for admission evidence")
		Expect(logs).NotTo(BeEmpty(), "the deployed controller logs are empty")
		Expect(logRecordsMutation(logs, eligibleName)).To(BeTrue(),
			"the deployed webhook never logged a successful mutation record for the eligible probe pod %s", eligibleName)
		Expect(logRecordsMutation(logs, noLabelName)).To(BeFalse(),
			"the deployed webhook logged a mutation record for the ineligible pod %s", noLabelName)
		Expect(logRecordsMutation(logs, gateName)).To(BeFalse(),
			"the deployed webhook logged a mutation record for the ineligible pod %s", gateName)
	})
})

// probeRecommendation describes one container of an admission probe pod: the
// initial requests the pod is created with and the exact values its
// production-format recommendation annotations carry. The values are
// deliberately NOT derived from the metrics fixture: the eligible pod must be
// mutated to exactly these hand-set annotation values, so the mutation is
// attributable to the installed webhook's admission alone.
type probeRecommendation struct {
	name          string
	initialCPU    string
	initialMemory string
	cpuRequest    string
	memoryRequest string
}

var probeRecommendations = []probeRecommendation{
	{name: "app", initialCPU: "100m", initialMemory: "64Mi", cpuRequest: "210m", memoryRequest: "192Mi"},
	{name: "sidecar", initialCPU: "50m", initialMemory: "32Mi", cpuRequest: "105m", memoryRequest: "96Mi"},
}

// newAdmissionProbePod builds a typed bare Pod for the hand-labelled admission
// probe. enablingLabel controls the supported optipod.io/webhook-enabled pod
// label the installed webhook configuration's objectSelector requires;
// annotations carry the production recommendation keys the deployed mutator
// reads. The pod is created directly through the real API, so any mutation can
// only come from live admission.
func newAdmissionProbePod(namespace, name string, enablingLabel bool, annotations map[string]string) *corev1.Pod {
	labels := map[string]string{"app": name}
	if enablingLabel {
		labels[optipodv1alpha1.AnnotationWebhookEnabled] = "true"
	}
	containers := make([]corev1.Container, 0, len(probeRecommendations))
	for _, p := range probeRecommendations {
		containers = append(containers, corev1.Container{
			Name:  p.name,
			Image: framework.WorkloadImage(),
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse(p.initialCPU),
					corev1.ResourceMemory: resource.MustParse(p.initialMemory),
				},
			},
		})
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   namespace,
			Name:        name,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{Containers: containers},
	}
}

// probeRecommendationAnnotations builds the production-format per-container
// recommendation annotations for a probe pod — the same key family the
// deployed webhook strategy stores on workloads and the pod mutator reads.
// withWebhookEnabled also sets the optipod.io/webhook-enabled annotation the
// deployed mutator requires before it will consider a pod eligible.
func probeRecommendationAnnotations(withWebhookEnabled bool) map[string]string {
	annotations := map[string]string{}
	if withWebhookEnabled {
		annotations[optipodv1alpha1.AnnotationWebhookEnabled] = "true"
	}
	for _, p := range probeRecommendations {
		annotations[fmt.Sprintf("%s.%s", optipodv1alpha1.AnnotationCPURequestPrefix, p.name)] = p.cpuRequest
		annotations[fmt.Sprintf("%s.%s", optipodv1alpha1.AnnotationMemoryRequestPrefix, p.name)] = p.memoryRequest
	}
	return annotations
}

// expectMutatedProbeRequests asserts the pod carries the probe's per-container
// recommendation values in resources.requests.
func expectMutatedProbeRequests(pod *corev1.Pod, what string) {
	for _, p := range probeRecommendations {
		cpu, memory := containerRequests(pod.Spec.Containers, p.name)
		Expect(cpu).NotTo(BeNil(), "%s lost container %s", what, p.name)
		Expect(memory).NotTo(BeNil(), "%s lost container %s memory request", what, p.name)
		Expect(cpu.String()).To(Equal(p.cpuRequest),
			"%s: container %s was not mutated to its recommended CPU request %s at admission", what, p.name, p.cpuRequest)
		Expect(memory.String()).To(Equal(p.memoryRequest),
			"%s: container %s was not mutated to its recommended memory request %s at admission", what, p.name, p.memoryRequest)
	}
}

// expectUnmutatedProbeRequests asserts the pod kept its initial requests.
func expectUnmutatedProbeRequests(pod *corev1.Pod, what string) {
	for _, p := range probeRecommendations {
		cpu, memory := containerRequests(pod.Spec.Containers, p.name)
		Expect(cpu).NotTo(BeNil(), "%s lost container %s", what, p.name)
		Expect(memory).NotTo(BeNil(), "%s lost container %s memory request", what, p.name)
		Expect(cpu.String()).To(Equal(p.initialCPU),
			"%s: container %s CPU request changed from %s", what, p.name, p.initialCPU)
		Expect(memory.String()).To(Equal(p.initialMemory),
			"%s: container %s memory request changed from %s", what, p.name, p.initialMemory)
	}
}

// expectIneligibleProbe creates the pod through the real API and asserts it is
// admitted with its initial per-container requests unchanged, in both the
// admission response and the persisted object: an ineligible pod must pass
// through admission untouched.
func expectIneligibleProbe(ctx context.Context, namespace string, pod *corev1.Pod, who string) {
	admitted, err := env.Clients.Kube.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred(), "the real API must admit %s", who)
	expectUnmutatedProbeRequests(admitted, who+" admission response")
	persisted := &corev1.Pod{}
	Expect(env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: pod.Name}, persisted)).
		To(Succeed(), "read the persisted %s", who)
	expectUnmutatedProbeRequests(persisted, who+" persisted pod")
}

// controllerLogs reads the deployed controller's current container logs through
// the private-kubeconfig typed client — the same read the failure-diagnostics
// collector performs. Nothing is archived by this helper.
func controllerLogs(ctx context.Context) (string, error) {
	pods, err := env.Clients.Kube.CoreV1().Pods(framework.SystemNamespace).List(ctx,
		metav1.ListOptions{LabelSelector: framework.ControllerPodSelector})
	if err != nil {
		return "", fmt.Errorf("list controller pods: %w", err)
	}
	var logs strings.Builder
	for _, pod := range pods.Items {
		raw, err := env.Clients.Kube.CoreV1().Pods(framework.SystemNamespace).
			GetLogs(pod.Name, &corev1.PodLogOptions{Container: framework.ControllerContainerName, Timestamps: true}).
			DoRaw(ctx)
		if err != nil {
			return "", fmt.Errorf("read controller logs of pod %s: %w", pod.Name, err)
		}
		logs.Write(raw)
	}
	return logs.String(), nil
}

// logRecordsMutation reports whether the controller log contains a successful
// pod-mutation record naming the given pod (the deployed webhook's structured
// success line, emitted before the admission response is returned).
func logRecordsMutation(logs, podName string) bool {
	for _, line := range strings.Split(logs, "\n") {
		if strings.Contains(line, "Pod mutation successful") && strings.Contains(line, podName) {
			return true
		}
	}
	return false
}
