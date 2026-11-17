package consolee2e

import (
	"context"
	"fmt"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/optipod/optipod/internal/console"
	"github.com/optipod/optipod/internal/consoleenroll"
	"github.com/optipod/optipod/internal/consoleinstall"
	"github.com/optipod/optipod/internal/consoletls"
	"github.com/optipod/optipod/test/e2e/framework"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Host must be explicitly supplied: a local interface reachable from both owned
// clusters. The temporary listener uses TLS and newly generated test credentials.
func TestInstallerPreflightResumeAndUninstall(t *testing.T) {
	host := os.Getenv("OPTIPOD_INSTALLER_E2E_HOST")
	if host == "" {
		t.Skip("set an explicit local interface reachable from the two owned clusters")
	}
	ctx, clients, paths := helmSecretEnvironment(t)
	store := isolatedOnboardingStore(t, ctx)
	pki := filepath.Join(t.TempDir(), "pki")
	if err := consoletls.Generate(pki, []string{host}); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(nil)
	_ = server.Listener.Close()
	server.Listener = listener
	endpoint := "https://" + listener.Addr().String()
	admin, bootstrap := uuid.NewString()+uuid.NewString(), uuid.NewString()+uuid.NewString()
	api := &console.Server{Store: store, Config: console.Config{PublicURL: endpoint, BootstrapTokenHash: console.TokenHash(bootstrap), AgentTLS: &console.AgentTLSConfig{Listen: listener.Addr().String(), PublicURL: endpoint, ServerCertificateFile: filepath.Join(pki, "server.crt"), ServerKeyFile: filepath.Join(pki, "server.key"), ServerCAFile: filepath.Join(pki, "ca.crt"), ClientCAFile: filepath.Join(pki, "ca.crt"), ClientCAKeyFile: filepath.Join(pki, "ca.key")}}}
	server.TLS, err = api.InitAgentTLS()
	if err != nil {
		t.Fatal(err)
	}
	server.Config.Handler = api.Handler()
	server.StartTLS()
	defer server.Close()
	trust, err := consoletls.Client([]byte(api.AgentTrustPEM), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer trust.CloseIdleConnections()
	call := func(method, path string, body any, status int, result any) {
		t.Helper()
		onboardingCallClient(t, trust, endpoint, method, path, admin, body, status, result)
	}
	onboardingCallClient(t, trust, endpoint, "POST", "/auth/bootstrap", bootstrap, map[string]string{"subject": "installer-admin", "token": admin}, 204, nil)
	adminFile := filepath.Join(t.TempDir(), "admin")
	if err = os.WriteFile(adminFile, []byte(admin), 0600); err != nil {
		t.Fatal(err)
	}
	installed := helmValues(t, ctx, paths[0])
	image := installed["image"].(map[string]any)
	probeImage := image["repository"].(string) + ":" + image["tag"].(string)
	for index := range clients {
		t.Run(fmt.Sprintf("cluster-%d", index), func(t *testing.T) {
			suffix := uuid.NewString()[:8]
			release := "onboard-" + suffix
			controllerRelease := "optimizer-" + suffix
			mode := "existing"
			if index == 1 {
				mode = "new"
			}
			system, err := clients[index].Kube.CoreV1().Namespaces().Get(ctx, "kube-system", metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			r := console.Registration{ID: release, Name: release, Environment: "e2e", KubeUID: string(system.UID), Namespaces: []string{consoleNamespace}, MetricsSource: "metrics-server", InstallMode: mode, InstallMethod: "helm", Installation: console.Installation{Namespace: consoleNamespace, Release: release, ControllerNamespace: consoleNamespace, ControllerRelease: controllerRelease, Stage: "preflight"}}
			call("POST", "/api/admin/clusters", r, 201, nil)
			var enrollment console.IssuedCredential
			call("POST", "/api/admin/enrollments", map[string]string{"clusterID": r.ID}, 200, &enrollment)
			file := privateValuesFile(t, consoleenroll.File{IssuedCredential: enrollment, ServerURL: endpoint, KubeUID: r.KubeUID})
			plan, err := consoleinstall.FromEnrollment(&consoleenroll.File{IssuedCredential: enrollment, ServerURL: endpoint, KubeUID: r.KubeUID}, false)
			if err != nil {
				t.Fatal(err)
			}
			plan.Image = probeImage
			planFile := filepath.Join(t.TempDir(), "plan.json")
			if err = consoleinstall.WritePlan(planFile, plan); err != nil {
				t.Fatal(err)
			}
			raw, err := clientcmd.LoadFromFile(paths[index])
			if err != nil {
				t.Fatal(err)
			}
			options := consoleinstall.Options{Kubeconfig: paths[index], Context: raw.CurrentContext, PlanFile: planFile, EnrollmentFile: file, StateDir: filepath.Join(t.TempDir(), "state"), ConsoleChart: consoleChartPath(), ControllerChart: filepath.Join(filepath.Dir(consoleChartPath()), "optipod"), ControllerImage: os.Getenv("OPTIPOD_INSTALLER_E2E_CONTROLLER_IMAGE")}
			t.Cleanup(func() {
				cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				helmCommand(t, cleanup, paths[index], "uninstall", release, "--ignore-not-found", "--wait")
				if index == 1 {
					helmCommand(t, cleanup, paths[index], "uninstall", controllerRelease, "--ignore-not-found", "--wait")
				}
				_ = clients[index].Kube.CoreV1().PersistentVolumeClaims(consoleNamespace).Delete(cleanup, "queue-"+release+"-agent-0", metav1.DeleteOptions{})
			})
			report, err := consoleinstall.Run(ctx, options)
			if err != nil {
				t.Fatalf("fresh install failed: %v; checks=%+v", err, report)
			}
			wait(t, ctx, "installed agent inventory", func() bool {
				var received bool
				_ = store.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM op_clusters WHERE id=$1)`, r.ID).Scan(&received)
				return received
			})
			pvc := verifyInstallerResume(t, ctx, clients[index], store, r.ID, options)
			target, err := consoleinstall.OpenTarget(paths[index], raw.CurrentContext)
			if err != nil {
				t.Fatal(err)
			}
			verifyInstallerDenials(t, ctx, target, plan)
			wrong := *plan
			wrong.Registration = plan.Registration
			wrong.Registration.KubeUID = uuid.NewString()
			if consoleinstall.VerifyBinding(ctx, target, &wrong) == nil {
				t.Fatal("wrong context binding accepted")
			}
			if index == 0 {
				prom := *plan
				prom.Registration = plan.Registration
				prom.Registration.MetricsSource = "prometheus"
				prom.Registration.Installation.PrometheusURL = "http://prometheus.optipod-e2e-backends.svc:9090"
				checks, e := consoleinstall.Preflight(ctx, target, &prom, false)
				if e != nil || !checks.Ready() {
					t.Fatalf("real Prometheus preflight: %v %+v", e, checks)
				}
				prom.Registration.Namespaces = []string{"missing-onboarding-metrics"}
				checks, e = consoleinstall.Preflight(ctx, target, &prom, false)
				if e != nil || checks.Ready() {
					t.Fatal("missing namespace Prometheus metrics accepted", e)
				}
			}
			verifyInstallerExport(t, ctx, plan, options, enrollment.Token)

			adminClient := consoleinstall.AdminClient{URL: endpoint, TokenFile: adminFile, CAFile: filepath.Join(pki, "ca.crt")}
			if err = consoleinstall.Uninstall(ctx, options, adminClient); err != nil {
				t.Fatal(err)
			}
			if _, err = clients[index].Kube.CoreV1().PersistentVolumeClaims(consoleNamespace).Get(ctx, pvc.Name, metav1.GetOptions{}); err != nil {
				t.Fatal("agent uninstall deleted queue")
			}
			if index == 1 {
				deps, e := clients[index].Kube.AppsV1().Deployments(consoleNamespace).List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/instance=" + controllerRelease})
				if e != nil || len(deps.Items) == 0 {
					t.Fatal("agent uninstall removed optimizer")
				}
			}
			var disconnected bool
			if err = store.Pool.QueryRow(ctx, `SELECT disconnected FROM op_registrations WHERE id=$1`, r.ID).Scan(&disconnected); err != nil || !disconnected {
				t.Fatal("uninstall did not revoke telemetry")
			}
		})
	}
	if t.Failed() {
		return
	}
	t.Log("fresh Helm installs, real metrics sources, mTLS, resume, GitOps export and agent-only uninstall passed on two owned clusters")
}

func verifyInstallerExport(t *testing.T, ctx context.Context, plan *consoleinstall.Plan, options consoleinstall.Options, token string) {
	t.Helper()
	// Exported manifests must contain only references, never identities.
	gitops := *plan
	gitops.Registration = plan.Registration
	gitops.Registration.InstallMethod = "gitops"
	render := options
	render.OutputDir = filepath.Join(t.TempDir(), "gitops")
	if err := consoleinstall.Render(ctx, &gitops, render); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(render.OutputDir)
	for _, entry := range entries {
		data, e := os.ReadFile(filepath.Join(render.OutputDir, entry.Name()))
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(string(data), token) || strings.Contains(string(data), "PRIVATE KEY") || strings.Contains(string(data), "kind: Secret\n") {
			t.Fatal("GitOps export contains a credential")
		}
	}
}

func verifyInstallerResume(t *testing.T, ctx context.Context, c *framework.Clients, store *console.Store, clusterID string, options consoleinstall.Options) *corev1.PersistentVolumeClaim {
	t.Helper()
	pvc, err := c.Kube.CoreV1().PersistentVolumeClaims(consoleNamespace).Get(ctx, "queue-"+clusterID+"-agent-0", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var before string
	if err = store.Pool.QueryRow(ctx, `SELECT stream_id FROM op_clusters WHERE id=$1`, clusterID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	// A retry must not consume the already-used enrollment package.
	report, err := consoleinstall.Run(ctx, options)
	if err != nil {
		t.Fatalf("resume failed: %v %+v", err, report)
	}
	after, err := c.Kube.CoreV1().PersistentVolumeClaims(consoleNamespace).Get(ctx, pvc.Name, metav1.GetOptions{})
	if err != nil || after.UID != pvc.UID {
		t.Fatal("resume replaced queue PVC")
	}
	var stream string
	if err = store.Pool.QueryRow(ctx, `SELECT stream_id FROM op_clusters WHERE id=$1`, clusterID).Scan(&stream); err != nil || stream != before {
		t.Fatal("resume changed queue stream")
	}
	return pvc
}

func verifyInstallerDenials(t *testing.T, ctx context.Context, target *consoleinstall.Target, plan *consoleinstall.Plan) {
	t.Helper()
	duplicate := *plan
	duplicate.Registration.InstallMode = "new"
	checks, err := consoleinstall.Preflight(ctx, target, &duplicate, false)
	if err != nil {
		t.Fatal(err)
	}
	rejected := false
	for _, check := range checks.Checks {
		if check.Name == "controller" && check.Status == "fail" {
			rejected = true
		}
	}
	if checks.Ready() || !rejected {
		t.Fatal("fresh install accepted an existing optimizer")
	}
	cfg := rest.CopyConfig(target.Config)
	cfg.Impersonate = rest.ImpersonationConfig{UserName: "system:serviceaccount:" + plan.Registration.Installation.Namespace + ":" + plan.Registration.Installation.Release + "-agent", Groups: []string{"system:authenticated", "system:serviceaccounts"}}
	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	restricted := &consoleinstall.Target{Config: cfg, Kube: kube}
	existing := *plan
	existing.Registration.InstallMode = "existing"
	checks, err = consoleinstall.Preflight(ctx, restricted, &existing, false)
	if err != nil {
		t.Fatal(err)
	}
	rejected = false
	for _, check := range checks.Checks {
		if strings.HasPrefix(check.Name, "permission") && check.Status == "fail" {
			rejected = true
		}
	}
	if checks.Ready() || !rejected {
		t.Fatal("read-only agent was allowed to install cluster resources")
	}
}

// Chart labels must identify an optimizer even with custom deployment and image
// names. A zero-replica fixture exercises discovery without running an optimizer.
func TestInstallerDetectsRenamedController(t *testing.T) {
	ctx, clients, paths := helmSecretEnvironment(t)
	name := "renamed-optimizer-" + uuid.NewString()[:8]
	zero := int32(0)
	labels := map[string]string{"app.kubernetes.io/part-of": "optipod", "app.kubernetes.io/name": "private-optimizer", "app.kubernetes.io/instance": name}
	_, err := clients[1].Kube.AppsV1().Deployments(consoleNamespace).Create(ctx, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}, Spec: appsv1.DeploymentSpec{Replicas: &zero, Selector: &metav1.LabelSelector{MatchLabels: labels}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "manager", Image: "private.example/resource-manager:1.0"}}}}}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = clients[1].Kube.AppsV1().Deployments(consoleNamespace).Delete(context.Background(), name, metav1.DeleteOptions{})
	})
	raw, err := clientcmd.LoadFromFile(paths[1])
	if err != nil {
		t.Fatal(err)
	}
	target, err := consoleinstall.OpenTarget(paths[1], raw.CurrentContext)
	if err != nil {
		t.Fatal(err)
	}
	system, err := target.Kube.CoreV1().Namespaces().Get(ctx, "kube-system", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	plan := &consoleinstall.Plan{APIVersion: consoleinstall.PlanVersion, ConsoleVersion: "0.3.1", ControllerVersion: consoleinstall.ControllerVersion, ServerURL: "https://console.example", Image: "example/console:0.3.1", Registration: console.Registration{ID: name, Name: name, KubeUID: string(system.UID), MetricsSource: "metrics-server", InstallMode: "new", InstallMethod: "helm", Installation: console.Installation{Namespace: consoleNamespace, Release: name, ControllerNamespace: consoleNamespace, ControllerRelease: name}}}
	report, err := consoleinstall.Preflight(ctx, target, plan, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range report.Checks {
		if check.Name == "controller" && check.Status == "fail" {
			t.Log("renamed privately mirrored optimizer rejected before enrollment or installation")
			return
		}
	}
	t.Fatal("renamed optimizer was not detected", report)
}
