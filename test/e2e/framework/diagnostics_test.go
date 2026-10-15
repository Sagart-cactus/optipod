package framework

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	admissionregv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

func TestCABundleMetadataDescribesCertificateOnly(t *testing.T) {
	cert, err := GenerateWebhookServingCert(WebhookServiceName, SystemNamespace)
	if err != nil {
		t.Fatalf("GenerateWebhookServingCert: %v", err)
	}
	// The caBundle []byte read through the API carries the decoded PEM.
	meta := caBundleMetadata(cert.CAPEM)
	if meta["present"] != true {
		t.Fatalf("metadata not marked present: %v", meta)
	}
	if subj, _ := meta["subject"].(string); !strings.Contains(subj, "optipod-e2e-webhook-ca") {
		t.Errorf("subject = %v", meta["subject"])
	}
	if fp, _ := meta["sha256Fingerprint"].(string); len(fp) != 64 {
		t.Errorf("fingerprint = %q", fp)
	}
	if _, err := os.Stat(string(cert.KeyPEM)); err == nil {
		t.Fatal("key pem treated as a path")
	}
	// The metadata map must never carry key material or the raw bundle.
	raw, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	if strings.Contains(string(raw), "PRIVATE KEY") || strings.Contains(string(raw), string(cert.KeyPEM)) {
		t.Errorf("metadata contains key material: %s", raw)
	}

	empty := caBundleMetadata(nil)
	if empty["present"] != false || empty["bundleBytes"] != 0 {
		t.Errorf("empty bundle metadata = %v", empty)
	}
	garbage := caBundleMetadata([]byte("not-a-pem-certificate"))
	if _, ok := garbage["pemError"]; !ok {
		t.Errorf("garbage bundle metadata = %v", garbage)
	}
}

func TestWebhookConfigurationDiagnosticsCarriesConfigWithoutBundle(t *testing.T) {
	cert, err := GenerateWebhookServingCert(WebhookServiceName, SystemNamespace)
	if err != nil {
		t.Fatalf("GenerateWebhookServingCert: %v", err)
	}
	ignore := admissionregv1.Ignore
	path := "/mutate"
	mwc := &admissionregv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: WebhookConfigurationName},
		Webhooks: []admissionregv1.MutatingWebhook{{
			Name:           "pod-resource-mutator.optipod.io",
			FailurePolicy:  &ignore,
			TimeoutSeconds: ptrTo(int32(10)),
			ClientConfig: admissionregv1.WebhookClientConfig{
				Service: &admissionregv1.ServiceReference{
					Namespace: SystemNamespace,
					Name:      WebhookServiceName,
					Path:      &path,
				},
				// The caBundle []byte read through the API carries the decoded PEM.
				CABundle: cert.CAPEM,
			},
		}},
	}
	diag := webhookConfigurationDiagnosticsFrom(mwc)
	if len(diag.Webhooks) != 1 {
		t.Fatalf("webhooks = %d", len(diag.Webhooks))
	}
	wh := diag.Webhooks[0]
	if wh.FailurePolicy != "Ignore" || wh.Path == nil || *wh.Path != WebhookPath {
		t.Errorf("webhook wiring wrong: %+v", wh)
	}
	if wh.ServiceName != WebhookServiceName || wh.ServiceNamespace != SystemNamespace {
		t.Errorf("service wiring wrong: %+v", wh)
	}
	raw, err := yaml.Marshal(diag)
	if err != nil {
		t.Fatalf("marshal diagnostics: %v", err)
	}
	diagText := string(raw)
	if strings.Contains(diagText, string(cert.CAPEM)) {
		t.Errorf("raw CA bundle leaked into diagnostics yaml")
	}
	if !strings.Contains(diagText, "caBundleMetadata") || !strings.Contains(diagText, "optipod-e2e-webhook-ca") {
		t.Errorf("CA metadata missing from diagnostics yaml:\n%s", diagText)
	}
}

func TestCollectorWritesRedactedFilesAndRecordsThem(t *testing.T) {
	dir := t.TempDir()
	c := &collector{
		scope:  DiagnosticsScope{ClusterName: "optipod-e2e-1-abcd"},
		bundle: &DiagnosticBundle{Dir: dir},
	}
	c.writeYAML(filepath.Join("namespaces", "ns1", "deployment.yaml"), map[string]string{
		"annotation": "Bearer " + canaryBearerToken,
		"image":      "registry.k8s.io/pause:3.10",
	})
	c.writeText("failures.txt", "original failure remains visible\n")
	c.recordError("list events in ns1: connection refused (with Bearer %s)", canaryBearerToken)

	deployPath := filepath.Join(dir, "namespaces", "ns1", "deployment.yaml")
	raw, err := os.ReadFile(deployPath)
	if err != nil {
		t.Fatalf("read collected deployment: %v", err)
	}
	if strings.Contains(string(raw), canaryBearerToken) {
		t.Errorf("canary token survived collection: %s", raw)
	}
	if !strings.Contains(string(raw), "[REDACTED]") {
		t.Errorf("redaction not visible in artifact: %s", raw)
	}
	if !strings.Contains(string(raw), "registry.k8s.io/pause:3.10") {
		t.Errorf("safe content lost during collection: %s", raw)
	}

	failures, err := os.ReadFile(filepath.Join(dir, "failures.txt"))
	if err != nil || !strings.Contains(string(failures), "original failure remains visible") {
		t.Errorf("failure text not preserved: %v (%s)", err, failures)
	}
	// Collection errors accumulate on the bundle; CollectDiagnostics writes
	// them out as collection-errors.txt.
	if len(c.bundle.CollectionErrors) != 1 {
		t.Fatalf("collection errors = %+v", c.bundle.CollectionErrors)
	}
	errText := c.bundle.CollectionErrors[0]
	if !strings.Contains(errText, "connection refused") {
		t.Errorf("collection error not recorded: %q", errText)
	}
	if strings.Contains(errText, canaryBearerToken) {
		t.Errorf("collection errors carry canary token: %q", errText)
	}
	if c.bundle.Redactions == 0 {
		t.Errorf("redactions not counted: %+v", c.bundle)
	}
	found := map[string]bool{}
	for _, f := range c.bundle.Files {
		found[f] = true
	}
	if !found["failures.txt"] || !found[filepath.Join("namespaces", "ns1", "deployment.yaml")] {
		t.Errorf("bundle files not recorded: %+v", c.bundle.Files)
	}
}

func TestCollectDiagnosticsRejectsMissingInputs(t *testing.T) {
	ctx := context.Background()
	if _, err := CollectDiagnostics(ctx, nil, DiagnosticsScope{}, t.TempDir()); err == nil {
		t.Fatal("nil clients accepted")
	}
	if _, err := CollectDiagnostics(ctx, &Clients{}, DiagnosticsScope{}, ""); err == nil {
		t.Fatal("empty out dir accepted")
	}
}

func ptrTo[T any](v T) *T { return &v }
