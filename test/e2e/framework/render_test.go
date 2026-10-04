package framework

import (
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestRenderManifestsProducesInstallableOverlay renders the real shipped
// config tree with the pinned kustomize binary into a temporary directory. It
// proves the harness gets the intended deployed state without touching tracked
// files: the uniquely tagged controller image, the fixture-backed Prometheus
// URL, the shipped webhook patch, and the harness CA bundle.
func TestRenderManifestsProducesInstallableOverlay(t *testing.T) {
	if _, err := exec.LookPath("kustomize"); err != nil {
		t.Skip("kustomize not on PATH")
	}
	cert, err := GenerateWebhookServingCert(WebhookServiceName, SystemNamespace)
	if err != nil {
		t.Fatal(err)
	}

	workDir := t.TempDir()
	rendered, err := RenderManifests(context.Background(), RenderOptions{
		RepoRoot:           DefaultRepoRoot(),
		WorkDir:            workDir,
		ControllerImageRef: "optipod-e2e/controller:e2e-test-tag",
		FixtureImageRef:    "optipod-e2e/metrics-fixture:e2e-test-tag",
		WebhookCAPEM:       cert.CAPEM,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rendered.Dir != workDir {
		t.Fatalf("rendered outside the temp workdir: %q", rendered.Dir)
	}

	out, err := os.ReadFile(rendered.Path)
	if err != nil {
		t.Fatal(err)
	}
	yaml := string(out)

	checks := []struct {
		name string
		want string
	}{
		{"controller image retagged to the unique build", "image: optipod-e2e/controller:e2e-test-tag"},
		{"fixture image is the unique build", "image: optipod-e2e/metrics-fixture:e2e-test-tag"},
		{"no floating controller:latest remains", ""},
		{"controller points at the in-cluster fixture", "prometheus-url: " + FixtureServiceURL()},
		{"short reconciliation interval for smoke", "reconciliation-interval: 10s"},
		{"shipped webhook patch enables the webhook server", "- --enable-webhook=true"},
		{"serving cert volume mount from shipped patch", "secretName: webhook-server-certs"},
		{"shipped controller service account", "serviceAccountName: optipod-controller-manager"},
		{"webhook service fronts controller pods", "name: " + WebhookServiceName},
		{"webhook object selector matches production", "optipod.io/webhook-enabled"},
		{"production admission path", "path: " + WebhookPath},
		{"fixture namespace", "name: " + FixtureNamespace},
		{"fixture service", "name: " + FixtureServiceName},
	}
	for _, check := range checks {
		if check.want == "" {
			continue
		}
		if !strings.Contains(yaml, check.want) {
			t.Errorf("rendered manifest missing %s (%q)", check.name, check.want)
		}
	}
	if strings.Contains(yaml, "image: controller:latest") {
		t.Error("rendered manifest still references the unpinned controller:latest")
	}

	// The MutatingWebhookConfiguration must carry the harness CA bundle so the
	// API server can verify the serving certificate.
	wantCA := base64.StdEncoding.EncodeToString(cert.CAPEM)
	if !strings.Contains(yaml, "caBundle: "+wantCA) {
		t.Error("rendered webhook configuration lacks the harness CA bundle")
	}

	// The tracked config tree must be untouched: everything the render wrote
	// lives under the temporary workdir.
	entries, err := os.ReadDir(workDir)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name()] = true
	}
	for _, want := range []string{"kustomization.yaml", "webhook-service.yaml",
		"webhook-configuration.yaml", "metrics-fixture.yaml", "rendered.yaml"} {
		if !names[want] {
			t.Errorf("expected %s in temp workdir, have %v", want, names)
		}
	}
	if _, err := os.Stat(filepath.Join(DefaultRepoRoot(), "config", "default", "rendered.yaml")); !os.IsNotExist(err) {
		t.Error("render leaked files into the tracked config tree")
	}
}

func TestRenderManifestsValidatesInputs(t *testing.T) {
	cert, err := GenerateWebhookServingCert(WebhookServiceName, SystemNamespace)
	if err != nil {
		t.Fatal(err)
	}
	base := RenderOptions{
		RepoRoot:           DefaultRepoRoot(),
		ControllerImageRef: "optipod-e2e/controller:e2e-x",
		FixtureImageRef:    "optipod-e2e/metrics-fixture:e2e-x",
		WebhookCAPEM:       cert.CAPEM,
	}

	missingCA := base
	missingCA.WebhookCAPEM = nil
	if _, err := RenderManifests(context.Background(), missingCA); err == nil {
		t.Error("expected error for missing CA bundle")
	}

	badImage := base
	badImage.ControllerImageRef = "no-tag"
	if _, err := RenderManifests(context.Background(), badImage); err == nil {
		t.Error("expected error for tagless controller image reference")
	}

	emptyRoot := base
	emptyRoot.RepoRoot = ""
	if _, err := RenderManifests(context.Background(), emptyRoot); err == nil {
		t.Error("expected error for missing repo root")
	}
}

func TestSplitImageRef(t *testing.T) {
	name, tag, err := splitImageRef("optipod-e2e/controller:e2e-1-abcd")
	if err != nil || name != "optipod-e2e/controller" || tag != "e2e-1-abcd" {
		t.Fatalf("splitImageRef = %q %q %v", name, tag, err)
	}
	if _, _, err := splitImageRef("notagged"); err == nil {
		t.Fatal("expected error for tagless reference")
	}
}
