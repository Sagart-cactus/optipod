package suite

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
)

// Support helpers shared by the conformance specs (modes, selectors,
// policy-weight, bounds, multi-container and metrics-fault scenarios).
// Everything here observes the installed cluster through the suite's private
// typed clients: no fixture field or local validator output is accepted as an
// outcome.

// optipodAnnotationPrefix is the annotation namespace every OptiPod-managed
// annotation key lives under (see AnnotationManaged and friends in
// api/v1alpha1). Excluded-workload assertions use it to prove no OptiPod
// annotation was written at all.
const optipodAnnotationPrefix = "optipod.io/"

// conformanceWorkloadReadyTimeout bounds the wait for a freshly created
// workload of any supported kind to schedule and report a ready pod.
const conformanceWorkloadReadyTimeout = 3 * time.Minute

// policyEventTimeout bounds the wait for an installed-controller Kubernetes
// Event on a policy. The controller emits events synchronously with its
// reconcile outcomes (through its broadcaster), so a matching event after a
// phase's reference time is the explicit per-reconcile outcome signal; the
// bound only absorbs broadcaster delivery lag, never a missing signal.
const policyEventTimeout = 90 * time.Second

// annotationThrottleMargin pads the production 30s recommendation-annotation
// write-throttle window when a spec waits the window out, absorbing
// controller/test clock skew on the recorded last-recommendation timestamp.
const annotationThrottleMargin = 2 * time.Second

// waitForAnnotationThrottleWindow waits until the production recommendation-
// annotation write throttle no longer suppresses metadata writes for the
// named deployment. The deployed processor records its last successful
// annotation write in optipod.io/last-recommendation and skips the whole
// annotation update (while still applying) for 30s after it, so a phase that
// needs NEW annotations stored must let that window elapse before triggering
// the reconcile. The wait is anchored on the recorded timestamp rather than
// on sleeps: it returns immediately when the window is already over.
func waitForAnnotationThrottleWindow(ctx context.Context, namespace, name string) {
	err := framework.WaitForCondition(ctx, framework.WaitOptions{
		Timeout:     policyStatusTimeout,
		Description: fmt.Sprintf("deployment %s/%s recommendation-annotation throttle window to elapse", namespace, name),
	}, func(ctx context.Context) (bool, error) {
		dep := &appsv1.Deployment{}
		if err := env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, dep); err != nil {
			return false, err
		}
		if dep.Annotations == nil {
			return false, nil
		}
		last, exists := dep.Annotations[optipodv1alpha1.AnnotationLastRecommendation]
		if !exists {
			// No write ever succeeded: nothing suppresses the next one.
			return true, nil
		}
		lastTime, err := time.Parse(time.RFC3339, last)
		if err != nil {
			// Unreadable record: proceed and let the assertions judge.
			return true, nil
		}
		return time.Since(lastTime) > 30*time.Second+annotationThrottleMargin, nil
	})
	Expect(err).NotTo(HaveOccurred(),
		"the recommendation-annotation throttle window never elapsed for deployment %s/%s", namespace, name)
}

// waitForPolicyEvent waits (bounded) until the installed controller emitted a
// Kubernetes Event on the named policy with the given reason at or after the
// reference time, whose message contains every fragment. The controller emits
// events per completed reconcile outcome (WorkloadSkipped for the whole-
// workload missing-metrics skip, ValidationFailed for a runtime-invalid
// policy), so a matching event is the explicit signal that a fresh reconcile
// completed with that outcome. Repeated identical messages bump the existing
// event's count and lastTimestamp, so either timestamp moving at or after the
// reference time (with the small clock-skew tolerance) is accepted.
func waitForPolicyEvent(ctx context.Context, namespace, policyName, reason string, since time.Time, fragments ...string) {
	err := framework.WaitForCondition(ctx, framework.WaitOptions{
		Timeout: policyEventTimeout,
		Description: fmt.Sprintf("a %s event on policy %s/%s at or after %s containing %v",
			reason, namespace, policyName, since.Format(time.RFC3339), fragments),
	}, func(ctx context.Context) (bool, error) {
		events, err := env.Clients.Kube.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
			FieldSelector: fmt.Sprintf("involvedObject.name=%s,involvedObject.namespace=%s,reason=%s",
				policyName, namespace, reason),
		})
		if err != nil {
			return false, err
		}
		for i := range events.Items {
			if !policyEventAtOrAfter(events.Items[i], since) {
				continue
			}
			if messageContainsFragments(events.Items[i].Message, fragments) {
				return true, nil
			}
		}
		return false, nil
	})
	Expect(err).NotTo(HaveOccurred(),
		"the installed controller never emitted a %s event on policy %s/%s containing %v",
		reason, namespace, policyName, fragments)
}

// policyEventAtOrAfter reports whether the event was created or last bumped at
// or after the reference time, within the suite's clock-skew tolerance (events
// carry API-server timestamps written from the controller's observation).
func policyEventAtOrAfter(event corev1.Event, since time.Time) bool {
	tolerated := since.Add(-clockSkewTolerance)
	if !event.FirstTimestamp.IsZero() && event.FirstTimestamp.After(tolerated) {
		return true
	}
	if !event.LastTimestamp.IsZero() && event.LastTimestamp.After(tolerated) {
		return true
	}
	return !event.EventTime.IsZero() && event.EventTime.After(tolerated)
}

// messageContainsFragments reports whether the message contains every
// fragment as a substring.
func messageContainsFragments(message string, fragments []string) bool {
	for _, fragment := range fragments {
		if !strings.Contains(message, fragment) {
			return false
		}
	}
	return true
}

// waitForReplacedReadyPods waits (bounded) until every pod from the before set
// is gone and at least as many NEW ready pods matching the selector exist, so
// multi-replica assertions can observe a fully completed rollout. It returns
// the fresh ready pods.
func waitForReplacedReadyPods(ctx context.Context, namespace, labelSelector string, before map[types.UID]string) []corev1.Pod {
	var observed []corev1.Pod
	err := framework.WaitForCondition(ctx, framework.WaitOptions{
		Timeout: rolloutWaitTimeout,
		Description: fmt.Sprintf("all replaced pods matching %q in %s to terminate and %d fresh ready pods to exist",
			labelSelector, namespace, len(before)),
	}, func(ctx context.Context) (bool, error) {
		list, err := env.Clients.Kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			return false, err
		}
		fresh := make([]corev1.Pod, 0, len(before))
		for i := range list.Items {
			if _, existed := before[list.Items[i].UID]; existed {
				return false, nil // a replaced pod is still terminating
			}
			if isReady(&list.Items[i]) {
				fresh = append(fresh, list.Items[i])
			}
		}
		if len(fresh) < len(before) {
			return false, nil
		}
		observed = append(observed[:0], fresh...)
		return true, nil
	})
	Expect(err).NotTo(HaveOccurred(), "the workload never finished replacing its pods")
	return observed
}

// updatePolicy applies mutate to the named policy's current spec through the
// API with a bounded conflict retry: the installed controller updates policy
// status concurrently, so a spec update can meet a resourceVersion conflict
// exactly like any other API client. The retry is bounded and every failure
// surfaces; it is not a silent functional retry.
func updatePolicy(ctx context.Context, namespace, name string, mutate func(*optipodv1alpha1.OptimizationPolicy)) *optipodv1alpha1.OptimizationPolicy {
	const maxAttempts = 10
	for attempt := 1; ; attempt++ {
		policy := &optipodv1alpha1.OptimizationPolicy{}
		Expect(env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, policy)).
			To(Succeed(), "read policy %s/%s before updating it", namespace, name)
		mutate(policy)
		err := env.Clients.Ctrl.Update(ctx, policy)
		if err == nil {
			return policy
		}
		if !apierrors.IsConflict(err) || attempt == maxAttempts {
			Fail(fmt.Sprintf("update policy %s/%s: %v (after %d attempts)", namespace, name, err, attempt))
		}
		// Bounded backoff between conflict retries, mirroring ordinary API
		// client behavior. This is not a functional wait.
		time.Sleep(time.Duration(attempt) * 50 * time.Millisecond)
	}
}

// touchPolicy performs a benign spec-annotation update on the named policy.
// Any policy object update triggers the production controller's watch-driven
// reconcile; conformance specs use the touch to observe selection outcomes
// promptly instead of waiting for the requeue timer. The annotation is never
// read by production code.
func touchPolicy(ctx context.Context, namespace, name string) {
	const touchKey = optipodAnnotationPrefix + "e2e-observe-touch"
	updatePolicy(ctx, namespace, name, func(p *optipodv1alpha1.OptimizationPolicy) {
		if p.Annotations == nil {
			p.Annotations = map[string]string{}
		}
		p.Annotations[touchKey] = time.Now().UTC().Format(time.RFC3339Nano)
	})
}

// workloadView is the per-kind projection conformance specs assert on: the
// workload object's annotations and its pod-template containers.
type workloadView struct {
	annotations map[string]string
	containers  []corev1.Container
}

// readWorkloadView reads one workload of the given kind through the private
// typed client and projects the fields the specs assert on. Poll loops treat
// a returned error as a transient observation and keep polling; one-shot
// callers propagate it.
func readWorkloadView(ctx context.Context, kind optipodv1alpha1.WorkloadType, namespace, name string) (workloadView, error) {
	key := types.NamespacedName{Namespace: namespace, Name: name}
	switch kind {
	case optipodv1alpha1.WorkloadTypeDeployment:
		obj := &appsv1.Deployment{}
		if err := env.Clients.Ctrl.Get(ctx, key, obj); err != nil {
			return workloadView{}, fmt.Errorf("read Deployment %s/%s: %w", namespace, name, err)
		}
		return workloadView{annotations: obj.Annotations, containers: obj.Spec.Template.Spec.Containers}, nil
	case optipodv1alpha1.WorkloadTypeStatefulSet:
		obj := &appsv1.StatefulSet{}
		if err := env.Clients.Ctrl.Get(ctx, key, obj); err != nil {
			return workloadView{}, fmt.Errorf("read StatefulSet %s/%s: %w", namespace, name, err)
		}
		return workloadView{annotations: obj.Annotations, containers: obj.Spec.Template.Spec.Containers}, nil
	case optipodv1alpha1.WorkloadTypeDaemonSet:
		obj := &appsv1.DaemonSet{}
		if err := env.Clients.Ctrl.Get(ctx, key, obj); err != nil {
			return workloadView{}, fmt.Errorf("read DaemonSet %s/%s: %w", namespace, name, err)
		}
		return workloadView{annotations: obj.Annotations, containers: obj.Spec.Template.Spec.Containers}, nil
	default:
		return workloadView{}, fmt.Errorf("unsupported workload kind %q in readWorkloadView", kind)
	}
}

// waitForWorkloadAnnotations waits (bounded) until the named workload of the
// given kind carries every expected annotation key/value pair and returns the
// observed view so callers can also assert management annotations and
// unchanged fields.
func waitForWorkloadAnnotations(ctx context.Context, kind optipodv1alpha1.WorkloadType, namespace, name string, want map[string]string) workloadView {
	var observed workloadView
	err := framework.WaitForCondition(ctx, framework.WaitOptions{
		Timeout:     policyStatusTimeout,
		Description: fmt.Sprintf("%s %s/%s to carry annotations %v", kind, namespace, name, want),
	}, func(ctx context.Context) (bool, error) {
		view, err := readWorkloadView(ctx, kind, namespace, name)
		if err != nil {
			return false, err
		}
		for key, value := range want {
			if view.annotations[key] != value {
				return false, nil
			}
		}
		observed = view
		return true, nil
	})
	Expect(err).NotTo(HaveOccurred(),
		"the deployed controller never wrote the expected annotations on %s %s/%s", kind, namespace, name)
	return observed
}

// waitForWorkloadTemplateRequests waits (bounded) until every sampled
// container of the named workload's pod template carries exactly its expected
// CPU/memory request, proving the recommendation was applied to the template
// regardless of workload kind.
func waitForWorkloadTemplateRequests(ctx context.Context, kind optipodv1alpha1.WorkloadType, namespace, name string, samples []containerSamples) {
	err := framework.WaitForCondition(ctx, framework.WaitOptions{
		Timeout: policyStatusTimeout,
		Description: fmt.Sprintf("%s %s/%s pod template to carry the applied requests of %d containers",
			kind, namespace, name, len(samples)),
	}, func(ctx context.Context) (bool, error) {
		view, err := readWorkloadView(ctx, kind, namespace, name)
		if err != nil {
			return false, err
		}
		for _, s := range samples {
			cpu, memory := containerRequests(view.containers, s.name)
			if cpu == nil || memory == nil ||
				cpu.String() != s.expectedCPU || memory.String() != s.expectedMemory {
				return false, nil
			}
		}
		return true, nil
	})
	Expect(err).NotTo(HaveOccurred(),
		"the deployed controller never applied the expected requests to the %s %s/%s pod template", kind, namespace, name)
}

// createKindWorkload creates a two-container workload of the given kind (with
// optional workload-object labels for label-selector scenarios), waits until
// one of its pods is ready and returns the workload's name. All names are
// unique; no spec shares a resource identity.
func createKindWorkload(ctx context.Context, kind optipodv1alpha1.WorkloadType, namespace, prefix string, samples []containerSamples, labels map[string]string) string {
	name, err := framework.UniqueName(prefix)
	Expect(err).NotTo(HaveOccurred(), "generate unique workload name from prefix %q", prefix)
	containers := smokeContainers(samples)
	switch kind {
	case optipodv1alpha1.WorkloadTypeDeployment:
		obj := framework.NewDeploymentFixture(namespace, name, 1, containers...)
		obj.Labels = labels
		Expect(env.Clients.Ctrl.Create(ctx, obj)).To(Succeed(), "create the Deployment workload")
		Expect(framework.WaitForDeploymentAvailable(ctx, env.Clients, namespace, name,
			framework.WaitOptions{Timeout: conformanceWorkloadReadyTimeout})).To(Succeed(),
			"the Deployment workload never became Available")
	case optipodv1alpha1.WorkloadTypeStatefulSet:
		obj := framework.NewStatefulSetFixture(namespace, name, 1, containers...)
		obj.Labels = labels
		Expect(env.Clients.Ctrl.Create(ctx, obj)).To(Succeed(), "create the StatefulSet workload")
	case optipodv1alpha1.WorkloadTypeDaemonSet:
		obj := framework.NewDaemonSetFixture(namespace, name, containers...)
		obj.Labels = labels
		Expect(env.Clients.Ctrl.Create(ctx, obj)).To(Succeed(), "create the DaemonSet workload")
	default:
		Fail(fmt.Sprintf("unsupported workload kind %q in createKindWorkload", kind))
	}
	pods, err := framework.WaitForPodCountReady(ctx, env.Clients, namespace, "app="+name, 1,
		framework.WaitOptions{Timeout: conformanceWorkloadReadyTimeout})
	Expect(err).NotTo(HaveOccurred(), "the %s workload never had a ready pod", kind)
	Expect(pods).NotTo(BeEmpty(), "no ready pods for the %s workload", kind)
	return name
}

// buildWorkloadSeries lists the live pods of the named workload (any
// supported kind) and builds deterministic per-container series for each pod
// WITHOUT writing them to the fixture, so multi-workload scenarios can
// accumulate one wholesale series set.
func buildWorkloadSeries(ctx context.Context, namespace, name string, samples []containerSamples) []framework.FixtureSeries {
	pods, err := env.Clients.Kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=" + name,
	})
	Expect(err).NotTo(HaveOccurred(), "list pods of workload %s", name)
	Expect(pods.Items).NotTo(BeEmpty(), "workload %s has no pods to register series for", name)

	series := make([]framework.FixtureSeries, 0, len(pods.Items)*len(samples))
	for _, pod := range pods.Items {
		for _, s := range samples {
			// Eleven identical samples; the production P90 rank over eleven
			// samples lands exactly on one sample, so the recommendation is
			// derived from the fixture input without interpolation.
			cpu := make([]float64, 11)
			memory := make([]float64, 11)
			for i := range cpu {
				cpu[i] = s.cpuCores
				memory[i] = float64(s.memoryBytes)
			}
			series = append(series, framework.FixtureSeries{
				Namespace:   namespace,
				Pod:         pod.Name,
				Container:   s.name,
				CPUCores:    cpu,
				MemoryBytes: memory,
			})
		}
	}
	return series
}

// registerWorkloadSeries registers (wholesale) the deterministic series for
// every live pod of ONE named workload, so whichever pod the deployed
// controller samples first is served known data. Multi-workload scenarios
// must use buildWorkloadSeries plus a single SetSeries instead, because
// SetSeries replaces the fixture's entire series set.
func registerWorkloadSeries(ctx context.Context, namespace, name string, samples []containerSamples) []framework.FixtureSeries {
	series := buildWorkloadSeries(ctx, namespace, name, samples)
	Expect(env.Fixture.SetSeries(ctx, series)).To(Succeed(),
		"register deterministic fixture series for workload %s", name)
	return series
}

// labeledNamespace creates a uniquely named namespace carrying extra labels
// (namespace-selector scenarios) plus the harness managed-by label used for
// diagnostics scoping and ownership.
func labeledNamespace(ctx context.Context, prefix string, labels map[string]string) *corev1.Namespace {
	unique, err := framework.UniqueName(prefix)
	Expect(err).NotTo(HaveOccurred(), "generate unique namespace name from prefix %q", prefix)
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: unique,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "optipod-e2e-harness",
			},
		},
	}
	for key, value := range labels {
		ns.Labels[key] = value
	}
	created, err := env.Clients.Kube.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred(), "create labeled spec namespace %s", unique)
	return created
}

// expectNoOptipodAnnotations asserts a workload carries no OptiPod-managed
// annotation key at all: neither the management family nor any per-container
// recommendation. Combined with the confirmed-reconcile policy status waits it
// is the excluded-workload negative for selector scenarios.
func expectNoOptipodAnnotations(annotations map[string]string, what string) {
	var found []string
	for key := range annotations {
		if strings.HasPrefix(key, optipodAnnotationPrefix) {
			found = append(found, key)
		}
	}
	sort.Strings(found)
	Expect(found).To(BeEmpty(),
		"%s must carry no OptiPod annotation after the confirmed reconcile, found %v", what, found)
}

// expectUntouchedWorkload asserts one excluded workload still carries exactly
// its original per-container requests, gained no limits and received no
// OptiPod annotation: the negative selection outcome.
func expectUntouchedWorkload(view workloadView, samples []containerSamples, what string) {
	expectNoOptipodAnnotations(view.annotations, what)
	expectUnappliedContainerRequests(view.containers, samples, what)
	for _, s := range samples {
		cpuLimit, memLimit := containerLimits(view.containers, s.name)
		Expect(cpuLimit).To(BeNil(), "%s: container %s was given a CPU limit", what, s.name)
		Expect(memLimit).To(BeNil(), "%s: container %s was given a memory limit", what, s.name)
	}
}

// expectWorkloadTypeCounts asserts the policy's persisted per-kind discovered
// counts. Production zeroes absent kinds on every status update, so a filter
// that excludes a kind shows as a zero count, not a stale one.
func expectWorkloadTypeCounts(policy *optipodv1alpha1.OptimizationPolicy, deployments, statefulSets, daemonSets int) {
	Expect(policy.Status.WorkloadsByType).NotTo(BeNil(),
		"policy %s has no per-kind workload counts", policy.Name)
	Expect(policy.Status.WorkloadsByType.Deployments).To(Equal(deployments),
		"policy %s Deployments count", policy.Name)
	Expect(policy.Status.WorkloadsByType.StatefulSets).To(Equal(statefulSets),
		"policy %s StatefulSets count", policy.Name)
	Expect(policy.Status.WorkloadsByType.DaemonSets).To(Equal(daemonSets),
		"policy %s DaemonSets count", policy.Name)
}

// expectNoFixtureQueryFor asserts the fixture recorded no metrics query for
// any of the given series, proving the deployed controller never collected
// metrics for those targets.
func expectNoFixtureQueryFor(ctx context.Context, series []framework.FixtureSeries, what string) {
	trace, err := env.Fixture.Trace(ctx)
	Expect(err).NotTo(HaveOccurred(), "read the fixture query trace")
	for _, s := range series {
		for _, q := range trace {
			if strings.Contains(q.Query, fmt.Sprintf("namespace=%q", s.Namespace)) &&
				strings.Contains(q.Query, fmt.Sprintf("pod=%q", s.Pod)) &&
				strings.Contains(q.Query, fmt.Sprintf("container=%q", s.Container)) {
				Fail(fmt.Sprintf("%s: the deployed controller unexpectedly queried metrics for %s/%s (%s)",
					what, s.Pod, s.Container, q.Query))
			}
		}
	}
}

// expectNoFixtureQuerySince asserts the fixture recorded no metrics query for
// any pod of the given namespace at or after the reference time, proving the
// controller skipped metrics collection for the whole namespace after that
// point.
func expectNoFixtureQuerySince(ctx context.Context, namespace string, since time.Time, what string) {
	trace, err := env.Fixture.Trace(ctx)
	Expect(err).NotTo(HaveOccurred(), "read the fixture query trace")
	for _, q := range trace {
		if !q.Time.Before(since) &&
			strings.Contains(q.Query, fmt.Sprintf("namespace=%q", namespace)) {
			Fail(fmt.Sprintf("%s: the deployed controller unexpectedly queried metrics for namespace %s after %s (%s)",
				what, namespace, since.Format(time.RFC3339), q.Query))
		}
	}
}

// waitForReconcileAfter waits (bounded) until the named policy reports a
// completed reconciliation strictly after the reference time and returns the
// observed policy. Unlike waitForNextReconcile's fixed bound, the caller
// supplies the deadline so slower production requeue paths (a Disabled
// policy's 4x interval plus the 1-minute summary refresh gate) stay inside a
// justified, explicit timeout.
func waitForReconcileAfter(ctx context.Context, namespace, name string, after time.Time, timeout time.Duration) *optipodv1alpha1.OptimizationPolicy {
	var observed *optipodv1alpha1.OptimizationPolicy
	err := framework.WaitForCondition(ctx, framework.WaitOptions{
		Timeout: timeout,
		Description: fmt.Sprintf("policy %s/%s to complete another reconciliation after %s",
			namespace, name, after.Format(time.RFC3339)),
	}, func(ctx context.Context) (bool, error) {
		policy := &optipodv1alpha1.OptimizationPolicy{}
		if err := env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, policy); err != nil {
			return false, err
		}
		if policy.Status.LastReconciliation == nil {
			return false, nil
		}
		if !policy.Status.LastReconciliation.After(after) {
			return false, nil
		}
		observed = policy
		return true, nil
	})
	Expect(err).NotTo(HaveOccurred(), "installed policy never completed a further bounded reconcile")
	return observed
}

// fetchPolicy reads the named policy through the private typed client.
func fetchPolicy(ctx context.Context, namespace, name string) *optipodv1alpha1.OptimizationPolicy {
	policy := &optipodv1alpha1.OptimizationPolicy{}
	Expect(env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, policy)).
		To(Succeed(), "read policy %s/%s", namespace, name)
	return policy
}

// expectManagementAnnotationsOn asserts the OptiPod management annotation
// family on any supported kind's annotation map: the managing-policy
// identity (name and UID) and a last-recommendation timestamp newer than the
// policy's creation.
func expectManagementAnnotationsOn(annotations map[string]string, policy *optipodv1alpha1.OptimizationPolicy, createdAfter time.Time) {
	Expect(annotations).NotTo(BeNil(), "workload has no annotations at all")
	Expect(annotations[optipodv1alpha1.AnnotationManaged]).To(Equal("true"),
		"workload lacks the optipod.io/managed annotation")
	Expect(annotations[optipodv1alpha1.AnnotationPolicy]).To(Equal(policy.Name),
		"workload names the wrong managing policy")
	Expect(annotations[optipodv1alpha1.AnnotationPolicyUID]).To(Equal(string(policy.UID)),
		"workload records the wrong policy UID")
	last, err := time.Parse(time.RFC3339, annotations[optipodv1alpha1.AnnotationLastRecommendation])
	Expect(err).NotTo(HaveOccurred(), "optipod.io/last-recommendation is not an RFC3339 timestamp")
	Expect(last.After(createdAfter.Add(-clockSkewTolerance))).To(BeTrue(),
		"last-recommendation timestamp predates policy creation")
}
