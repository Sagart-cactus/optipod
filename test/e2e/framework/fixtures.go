package framework

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
)

var dns1123Prefix = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)

// UniqueName returns a collision-resistant DNS-1123-safe name derived from the
// caller's prefix. Specs use it (or GenerateName via the Factory) so no two
// specs ever share a fixed resource identity.
func UniqueName(prefix string) (string, error) {
	if !dns1123Prefix.MatchString(prefix) || len(prefix) > 40 {
		return "", fmt.Errorf("prefix %q is not a valid DNS-1123 label prefix of at most 40 chars", prefix)
	}
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate name suffix: %w", err)
	}
	return prefix + "-" + hex.EncodeToString(buf), nil
}

// Factory creates per-spec resources through the typed clients with strict
// error propagation. Names are server-assigned from unique prefixes
// (GenerateName) so parallel or repeated specs never collide.
type Factory struct {
	Clients *Clients
}

// NewFactory builds a Factory over typed clients derived from the private
// cluster configuration.
func NewFactory(clients *Clients) *Factory {
	return &Factory{Clients: clients}
}

// CreateNamespace creates a uniquely named namespace for one spec. The returned
// object carries the server-assigned name; cleanup is DeleteNamespace.
func (f *Factory) CreateNamespace(ctx context.Context, prefix string) (*corev1.Namespace, error) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: prefix + "-",
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "optipod-e2e-harness",
			},
		},
	}
	created, err := f.Clients.Kube.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("create spec namespace with prefix %q: %w", prefix, err)
	}
	return created, nil
}

// ContainerFixture describes one workload container.
type ContainerFixture struct {
	Name          string
	CPURequest    string // e.g. "100m"
	MemoryRequest string // e.g. "128Mi"
	// WebhookEnabled adds the production optipod.io/webhook-enabled=true label
	// to the pod template, making naturally generated pods eligible for the
	// mutating webhook's object selector. Set it before workload creation.
	WebhookEnabled bool
}

// container builds a typed container using the pinned inert workload image so
// workload pods never trigger an in-cluster registry pull.
func (cf ContainerFixture) container() corev1.Container {
	return corev1.Container{
		Name:  cf.Name,
		Image: WorkloadImage(),
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(cf.CPURequest),
				corev1.ResourceMemory: resource.MustParse(cf.MemoryRequest),
			},
		},
	}
}

// appLabelKey links a workload fixture's selector and pod template labels.
const appLabelKey = "app"

// podTemplate builds the shared pod template for workload fixtures.
func podTemplate(name string, containers []ContainerFixture) corev1.PodTemplateSpec {
	labels := map[string]string{appLabelKey: name}
	specContainers := make([]corev1.Container, 0, len(containers))
	for _, cf := range containers {
		specContainers = append(specContainers, cf.container())
		if cf.WebhookEnabled {
			labels["optipod.io/webhook-enabled"] = "true"
		}
	}
	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: labels},
		Spec: corev1.PodSpec{
			Containers: specContainers,
		},
	}
}

// NewDeploymentFixture builds a typed Deployment. Names must come from
// UniqueName or GenerateName; fixtures never use shared fixed identities.
func NewDeploymentFixture(namespace, name string, replicas int32, containers ...ContainerFixture) *appsv1.Deployment {
	template := podTemplate(name, containers)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{appLabelKey: name}},
			Template: template,
		},
	}
}

// NewStatefulSetFixture builds a typed StatefulSet with a headless Service name
// set for schema validity.
func NewStatefulSetFixture(namespace, name string, replicas int32, containers ...ContainerFixture) *appsv1.StatefulSet {
	template := podTemplate(name, containers)
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &replicas,
			ServiceName: name,
			Selector:    &metav1.LabelSelector{MatchLabels: map[string]string{appLabelKey: name}},
			Template:    template,
		},
	}
}

// NewDaemonSetFixture builds a typed DaemonSet.
func NewDaemonSetFixture(namespace, name string, containers ...ContainerFixture) *appsv1.DaemonSet {
	template := podTemplate(name, containers)
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{appLabelKey: name}},
			Template: template,
		},
	}
}

// PolicyOption customizes a policy fixture.
type PolicyOption func(*optipodv1alpha1.OptimizationPolicy)

// WithStrategy sets the update strategy ("ssa" or "webhook") and, for webhook,
// the rollout strategy ("immediate" or "onNextRestart").
func WithStrategy(strategy, rollout string) PolicyOption {
	return func(p *optipodv1alpha1.OptimizationPolicy) {
		p.Spec.UpdateStrategy.Strategy = strPtr(strategy)
		if rollout != "" {
			p.Spec.UpdateStrategy.RolloutStrategy = strPtr(rollout)
		}
	}
}

// WithDefaultStrategy clears the strategy and rollout pointers so the deployed
// controller exercises its own production defaults (webhook strategy with
// onNextRestart rollout) instead of fixture-explicit values; specs proving the
// default strategy use it so the default is observed live, not hard-coded.
func WithDefaultStrategy() PolicyOption {
	return func(p *optipodv1alpha1.OptimizationPolicy) {
		p.Spec.UpdateStrategy.Strategy = nil
		p.Spec.UpdateStrategy.RolloutStrategy = nil
	}
}

// WithWeight sets the policy weight.
func WithWeight(weight int32) PolicyOption {
	return func(p *optipodv1alpha1.OptimizationPolicy) {
		p.Spec.Weight = &weight
	}
}

// WithResourceBounds overrides the default CPU/memory bounds.
func WithResourceBounds(cpuMin, cpuMax, memMin, memMax string) PolicyOption {
	return func(p *optipodv1alpha1.OptimizationPolicy) {
		p.Spec.ResourceBounds = optipodv1alpha1.ResourceBounds{
			CPU: optipodv1alpha1.ResourceBound{
				Min: resource.MustParse(cpuMin),
				Max: resource.MustParse(cpuMax),
			},
			Memory: optipodv1alpha1.ResourceBound{
				Min: resource.MustParse(memMin),
				Max: resource.MustParse(memMax),
			},
		}
	}
}

// WithLimitsConfig enables limit updates (clearing the fixture's default
// requests-only behavior) with explicit CPU/memory limit multipliers, so
// specs can observe the production limit arithmetic through the installed
// apply path: applied limits must equal the applied requests multiplied by
// these configured factors.
func WithLimitsConfig(cpuMultiplier, memoryMultiplier float64) PolicyOption {
	return func(p *optipodv1alpha1.OptimizationPolicy) {
		p.Spec.UpdateStrategy.UpdateRequestsOnly = false
		p.Spec.UpdateStrategy.LimitConfig = &optipodv1alpha1.LimitConfig{
			CPULimitMultiplier:    &cpuMultiplier,
			MemoryLimitMultiplier: &memoryMultiplier,
		}
	}
}

// PolicyUnstructured converts a typed policy fixture into an unstructured
// object whose wire format preserves explicit false boolean fields the typed
// Go encoding would drop. The one that matters is updateRequestsOnly: it is a
// non-pointer bool with omitempty, so `updateRequestsOnly: false` is dropped
// from typed-client JSON and the API server then re-applies the CRD schema
// default (true) — a limits-enabled policy is unreachable through the typed
// client. A user's YAML apply sends the explicit false and persists it, and
// this helper mirrors exactly that wire format so installed behavior can be
// observed for the configuration users can actually express.
func PolicyUnstructured(policy *optipodv1alpha1.OptimizationPolicy) (*unstructured.Unstructured, error) {
	raw, err := json.Marshal(policy)
	if err != nil {
		return nil, fmt.Errorf("marshal policy fixture: %w", err)
	}
	obj := map[string]interface{}{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("unmarshal policy fixture: %w", err)
	}
	obj["apiVersion"] = optipodv1alpha1.GroupVersion.String()
	obj["kind"] = "OptimizationPolicy"
	spec, ok := obj["spec"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("policy fixture %s/%s marshalled without a spec object",
			policy.Namespace, policy.Name)
	}
	strategy, ok := spec["updateStrategy"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("policy fixture %s/%s marshalled without an updateStrategy object",
			policy.Namespace, policy.Name)
	}
	strategy["updateRequestsOnly"] = policy.Spec.UpdateStrategy.UpdateRequestsOnly
	return &unstructured.Unstructured{Object: obj}, nil
}

// CreatePolicyPreservingExplicitBools creates the named policy fixture through
// the raw/unstructured API path (see PolicyUnstructured), so explicit false
// booleans persist exactly as a user's kubectl apply would send them.
func CreatePolicyPreservingExplicitBools(ctx context.Context, c *Clients, policy *optipodv1alpha1.OptimizationPolicy) error {
	u, err := PolicyUnstructured(policy)
	if err != nil {
		return err
	}
	if err := c.Ctrl.Create(ctx, u); err != nil {
		return fmt.Errorf("create policy %s/%s through the raw API path: %w",
			policy.Namespace, policy.Name, err)
	}
	return nil
}

// WithWorkloadSelector restricts the policy to workloads with matching labels.
func WithWorkloadSelector(matchLabels map[string]string) PolicyOption {
	return func(p *optipodv1alpha1.OptimizationPolicy) {
		p.Spec.Selector.WorkloadSelector = &metav1.LabelSelector{MatchLabels: matchLabels}
	}
}

// WithNamespaceFilter overrides the namespace allow/deny lists. An empty allow
// list together with a non-nil filter clears the fixture's default allow list
// so another selector (for example WithNamespaceSelector) alone governs
// matching; a deny entry always takes precedence over allow.
func WithNamespaceFilter(allow, deny []string) PolicyOption {
	return func(p *optipodv1alpha1.OptimizationPolicy) {
		p.Spec.Selector.Namespaces = &optipodv1alpha1.NamespaceFilter{
			Allow: allow,
			Deny:  deny,
		}
	}
}

// WithNamespaceSelector restricts the policy to namespaces carrying the given
// labels.
func WithNamespaceSelector(matchLabels map[string]string) PolicyOption {
	return func(p *optipodv1alpha1.OptimizationPolicy) {
		p.Spec.Selector.NamespaceSelector = &metav1.LabelSelector{MatchLabels: matchLabels}
	}
}

// WithWorkloadTypes sets the include/exclude workload-type filters. Production
// selection applies exclude with precedence over include, and an omitted
// filter means all three kinds are eligible.
func WithWorkloadTypes(include, exclude []optipodv1alpha1.WorkloadType) PolicyOption {
	return func(p *optipodv1alpha1.OptimizationPolicy) {
		p.Spec.Selector.WorkloadTypes = &optipodv1alpha1.WorkloadTypeFilter{
			Include: include,
			Exclude: exclude,
		}
	}
}

// WithReconciliationInterval sets the policy's supported requeue interval so
// installed-controller scenarios observe fresh reconciles within their bounded
// waits instead of relying on long fixed sleeps or repeated manual triggers.
func WithReconciliationInterval(d time.Duration) PolicyOption {
	return func(p *optipodv1alpha1.OptimizationPolicy) {
		p.Spec.ReconciliationInterval = metav1.Duration{Duration: d}
	}
}

// NewPolicyFixture builds a minimal valid OptimizationPolicy selecting the
// given namespace (via the allow list) against the Prometheus provider. All
// schema-required fields are populated; options refine the fixture.
func NewPolicyFixture(namespace, name string, mode optipodv1alpha1.PolicyMode, opts ...PolicyOption) *optipodv1alpha1.OptimizationPolicy {
	policy := &optipodv1alpha1.OptimizationPolicy{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec: optipodv1alpha1.OptimizationPolicySpec{
			Mode: mode,
			Selector: optipodv1alpha1.WorkloadSelector{
				Namespaces: &optipodv1alpha1.NamespaceFilter{Allow: []string{namespace}},
			},
			MetricsConfig: optipodv1alpha1.MetricsConfig{
				Provider: "prometheus",
			},
			ResourceBounds: optipodv1alpha1.ResourceBounds{
				CPU: optipodv1alpha1.ResourceBound{
					Min: resource.MustParse("50m"),
					Max: resource.MustParse("4"),
				},
				Memory: optipodv1alpha1.ResourceBound{
					Min: resource.MustParse("64Mi"),
					Max: resource.MustParse("8Gi"),
				},
			},
			UpdateStrategy: optipodv1alpha1.UpdateStrategy{
				Strategy: strPtr(string(optipodv1alpha1.StrategyWebhook)),
			},
		},
	}
	for _, opt := range opts {
		opt(policy)
	}
	return policy
}

func strPtr(s string) *string { return &s }

// NormalizePrefix lowercases and sanitizes a spec label (e.g. a Ginkgo spec
// name fragment) into a DNS-1123-safe prefix for UniqueName/GenerateName.
func NormalizePrefix(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	lastSep := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastSep = false
		default:
			if !lastSep {
				b.WriteRune('-')
				lastSep = true
			}
		}
	}
	out := strings.Trim(b.String(), "-.")
	if len(out) > 40 {
		out = strings.Trim(out[:40], "-.")
	}
	if out == "" {
		out = "spec"
	}
	return out
}
