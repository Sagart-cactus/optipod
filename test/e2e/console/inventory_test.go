package consolee2e

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/console"
	"github.com/optipod/optipod/internal/consoleagent"
	"github.com/optipod/optipod/internal/report"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

func TestInventoryBeforePolicyAndHeartbeat(t *testing.T) {
	paths := filepath.SplitList(os.Getenv("OPTIPOD_CONSOLE_E2E_KUBECONFIGS"))
	if testing.Short() || len(paths) != 2 {
		t.Skip("set two owned Kind kubeconfigs")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	clients := ownedClients(t, ctx, paths)
	store := isolatedOnboardingStore(t, ctx)
	ns := "onboarding-inventory-" + uuid.NewString()[:8]
	_, err := clients[0].Kube.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = clients[0].Kube.CoreV1().Namespaces().Delete(context.Background(), ns, metav1.DeleteOptions{})
	})
	dep := seedWorkload(t, ctx, clients[0], ns, 0)
	token := uuid.NewString() + uuid.NewString()
	server := httptest.NewUnstartedServer(nil)
	cfg := console.Config{PublicURL: "http://" + server.Listener.Addr().String(), Agents: []console.AgentCredential{{ID: "unmanaged", Name: "Unmanaged", TokenHash: console.TokenHash(token)}}}
	server.Config.Handler = (&console.Server{Store: store, Config: cfg}).Handler()
	server.Start()
	defer server.Close()
	tokenPath := filepath.Join(t.TempDir(), "token")
	if err = os.WriteFile(tokenPath, []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	options := consoleagent.Options{Kubeconfig: paths[0], ClusterID: "unmanaged", QueueDir: t.TempDir(), URL: server.URL, TokenFile: tokenPath, Namespaces: []string{ns}, Interval: time.Second, QueueBytes: 16 * 1024 * 1024, AllowHTTP: true, Once: true}
	if err = consoleagent.Run(ctx, options); err != nil {
		t.Fatal(err)
	}
	page, err := store.List(ctx, "workloads", console.Query{Clusters: []string{"unmanaged"}})
	if err != nil || len(page.Items) != 1 {
		t.Fatal("unmanaged inventory missing", err)
	}
	var workload report.Workload
	if err = json.Unmarshal(page.Items[0].Data, &workload); err != nil || workload.Mode != "Unmanaged" || workload.Policy != "" {
		t.Fatal("inventory was not marked unmanaged", err)
	}
	health, err := store.Health(ctx)
	if err != nil || len(health.Clusters) != 1 || health.Clusters[0].HeartbeatReceived == nil || health.Clusters[0].Agent.Version == "" {
		t.Fatal("health heartbeat missing", err)
	}
	// Apply the generated artifact to the real API and confirm inventory changes
	// from Unmanaged to Recommend while workload requests remain untouched.
	artifact, e := console.RecommendPolicy(console.FirstPolicy{ClusterID: "unmanaged", Name: "first-policy", Namespace: ns, LabelKey: "app", LabelValue: "test-workload", CPUMin: "50m", CPUMax: "2", MemoryMin: "32Mi", MemoryMax: "2Gi"}, console.Registration{ID: "unmanaged", Namespaces: []string{ns}, MetricsSource: "prometheus"})
	if e != nil {
		t.Fatal(e)
	}
	var policy api.OptimizationPolicy
	if e = yaml.Unmarshal([]byte(artifact), &policy); e != nil {
		t.Fatal(e)
	}
	if e = clients[0].Ctrl.Create(ctx, &policy); e != nil {
		t.Fatal("generated Recommend policy rejected by Kubernetes", e)
	}
	if e = consoleagent.Run(ctx, options); e != nil {
		t.Fatal(e)
	}
	page, e = store.List(ctx, "workloads", console.Query{Clusters: []string{"unmanaged"}})
	if e != nil || len(page.Items) != 1 || json.Unmarshal(page.Items[0].Data, &workload) != nil || workload.Mode != "Recommend" {
		t.Fatal("first policy did not appear as Recommend", e)
	}
	after, err := clients[0].Kube.AppsV1().Deployments(ns).Get(ctx, dep.Name, metav1.GetOptions{})
	if err != nil || after.Spec.Template.Spec.Containers[0].Resources.Requests.Cpu().MilliValue() != 100 {
		t.Fatal("inventory changed resources", err)
	}
	t.Log("unmanaged inventory, generated Recommend policy admission, heartbeat and unchanged workload requests passed")
}
