package suite

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/webhook"
	"github.com/optipod/optipod/test/e2e/framework"
)

// Shared helpers for the rollout-timing, immediate-rollout and
// strategy-boundary conformance specs (VAL-CONF-007..009). The immediate
// rollout's restart marker is read from the production rollout controller so
// the specs assert the supported marker rather than a test-side literal, and
// every webhook-eligible workload is created with the production enabling
// pod-template label BEFORE creation so the pods under test are naturally
// generated, never hand-labelled afterwards.

// restartMarkerKey is the pod-template annotation key the production rollout
// controller writes to trigger a workload's rolling restart under the
// immediate rollout strategy (internal/webhook/rollout.go). It is obtained
// from production code so the specs track the supported marker.
func restartMarkerKey() string {
	return webhook.NewRolloutController(nil).GetRestartAnnotationKey()
}

// readTemplateAnnotations reads one supported workload kind's pod-template
// annotation map through the private typed client. Poll loops treat a
// returned error as a transient observation; one-shot callers propagate it.
func readTemplateAnnotations(ctx context.Context, kind optipodv1alpha1.WorkloadType, namespace, name string) (map[string]string, error) {
	key := types.NamespacedName{Namespace: namespace, Name: name}
	switch kind {
	case optipodv1alpha1.WorkloadTypeDeployment:
		obj := &appsv1.Deployment{}
		if err := env.Clients.Ctrl.Get(ctx, key, obj); err != nil {
			return nil, fmt.Errorf("read Deployment %s/%s pod template: %w", namespace, name, err)
		}
		return obj.Spec.Template.Annotations, nil
	case optipodv1alpha1.WorkloadTypeStatefulSet:
		obj := &appsv1.StatefulSet{}
		if err := env.Clients.Ctrl.Get(ctx, key, obj); err != nil {
			return nil, fmt.Errorf("read StatefulSet %s/%s pod template: %w", namespace, name, err)
		}
		return obj.Spec.Template.Annotations, nil
	case optipodv1alpha1.WorkloadTypeDaemonSet:
		obj := &appsv1.DaemonSet{}
		if err := env.Clients.Ctrl.Get(ctx, key, obj); err != nil {
			return nil, fmt.Errorf("read DaemonSet %s/%s pod template: %w", namespace, name, err)
		}
		return obj.Spec.Template.Annotations, nil
	default:
		return nil, fmt.Errorf("unsupported workload kind %q in readTemplateAnnotations", kind)
	}
}

// waitForTemplateRestartMarker waits (bounded) until the workload's pod
// template carries the production immediate-rollout restart marker and
// returns the observed marker value. The bound absorbs the production write
// sequence: the engine triggers the rolling restart only after the
// recommendation annotations are stored, so the marker cannot precede them.
func waitForTemplateRestartMarker(ctx context.Context, kind optipodv1alpha1.WorkloadType, namespace, name string) string {
	key := restartMarkerKey()
	var observed string
	err := framework.WaitForCondition(ctx, framework.WaitOptions{
		Timeout: policyStatusTimeout,
		Description: fmt.Sprintf("%s %s/%s pod template to carry the immediate-rollout restart marker %q",
			kind, namespace, name, key),
	}, func(ctx context.Context) (bool, error) {
		annotations, err := readTemplateAnnotations(ctx, kind, namespace, name)
		if err != nil {
			return false, err
		}
		value, exists := annotations[key]
		if !exists {
			return false, nil
		}
		observed = value
		return true, nil
	})
	Expect(err).NotTo(HaveOccurred(),
		"the installed controller never wrote the immediate-rollout restart marker on the %s %s/%s pod template",
		kind, namespace, name)
	return observed
}

// expectNoTemplateRestartMarker asserts the workload's pod template carries
// no immediate-rollout restart marker: the onNextRestart negative. Call it
// only after the relevant reconcile's completion was observed, so the absence
// is a decided outcome rather than an instantaneous check.
func expectNoTemplateRestartMarker(ctx context.Context, kind optipodv1alpha1.WorkloadType, namespace, name, what string) {
	annotations, err := readTemplateAnnotations(ctx, kind, namespace, name)
	Expect(err).NotTo(HaveOccurred(), "read the %s %s/%s pod template for the restart-marker check",
		kind, namespace, name)
	Expect(annotations).NotTo(HaveKey(restartMarkerKey()),
		"%s: the pod template unexpectedly carries the immediate-rollout restart marker %q", what, restartMarkerKey())
}

// expectRestartMarkerRecent asserts the observed restart marker is a
// parseable RFC3339 timestamp at or after the reference time (within the
// suite's clock-skew tolerance), so a stale or malformed marker cannot pass.
func expectRestartMarkerRecent(marker string, after time.Time) {
	parsed, err := time.Parse(time.RFC3339, marker)
	Expect(err).NotTo(HaveOccurred(), "the restart marker %q is not an RFC3339 timestamp", marker)
	Expect(parsed.After(after.Add(-clockSkewTolerance))).To(BeTrue(),
		"the restart marker timestamp %s predates the reference time %s", marker, after.Format(time.RFC3339))
}

// deleteWorkloadPod performs the deliberate workload restart the rollout
// specs use: the user deletes the workload's single pod through the API and
// the workload controller recreates it through the real admission path, so
// the replacement pod is naturally generated rather than hand-built.
func deleteWorkloadPod(ctx context.Context, namespace, labelSelector string) {
	pods, err := env.Clients.Kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	Expect(err).NotTo(HaveOccurred(), "list the workload's pods before the deliberate restart")
	Expect(pods.Items).To(HaveLen(1),
		"the deliberate-restart helper expects exactly one pod, found %d", len(pods.Items))
	Expect(env.Clients.Kube.CoreV1().Pods(namespace).Delete(ctx, pods.Items[0].Name, metav1.DeleteOptions{})).
		To(Succeed(), "delete pod %s for the deliberate restart", pods.Items[0].Name)
}

// createWebhookKindWorkload creates a two-container workload of the given
// kind whose pod template carries the production optipod.io/webhook-enabled
// label BEFORE creation, so every pod the workload controller generates is
// routed to the installed mutating webhook: natural admission, not a
// hand-labelled probe. It waits until one pod is ready and returns the
// workload's name.
func createWebhookKindWorkload(ctx context.Context, kind optipodv1alpha1.WorkloadType, namespace, prefix string, samples []containerSamples) string {
	name, err := framework.UniqueName(prefix)
	Expect(err).NotTo(HaveOccurred(), "generate unique workload name from prefix %q", prefix)
	containers := smokeWebhookContainers(samples)
	switch kind {
	case optipodv1alpha1.WorkloadTypeDeployment:
		obj := framework.NewDeploymentFixture(namespace, name, 1, containers...)
		Expect(env.Clients.Ctrl.Create(ctx, obj)).To(Succeed(), "create the Deployment workload")
		Expect(framework.WaitForDeploymentAvailable(ctx, env.Clients, namespace, name,
			framework.WaitOptions{Timeout: conformanceWorkloadReadyTimeout})).To(Succeed(),
			"the Deployment workload never became Available")
	case optipodv1alpha1.WorkloadTypeStatefulSet:
		obj := framework.NewStatefulSetFixture(namespace, name, 1, containers...)
		Expect(env.Clients.Ctrl.Create(ctx, obj)).To(Succeed(), "create the StatefulSet workload")
	case optipodv1alpha1.WorkloadTypeDaemonSet:
		obj := framework.NewDaemonSetFixture(namespace, name, containers...)
		Expect(env.Clients.Ctrl.Create(ctx, obj)).To(Succeed(), "create the DaemonSet workload")
	default:
		Fail(fmt.Sprintf("unsupported workload kind %q in createWebhookKindWorkload", kind))
	}
	pods, err := framework.WaitForPodCountReady(ctx, env.Clients, namespace, "app="+name, 1,
		framework.WaitOptions{Timeout: conformanceWorkloadReadyTimeout})
	Expect(err).NotTo(HaveOccurred(), "the %s workload never had a ready pod", kind)
	Expect(pods).NotTo(BeEmpty(), "no ready pods for the %s workload", kind)
	return name
}

// expectContainerRequestsEqual asserts every sampled container of a
// container list carries exactly the sample's expected requests. Used for
// pod-template assertions where the expected values may be a previously
// applied recommendation rather than the fixture's initial requests.
func expectContainerRequestsEqual(containers []corev1.Container, samples []containerSamples, what string) {
	for _, s := range samples {
		cpu, memory := containerRequests(containers, s.name)
		Expect(cpu).NotTo(BeNil(), "%s lost container %s", what, s.name)
		Expect(memory).NotTo(BeNil(), "%s lost container %s memory request", what, s.name)
		Expect(cpu.String()).To(Equal(s.expectedCPU),
			"%s: container %s CPU request is not the expected %s", what, s.name, s.expectedCPU)
		Expect(memory.String()).To(Equal(s.expectedMemory),
			"%s: container %s memory request is not the expected %s", what, s.name, s.expectedMemory)
	}
}
