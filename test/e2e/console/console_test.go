// Package consolee2e validates real Kubernetes sources and PostgreSQL together.
package consolee2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/console"
	"github.com/optipod/optipod/internal/consoleagent"
	"github.com/optipod/optipod/internal/discovery"
	"github.com/optipod/optipod/internal/history"
	"github.com/optipod/optipod/test/e2e/framework"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestMultiClusterPersistence(t *testing.T) {
	paths := filepath.SplitList(os.Getenv("OPTIPOD_CONSOLE_E2E_KUBECONFIGS"))
	if testing.Short() || len(paths) != 2 {
		t.Skip("set two explicit owned Kind kubeconfigs for console E2E")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	clients := ownedClients(t, ctx, paths)
	store := openStore(t, ctx)
	var err error
	suffix := uuid.NewString()[:8]
	nsName := "console-e2e-" + suffix
	ids := []string{"qa-" + suffix, "prod-" + suffix}
	cfg := console.Config{Readers: []console.Reader{{Subject: "tester", TokenHash: console.TokenHash("test-viewer"), Clusters: ids}}}
	tokens := []string{uuid.NewString(), uuid.NewString()}
	for i, id := range ids {
		cfg.Agents = append(cfg.Agents, console.AgentCredential{ID: id, Name: id, TokenHash: console.TokenHash(tokens[i])})
	}
	var unavailable atomic.Bool
	server := httptest.NewUnstartedServer(nil)
	cfg.PublicURL = "http://" + server.Listener.Addr().String()
	apiServer := &console.Server{Store: store, Config: cfg}
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if unavailable.Load() && r.URL.Path == "/api/ingest" {
			http.Error(w, "offline", http.StatusServiceUnavailable)
			return
		}
		apiServer.Handler().ServeHTTP(w, r)
	})
	server.Start()
	defer server.Close()
	options := prepareSources(t, ctx, clients, paths, ids, tokens, nsName, server.URL)
	page, err := store.List(ctx, "workloads", console.Query{Clusters: ids})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("two clusters: %+v %v", page, err)
	}
	if page.Items[0].ClusterID == page.Items[1].ClusterID {
		t.Fatal("cluster collision")
	}
	// Capture while the service is unavailable, restart the agent, and replay.
	unavailable.Store(true)
	if err = consoleagent.Run(ctx, options[0]); err == nil {
		t.Fatal("outage reported success")
	}
	pending, _ := filepath.Glob(filepath.Join(options[0].QueueDir, "batch-*.json"))
	if len(pending) == 0 {
		t.Fatal("outage was not queued")
	}
	unavailable.Store(false)
	if err = consoleagent.Run(ctx, options[0]); err != nil {
		t.Fatal(err)
	}
	pending, _ = filepath.Glob(filepath.Join(options[0].QueueDir, "batch-*.json"))
	if len(pending) != 0 {
		t.Fatal("replay not acknowledged")
	}
	// Delete an entire source namespace. Central history remains queryable.
	if err = clients[0].Ctrl.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}); err != nil {
		t.Fatal(err)
	}
	for {
		ns := &corev1.Namespace{}
		err = clients[0].Ctrl.Get(ctx, client.ObjectKey{Name: nsName}, ns)
		if client.IgnoreNotFound(err) == nil && err != nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("namespace deletion timed out")
		case <-time.After(time.Second):
		}
	}
	if err = consoleagent.Run(ctx, options[0]); err != nil {
		t.Fatal(err)
	}
	page, err = store.List(ctx, "workloads", console.Query{Clusters: []string{ids[0]}})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("deleted source remained active")
	}
	page, err = store.List(ctx, "history", console.Query{Clusters: []string{ids[0]}})
	if err != nil || len(page.Items) == 0 {
		t.Fatal("source deletion lost persistent history")
	}
	// HTTP query scope is enforced, and exported JSON is a stable API artifact.
	r, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/history?cluster="+ids[0], nil)
	r.Header.Set("Authorization", "Bearer test-viewer")
	response, err := server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if err = json.NewDecoder(response.Body).Decode(&page); err != nil || response.StatusCode != 200 || len(page.Items) == 0 {
		t.Fatal("history API failed")
	}
	t.Logf("two real clusters, durable replay, namespace deletion and persistent history passed (%s)", suffix)
}

func seedWorkload(t *testing.T, ctx context.Context, c *framework.Clients, ns string, index int) *appsv1.Deployment {
	t.Helper()
	replicas := int32(1)
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "same-name", Namespace: ns, Labels: map[string]string{"app": "test-workload"}}, Spec: appsv1.DeploymentSpec{Replicas: &replicas, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test-workload"}}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "test-workload"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: framework.WorkloadImage(), Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(fmt.Sprintf("%dm", 100+index*100)), corev1.ResourceMemory: resource.MustParse("32Mi")}}}}}}}}
	if err := c.Ctrl.Create(ctx, dep); err != nil {
		t.Fatal(err)
	}
	return dep
}

func prepareSources(t *testing.T, ctx context.Context, clients []*framework.Clients, paths, ids, tokens []string, nsName, serverURL string) []consoleagent.Options {
	t.Helper()
	options := make([]consoleagent.Options, 2)
	for i, c := range clients {
		if err := c.Ctrl.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = c.Ctrl.Delete(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}})
		})
		dep := seedWorkload(t, ctx, c, nsName, i)
		p := framework.NewPolicyFixture(nsName, "shared-policy", api.ModeDisabled)
		if err := c.Ctrl.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
		w := &discovery.Workload{Object: dep, Kind: "Deployment", Namespace: nsName, Name: dep.Name}
		if err := history.ObserveDecision(ctx, c.Ctrl, w, p, &api.WorkloadStatus{Status: "Skipped", Reason: "E2E observed decision"}, nil); err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		tokenFile := filepath.Join(dir, "token")
		if err := os.WriteFile(tokenFile, []byte(tokens[i]), 0600); err != nil {
			t.Fatal(err)
		}
		options[i] = consoleagent.Options{ClusterID: ids[i], Kubeconfig: paths[i], QueueDir: filepath.Join(dir, "queue"), URL: serverURL, TokenFile: tokenFile, Namespaces: []string{nsName}, Interval: time.Second, QueueBytes: 32 * 1024 * 1024, Once: true, AllowHTTP: true}
		if err := consoleagent.Run(ctx, options[i]); err != nil {
			t.Fatal(err)
		}
	}

	return options
}

func ownedClients(t *testing.T, ctx context.Context, paths []string) []*framework.Clients {
	t.Helper()
	clients := make([]*framework.Clients, 2)
	for i, name := range []string{"optipod-e2e-dev", "optipod-e2e-console-peer"} {
		cluster, err := framework.ReuseOwnedCluster(ctx, name, paths[i], framework.ClusterOptions{})
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := cluster.RESTConfig()
		if err != nil {
			t.Fatal(err)
		}
		clients[i], err = framework.NewClients(cfg)
		if err != nil {
			t.Fatal(err)
		}
	}

	return clients
}

func openStore(t *testing.T, ctx context.Context) *console.Store {
	t.Helper()
	dsn, err := os.ReadFile(os.Getenv("OPTIPOD_TEST_DATABASE_URL_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := console.Open(ctx, string(dsn))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Pool.Close)
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	return store
}
