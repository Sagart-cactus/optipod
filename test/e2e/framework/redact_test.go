package framework

import (
	"encoding/xml"
	"strings"
	"testing"
)

// Canary sensitive data used only in tests. None of these strings is a real
// credential; they exist to prove the scrubber and the collector's negative
// sensitive-data scan. PEM-shaped canaries are assembled at runtime from
// fence and body constants (canaryPEM) so this file contains no literal PEM
// block; commit guards reject literal key blocks in source.
const (
	canaryBearerToken  = "canary-bearer-token-0123456789abcdef"                                        // pragma: allowlist secret
	canaryPassword     = "canary-password-hunter2"                                                     // pragma: allowlist secret
	canaryJWT          = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJjYW5hcnktdG9rZW4ifQ.canary-signature-abcdef" // pragma: allowlist secret
	canaryPEMBodyLong  = "MIICANARYTHISISNOTAREALPRIVATEKEY0123456789abcdef=="                         // pragma: allowlist secret
	canaryPEMBodyShort = "MIICANARY0123456789abcdef=="                                                 // pragma: allowlist secret
	canaryPEMBodyTiny  = "MIICANARY0123"
)

// canaryPEM assembles a clearly fake PEM-shaped canary at runtime by joining
// fence and body constants. It is test data only, never real key material.
func canaryPEM(kind, body string) string {
	return "-----BEGIN " + kind + " PRIVATE KEY-----\n" + body + "\n" +
		"-----END " + kind + " PRIVATE KEY-----"
}

func TestRedactRemovesPrivateKeyPEM(t *testing.T) {
	cases := []string{
		// Raw multiline block.
		canaryPEM("EC", canaryPEMBodyLong),
		// Kubernetes-serialised single-line annotation value with escaped newlines.
		`"optipod-e2e-harness.io/canary-key": "` +
			strings.ReplaceAll(canaryPEM("EC", canaryPEMBodyShort), "\n", `\n`) + `"`,
		// RSA and OPENSSH variants.
		canaryPEM("RSA", canaryPEMBodyTiny),
		canaryPEM("OPENSSH", canaryPEMBodyTiny),
	}
	for _, in := range cases {
		out, n := RedactCount(in)
		if n == 0 {
			t.Errorf("no redaction applied to %q", in)
		}
		if strings.Contains(out, "MIICANARY") {
			t.Errorf("private key material survived redaction: %q", out)
		}
		if !strings.Contains(out, RedactPlaceholder) {
			t.Errorf("redaction placeholder missing from %q", out)
		}
		if !strings.Contains(out, "PRIVATE KEY") {
			t.Errorf("PEM marker lost, redaction not visible: %q", out)
		}
	}
}

func TestRedactRemovesBearerTokensAndJWTs(t *testing.T) {
	in := "Authorization: Bearer " + canaryBearerToken + " and jwt " + canaryJWT
	out, n := RedactCount(in)
	if n < 2 {
		t.Errorf("expected bearer and jwt redactions, got %d", n)
	}
	if strings.Contains(out, canaryBearerToken) || strings.Contains(out, canaryJWT) {
		t.Errorf("token material survived redaction: %q", out)
	}
	if !strings.Contains(out, "Bearer "+RedactPlaceholder) {
		t.Errorf("bearer structure not preserved: %q", out)
	}
}

func TestRedactRemovesSensitiveYAMLAndJSONValues(t *testing.T) {
	yamlIn := `apiVersion: v1
kind: Secret
metadata:
  name: canary-secret
data:
  password: cGFzc3dvcmQ=
  tls.key: LS0tLS1CRUdJTiBFQyBQUklWQVRFIEtFWS0tLS0KTUlJQ0FOQVJZMDAyCg==
stringData:
  token: ` + canaryBearerToken + `
  unrelated: keep-me-please`
	yamlOut, n := RedactCount(yamlIn)
	if n < 3 {
		t.Errorf("expected >=3 yaml redactions, got %d in %q", n, yamlOut)
	}
	for _, want := range []string{"cGFzc3dvcmQ=", "LS0tLS1CRUdJTi", canaryBearerToken} {
		if strings.Contains(yamlOut, want) {
			t.Errorf("sensitive yaml value %q survived: %q", want, yamlOut)
		}
	}
	if !strings.Contains(yamlOut, "unrelated: keep-me-please") {
		t.Errorf("unrelated value wrongly redacted: %q", yamlOut)
	}
	if !strings.Contains(yamlOut, "kind: Secret") || !strings.Contains(yamlOut, "name: canary-secret") {
		t.Errorf("safe metadata wrongly redacted: %q", yamlOut)
	}

	jsonIn := `{"kind":"Secret","data":{"password":"pw","tls.key":"LS0tLS1CRUdJTiBFQyBQUklWQVRFIEtFWS0tLS0KTUlJQ0FOQVJZMDAyCg=="},"other":"fine"}` // pragma: allowlist secret
	jsonOut, n2 := RedactCount(jsonIn)
	if n2 < 2 {
		t.Errorf("expected >=2 json redactions, got %d in %q", n2, jsonOut)
	}
	if strings.Contains(jsonOut, "\"pw\"") || strings.Contains(jsonOut, "LS0tLS1CRUdJTi") {
		t.Errorf("sensitive json value survived: %q", jsonOut)
	}
	if !strings.Contains(jsonOut, `"kind":"Secret"`) || !strings.Contains(jsonOut, `"other":"fine"`) {
		t.Errorf("safe json content wrongly redacted: %q", jsonOut)
	}
}

func TestRedactRemovesURLCredentialsAndKubeconfigData(t *testing.T) {
	kubeconfigIn := `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://127.0.0.1:64321
    certificate-authority-data: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCmNhbmFyeQo=
    client-key-data: LS0tLS1CRUdJTiBFQyBQUklWQVRFIEtFWS0tLS0KTUlJQ0FOQVJZMDAyCg==
users:
- name: canary-user
  user:
    token: ` + canaryJWT + `
    password: ` + canaryPassword + `
current-context: canary-context`
	out, n := RedactCount(kubeconfigIn)
	if n < 4 {
		t.Errorf("expected >=4 kubeconfig redactions, got %d in %q", n, out)
	}
	for _, want := range []string{"LS0tLS1CRUdJTiBFQyBQUklWQVRFIEtFWS0tLS0", canaryJWT, canaryPassword} {
		if strings.Contains(out, want) {
			t.Errorf("kubeconfig credential %q survived: %q", want, out)
		}
	}
	if !strings.Contains(out, "server: https://127.0.0.1:64321") ||
		!strings.Contains(out, "current-context: canary-context") {
		t.Errorf("non-sensitive kubeconfig fields wrongly redacted: %q", out)
	}

	urlOut, n2 := RedactCount("metrics url https://user:supersecret@prometheus.example:9090/api")
	if n2 == 0 || strings.Contains(urlOut, "supersecret") {
		t.Errorf("url credentials survived: %q (%d)", urlOut, n2)
	}
	if !strings.Contains(urlOut, "prometheus.example:9090/api") {
		t.Errorf("url host wrongly redacted: %q", urlOut)
	}
}

func TestRedactRemovesCredentialShapedConfigKeys(t *testing.T) {
	// The controller configuration supports Prometheus auth keys; values under
	// any key whose segments are credential-shaped must never reach artifacts.
	in := `prometheus-url: http://optipod-metrics-fixture.optipod-e2e-metrics.svc:9090
reconciliation-interval: 10s
prometheus-auth-type: bearer
prometheus-username: canary-user
prometheus-password: canary-password-hunter2
prometheus-bearer-token: canary-bearer-token-0123456789abcdef
prometheus-tls-insecure-skip-verify: false
data:
  tls.key: LS0tLS1CRUdJTiBFQyBQUklWQVRFIEtFWS0tLS0KTUlJQ0FOQVJZMDAyCg==`
	out, n := RedactCount(in)
	if n < 4 {
		t.Errorf("expected >=4 redactions, got %d in %q", n, out)
	}
	for _, want := range []string{canaryPassword, canaryBearerToken, "LS0tLS1CRUdJTiBFQyBQUklWQVRFIEtFWS0tLS0", "canary-user"} {
		if strings.Contains(out, want) {
			t.Errorf("credential-shaped value %q survived: %q", want, out)
		}
	}
	for _, want := range []string{
		"prometheus-url: http://optipod-metrics-fixture.optipod-e2e-metrics.svc:9090",
		"reconciliation-interval: 10s",
		"prometheus-auth-type: bearer",
		"prometheus-tls-insecure-skip-verify: false",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("safe configuration %q wrongly redacted: %q", want, out)
		}
	}
}

func TestRedactPreservesDiagnosticContent(t *testing.T) {
	in := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: canary-ns
  annotations:
    optipod.io/recommendation-cpu: 250m
spec:
  template:
    spec:
      containers:
      - name: app
        image: registry.k8s.io/pause:3.10@sha256:ee6521f290b2168b6e0935a181d4cff9be1ac3f505666ef0e3c98fae8199917a
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
secretName: webhook-server-certs
message: "pod web-abc created"`
	out, n := RedactCount(in)
	if n != 0 {
		t.Errorf("safe diagnostic content was redacted (%d substitutions): %q", n, out)
	}
	for _, want := range []string{
		"optipod.io/recommendation-cpu: 250m",
		"image: registry.k8s.io/pause:3.10@sha256:ee6521f290b2168b6e0935a181d4cff9be1ac3f505666ef0e3c98fae8199917a",
		"cpu: 100m", "memory: 128Mi",
		"secretName: webhook-server-certs",
		`message: "pod web-abc created"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("safe content %q not preserved: %q", want, out)
		}
	}
}

func TestRedactRemovesXMLEscapedSensitiveValues(t *testing.T) {
	// Go's encoding/xml escapes quotes as &#34;, so JUnit failure-message
	// attributes carry credential key-value content in this shape.
	in := `<failure message="Expected success: &#34;token&#34;: &#34;` + canaryBearerToken + `&#34; and &#34;tls.key&#34;: &#34;LS0tLS1CRUdJTiBFQyBQUklWQVRFIEtFWS0tLS0KTUlJQ0FOQVJZMDAyCg==&#34;" type="failed"/>`
	out, n := RedactCount(in)
	if n < 2 {
		t.Errorf("expected >=2 xml-escaped redactions, got %d in %q", n, out)
	}
	if strings.Contains(out, canaryBearerToken) || strings.Contains(out, "LS0tLS1CRUdJTiBFQyBQUklWQVRFIEtFWS0tLS0") {
		t.Errorf("xml-escaped credential survived: %q", out)
	}
	if !strings.Contains(out, `&#34;token&#34;: &#34;`+RedactPlaceholder+`&#34;`) {
		t.Errorf("entity quoting not preserved after redaction: %q", out)
	}
	// The XML must remain well formed (parseable) after scrubbing.
	var probe struct {
		Message string `xml:"message,attr"`
	}
	if err := xml.Unmarshal([]byte(out), &probe); err != nil {
		t.Fatalf("scrubbed xml is not parseable: %v\n%s", err, out)
	}
	if !strings.Contains(probe.Message, "Expected success") {
		t.Errorf("failure message lost its identifying prefix: %q", probe.Message)
	}
}

func TestRedactEmptyAndCleanInput(t *testing.T) {
	if got := Redact(""); got != "" {
		t.Errorf("Redact(empty) = %q", got)
	}
	out, n := RedactCount("plain line without any sensitive shape")
	if n != 0 || out != "plain line without any sensitive shape" {
		t.Errorf("clean input altered: %q (%d)", out, n)
	}
}
