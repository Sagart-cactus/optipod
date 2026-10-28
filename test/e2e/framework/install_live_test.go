package framework

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// userState captures the developer-visible state the harness must preserve.
type userState struct {
	clusters   []string
	context    string
	kubeconfig string
	gitStatus  string
}

func snapshotUserState(t *testing.T) userState {
	t.Helper()
	return userState{
		clusters:   kindClusters(t),
		context:    currentContext(t),
		kubeconfig: defaultKubeconfigChecksum(t),
		gitStatus:  gitStatusShort(t),
	}
}

func (s userState) assertUnchanged(t *testing.T, ownedCluster string) {
	t.Helper()
	after := kindClusters(t)
	if contains(after, ownedCluster) {
		t.Errorf("owned cluster %q still present after teardown", ownedCluster)
	}
	for _, pre := range s.clusters {
		if !contains(after, pre) {
			t.Errorf("pre-existing cluster %q removed by teardown", pre)
		}
	}
	if got := currentContext(t); got != s.context {
		t.Errorf("current context changed: %q -> %q", s.context, got)
	}
	if got := defaultKubeconfigChecksum(t); got != s.kubeconfig {
		t.Error("default kubeconfig bytes changed")
	}
	if got := gitStatusShort(t); got != s.gitStatus {
		t.Errorf("git status changed:\nbefore:\n%s\nafter:\n%s", s.gitStatus, got)
	}
}

// TestLiveInstallReadyIdentityAndFixture runs the full M2 harness install path
// against a real owned Kind cluster: one build, one load, one temporary render,
// one install, then bounded readiness and identity verification. It proves the
// running controller pod uses the freshly built image and shipped service
// account, that the webhook configuration carries the harness CA, and that the
// deployed controller points at the in-cluster deterministic metrics fixture
// through temporary configuration. Gated behind OPTIPOD_E2E_LIVE and skipped
// under -short so the standing fast gate never provisions Docker/Kind.
func TestLiveInstallReadyIdentityAndFixture(t *testing.T) {
	if testing.Short() || os.Getenv("OPTIPOD_E2E_LIVE") == "" {
		t.Skip("set OPTIPOD_E2E_LIVE=1 (and omit -short) to run the live install test")
	}

	ctx := context.Background()
	before := snapshotUserState(t)

	env, err := SetupEnvironment(ctx, EnvironmentOptions{
		Cluster: ClusterOptions{CreateTimeout: 6 * time.Minute},
	})
	if err != nil {
		t.Fatalf("SetupEnvironment: %v", err)
	}
	tornDown := false
	t.Cleanup(func() {
		if !tornDown {
			_ = env.Teardown()
		}
	})

	assertPhases(t, env)
	assertRenderOutsideRepo(t, env)

	// Identity was already verified inside SetupEnvironment; verify explicitly
	// for direct evidence: the running pod uses the built image ID and shipped
	// service account.
	if err := VerifyControllerIdentity(ctx, env.Clients, env.Installation.ControllerImage); err != nil {
		t.Fatalf("VerifyControllerIdentity: %v", err)
	}

	assertWebhookConfiguration(t, ctx, env)
	assertControllerPointsAtFixture(t, ctx, env)
	assertFixtureServesMatrix(t, ctx, env)

	if err := env.Teardown(); err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	tornDown = true
	before.assertUnchanged(t, env.Cluster.Name)
}

func assertPhases(t *testing.T, env *Environment) {
	t.Helper()
	wantPhases := []string{
		"preflight", "cluster-create", "clients", "image-build",
		"image-load", "render", "install-apply", "await-ready",
	}
	if len(env.Phases) != len(wantPhases) {
		t.Fatalf("phases = %v, want %v", phaseNames(env.Phases), wantPhases)
	}
	for i, name := range wantPhases {
		if env.Phases[i].Name != name {
			t.Fatalf("phase %d = %q, want %q", i, env.Phases[i].Name, name)
		}
		t.Logf("phase %-15s %s", env.Phases[i].Name, env.Phases[i].Duration)
	}
}

func assertRenderOutsideRepo(t *testing.T, env *Environment) {
	t.Helper()
	resolvedRender, _ := filepath.EvalSymlinks(env.Installation.Rendered.Dir)
	resolvedRepo, _ := filepath.EvalSymlinks(DefaultRepoRoot())
	if strings.HasPrefix(resolvedRender, resolvedRepo+string(filepath.Separator)) {
		t.Fatalf("rendered overlay %q is inside the tracked tree", resolvedRender)
	}
	if _, err := os.Stat(env.Installation.Rendered.Path); err != nil {
		t.Fatalf("rendered manifest missing: %v", err)
	}
}

func assertWebhookConfiguration(t *testing.T, ctx context.Context, env *Environment) {
	t.Helper()
	mwc, err := env.Clients.Kube.AdmissionregistrationV1().MutatingWebhookConfigurations().
		Get(ctx, WebhookConfigurationName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get webhook configuration: %v", err)
	}
	if len(mwc.Webhooks) != 1 {
		t.Fatalf("webhooks = %d, want 1", len(mwc.Webhooks))
	}
	wh := mwc.Webhooks[0]
	if wh.ClientConfig.Service == nil || wh.ClientConfig.Service.Path == nil ||
		*wh.ClientConfig.Service.Path != WebhookPath {
		t.Fatalf("webhook service path = %+v, want %s", wh.ClientConfig.Service, WebhookPath)
	}
	if string(wh.ClientConfig.CABundle) != string(env.Installation.WebhookCert.CAPEM) {
		t.Fatal("webhook caBundle does not match the harness CA")
	}
	if wh.FailurePolicy == nil || *wh.FailurePolicy != "Ignore" {
		t.Fatalf("failurePolicy = %v, want shipped Ignore", wh.FailurePolicy)
	}
}

func assertControllerPointsAtFixture(t *testing.T, ctx context.Context, env *Environment) {
	t.Helper()
	pods, err := env.Clients.Kube.CoreV1().Pods(SystemNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: "control-plane=controller-manager",
	})
	if err != nil || len(pods.Items) == 0 {
		t.Fatalf("list controller pods: %v (items %d)", err, len(pods.Items))
	}
	logCtx, logCancel := context.WithTimeout(ctx, 30*time.Second)
	defer logCancel()
	logs, err := env.Clients.Kube.CoreV1().Pods(SystemNamespace).
		GetLogs(pods.Items[0].Name, &corev1.PodLogOptions{Container: "manager"}).DoRaw(logCtx)
	if err != nil {
		t.Fatalf("read controller logs: %v", err)
	}
	if !strings.Contains(string(logs), env.Installation.FixtureURL) {
		t.Fatalf("controller logs do not reference fixture URL %q", env.Installation.FixtureURL)
	}
}

func assertFixtureServesMatrix(t *testing.T, ctx context.Context, env *Environment) {
	t.Helper()
	series := FixtureSeries{
		Namespace: "probe", Pod: "web-0", Container: "app",
		CPUCores: []float64{0.125, 0.25}, MemoryBytes: []float64{134217728},
	}
	if err := env.Fixture.SetSeries(ctx, []FixtureSeries{series}); err != nil {
		t.Fatalf("SetSeries: %v", err)
	}
	body := fmt.Sprintf("query=%s&start=%d&end=%d&step=30",
		`rate(container_cpu_usage_seconds_total{namespace="probe",pod="web-0",container="app"}[24h])`,
		time.Now().Add(-time.Hour).Unix(), time.Now().Unix())
	raw, err := env.Clients.Kube.CoreV1().RESTClient().Post().
		Namespace(FixtureNamespace).
		Resource("services").
		Name(fmt.Sprintf("%s:%d", FixtureServiceName, FixturePort)).
		SubResource("proxy").
		Suffix("api", "v1", "query_range").
		SetHeader("Content-Type", "application/x-www-form-urlencoded").
		Body(strings.NewReader(body)).
		DoRaw(ctx)
	if err != nil {
		t.Fatalf("query fixture through service proxy: %v", err)
	}
	if !strings.Contains(string(raw), `"resultType":"matrix"`) ||
		!strings.Contains(string(raw), `"0.25"`) {
		t.Fatalf("fixture did not serve the configured matrix samples: %s", raw)
	}

	trace, err := env.Fixture.Trace(ctx)
	if err != nil {
		t.Fatalf("Trace: %v", err)
	}
	if len(trace) == 0 || !strings.Contains(trace[len(trace)-1].Query, `namespace="probe"`) {
		t.Fatalf("fixture trace missing the probe query: %+v", trace)
	}
}

func phaseNames(phases []Phase) []string {
	names := make([]string, 0, len(phases))
	for _, p := range phases {
		names = append(names, p.Name)
	}
	return names
}

func gitStatusShort(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("git", "-C", DefaultRepoRoot(), "status", "--short")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v (%s)", err, out)
	}
	return string(out)
}
