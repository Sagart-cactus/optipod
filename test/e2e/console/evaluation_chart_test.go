package consolee2e

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/optipod/optipod/internal/console"
	"github.com/optipod/optipod/internal/consoletls"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestEvaluationConsolePostgresAndTLSChart(t *testing.T) {
	ctx, clients, paths := helmSecretEnvironment(t)
	release := "evaluation-" + uuid.NewString()[:8]
	installed := helmValues(t, ctx, paths[0])
	pki := filepath.Join(t.TempDir(), "pki")
	if err := consoletls.Generate(pki, []string{"127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	keys := map[string]any{}
	for key, file := range map[string]string{"server.crt": "server.crt", "server.key": "server.key", "client-ca.crt": "ca.crt", "client-ca.key": "ca.key", "server-ca.crt": "ca.crt"} {
		data, err := os.ReadFile(filepath.Join(pki, file))
		if err != nil {
			t.Fatal(err)
		}
		keys[key] = string(data)
	}
	values := map[string]any{
		"image":         installed["image"],
		"networkPolicy": map[string]any{"enabled": true},
		"postgresql":    map[string]any{"enabled": true, "auth": map[string]any{"create": true, "password": uuid.NewString() + uuid.NewString()}, "persistence": map[string]any{"size": "1Gi"}},
		"server":        map[string]any{"secret": map[string]any{"create": true, "config": map[string]any{"publicURL": "http://127.0.0.1:8766", "bootstrapTokenHash": console.TokenHash(uuid.NewString() + uuid.NewString())}}, "agentTLS": map[string]any{"enabled": true, "publicURL": "https://127.0.0.1:8443", "secret": map[string]any{"create": true, "data": keys}}},
	}
	file := privateValuesFile(t, values)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		helmCommand(t, cleanup, paths[0], "uninstall", release, "--ignore-not-found", "--wait", "--timeout", "90s")
		_ = clients[0].Kube.CoreV1().PersistentVolumeClaims(consoleNamespace).Delete(cleanup, "data-"+release+"-postgresql-0", metav1.DeleteOptions{})
	})
	helmCommand(t, ctx, paths[0], "install", release, consoleChartPath(), "-f", file, "--wait", "--timeout", "180s")
	policy, err := clients[0].Kube.NetworkingV1().NetworkPolicies(consoleNamespace).Get(ctx, release+"-console", metav1.GetOptions{})
	if err != nil || len(policy.Spec.PolicyTypes) != 2 || len(policy.Spec.Egress) < 2 {
		t.Fatal("console NetworkPolicy missing default isolation and DNS/database paths", err)
	}
	pvc, err := clients[0].Kube.CoreV1().PersistentVolumeClaims(consoleNamespace).Get(ctx, "data-"+release+"-postgresql-0", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{release + "-server-config", release + "-agent-tls", release + "-postgresql"} {
		secret, err := clients[0].Kube.CoreV1().Secrets(consoleNamespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil || secret.Annotations["meta.helm.sh/release-name"] != release {
			t.Fatal("evaluation credential is not release-owned", err)
		}
	}
	service, err := clients[0].Kube.CoreV1().Services(consoleNamespace).Get(ctx, release+"-agent-api", metav1.GetOptions{})
	if err != nil || service.Spec.Ports[0].Port != 8443 {
		t.Fatal("dedicated mTLS service missing", err)
	}
	pods, err := clients[0].Kube.CoreV1().Pods(consoleNamespace).List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/instance=" + release})
	if err != nil || len(pods.Items) != 2 {
		t.Fatal("console-only evaluation installed unexpected components", err)
	}
	for _, pod := range pods.Items {
		if pod.Status.Phase != corev1.PodRunning {
			t.Fatal("evaluation pod did not start")
		}
	}
	helmCommand(t, ctx, paths[0], "upgrade", release, consoleChartPath(), "--reuse-values", "--wait", "--timeout", "180s")
	after, err := clients[0].Kube.CoreV1().PersistentVolumeClaims(consoleNamespace).Get(ctx, pvc.Name, metav1.GetOptions{})
	if err != nil || after.UID != pvc.UID {
		t.Fatal("evaluation database volume replaced", err)
	}
	t.Log("console-only release started with the PostgreSQL subchart and mTLS listener; all credentials are Helm owned and database PVC survived upgrade")
}
