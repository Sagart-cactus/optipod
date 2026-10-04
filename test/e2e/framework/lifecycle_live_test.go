package framework

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// TestLiveClusterLifecyclePreservesUserState exercises the real pinned Kind CLI.
// It creates an owned cluster, reaches its API through the private kubeconfig,
// then deletes it, asserting throughout that the developer's current context,
// default kubeconfig bytes, and pre-existing clusters are untouched. It is gated
// behind OPTIPOD_E2E_LIVE and skipped under -short so the standing fast gate does
// not provision Docker/Kind.
func TestLiveClusterLifecyclePreservesUserState(t *testing.T) {
	if testing.Short() || os.Getenv("OPTIPOD_E2E_LIVE") == "" {
		t.Skip("set OPTIPOD_E2E_LIVE=1 (and omit -short) to run the live Kind lifecycle test")
	}

	ctx := context.Background()
	beforeClusters := kindClusters(t)
	beforeContext := currentContext(t)
	beforeKubeconfig := defaultKubeconfigChecksum(t)

	// Preflight must pass before we attempt to own a cluster.
	if err := NewPreflighter().Check(ctx); err != nil {
		t.Fatalf("preflight failed on a host expected to be ready: %v", err)
	}

	c, err := CreateCluster(ctx, ClusterOptions{CreateTimeout: 6 * time.Minute})
	if err != nil {
		t.Fatalf("CreateCluster: %v", err)
	}
	deleted := false
	t.Cleanup(func() {
		if !deleted {
			_ = c.Delete(context.Background())
		}
	})

	// The owned cluster is present; pre-existing clusters are preserved.
	during := kindClusters(t)
	if !contains(during, c.Name) {
		t.Fatalf("owned cluster %q not listed after create: %v", c.Name, during)
	}
	for _, pre := range beforeClusters {
		if !contains(during, pre) {
			t.Fatalf("pre-existing cluster %q disappeared after create", pre)
		}
	}

	// The API is reachable through the private kubeconfig alone.
	cfg, err := c.RESTConfig()
	if err != nil {
		t.Fatalf("RESTConfig: %v", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("clientset: %v", err)
	}
	nsCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if _, err := cs.CoreV1().Namespaces().List(nsCtx, metav1.ListOptions{}); err != nil {
		t.Fatalf("list namespaces via private kubeconfig: %v", err)
	}

	// The developer's context and default kubeconfig are unchanged mid-run.
	if got := currentContext(t); got != beforeContext {
		t.Fatalf("current context changed during run: %q -> %q", beforeContext, got)
	}
	if got := defaultKubeconfigChecksum(t); got != beforeKubeconfig {
		t.Fatal("default kubeconfig bytes changed during run")
	}

	if err := c.Delete(ctx); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	deleted = true

	// Only the owned cluster was removed; everything else is preserved.
	after := kindClusters(t)
	if contains(after, c.Name) {
		t.Fatalf("owned cluster %q still present after delete: %v", c.Name, after)
	}
	for _, pre := range beforeClusters {
		if !contains(after, pre) {
			t.Fatalf("pre-existing cluster %q removed by cleanup", pre)
		}
	}
	if got := currentContext(t); got != beforeContext {
		t.Fatalf("current context changed after cleanup: %q -> %q", beforeContext, got)
	}
	if got := defaultKubeconfigChecksum(t); got != beforeKubeconfig {
		t.Fatal("default kubeconfig bytes changed after cleanup")
	}
	if _, err := os.Stat(c.Kubeconfig); !os.IsNotExist(err) {
		t.Errorf("private kubeconfig not cleaned up, stat err=%v", err)
	}
}

func kindClusters(t *testing.T) []string {
	t.Helper()
	out, err := exec.Command("kind", "get", "clusters").CombinedOutput()
	if err != nil {
		t.Fatalf("kind get clusters: %v (%s)", err, out)
	}
	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		if s := strings.TrimSpace(line); s != "" && s != "No kind clusters found." {
			names = append(names, s)
		}
	}
	return names
}

func currentContext(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("kubectl", "config", "current-context").CombinedOutput()
	if err != nil {
		// No current context is a valid, stable state to compare against.
		return "<none>: " + strings.TrimSpace(string(out))
	}
	return strings.TrimSpace(string(out))
}

func defaultKubeconfigChecksum(t *testing.T) string {
	t.Helper()
	path := os.Getenv("KUBECONFIG")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("resolve home: %v", err)
		}
		path = filepath.Join(home, ".kube", "config")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "<absent>"
		}
		t.Fatalf("read default kubeconfig: %v", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func contains(items []string, want string) bool {
	for _, i := range items {
		if i == want {
			return true
		}
	}
	return false
}
