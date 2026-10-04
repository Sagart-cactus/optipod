package framework

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultRenderTimeout = 2 * time.Minute

// RenderOptions configures a one-shot manifest render into a temporary
// directory. No tracked file under the repository's config/ tree is modified;
// every override (image, metrics endpoint, webhook CA) is applied to temporary
// copies through kustomize.
type RenderOptions struct {
	// RepoRoot is the repository checkout root (contains config/).
	RepoRoot string
	// WorkDir receives the generated overlay and rendered manifest. A new
	// temporary directory is created when empty.
	WorkDir string
	// KustomizePath overrides the kustomize binary; defaults to PATH lookup.
	KustomizePath string
	// Run overrides command execution (tests).
	Run commandRunner

	// ControllerImageRef is the uniquely tagged locally built controller image.
	ControllerImageRef string
	// FixtureImageRef is the uniquely tagged locally built fixture image.
	FixtureImageRef string
	// WebhookCAPEM is the PEM test CA the API server uses to verify the
	// webhook serving certificate; it becomes the caBundle.
	WebhookCAPEM []byte
	// ReconciliationInterval overrides the shipped 5m interval so smoke
	// scenarios observe reconciles promptly. Defaults to 10s.
	ReconciliationInterval string
}

// RenderedManifests locates the rendered output. Dir and Path are temporary
// paths; nothing under the repository checkout is written.
type RenderedManifests struct {
	// Dir is the temporary overlay directory.
	Dir string
	// Path is the rendered all-in-one manifest applied to the cluster.
	Path string
}

// RenderManifests composes a temporary kustomize overlay over the shipped
// config/default tree plus harness webhook/fixture manifests and renders it to
// a single temporary manifest file. The overlay:
//   - retags the controller image to the freshly built unique tag (matching the
//     image under both its original "controller" name and the tracked
//     "optipod-controller" override so no source-tree edit is needed),
//   - applies the shipped manager webhook patch (args + serving-cert volume),
//   - points the deployed controller at the in-cluster metrics fixture through
//     the supported optipod-config ConfigMap keys,
//   - installs the webhook Service and MutatingWebhookConfiguration with the
//     harness CA bundle, the production admission path and shipped selectors.
func RenderManifests(ctx context.Context, opts RenderOptions) (*RenderedManifests, error) {
	if opts.RepoRoot == "" {
		return nil, fmt.Errorf("render: RepoRoot is required")
	}
	if opts.ControllerImageRef == "" || opts.FixtureImageRef == "" {
		return nil, fmt.Errorf("render: ControllerImageRef and FixtureImageRef are required")
	}
	if _, _, err := splitImageRef(opts.ControllerImageRef); err != nil {
		return nil, fmt.Errorf("render: controller image: %w", err)
	}
	if _, _, err := splitImageRef(opts.FixtureImageRef); err != nil {
		return nil, fmt.Errorf("render: fixture image: %w", err)
	}
	if len(opts.WebhookCAPEM) == 0 {
		return nil, fmt.Errorf("render: WebhookCAPEM is required")
	}
	interval := opts.ReconciliationInterval
	if interval == "" {
		interval = "10s"
	}

	dir := opts.WorkDir
	if dir == "" {
		tmp, err := os.MkdirTemp("", "optipod-e2e-render-")
		if err != nil {
			return nil, fmt.Errorf("render: create temp dir: %w", err)
		}
		dir = tmp
	}
	// macOS temp dirs are symlinked (/var -> /private/var); kustomize evaluates
	// symlinks on base paths, so resolve both ends before computing the
	// relative path from the overlay to the shipped config tree.
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, fmt.Errorf("render: resolve temp dir: %w", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(opts.RepoRoot)
	if err != nil {
		return nil, fmt.Errorf("render: resolve repo root: %w", err)
	}
	defaultRel, err := filepath.Rel(resolvedDir, filepath.Join(resolvedRoot, "config", "default"))
	if err != nil {
		return nil, fmt.Errorf("render: relativize config/default: %w", err)
	}
	webhookPatchRel, err := filepath.Rel(resolvedDir,
		filepath.Join(resolvedRoot, "config", "webhook-enabled", "manager_webhook_patch.yaml"))
	if err != nil {
		return nil, fmt.Errorf("render: relativize manager webhook patch: %w", err)
	}

	files := map[string]string{
		"kustomization.yaml": overlayKustomization(defaultRel, webhookPatchRel,
			opts.ControllerImageRef, interval),
		"webhook-service.yaml":       webhookServiceManifest(),
		"webhook-configuration.yaml": webhookConfigurationManifest(opts.WebhookCAPEM),
		"metrics-fixture.yaml":       metricsFixtureManifest(opts.FixtureImageRef),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			return nil, fmt.Errorf("render: write %s: %w", name, err)
		}
	}

	kustomize := opts.KustomizePath
	if kustomize == "" {
		kustomize = "kustomize"
	}
	run := opts.Run
	if run == nil {
		run = execRunner
	}
	renderedPath := filepath.Join(dir, "rendered.yaml")
	cctx, cancel := context.WithTimeout(ctx, defaultRenderTimeout)
	defer cancel()
	out, err := run(cctx, kustomize, "build", dir,
		"--load-restrictor=LoadRestrictionsNone", "-o", renderedPath)
	if err != nil {
		return nil, fmt.Errorf("render: kustomize build: %w (%s)", err, tailOutput(string(out)))
	}
	return &RenderedManifests{Dir: dir, Path: renderedPath}, nil
}

// splitImageRef splits a repo:tag reference produced by ImageBuilder.
func splitImageRef(ref string) (name, tag string, err error) {
	idx := strings.LastIndex(ref, ":")
	if idx <= 0 || idx == len(ref)-1 {
		return "", "", fmt.Errorf("image reference %q is not in name:tag form", ref)
	}
	return ref[:idx], ref[idx+1:], nil
}

func overlayKustomization(defaultRel, webhookPatchRel, controllerImageRef, interval string) string {
	// RenderManifests validates the reference before this is reachable.
	imageName, imageTag, _ := splitImageRef(controllerImageRef)
	return fmt.Sprintf(`apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
- %s
- webhook-service.yaml
- webhook-configuration.yaml
- metrics-fixture.yaml

# Retag the controller image to the freshly built unique tag. Both names are
# listed: the shipped config/manager kustomization currently carries a tracked
# override to "optipod-controller", and kustomize image matching applies to the
# current name.
images:
- name: controller
  newName: %s
  newTag: %s
- name: optipod-controller
  newName: %s
  newTag: %s

patches:
# Enable the production in-process webhook server (shipped patch).
- target:
    kind: Deployment
    name: controller-manager
    namespace: system
  path: %s
# Point the deployed controller at the in-cluster deterministic metrics
# fixture through the supported optipod-config ConfigMap keys, and shorten the
# reconciliation interval so smoke scenarios observe reconciles promptly.
- target:
    kind: ConfigMap
    name: optipod-config
    namespace: system
  patch: |-
    apiVersion: v1
    kind: ConfigMap
    metadata:
      name: optipod-config
      namespace: system
    data:
      prometheus-url: %q
      reconciliation-interval: %q
`, defaultRel, imageName, imageTag, imageName, imageTag, webhookPatchRel,
		FixtureServiceURL(), interval)
}

// webhookServiceManifest mirrors the shipped config/webhook/service.yaml shape
// (443 -> 9443) but selects the controller-manager pods, because the harness
// installs the production in-process webhook server (the shipped standalone
// webhook Deployment references flags the current manager binary does not
// implement).
func webhookServiceManifest() string {
	return fmt.Sprintf(`apiVersion: v1
kind: Service
metadata:
  name: %s
  namespace: %s
  labels:
    control-plane: webhook-server
    app.kubernetes.io/name: optipod
    app.kubernetes.io/managed-by: kustomize
spec:
  selector:
    control-plane: controller-manager
    app.kubernetes.io/name: optipod
  ports:
  - name: webhook
    port: %d
    targetPort: %d
    protocol: TCP
  type: ClusterIP
`, WebhookServiceName, SystemNamespace, WebhookServicePort, WebhookServerPort)
}

// webhookConfigurationManifest mirrors the shipped
// config/webhook/webhook-config.yaml (webhook name, rules, namespace and object
// selectors, failurePolicy, timeout, reinvocationPolicy) with deliberate
// harness corrections: the service reference targets the installed
// service/namespace concretely, the admission path is the production "/mutate"
// handler (the shipped kustomize copy's "/mutate-v1-pod" predates it), and the
// caBundle carries the harness test CA in place of cert-manager injection.
func webhookConfigurationManifest(caPEM []byte) string {
	caBundle := base64.StdEncoding.EncodeToString(caPEM)
	return fmt.Sprintf(`apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: %s
  labels:
    app.kubernetes.io/name: optipod
    app.kubernetes.io/managed-by: kustomize
webhooks:
- name: pod-resource-mutator.optipod.io
  clientConfig:
    service:
      name: %s
      namespace: %s
      path: %s
      port: %d
    caBundle: %s
  rules:
  - operations: ["CREATE"]
    apiGroups: [""]
    apiVersions: ["v1"]
    resources: ["pods"]
  namespaceSelector:
    matchExpressions:
    - key: name
      operator: NotIn
      values: ["kube-system", "kube-public", "kube-node-lease"]
    - key: control-plane
      operator: DoesNotExist
  objectSelector:
    matchExpressions:
    - key: optipod.io/webhook-enabled
      operator: In
      values: ["true"]
  admissionReviewVersions: ["v1", "v1beta1"]
  sideEffects: None
  failurePolicy: Ignore
  timeoutSeconds: 10
  reinvocationPolicy: Never
`, WebhookConfigurationName, WebhookServiceName, SystemNamespace,
		WebhookPath, WebhookServicePort, caBundle)
}

// metricsFixtureManifest deploys the deterministic Prometheus-compatible
// fixture behind a ClusterIP Service inside the owned cluster.
func metricsFixtureManifest(fixtureImageRef string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %s
  labels:
    app.kubernetes.io/name: optipod
    app.kubernetes.io/component: metrics-fixture
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  namespace: %s
  labels:
    app.kubernetes.io/name: optipod
    app.kubernetes.io/component: metrics-fixture
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/component: metrics-fixture
  template:
    metadata:
      labels:
        app.kubernetes.io/component: metrics-fixture
    spec:
      securityContext:
        runAsNonRoot: true
        seccompProfile:
          type: RuntimeDefault
      containers:
      - name: fixture
        image: %s
        ports:
        - containerPort: %d
          name: http
          protocol: TCP
        securityContext:
          readOnlyRootFilesystem: true
          allowPrivilegeEscalation: false
          capabilities:
            drop:
            - "ALL"
        readinessProbe:
          httpGet:
            path: /readyz
            port: %d
          initialDelaySeconds: 1
          periodSeconds: 2
        resources:
          limits:
            cpu: 100m
            memory: 64Mi
          requests:
            cpu: 10m
            memory: 16Mi
---
apiVersion: v1
kind: Service
metadata:
  name: %s
  namespace: %s
  labels:
    app.kubernetes.io/name: optipod
    app.kubernetes.io/component: metrics-fixture
spec:
  selector:
    app.kubernetes.io/component: metrics-fixture
  ports:
  - name: http
    port: %d
    targetPort: %d
    protocol: TCP
  type: ClusterIP
`, FixtureNamespace, FixtureServiceName, FixtureNamespace, fixtureImageRef,
		FixturePort, FixturePort, FixtureServiceName, FixtureNamespace,
		FixturePort, FixturePort)
}
