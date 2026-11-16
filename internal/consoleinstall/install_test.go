package consoleinstall

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/optipod/optipod/internal/console"
	"github.com/optipod/optipod/internal/consoleenroll"
)

func samplePlan() *Plan {
	return &Plan{APIVersion: PlanVersion, Registration: console.Registration{ID: "qa", Name: "QA", KubeUID: "00000000-0000-4000-8000-000000000000", Namespaces: []string{"app"}, MetricsSource: "prometheus", InstallMode: "existing", InstallMethod: "helm", Installation: console.Installation{Namespace: "optipod-system", Release: "console", ControllerNamespace: "optipod-system", ControllerRelease: "optipod", PrometheusURL: "http://prometheus:9090"}}, ServerURL: "https://console.example", ConsoleVersion: "0.3.0", ControllerVersion: ControllerVersion, Image: "registry.example/console:0.3.0"}
}
func TestEnrollmentCannotChangeSavedIdentityOrScope(t *testing.T) {
	p := samplePlan()
	f := consoleenroll.File{IssuedCredential: console.IssuedCredential{ClusterID: p.Registration.ID, Token: strings.Repeat("x", 48), ExpiresAt: time.Now().Add(time.Minute), Registration: &p.Registration}, ServerURL: p.ServerURL, KubeUID: p.Registration.KubeUID}
	path := filepath.Join(t.TempDir(), "enrollment.json")
	data, _ := json.Marshal(f)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyEnrollment(path, p, false); err != nil {
		t.Fatal(err)
	}
	other := *p
	other.Registration = p.Registration
	other.Registration.Namespaces = []string{"another"}
	if verifyEnrollment(path, &other, false) == nil {
		t.Fatal("scope substitution accepted")
	}
	other = *p
	other.ServerURL = "https://another.example"
	if verifyEnrollment(path, &other, false) == nil {
		t.Fatal("endpoint substitution accepted")
	}
	other = *p
	other.Registration = p.Registration
	other.Registration.RequireMTLS = true
	if verifyEnrollment(path, &other, false) == nil {
		t.Fatal("transport downgrade accepted")
	}
}
func TestPrivateStateCannotLeakIntoGitOpsOutput(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	if err := PrivateDirectory(state); err != nil {
		t.Fatal(err)
	}
	for _, out := range []string{state, root, filepath.Join(state, "manifests")} {
		if separateDirectories(state, out) == nil {
			t.Fatalf("accepted overlap %s", out)
		}
	}
	link := filepath.Join(root, "alias")
	if err := os.Symlink(state, link); err != nil {
		t.Fatal(err)
	}
	if separateDirectories(state, filepath.Join(link, "output")) == nil {
		t.Fatal("symlink overlap accepted")
	}
	if err := separateDirectories(state, filepath.Join(root, "gitops")); err != nil {
		t.Fatal(err)
	}
}
func TestPrometheusPreflightScopesAndHistory(t *testing.T) {
	metrics := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		value := "15"
		if strings.Contains(query, "count_over_time") {
			value = "2"
		}
		if strings.Contains(query, `namespace="missing"`) {
			value = "0"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"result": []any{map[string]any{"value": []any{0, value}}}}})
	}))
	defer metrics.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"version": "0.3.0", "protocol": 1})
	}))
	defer server.Close()
	o := ProbeOptions{ServerURL: server.URL, MetricsSource: "prometheus", PrometheusURL: metrics.URL, Namespaces: []string{"app"}, AllowHTTP: true, VolumePath: t.TempDir()}
	report := Probe(context.Background(), o)
	if !report.Ready() {
		t.Fatalf("healthy metrics rejected: %+v", report)
	}
	warning := false
	for _, check := range report.Checks {
		if check.Name == "history/app" && check.Status == "warn" {
			warning = true
		}
	}
	if !warning {
		t.Fatal("insufficient history not reported")
	}
	o.Namespaces = append(o.Namespaces, "missing")
	report = Probe(context.Background(), o)
	if report.Ready() {
		t.Fatal("healthy namespace masked missing namespace metrics")
	}
	o.PrometheusURL = server.URL
	report = Probe(context.Background(), o)
	if report.Ready() {
		t.Fatal("non-Prometheus response accepted")
	}
}
func TestPrometheusCredentialsDoNotImplicitlyEnableTLS(t *testing.T) {
	p := samplePlan()
	p.PrometheusSecret = "metrics-auth" // pragma: allowlist secret - Kubernetes Secret reference or fixture name
	p.PrometheusAuth = "bearer"
	v, err := controllerValues(p, "")
	if err != nil {
		t.Fatal(err)
	}
	prom := v["metricsProvider"].(map[string]any)["prometheus"].(map[string]any)
	if _, ok := prom["tls"]; ok {
		t.Fatal("bearer secret unexpectedly requires a CA")
	}
	p.PrometheusCASecret = "metrics-ca" // pragma: allowlist secret - Kubernetes Secret reference or fixture name
	v, err = controllerValues(p, "")
	if err != nil {
		t.Fatal(err)
	}
	prom = v["metricsProvider"].(map[string]any)["prometheus"].(map[string]any)
	if prom["tls"].(map[string]any)["existingSecret"].(map[string]string)["certKey"] != "" {
		t.Fatal("CA-only setup requires a client certificate")
	}
}

func TestAgentCustomTrustSurvivesBothInstallMethods(t *testing.T) {
	p := samplePlan()
	p.ConsoleCASecret = "console-trust" // pragma: allowlist secret - Kubernetes Secret reference or fixture name
	for _, gitops := range []bool{false, true} {
		values, err := agentValues(p, gitops)
		if err != nil {
			t.Fatal(err)
		}
		if values["agent"].(map[string]any)["caSecret"] != p.ConsoleCASecret { // pragma: allowlist secret - Kubernetes Secret reference or fixture name
			t.Fatal("private console CA reference lost")
		}
	}
	p.ConsoleCASecret = "not a secret name" // pragma: allowlist secret - Kubernetes Secret reference or fixture name
	if p.Validate(false) == nil {
		t.Fatal("invalid CA Secret reference accepted")
	}
}
