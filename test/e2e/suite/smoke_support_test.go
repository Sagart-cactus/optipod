package suite

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	. "github.com/onsi/gomega"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
)

// clockSkewTolerance covers small clock differences between the API server
// (object creation timestamps) and the controller pod (status timestamps) when
// asserting that a reconciliation happened after creation.
const clockSkewTolerance = 2 * time.Second

// policyStatusTimeout bounds how long a spec waits for the installed controller
// to reconcile a policy into the expected status.
const policyStatusTimeout = 3 * time.Minute

// policyStatusWait names the reconciliation state an installed policy must
// reach before a scenario continues. Every field is observed through the
// private-config typed API, never inferred from a local validator.
type policyStatusWait struct {
	// readyStatus/readyReason expect the policy's Ready condition.
	readyStatus metav1.ConditionStatus
	readyReason string
	// messageContains is a fragment of the expected condition message.
	messageContains string
	// expectSummary requires the reconciliation summary (LastReconciliation
	// plus discovered/processed counts). The runtime-validation failure path
	// writes only the Ready condition and never processes workloads, so specs
	// asserting an invalid policy skip the summary.
	expectSummary bool
	// discovered/processed expect the reconciliation summary counts.
	discovered int
	processed  int
	// reconciledAfter expects LastReconciliation at or after this time.
	reconciledAfter time.Time
}

// waitForPolicyStatus waits (bounded) until the installed controller has
// reconciled the named policy into the expected state: the Ready condition with
// its reason/message, the workload summary counts, and a LastReconciliation
// timestamp newer than the reference. It returns the observed policy so callers
// can assert on the exact persisted status.
func waitForPolicyStatus(ctx context.Context, namespace, name string, want policyStatusWait) *optipodv1alpha1.OptimizationPolicy {
	var observed *optipodv1alpha1.OptimizationPolicy
	err := framework.WaitForCondition(ctx, framework.WaitOptions{
		Timeout: policyStatusTimeout,
		Description: fmt.Sprintf("policy %s/%s to reach Ready=%s reason=%s with %d discovered/%d processed workloads",
			namespace, name, want.readyStatus, want.readyReason, want.discovered, want.processed),
	}, func(ctx context.Context) (bool, error) {
		policy := &optipodv1alpha1.OptimizationPolicy{}
		if err := env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, policy); err != nil {
			return false, err
		}
		cond := meta.FindStatusCondition(policy.Status.Conditions, "Ready")
		if cond == nil {
			return false, nil
		}
		if cond.Status != want.readyStatus || cond.Reason != want.readyReason {
			return false, nil
		}
		if want.messageContains != "" && !strings.Contains(cond.Message, want.messageContains) {
			return false, nil
		}
		if want.expectSummary {
			if policy.Status.WorkloadsDiscovered != want.discovered ||
				policy.Status.WorkloadsProcessed != want.processed {
				return false, nil
			}
			if policy.Status.LastReconciliation == nil ||
				policy.Status.LastReconciliation.Time.Before(want.reconciledAfter.Add(-clockSkewTolerance)) {
				return false, nil
			}
		}
		observed = policy
		return true, nil
	})
	Expect(err).NotTo(HaveOccurred(), "installed policy never reached the expected reconciliation status")
	return observed
}

// waitForDeploymentAnnotation waits (bounded) until the deployment carries all
// the expected annotation key/value pairs, and returns the observed deployment
// so callers can also assert the management annotations and unchanged fields.
func waitForDeploymentAnnotation(ctx context.Context, namespace, name string, want map[string]string) *appsv1.Deployment {
	var observed *appsv1.Deployment
	err := framework.WaitForCondition(ctx, framework.WaitOptions{
		Timeout:     policyStatusTimeout,
		Description: fmt.Sprintf("deployment %s/%s to carry recommendation annotations %v", namespace, name, want),
	}, func(ctx context.Context) (bool, error) {
		dep := &appsv1.Deployment{}
		if err := env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, dep); err != nil {
			return false, err
		}
		for key, value := range want {
			if dep.Annotations[key] != value {
				return false, nil
			}
		}
		observed = dep
		return true, nil
	})
	Expect(err).NotTo(HaveOccurred(), "deployed controller never wrote the expected workload annotations")
	return observed
}

// waitForDeploymentRequests waits (bounded) until the deployment's container
// template carries exactly the expected CPU/memory requests.
func waitForDeploymentRequests(ctx context.Context, namespace, name, container, cpu, memory string) {
	err := framework.WaitForCondition(ctx, framework.WaitOptions{
		Timeout: policyStatusTimeout,
		Description: fmt.Sprintf("deployment %s/%s container %q requests to become cpu=%s memory=%s",
			namespace, name, container, cpu, memory),
	}, func(ctx context.Context) (bool, error) {
		dep := &appsv1.Deployment{}
		if err := env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, dep); err != nil {
			return false, err
		}
		gotCPU, gotMemory := containerRequests(dep.Spec.Template.Spec.Containers, container)
		if gotCPU == nil || gotMemory == nil {
			return false, nil
		}
		if gotCPU.String() != cpu || gotMemory.String() != memory {
			return false, nil
		}
		return true, nil
	})
	Expect(err).NotTo(HaveOccurred(), "deployed controller never applied the expected workload requests")
}

// containerRequests returns the CPU/memory requests of the named container, or
// nils when the container or either request is absent.
func containerRequests(containers []corev1.Container, name string) (*resource.Quantity, *resource.Quantity) {
	for i := range containers {
		if containers[i].Name != name {
			continue
		}
		cpu, hasCPU := containers[i].Resources.Requests[corev1.ResourceCPU]
		memory, hasMemory := containers[i].Resources.Requests[corev1.ResourceMemory]
		if !hasCPU || !hasMemory {
			return nil, nil
		}
		return &cpu, &memory
	}
	return nil, nil
}

// containerLimits returns the CPU/memory limits of the named container, or
// nils when the container or either limit is absent.
func containerLimits(containers []corev1.Container, name string) (*resource.Quantity, *resource.Quantity) {
	for i := range containers {
		if containers[i].Name != name {
			continue
		}
		cpu, hasCPU := containers[i].Resources.Limits[corev1.ResourceCPU]
		memory, hasMemory := containers[i].Resources.Limits[corev1.ResourceMemory]
		if !hasCPU || !hasMemory {
			return nil, nil
		}
		return &cpu, &memory
	}
	return nil, nil
}

// podUIDs returns the name->UID map of every pod in the namespace matching the
// label selector, for before/after pod-identity assertions.
func podUIDs(ctx context.Context, namespace, labelSelector string) map[types.UID]string {
	pods, err := env.Clients.Kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	Expect(err).NotTo(HaveOccurred(), "list pods with selector %q", labelSelector)
	out := make(map[types.UID]string, len(pods.Items))
	for _, pod := range pods.Items {
		out[pod.UID] = pod.Name
	}
	return out
}

// rolloutWaitTimeout bounds the rollout-related waits shared by every rollout
// assertion in the suite: a replacement pod becoming ready and the replaced
// pod finishing termination.
const rolloutWaitTimeout = 3 * time.Minute

// waitForReplacementPod waits (up to rolloutWaitTimeout) until at least one
// ready pod matching the selector exists whose UID is not in the before set,
// proving the workload actually rolled rather than merely changing metadata.
// It returns the new ready pods.
func waitForReplacementPod(ctx context.Context, namespace, labelSelector string, before map[types.UID]string) []corev1.Pod {
	var fresh []corev1.Pod
	err := framework.WaitForCondition(ctx, framework.WaitOptions{
		Timeout:     rolloutWaitTimeout,
		Description: fmt.Sprintf("a ready replacement pod matching %q in %s", labelSelector, namespace),
	}, func(ctx context.Context) (bool, error) {
		list, err := env.Clients.Kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			return false, err
		}
		fresh = fresh[:0]
		for _, pod := range list.Items {
			if _, existed := before[pod.UID]; existed || !isReady(&pod) {
				continue
			}
			fresh = append(fresh, pod)
		}
		return len(fresh) > 0, nil
	})
	Expect(err).NotTo(HaveOccurred(), "the workload never rolled to a ready replacement pod")
	return fresh
}

func isReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// waitForPodsToSettle waits (up to rolloutWaitTimeout) until exactly one pod
// matching the selector remains and none of the before UIDs survive. A
// replaced pod stays visible (Terminating) after its replacement is ready,
// so a pod-identity snapshot taken too early could change without any
// rollout churn; this wait bounds that window before steady-state captures.
func waitForPodsToSettle(ctx context.Context, namespace, labelSelector string, before map[types.UID]string) {
	err := framework.WaitForCondition(ctx, framework.WaitOptions{
		Timeout: rolloutWaitTimeout,
		Description: fmt.Sprintf("pods matching %q in %s to settle at exactly one with none of the replaced identities",
			labelSelector, namespace),
	}, func(ctx context.Context) (bool, error) {
		list, err := env.Clients.Kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			return false, err
		}
		if len(list.Items) != 1 {
			return false, nil
		}
		for _, pod := range list.Items {
			if _, existed := before[pod.UID]; existed {
				return false, nil
			}
		}
		return true, nil
	})
	Expect(err).NotTo(HaveOccurred(), "replaced pods never finished terminating before the steady-state snapshot")
}

// containerSamples describes the deterministic fixture input for one workload
// container, plus the exact annotation/request values the deployed controller
// must derive from it.
//
// The fixture serves eleven identical samples per metric. The production
// percentile rule is nearest-rank with linear interpolation; the P90 rank over
// eleven samples is 0.9*(11-1) = 9.0, an exact index, so P90 equals the sample
// itself and no interpolation is involved. The recommendation is then
// P90 * 1.2 (the default safety factor) clamped to the fixture policy bounds
// (CPU 50m..4, memory 64Mi..8Gi); the values below stay inside those bounds.
// The expected strings are therefore derived from the deterministic fixture
// input independently of the production code under test.
type containerSamples struct {
	name string
	// cpuCores is the identical CPU-usage sample, in cores.
	cpuCores float64
	// memoryBytes is the identical memory working-set sample, in bytes.
	memoryBytes int64
	// initialCPU/initialMemory are the workload's original container requests,
	// chosen below the expected recommendation so an applied change is an
	// increase for every resource of every container.
	initialCPU    string
	initialMemory string
	// expectedCPU/expectedMemory are the exact recommendation values the
	// deployed controller must compute, annotate and (in Auto mode) apply.
	expectedCPU    string
	expectedMemory string
}

var (
	// smokeApp: CPU 0.25 cores -> 250m; 250m * 1.2 = 300m.
	// Memory 100Mi -> P90 100Mi; 100Mi * 1.2 = 120Mi.
	smokeApp = containerSamples{
		name: "app", cpuCores: 0.25, memoryBytes: 100 << 20,
		initialCPU: "100m", initialMemory: "64Mi",
		expectedCPU: "300m", expectedMemory: "120Mi",
	}
	// smokeSidecar: CPU 0.125 cores -> 125m; 125m * 1.2 = 150m.
	// Memory 60Mi -> P90 60Mi; 60Mi * 1.2 = 72Mi (above the 64Mi min bound, so
	// no clamping is involved; every expected value is raw metric arithmetic).
	smokeSidecar = containerSamples{
		name: "sidecar", cpuCores: 0.125, memoryBytes: 60 << 20,
		initialCPU: "50m", initialMemory: "32Mi",
		expectedCPU: "150m", expectedMemory: "72Mi",
	}
)

// smokeSamples returns the two-container smoke fixture pair: distinct
// per-container samples prove per-container recommendations and applications
// rather than a single-container special case.
func smokeSamples() []containerSamples {
	return []containerSamples{smokeApp, smokeSidecar}
}

// smokeContainers builds the workload container fixtures matching samples.
func smokeContainers(samples []containerSamples) []framework.ContainerFixture {
	containers := make([]framework.ContainerFixture, 0, len(samples))
	for _, s := range samples {
		containers = append(containers, framework.ContainerFixture{
			Name:          s.name,
			CPURequest:    s.initialCPU,
			MemoryRequest: s.initialMemory,
		})
	}
	return containers
}

// registerContainerSeries registers deterministic fixture series for every pod
// of the deployment (per container), so whichever pod the deployed controller
// samples first is served known data. It returns the fixture series that were
// registered for trace attribution.
func registerContainerSeries(ctx context.Context, namespace string, deployment *appsv1.Deployment, samples []containerSamples) []framework.FixtureSeries {
	pods, err := env.Clients.Kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=" + deployment.Name,
	})
	Expect(err).NotTo(HaveOccurred(), "list pods of deployment %s", deployment.Name)
	Expect(pods.Items).NotTo(BeEmpty(), "deployment %s has no pods to register series for", deployment.Name)

	series := make([]framework.FixtureSeries, 0, len(pods.Items)*len(samples))
	for _, pod := range pods.Items {
		for _, s := range samples {
			// Eleven identical samples; P90 (rank 9.0) lands exactly on one.
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
	Expect(env.Fixture.SetSeries(ctx, series)).To(Succeed(),
		"register deterministic fixture series for deployment %s", deployment.Name)
	return series
}

// expectFixtureQueried asserts the deployed controller actually issued
// Prometheus queries for the given targets, proving the fixture was consumed
// by the controller rather than merely deployed.
func expectFixtureQueried(ctx context.Context, series []framework.FixtureSeries) {
	trace, attempts, err := env.Fixture.TraceWithRetry(ctx, traceReadAttempts, traceReadBackoff)
	Expect(err).NotTo(HaveOccurred(),
		"fixture trace read failed after %d attempts (last error: %v)", attempts, err)
	Expect(trace).NotTo(BeEmpty(), "the deployed controller never queried the metrics fixture")
	missing := fixtureTargetsMissing(trace, series, time.Time{})
	Expect(missing).To(BeEmpty(),
		"the deployed controller never issued a metrics query for targets [%s] "+
			"(expected a Prometheus query matching each namespace/container pair for one of its registered pods)",
		strings.Join(missing, ", "))
}

// traceReadAttempts/traceReadBackoff bound every fixture-trace read in the
// suite. Every expectFixtureQueriedAfter call site anchors on an
// already-completed reconcile (a WorkloadSkipped event, the stored
// recommendation annotations, or an observed next reconciliation), and the
// fixture records a query before the controller finishes the reconcile that
// issued it — so the queries are already recorded when the helper runs. Reads
// are one-shot verifications with a small retry budget for transient
// API-server proxy errors, never a minutes-long re-poll that would swallow
// the real transport error behind a timeout (the M3 VAL-CONF-005 incident).
const (
	traceReadAttempts = 3
	traceReadBackoff  = 5 * time.Second
)

// expectFixtureQueriedAfter asserts the deployed controller issued a
// Prometheus query for every given target at or after the supplied time. A
// missing-metrics reconcile still records a query, so pairing a
// post-registration timestamp with the re-registered series proves a reconcile
// that ran after the samples were registered consumed them, rather than the
// workload being skipped for missing data.
//
// The two failure modes are reported distinctly: a transport failure surfaces
// as "fixture trace read failed after N attempts (last error: ...)", while a
// successful read without a matching query reports "the fixture trace read
// succeeded but recorded no matching query" — real read errors are never
// discarded into a behavioral-looking timeout.
func expectFixtureQueriedAfter(ctx context.Context, series []framework.FixtureSeries, after time.Time) {
	trace, attempts, err := env.Fixture.TraceWithRetry(ctx, traceReadAttempts, traceReadBackoff)
	Expect(err).NotTo(HaveOccurred(),
		"fixture trace read failed after %d attempts (last error: %v); the completed-reconcile signal was "+
			"already observed, so this is a harness/transport failure, not evidence the controller skipped the targets",
		attempts, err)
	missing := fixtureTargetsMissing(trace, series, after)
	Expect(missing).To(BeEmpty(),
		"the fixture trace read succeeded (%d queries recorded) but recorded no matching query for targets [%s] "+
			"at or after %s; the anchored reconcile completed, so the controller never queried these targets",
		len(trace), strings.Join(missing, ", "), after.Format(time.RFC3339))
}

// fixtureTargetsMissing returns the namespace/container targets (one entry per
// distinct pair in series) with no recorded query at or after `after`.
//
// The controller samples ONE of the workload's live pods per container per
// reconcile: getFirstPodName (internal/controller/workload_processor.go)
// lists pods through the informer cache, whose iteration order is
// unspecified, so with two or more replicas the sampled pod is arbitrary (and
// can differ between containers of one reconcile). The harness registers
// series for every live pod of the workload, so a target is proven queried
// when the trace holds a query matching its namespace/container for ANY of
// its registered pods. A zero `after` matches every recorded query.
func fixtureTargetsMissing(trace []framework.RecordedQuery, series []framework.FixtureSeries, after time.Time) []string {
	type target struct {
		namespace string
		container string
	}
	podsByTarget := make(map[target]map[string]bool)
	var order []target
	for _, s := range series {
		t := target{namespace: s.Namespace, container: s.Container}
		if podsByTarget[t] == nil {
			podsByTarget[t] = make(map[string]bool)
			order = append(order, t)
		}
		podsByTarget[t][s.Pod] = true
	}
	var missing []string
	for _, t := range order {
		if !fixtureTargetQueried(trace, t.namespace, t.container, podsByTarget[t], after) {
			missing = append(missing, t.namespace+"/"+t.container)
		}
	}
	return missing
}

// fixtureTargetQueried reports whether the trace holds a query at or after
// `after` matching the namespace/container pair for any of the given pods.
func fixtureTargetQueried(trace []framework.RecordedQuery, namespace, container string, pods map[string]bool, after time.Time) bool {
	namespaceMatcher := fmt.Sprintf("namespace=%q", namespace)
	containerMatcher := fmt.Sprintf("container=%q", container)
	for _, q := range trace {
		if q.Time.Before(after) {
			continue
		}
		if !strings.Contains(q.Query, namespaceMatcher) ||
			!strings.Contains(q.Query, containerMatcher) {
			continue
		}
		for pod := range pods {
			if strings.Contains(q.Query, fmt.Sprintf("pod=%q", pod)) {
				return true
			}
		}
	}
	return false
}

// expectManagementAnnotations asserts the OptiPod management annotations the
// processor writes alongside the per-container recommendations.
func expectManagementAnnotations(dep *appsv1.Deployment, policy *optipodv1alpha1.OptimizationPolicy, createdAfter time.Time) {
	expectManagementAnnotationsOn(dep.Annotations, policy, createdAfter)
}

// nextReconcileTimeout bounds the wait for one further policy reconciliation
// after a reference reconcile. The smoke fixtures set a 30s reconciliation
// interval, so three intervals absorb requeue jitter without hiding a stalled
// controller behind an overlong deadline.
const nextReconcileTimeout = 90 * time.Second

// waitForNextReconcile waits (bounded) until the installed policy reports a
// completed reconciliation strictly after the reference time, and returns the
// observed policy. This observes the controller's own requeue loop — no fixed
// sleep is involved — and is how steady-state specs prove behavior across two
// distinct completed reconciles.
func waitForNextReconcile(ctx context.Context, namespace, name string, after time.Time) *optipodv1alpha1.OptimizationPolicy {
	var observed *optipodv1alpha1.OptimizationPolicy
	err := framework.WaitForCondition(ctx, framework.WaitOptions{
		Timeout: nextReconcileTimeout,
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

// steadyState captures the rollout-relevant observables of a smoke workload:
// the Deployment revision annotation (bumped only on pod-template changes by
// the Deployment controller), the ReplicaSet set (a rollout always adds one),
// the live pod identities and the pod-template spec.
//
// metadata.generation is deliberately NOT part of this snapshot: the upstream
// Deployment registry strategy bumps generation on annotation-only updates too
// (annotations are copied from Deployments to their ReplicaSets), and the
// controller writes a fresh optipod.io/last-recommendation timestamp on every
// reconcile, so generation moves by design while a converged workload must
// not. The stable no-churn observables are the revision, the ReplicaSet set,
// the pod identities and the pod-template contents.
type steadyState struct {
	revision    string
	replicaSets []string
	podUIDs     map[types.UID]string
	template    corev1.PodTemplateSpec
}

// captureSteadyState reads one workload's steady-state observables through the
// private-config typed API.
func captureSteadyState(ctx context.Context, namespace, name, podSelector string) steadyState {
	dep := &appsv1.Deployment{}
	Expect(env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, dep)).
		To(Succeed(), "read workload %s/%s for the steady-state snapshot", namespace, name)
	return steadyState{
		revision:    dep.Annotations["deployment.kubernetes.io/revision"],
		replicaSets: replicaSetNames(ctx, namespace, podSelector),
		podUIDs:     podUIDs(ctx, namespace, podSelector),
		template:    *dep.Spec.Template.DeepCopy(),
	}
}

// expectSteadyWorkload re-reads the workload and asserts every steady-state
// observable is unchanged, proving the completed reconcile between the
// snapshot and this check caused no rollout churn.
func expectSteadyWorkload(ctx context.Context, before steadyState, namespace, name, podSelector, afterWhat string) {
	dep := &appsv1.Deployment{}
	Expect(env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, dep)).
		To(Succeed(), "read workload %s/%s after %s", namespace, name, afterWhat)
	Expect(dep.Annotations["deployment.kubernetes.io/revision"]).To(Equal(before.revision),
		"%s triggered a new Deployment revision (rollout churn)", afterWhat)
	Expect(replicaSetNames(ctx, namespace, podSelector)).To(Equal(before.replicaSets),
		"%s created or removed ReplicaSets (rollout churn)", afterWhat)
	Expect(podUIDs(ctx, namespace, podSelector)).To(Equal(before.podUIDs),
		"%s restarted or replaced workload pods (rollout churn)", afterWhat)
	Expect(apiequality.Semantic.DeepEqual(dep.Spec.Template, before.template)).To(BeTrue(),
		"%s changed the pod template", afterWhat)
}

// expectAppliedContainerRequests asserts every sampled container of a live pod
// carries exactly the fixture-derived applied requests and no limits (the
// default updateRequestsOnly behavior): no container may keep its previous
// values or lose its requests.
func expectAppliedContainerRequests(containers []corev1.Container, samples []containerSamples, what string) {
	for _, s := range samples {
		cpu, memory := containerRequests(containers, s.name)
		Expect(cpu).NotTo(BeNil(), "%s lost container %s", what, s.name)
		Expect(memory).NotTo(BeNil(), "%s lost container %s memory request", what, s.name)
		Expect(cpu.String()).To(Equal(s.expectedCPU),
			"%s: container %s was not set to its recommended CPU request %s", what, s.name, s.expectedCPU)
		Expect(memory.String()).To(Equal(s.expectedMemory),
			"%s: container %s was not set to its recommended memory request %s", what, s.name, s.expectedMemory)
		Expect(cpu.String()).NotTo(Equal(s.initialCPU),
			"%s: container %s was left with its previous CPU request", what, s.name)
		Expect(memory.String()).NotTo(Equal(s.initialMemory),
			"%s: container %s was left with its previous memory request", what, s.name)
		cpuLimit, memLimit := containerLimits(containers, s.name)
		Expect(cpuLimit).To(BeNil(),
			"%s: container %s was given a CPU limit the default updateRequestsOnly behavior must not add", what, s.name)
		Expect(memLimit).To(BeNil(),
			"%s: container %s was given a memory limit the default updateRequestsOnly behavior must not add", what, s.name)
	}
}
