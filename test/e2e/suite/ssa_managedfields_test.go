package suite

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/application"
	"github.com/optipod/optipod/test/e2e/framework"
)

// These scenarios prove the deployed controller's explicit strategy:ssa apply
// path at the field-ownership level (VAL-SMOKE-003): on an eligible installed
// workload, the intended resource fields change to the fixture-derived values
// AND the live object's managedFields show the production "optipod" field
// manager owning exactly the per-container request leaves it applied, while
// unrelated user-owned fields persist byte-identical and outside that
// ownership. A rendered manifest, a hand-built patch object, or the mutator
// invoked directly cannot pass: the ownership is read back from the real
// Deployment's managedFields through the private-config API.
var _ = Describe("Explicit SSA managed-field ownership on the installed workload", Label("smoke", "ssa"), func() {
	const waitTimeout = 3 * time.Minute

	It("applies intended request fields under the optipod field manager and preserves unrelated fields", Label("managed-fields"), func(ctx context.Context) {
		ns := specNamespace(ctx, "smoke-ssa-managedfields")
		DeferCleanup(func(ctx context.Context) {
			Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed(),
				"clean up the SSA managed-fields spec namespace")
		})
		samples := smokeSamples()

		By("creating a two-container workload carrying unrelated user-owned fields")
		workloadName, err := framework.UniqueName("workload")
		Expect(err).NotTo(HaveOccurred(), "generate unique SSA workload name")
		workload := framework.NewDeploymentFixture(ns.Name, workloadName, 1, smokeContainers(samples)...)
		enrichWithUnrelatedUserFields(workload)
		Expect(env.Clients.Ctrl.Create(ctx, workload)).To(Succeed(), "create the SSA workload deployment")
		Expect(framework.WaitForDeploymentAvailable(ctx, env.Clients, ns.Name, workloadName,
			framework.WaitOptions{Timeout: waitTimeout})).To(Succeed(),
			"the SSA workload never became Available")
		_, err = framework.WaitForPodCountReady(ctx, env.Clients, ns.Name, "app="+workloadName, 1,
			framework.WaitOptions{Timeout: waitTimeout})
		Expect(err).NotTo(HaveOccurred(), "the SSA workload never had a ready pod")

		before := &appsv1.Deployment{}
		Expect(env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: workloadName}, before)).
			To(Succeed(), "read the workload before the policy exists")

		By("registering deterministic metric samples for the workload's pod")
		series := registerContainerSeries(ctx, ns.Name, workload, samples)
		Expect(env.Fixture.ClearTrace(ctx)).To(Succeed(), "reset the fixture trace for this spec")

		By("creating an Auto policy with the explicit SSA strategy")
		policyName, err := framework.UniqueName("policy")
		Expect(err).NotTo(HaveOccurred(), "generate unique SSA policy name")
		policy := framework.NewPolicyFixture(ns.Name, policyName, optipodv1alpha1.ModeAuto,
			framework.WithStrategy("ssa", ""),
			framework.WithReconciliationInterval(30*time.Second))
		Expect(env.Clients.Ctrl.Create(ctx, policy)).To(Succeed(), "create the Auto SSA policy")
		createdAt := policy.CreationTimestamp.Time

		By("waiting for the exact per-container request values to be applied")
		waitForDeploymentRequests(ctx, ns.Name, workloadName, smokeApp.name,
			smokeApp.expectedCPU, smokeApp.expectedMemory)
		waitForDeploymentRequests(ctx, ns.Name, workloadName, smokeSidecar.name,
			smokeSidecar.expectedCPU, smokeSidecar.expectedMemory)

		By("reading the applied workload back through the private-config API")
		applied := &appsv1.Deployment{}
		Expect(env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: workloadName}, applied)).
			To(Succeed(), "read the applied SSA workload")
		Expect(applied.ManagedFields).NotTo(BeEmpty(), "the applied workload carries no managedFields at all")

		By("asserting both containers' requests changed to the exact recommended values")
		for _, s := range samples {
			cpu, memory := containerRequests(applied.Spec.Template.Spec.Containers, s.name)
			Expect(cpu).NotTo(BeNil(), "applied workload lost container %s", s.name)
			Expect(memory).NotTo(BeNil(), "applied workload lost container %s memory request", s.name)
			Expect(cpu.String()).To(Equal(s.expectedCPU),
				"SSA did not apply the expected CPU request to container %s", s.name)
			Expect(memory.String()).To(Equal(s.expectedMemory),
				"SSA did not apply the expected memory request to container %s", s.name)
			Expect(cpu.String()).NotTo(Equal(s.initialCPU),
				"container %s requests were left at their initial value", s.name)
			cpuLimit, memLimit := containerLimits(applied.Spec.Template.Spec.Containers, s.name)
			Expect(cpuLimit).To(BeNil(),
				"the default updateRequestsOnly=true behavior added a CPU limit to %s", s.name)
			Expect(memLimit).To(BeNil(),
				"the default updateRequestsOnly=true behavior added a memory limit to %s", s.name)
		}

		By("asserting the recommendation and management annotations on the applied workload")
		expectManagementAnnotations(applied, policy, createdAt)
		for _, s := range samples {
			for _, kind := range []struct {
				suffix string
				value  string
			}{
				{"cpu-request", s.expectedCPU},
				{"memory-request", s.expectedMemory},
			} {
				key := fmt.Sprintf("%s.%s.%s", optipodv1alpha1.AnnotationRecommendationPrefix, s.name, kind.suffix)
				Expect(applied.Annotations).To(HaveKeyWithValue(key, kind.value),
					"applied workload recommendation annotation %q is wrong", key)
			}
		}

		By("asserting unrelated user-owned fields persisted byte-identical")
		Expect(applied.Annotations[unrelatedAnnotationKey]).To(Equal(unrelatedAnnotationValue),
			"the SSA apply or the controller dropped the user-owned deployment annotation %q", unrelatedAnnotationKey)
		Expect(applied.Labels[unrelatedLabelKey]).To(Equal(unrelatedLabelValue),
			"the SSA apply or the controller dropped the user-owned deployment label %q", unrelatedLabelKey)
		Expect(applied.Spec.Replicas).NotTo(BeNil(), "the applied workload lost its replica count")
		Expect(*applied.Spec.Replicas).To(Equal(int32(1)), "SSA changed the workload replica count")
		Expect(applied.Spec.RevisionHistoryLimit).To(Equal(before.Spec.RevisionHistoryLimit),
			"SSA pruned the unrelated revisionHistoryLimit field")
		Expect(applied.Spec.ProgressDeadlineSeconds).To(Equal(before.Spec.ProgressDeadlineSeconds),
			"SSA pruned the unrelated progressDeadlineSeconds field")
		Expect(applied.Spec.Strategy).To(Equal(before.Spec.Strategy),
			"SSA pruned the unrelated deployment strategy fields")
		template := applied.Spec.Template
		Expect(template.Labels["app"]).To(Equal(workloadName),
			"SSA pruned the pod template selector labels")
		Expect(template.Annotations[unrelatedTemplateAnnotationKey]).To(Equal(unrelatedTemplateAnnotationVal),
			"the SSA apply dropped the user-owned pod template annotation %q", unrelatedTemplateAnnotationKey)
		Expect(template.Spec.TerminationGracePeriodSeconds).NotTo(BeNil(),
			"SSA pruned the pod template terminationGracePeriodSeconds")
		Expect(*template.Spec.TerminationGracePeriodSeconds).To(Equal(int64(unrelatedGraceSeconds)),
			"SSA changed the pod template terminationGracePeriodSeconds")
		appIdx := containerIndex(template.Spec.Containers, smokeApp.name)
		Expect(appIdx).NotTo(Equal(-1), "the applied pod template lost the app container")
		appContainer := template.Spec.Containers[appIdx]
		Expect(appContainer.Image).To(Equal(framework.WorkloadImage()),
			"SSA replaced the container image, an unrelated field")
		gotEnv, hasEnv := envValue(appContainer.Env, unrelatedEnvKey)
		Expect(hasEnv).To(BeTrue(), "SSA or the controller dropped the user-owned container env %q", unrelatedEnvKey)
		Expect(gotEnv).To(Equal(unrelatedEnvValue),
			"the user-owned container env %q changed", unrelatedEnvKey)

		By("asserting observable managed-field ownership by the optipod Server-Side Apply")
		applyEntries := optipodApplyEntries(applied)
		Expect(applyEntries).NotTo(BeEmpty(),
			"the workload has no managedFields entry from field manager %q created by Server-Side Apply; "+
				"the request values were not applied through the production SSA path",
			application.FieldManagerName)
		required := map[string]bool{}
		for _, s := range samples {
			required[ssaRequestLeaf(s.name, "cpu")] = true
			required[ssaRequestLeaf(s.name, "memory")] = true
		}
		for _, entry := range applyEntries {
			Expect(entry.Subresource).To(BeEmpty(),
				"the optipod Server-Side Apply entry owns status subresource fields")
			leaves, err := managedFieldLeaves(entry)
			Expect(err).NotTo(HaveOccurred(), "decode the optipod apply managed-fields set")
			for _, leaf := range leaves {
				delete(required, leaf)
				for _, fragment := range unrelatedLeafFragments {
					Expect(leaf).NotTo(ContainSubstring(fragment),
						"the optipod Server-Side Apply owns the unrelated field path %q", leaf)
				}
			}
		}
		Expect(required).To(BeEmpty(),
			"the optipod Server-Side Apply does not own the required request leaves %v", sortedKeys(required))

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

// Unrelated user-owned field constants: every one of these values is set
// before the policy exists and must survive the SSA apply untouched, proving
// Server-Side Apply under the "optipod" manager changed only intended fields.
const (
	unrelatedAnnotationKey         = "team.example.com/owner"
	unrelatedAnnotationValue       = "platform-team"
	unrelatedLabelKey              = "team.example.com/tier"
	unrelatedLabelValue            = "unrelated"
	unrelatedTemplateAnnotationKey = "team.example.com/template-owner"
	unrelatedTemplateAnnotationVal = "keep-me"
	unrelatedEnvKey                = "PROBE_USER_FIELD"
	unrelatedEnvValue              = "unrelated-user-field"
	unrelatedGraceSeconds          = 7
)

// unrelatedLeafFragments are field-path fragments the optipod Server-Side
// Apply must never own: the production SSA patch carries only per-container
// resource requests, so owning any of these would mean the apply stole or
// wrote unrelated fields (the old per-container-applies defect shape).
var unrelatedLeafFragments = []string{
	"/image", "/env", "/command", "/args", "/ports", "/probes",
	"/limits", "/replicas", "/labels", "/annotations",
	"/terminationGracePeriodSeconds", "/restartPolicy", "/schedulerName", "/tolerations",
}

// enrichWithUnrelatedUserFields adds deployment- and pod-template-level fields
// no OptiPod component is expected to own or modify: metadata annotations and
// labels, a pod-template annotation, terminationGracePeriodSeconds and a
// container environment variable. The managed-field assertions above require
// every one of these values to still be present afterwards.
func enrichWithUnrelatedUserFields(dep *appsv1.Deployment) {
	if dep.Annotations == nil {
		dep.Annotations = map[string]string{}
	}
	dep.Annotations[unrelatedAnnotationKey] = unrelatedAnnotationValue
	if dep.Labels == nil {
		dep.Labels = map[string]string{}
	}
	dep.Labels[unrelatedLabelKey] = unrelatedLabelValue
	template := &dep.Spec.Template
	if template.Annotations == nil {
		template.Annotations = map[string]string{}
	}
	template.Annotations[unrelatedTemplateAnnotationKey] = unrelatedTemplateAnnotationVal
	grace := int64(unrelatedGraceSeconds)
	template.Spec.TerminationGracePeriodSeconds = &grace
	idx := containerIndex(template.Spec.Containers, smokeApp.name)
	Expect(idx).NotTo(Equal(-1), "the fixture lost the %s container before enrichment", smokeApp.name)
	template.Spec.Containers[idx].Env = append(template.Spec.Containers[idx].Env, corev1.EnvVar{
		Name:  unrelatedEnvKey,
		Value: unrelatedEnvValue,
	})
}

// containerIndex returns the position of the named container, or -1.
func containerIndex(containers []corev1.Container, name string) int {
	for i := range containers {
		if containers[i].Name == name {
			return i
		}
	}
	return -1
}

// envValue returns the value of the named env var and whether it exists.
func envValue(envs []corev1.EnvVar, key string) (string, bool) {
	for _, env := range envs {
		if env.Name == key {
			return env.Value, true
		}
	}
	return "", false
}

// ssaRequestLeaf is the normalized managed-fields leaf path the optipod SSA
// apply must own for one container's applied request on a Deployment.
func ssaRequestLeaf(container, resource string) string {
	return fmt.Sprintf("spec/template/spec/containers[name=%s]/resources/requests/%s", container, resource)
}

// optipodApplyEntries returns the managedFields entries the production
// "optipod" field manager created through Server-Side Apply (the strategy:ssa
// application path).
func optipodApplyEntries(dep *appsv1.Deployment) []metav1.ManagedFieldsEntry {
	var entries []metav1.ManagedFieldsEntry
	for _, entry := range dep.ManagedFields {
		if entry.Manager == application.FieldManagerName && entry.Operation == metav1.ManagedFieldsOperationApply {
			entries = append(entries, entry)
		}
	}
	return entries
}

// managedFieldLeaves decodes a managedFields entry's FieldsV1 set into
// normalized leaf paths, e.g. spec/template/spec/containers[name=app]/resources/requests/cpu.
// "f:" map keys become path segments, associative list keys ("k:{\"name\":\"app\"}")
// become [name=app] segments and positional keys ("i:0") become [0] segments;
// only leaves (empty sets) are returned, sorted for stable failure output.
func managedFieldLeaves(entry metav1.ManagedFieldsEntry) ([]string, error) {
	if entry.FieldsV1 == nil {
		return nil, fmt.Errorf("managedFields entry of manager %q carries no FieldsV1 set", entry.Manager)
	}
	var set map[string]interface{}
	if err := json.Unmarshal(entry.FieldsV1.GetRawBytes(), &set); err != nil {
		return nil, fmt.Errorf("decode managed-fields set of manager %q: %w", entry.Manager, err)
	}
	leaves := make([]string, 0)
	var walk func(prefix string, node map[string]interface{})
	walk = func(prefix string, node map[string]interface{}) {
		keys := make([]string, 0, len(node))
		for key := range node {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			child, ok := node[key].(map[string]interface{})
			if !ok {
				continue
			}
			var segment string
			switch {
			case strings.HasPrefix(key, "f:"):
				segment = strings.TrimPrefix(key, "f:")
			case strings.HasPrefix(key, "k:"):
				segment = keySetSegment(strings.TrimPrefix(key, "k:"))
			case strings.HasPrefix(key, "i:"):
				segment = "[" + strings.TrimPrefix(key, "i:") + "]"
			default:
				continue
			}
			path := prefix + "/" + segment
			if prefix == "" || strings.HasPrefix(segment, "[") {
				// List-key segments extend their parent path (containers[name=app])
				// instead of opening a new slash-separated component.
				path = prefix + segment
			}
			if len(child) == 0 {
				leaves = append(leaves, path)
				continue
			}
			walk(path, child)
		}
	}
	walk("", set)
	return leaves, nil
}

// keySetSegment renders an associative-list key's JSON object ({"name":"app"})
// as a [name=app] path segment with sorted key=value parts.
func keySetSegment(rawKey string) string {
	var kv map[string]interface{}
	if err := json.Unmarshal([]byte(rawKey), &kv); err != nil || len(kv) == 0 {
		return "[" + rawKey + "]"
	}
	parts := make([]string, 0, len(kv))
	for key, value := range kv {
		parts = append(parts, fmt.Sprintf("%s=%v", key, value))
	}
	sort.Strings(parts)
	return "[" + strings.Join(parts, ",") + "]"
}

// sortedKeys returns the sorted keys of a string-keyed set for deterministic
// failure messages.
func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
