// Package framework provides the test-only harness that owns a private,
// ephemeral Kind cluster for OptiPod's end-to-end smoke and conformance suites.
// It never reads or mutates the developer's default kubeconfig or current
// context, and it only ever creates or deletes a collision-resistant cluster
// that this run demonstrably owns.
package framework

import "runtime"

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
)

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
		{Name: "host-arch", Version: runtime.GOARCH},
	}
}
