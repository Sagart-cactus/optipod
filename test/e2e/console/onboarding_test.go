package consolee2e

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/optipod/optipod/internal/console"
	"github.com/optipod/optipod/internal/consoleenroll"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
)

func TestEmptyConsoleEnrollsTwoOwnedClusters(t *testing.T) {
	paths := filepath.SplitList(os.Getenv("OPTIPOD_CONSOLE_E2E_KUBECONFIGS"))
	if testing.Short() || len(paths) != 2 {
		t.Skip("set two explicit owned Kind kubeconfigs for onboarding E2E")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	clients := ownedClients(t, ctx, paths)
	store := isolatedOnboardingStore(t, ctx)
	bootstrap, admin := uuid.NewString()+uuid.NewString(), uuid.NewString()+uuid.NewString()
	server := httptest.NewUnstartedServer(nil)
	cfg := console.Config{PublicURL: "http://" + server.Listener.Addr().String(), BootstrapTokenHash: console.TokenHash(bootstrap)}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	server.Config.Handler = (&console.Server{Store: store, Config: cfg}).Handler()
	server.Start()
	defer server.Close()
	onboardingCall(t, server.URL, "POST", "/auth/bootstrap", bootstrap, map[string]string{"subject": "installer", "token": admin}, 204, nil)
	var registered []console.Registration
	onboardingCall(t, server.URL, "GET", "/api/admin/clusters", admin, nil, 200, &registered)
	if len(registered) != 0 {
		t.Fatal("new console is not empty")
	}
	suffix := uuid.NewString()[:8]
	ids, tokens := []string{"qa-" + suffix, "prod-" + suffix}, []string{}
	for i, clusterID := range ids {
		ns, err := clients[i].Kube.CoreV1().Namespaces().Get(ctx, "kube-system", metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		metrics := "prometheus"
		if i == 1 {
			metrics = "metrics-server"
		}
		reg := console.Registration{ID: clusterID, Name: clusterID, Environment: "local", KubeUID: string(ns.UID), Namespaces: []string{"console-e2e-" + suffix}, MetricsSource: metrics, InstallMode: "existing", InstallMethod: "helm"}
		onboardingCall(t, server.URL, "POST", "/api/admin/clusters", admin, reg, 201, nil)
		var enrollment console.IssuedCredential
		onboardingCall(t, server.URL, "POST", "/api/admin/enrollments", admin, map[string]string{"clusterID": clusterID}, 200, &enrollment)
		file := privateValuesFile(t, consoleenroll.File{IssuedCredential: enrollment, ServerURL: server.URL, KubeUID: reg.KubeUID})
		contextConfig, err := clientcmd.LoadFromFile(paths[i])
		if err != nil {
			t.Fatal(err)
		}
		output := filepath.Join(t.TempDir(), "runtime-token")
		o := consoleenroll.Options{EnrollmentFile: file, Kubeconfig: paths[i], Context: contextConfig.CurrentContext, TokenOutput: output, AllowHTTP: true}
		wrong := o
		wrong.Kubeconfig = paths[1-i]
		wrongConfig, err := clientcmd.LoadFromFile(wrong.Kubeconfig)
		if err != nil {
			t.Fatal(err)
		}
		wrong.Context = wrongConfig.CurrentContext
		if _, err = consoleenroll.Run(ctx, wrong); err == nil {
			t.Fatal("enrollment accepted the other real cluster")
		}
		if _, err = consoleenroll.Run(ctx, o); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(output)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("runtime credential file is not private")
		}
		token, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, string(token))
		o.TokenOutput = filepath.Join(t.TempDir(), "replayed-token")
		if _, err = consoleenroll.Run(ctx, o); err == nil {
			t.Fatal("enrollment file replay accepted")
		}
	}
	prepareSources(t, ctx, clients, paths, ids, tokens, "console-e2e-"+suffix, server.URL)
	var page console.Page
	onboardingCall(t, server.URL, "GET", "/api/workloads", admin, nil, 200, &page)
	if len(page.Items) != 2 || page.Items[0].ClusterID == page.Items[1].ClusterID {
		t.Fatal("two dynamically enrolled real clusters are not isolated and visible")
	}
	onboardingCall(t, server.URL, "GET", "/api/admin/clusters", admin, nil, 200, &registered)
	if len(registered) != 2 || registered[0].ReceivedAt == nil || registered[1].ReceivedAt == nil {
		t.Fatal("registration progress did not record successful ingestion")
	}
	// A completed registration survives reconnection. Disconnect revokes the
	// old identity immediately without deleting either cluster's observations.
	onboardingCall(t, server.URL, "POST", "/api/admin/connection", admin, map[string]any{"id": ids[0], "connected": false}, 200, nil)
	onboardingCall(t, server.URL, "POST", "/api/heartbeat", tokens[0], console.AgentHealth{}, 401, nil)
	onboardingCall(t, server.URL, "POST", "/api/admin/connection", admin, map[string]any{"id": ids[0], "connected": true}, 200, nil)
	onboardingCall(t, server.URL, "POST", "/api/heartbeat", tokens[0], console.AgentHealth{}, 401, nil)
	onboardingCall(t, server.URL, "GET", "/api/workloads", admin, nil, 200, &page)
	if len(page.Items) != 2 {
		t.Fatal("disconnect removed persisted inventory")
	}
	var viewer map[string]string
	onboardingCall(t, server.URL, "POST", "/api/admin/users", admin, console.UserGrant{Subject: "qa-reader", Role: "viewer", Clusters: []string{ids[0]}, IssueToken: true}, 200, &viewer)
	onboardingCall(t, server.URL, "GET", "/api/workloads", viewer["token"], nil, 200, &page)
	if len(page.Items) != 1 || page.Items[0].ClusterID != ids[0] {
		t.Fatal("dynamic viewer grant escaped cluster scope")
	}
	onboardingCall(t, server.URL, "GET", "/api/admin/health", viewer["token"], nil, 403, nil)
	for i, c := range clients {
		deployments, err := c.Kube.AppsV1().Deployments("console-e2e-"+suffix).List(ctx, metav1.ListOptions{})
		if err != nil || len(deployments.Items) != 1 {
			t.Fatal("onboarding workload fixture missing")
		}
		requests := deployments.Items[0].Spec.Template.Spec.Containers[0].Resources.Requests
		if requests.Cpu().MilliValue() != int64(100+i*100) || requests.Memory().Value() != 32*1024*1024 {
			t.Fatal("onboarding unexpectedly changed workload resources")
		}
	}
	t.Log("empty protected console enrolled two real clusters with private credential files; wrong-context and replay attempts rejected; both agents exported Recommend inventory without server-config edits")
}

func onboardingCall(t *testing.T, endpoint, method, path, token string, body any, status int, result any) {
	onboardingCallClient(t, &http.Client{Timeout: 20 * time.Second}, endpoint, method, path, token, body, status, result)
}

func onboardingCallClient(t *testing.T, c *http.Client, endpoint, method, path, token string, body any, status int, result any) {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r, err := http.NewRequest(method, endpoint+path, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+token)
	response, err := c.Do(r)
	if err != nil {
		t.Fatal("onboarding HTTP request failed")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != status {
		t.Fatalf("%s %s: HTTP %d, expected %d", method, path, response.StatusCode, status)
	}
	if result != nil && json.NewDecoder(response.Body).Decode(result) != nil {
		t.Fatal("invalid onboarding response")
	}
}

func isolatedOnboardingStore(t *testing.T, ctx context.Context) *console.Store {
	t.Helper()
	dsn, err := os.ReadFile(os.Getenv("OPTIPOD_TEST_DATABASE_URL_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	admin, err := console.Open(ctx, string(dsn))
	if err != nil {
		t.Fatal(err)
	}
	name := "onboarding_" + uuid.NewString()[:8]
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err = admin.Pool.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		admin.Pool.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Pool.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE")
		admin.Pool.Close()
	})
	cfg, err := pgxpool.ParseConfig(string(dsn))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	s := &console.Store{Pool: pool}
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return s
}
