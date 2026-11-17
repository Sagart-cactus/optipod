package consolee2e

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/application"
	"github.com/optipod/optipod/internal/console"
	"github.com/optipod/optipod/internal/consoleagent"
	"github.com/optipod/optipod/internal/controller"
	"github.com/optipod/optipod/internal/discovery"
	"github.com/optipod/optipod/internal/metrics"
	"github.com/optipod/optipod/internal/recommendation"
	"github.com/optipod/optipod/internal/report"
	"github.com/optipod/optipod/test/e2e/framework"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubediscovery "k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/retry"
)

type longNameMetrics struct{}

func (longNameMetrics) HealthCheck(context.Context) error { return nil }
func (longNameMetrics) GetContainerMetrics(_ context.Context, _, _, container string, _ time.Duration) (*metrics.ContainerMetrics, error) {
	cpu, memory := "300m", "256Mi"
	if len(container) == 63 {
		cpu, memory = "600m", "512Mi"
	}
	return &metrics.ContainerMetrics{CPU: metrics.ResourceMetrics{P90: resource.MustParse(cpu), Samples: 50}, Memory: metrics.ResourceMetrics{P90: resource.MustParse(memory), Samples: 50}}, nil
}

func TestLongContainerRecommendationsReachConsole(t *testing.T) {
	paths := filepath.SplitList(os.Getenv("OPTIPOD_CONSOLE_E2E_KUBECONFIGS"))
	if testing.Short() || len(paths) != 2 {
		t.Skip("set two owned Kind kubeconfigs")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	clients := ownedClients(t, ctx, paths)
	store := isolatedOnboardingStore(t, ctx)
	for index, c := range clients {
		ns := "long-name-recommend-" + uuid.NewString()[:8]
		if err := c.Ctrl.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = c.Ctrl.Delete(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})
		})
		first, second := "application-background-processing-worker", strings.Repeat("b", 63)
		dep := framework.NewDeploymentFixture(ns, "long-names", 1,
			framework.ContainerFixture{Name: first, CPURequest: "100m", MemoryRequest: "32Mi"},
			framework.ContainerFixture{Name: second, CPURequest: "100m", MemoryRequest: "32Mi"})
		if err := c.Ctrl.Create(ctx, dep); err != nil {
			t.Fatal(err)
		}
		wait(t, ctx, "long-name workload ready", func() bool {
			x, err := c.Kube.AppsV1().Deployments(ns).Get(ctx, dep.Name, metav1.GetOptions{})
			if err != nil || x.Status.ReadyReplicas != 1 || x.Status.ObservedGeneration != x.Generation {
				return false
			}
			dep = x
			return true
		})
		before := dep.Spec.DeepCopy()
		policy := framework.NewPolicyFixture(ns, "recommend-long-names", api.ModeRecommend)
		policy.Spec.MetricsConfig.Provider = "prometheus"
		policy.Spec.MetricsConfig.Percentile = "P90"
		factor := 1.0
		policy.Spec.MetricsConfig.SafetyFactor = &factor
		if err := c.Ctrl.Create(ctx, policy); err != nil {
			t.Fatal(err)
		}
		dyn, err := dynamic.NewForConfig(c.RESTConfig())
		if err != nil {
			t.Fatal(err)
		}
		disc, err := kubediscovery.NewDiscoveryClientForConfig(c.RESTConfig())
		if err != nil {
			t.Fatal(err)
		}
		processor := controller.NewWorkloadProcessor(longNameMetrics{}, recommendation.NewEngine(), application.NewEngine(c.Ctrl, dyn, disc, true), c.Ctrl)
		var status *api.WorkloadStatus
		err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
			current, getErr := c.Kube.AppsV1().Deployments(ns).Get(ctx, dep.Name, metav1.GetOptions{})
			if getErr != nil {
				return getErr
			}
			var processErr error
			status, processErr = processor.ProcessWorkload(ctx, &discovery.Workload{Kind: "Deployment", Namespace: ns, Name: dep.Name, Object: current}, policy)
			return processErr
		})
		if err != nil || status == nil || status.Status != controller.StatusRecommended {
			t.Fatalf("long-name recommendation failed: status=%+v error=%v", status, err)
		}
		after, err := c.Kube.AppsV1().Deployments(ns).Get(ctx, dep.Name, metav1.GetOptions{})
		if err != nil || !reflect.DeepEqual(*before, after.Spec) {
			t.Fatal("recommendation changed the workload specification", err)
		}
		token := uuid.NewString() + uuid.NewString()
		server := httptest.NewUnstartedServer(nil)
		config := console.Config{PublicURL: "http://" + server.Listener.Addr().String(), Agents: []console.AgentCredential{{ID: ns, Name: ns, TokenHash: console.TokenHash(token)}}}
		server.Config.Handler = (&console.Server{Store: store, Config: config}).Handler()
		server.Start()
		t.Cleanup(server.Close)
		tokenFile := filepath.Join(t.TempDir(), "token")
		if err = os.WriteFile(tokenFile, []byte(token), 0600); err != nil {
			t.Fatal(err)
		}
		if err = consoleagent.Run(ctx, consoleagent.Options{Kubeconfig: paths[index], ClusterID: ns, QueueDir: t.TempDir(), URL: server.URL, TokenFile: tokenFile, Namespaces: []string{ns}, Interval: time.Second, QueueBytes: 16 * 1024 * 1024, AllowHTTP: true, Once: true}); err != nil {
			t.Fatal(err)
		}
		page, err := store.List(ctx, "workloads", console.Query{Clusters: []string{ns}})
		if err != nil || len(page.Items) != 1 {
			t.Fatal("long-name inventory missing", err)
		}
		var workload report.Workload
		if err = json.Unmarshal(page.Items[0].Data, &workload); err != nil || len(workload.Containers) != 2 {
			t.Fatal("long-name containers missing", err)
		}
		for _, container := range workload.Containers {
			want := "300m"
			if container.Name == second {
				want = "600m"
			}
			if container.Recommended["cpu-request"] != want {
				t.Fatalf("container %s recommendation missing from PostgreSQL inventory", container.Name)
			}
		}
	}
	t.Log("long-name recommendations persisted through two real Kubernetes APIs, agent ingestion and PostgreSQL without workload spec changes")
}
