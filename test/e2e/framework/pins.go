// Package framework provides the test-only harness that owns a private,
// ephemeral Kind cluster for OptiPod's end-to-end smoke and conformance suites.
// It never reads or mutates the developer's default kubeconfig or current
// context, and it only ever creates or deletes a collision-resistant cluster
// that this run demonstrably owns.
package framework

import (
	"fmt"
	"runtime"
)

// Pinned tool and image inputs for the hermetic Kind harness. The test path must
// never request a floating tag such as "latest"; every external input is pinned
// here so preflight can verify it and diagnostics can record it.
const (
	// KindCLIVersion is the only Kind CLI version this harness is validated with.
	KindCLIVersion = "v0.31.0"
	// NodeImageRepo and NodeImageTag identify the pinned Kubernetes node image.
	NodeImageRepo = "kindest/node"
	NodeImageTag  = "v1.35.0"
	// NodeImageDigest pins the node image content so a mutated tag cannot be used.
	NodeImageDigest = "sha256:4613778f3cfcd10e615029370f5786704559103cf27bef934597ba562b269661"
	// KustomizeVersion is the pinned manifest renderer.
	KustomizeVersion = "v5.8.1"
	// ClusterNamePrefix identifies clusters created by this harness. Cleanup and
	// focused reuse refuse any cluster whose name lacks this prefix.
	ClusterNamePrefix = "optipod-e2e-"

	// ControllerImageRepo is the local-only repository name the harness tags the
	// freshly built production controller image with. It is built from the
	// checkout and loaded into the owned cluster; it is never pushed or pulled
	// from a registry, so its pin is the checkout itself plus the pinned
	// production Dockerfile base images.
	ControllerImageRepo = "optipod-e2e/controller"
	// MetricsFixtureImageRepo is the local-only repository name for the
	// deterministic Prometheus-compatible fixture image, built from
	// test/e2e/metricsfixture with the same pinned base images as the
	// production Dockerfile.
	MetricsFixtureImageRepo = "optipod-e2e/metrics-fixture"
	// WorkloadImageRepo/Tag/Digest pin the inert workload container image used
	// by per-spec Deployment/StatefulSet/DaemonSet fixtures. The digest selects
	// the multi-architecture manifest list so amd64 and arm64 hosts resolve the
	// same content.
	WorkloadImageRepo   = "registry.k8s.io/pause"
	WorkloadImageTag    = "3.10"
	WorkloadImageDigest = "sha256:ee6521f290b2168b6e0935a181d4cff9be1ac3f505666ef0e3c98fae8199917a"

	// SystemNamespace is the namespace the shipped kustomize configuration
	// installs the controller into (config/default sets namespace/namePrefix).
	SystemNamespace = "optipod-system"
	// ControllerDeploymentName and ControllerServiceAccountName are the shipped,
	// name-prefixed identities the harness verifies after install.
	ControllerDeploymentName     = "optipod-controller-manager"
	ControllerServiceAccountName = "optipod-controller-manager"
	// ControllerClusterRoleName is the name-prefixed ClusterRole config/default
	// installs for the controller (config/rbac/role.yaml metadata.name
	// "manager-role" plus the default kustomization's "optipod-" namePrefix).
	// Admission specs read it to prove the installed role grants the reads the
	// deployed mutator needs.
	ControllerClusterRoleName = "optipod-manager-role"
	// ControllerContainerName is the shipped manager container name inside
	// the controller pod.
	ControllerContainerName = "manager"
	// ControllerPodSelector matches the shipped controller-manager pods.
	ControllerPodSelector     = "control-plane=controller-manager"
	OptimizationPolicyCRDName = "optimizationpolicies.optipod.optipod.io"
	WebhookServiceName        = "optipod-webhook-service"
	WebhookConfigurationName  = "mutating-webhook-configuration"
	WebhookCertSecretName     = "webhook-server-certs" // pragma: allowlist secret
	WebhookServicePort        = 443
	WebhookServerPort         = 9443
	// WebhookPath is the admission path served by the production webhook server
	// (internal/webhook/server.go registers "/mutate", matching the shipped
	// Helm chart; config/webhook/webhook-config.yaml's "/mutate-v1-pod" predates
	// that handler and is corrected in the rendered harness configuration).
	WebhookPath = "/mutate"
	// PodMutatorWebhookName is the mutating webhook's registered name in the
	// shipped and harness-rendered MutatingWebhookConfiguration; the readiness
	// gate and the admission specs identify the installed webhook by it. (On
	// the pinned Kubernetes, mutations applied at pod CREATE are absorbed into
	// the creator's managedFields entry rather than attributed to the webhook
	// name, so admission itself is proven by observed request values.)
	PodMutatorWebhookName = "pod-resource-mutator.optipod.io"

	// FixtureNamespace and FixtureServiceName locate the deterministic metrics
	// fixture inside the owned cluster.
	FixtureNamespace   = "optipod-e2e-metrics"
	FixtureServiceName = "optipod-metrics-fixture"
	FixturePort        = 9090
	// FixtureServiceAccountName is the fixture pod's own service account. The
	// harness binds it to the shipped metrics-reader ClusterRole so the
	// in-cluster controller-metrics scrape authenticates with exactly the
	// identity shape the shipped ServiceMonitor configures for Prometheus.
	FixtureServiceAccountName = "optipod-metrics-fixture"
	// FixtureMetricsReaderBindingName is the harness ClusterRoleBinding from
	// the fixture service account to the shipped metrics-reader ClusterRole
	// (the shipped manifests define that role and deliberately leave it
	// unbound; binding it is the documented operator step).
	FixtureMetricsReaderBindingName = "optipod-e2e-metrics-fixture-reader"
)

// WorkloadImage returns the fully pinned workload image reference.
func WorkloadImage() string {
	return WorkloadImageRepo + ":" + WorkloadImageTag + "@" + WorkloadImageDigest
}

// FixtureServiceURL returns the in-cluster URL the deployed controller uses to
// reach the metrics fixture. It is deterministic because the fixture Service
// name and namespace are fixed within the owned cluster.
func FixtureServiceURL() string {
	return fmt.Sprintf("http://%s.%s.svc:%d", FixtureServiceName, FixtureNamespace, FixturePort)
}

// NodeImage returns the fully pinned Kind node image reference (tag plus digest).
func NodeImage() string {
	return NodeImageRepo + ":" + NodeImageTag + "@" + NodeImageDigest
}

// SupportedArches lists host architectures the harness is validated on.
var SupportedArches = map[string]bool{"amd64": true, "arm64": true}

// Pin describes a single pinned input for the version/checksum manifest.
type Pin struct {
	Name    string
	Version string
	Digest  string
}

// PinManifest returns the pinned inputs for logging and evidence. It
// intentionally contains no floating references.
func PinManifest() []Pin {
	return []Pin{
		{Name: kindCLIID, Version: KindCLIVersion},
		{Name: "kind-node-image", Version: NodeImageRepo + ":" + NodeImageTag, Digest: NodeImageDigest},
		{Name: "kustomize", Version: KustomizeVersion},
		{Name: "workload-image", Version: WorkloadImageRepo + ":" + WorkloadImageTag, Digest: WorkloadImageDigest},
		{Name: "controller-image", Version: "built from checkout as " + ControllerImageRepo},
		{Name: "metrics-fixture-image", Version: "built from checkout as " + MetricsFixtureImageRepo},
		{Name: "host-arch", Version: runtime.GOARCH},
	}
}
