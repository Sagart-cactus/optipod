package framework

import (
	"strings"
	"testing"
)

func TestNodeImagePinnedByDigest(t *testing.T) {
	img := NodeImage()
	if !strings.Contains(img, "@sha256:") {
		t.Fatalf("node image %q is not pinned by digest", img)
	}
	if !strings.HasPrefix(img, NodeImageRepo+":"+NodeImageTag+"@") {
		t.Fatalf("node image %q does not carry the pinned tag", img)
	}
}

func TestPinManifestHasNoFloatingReferences(t *testing.T) {
	for _, p := range PinManifest() {
		if strings.Contains(strings.ToLower(p.Version), "latest") {
			t.Errorf("pin %q uses a floating reference: %q", p.Name, p.Version)
		}
		if p.Version == "" {
			t.Errorf("pin %q has an empty version", p.Name)
		}
	}
}

func TestPinnedVersionsMatchContract(t *testing.T) {
	if KindCLIVersion != "v0.31.0" {
		t.Errorf("Kind CLI pin drifted: %q", KindCLIVersion)
	}
	if NodeImageTag != "v1.35.0" {
		t.Errorf("node image tag pin drifted: %q", NodeImageTag)
	}
	if NodeImageDigest != "sha256:4613778f3cfcd10e615029370f5786704559103cf27bef934597ba562b269661" {
		t.Errorf("node image digest pin drifted: %q", NodeImageDigest)
	}
}
