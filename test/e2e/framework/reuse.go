package framework

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/client-go/tools/clientcmd"
)

// KindContextName returns the kubeconfig context and cluster name Kind writes
// for a cluster: always kind-<cluster name>. It is the stable identity a
// harness-reuse kubeconfig must carry for the requested cluster.
func KindContextName(cluster string) string { return "kind-" + cluster }

// isDefaultKubeconfigPath reports whether path names a developer's ambient
// kubeconfig rather than a harness-private file. Reuse must never accept
// $HOME/.kube/config or any path listed in $KUBECONFIG: those are the user's
// own clusters and pointing focused reuse at them would mutate state the
// harness does not own.
func isDefaultKubeconfigPath(path string) bool {
	target := canonicalPath(path)
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if canonicalPath(filepath.Join(home, ".kube", "config")) == target {
			return true
		}
	}
	for _, entry := range filepath.SplitList(os.Getenv("KUBECONFIG")) {
		if entry = strings.TrimSpace(entry); entry == "" {
			continue
		}
		if canonicalPath(entry) == target {
			return true
		}
	}
	return false
}

// canonicalPath resolves a path to a cleaned absolute form, following symlinks
// when the path exists, so equivalent spellings of the same file compare equal.
func canonicalPath(path string) string {
	cleaned := filepath.Clean(path)
	if abs, err := filepath.Abs(cleaned); err == nil {
		cleaned = abs
	}
	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
		cleaned = resolved
	}
	return cleaned
}

// bindKubeconfigToCluster proves the supplied private kubeconfig actually
// targets the named retained cluster instead of merely existing. It requires
// the kubeconfig's current context to select the cluster Kind derives for that
// name (kind-<name>), and cross-checks the kubeconfig's server endpoint against
// the endpoint Kind reports for the named cluster, so a config belonging to a
// different cluster (or the user's default config) cannot be passed off as the
// requested retained cluster's private kubeconfig.
func bindKubeconfigToCluster(ctx context.Context, run commandRunner, kindPath, name, kubeconfig string) error {
	supplied, err := clientcmd.LoadFromFile(kubeconfig)
	if err != nil {
		return fmt.Errorf("read private kubeconfig %q: %w", kubeconfig, err)
	}
	if supplied.CurrentContext == "" {
		return fmt.Errorf("refusing to reuse cluster %q: private kubeconfig %q names no current context", name, kubeconfig)
	}
	contextEntry, ok := supplied.Contexts[supplied.CurrentContext]
	if !ok {
		return fmt.Errorf("refusing to reuse cluster %q: private kubeconfig %q has no context %q",
			name, kubeconfig, supplied.CurrentContext)
	}
	wantName := KindContextName(name)
	if contextEntry.Cluster != wantName {
		return fmt.Errorf("refusing to reuse cluster %q: private kubeconfig %q targets cluster %q, not %q",
			name, kubeconfig, contextEntry.Cluster, wantName)
	}
	suppliedCluster, ok := supplied.Clusters[contextEntry.Cluster]
	if !ok {
		return fmt.Errorf("refusing to reuse cluster %q: private kubeconfig %q has no cluster %q",
			name, kubeconfig, contextEntry.Cluster)
	}
	suppliedServer := strings.TrimSpace(suppliedCluster.Server)
	if suppliedServer == "" {
		return fmt.Errorf("refusing to reuse cluster %q: private kubeconfig %q has no server endpoint", name, kubeconfig)
	}

	out, err := run(ctx, kindPath, "get", "kubeconfig", "--name", name)
	if err != nil {
		return fmt.Errorf("read kind kubeconfig for %q: %w (%s)", name, err, strings.TrimSpace(string(out)))
	}
	authoritative, err := clientcmd.Load(out)
	if err != nil {
		return fmt.Errorf("parse kind kubeconfig for cluster %q: %w", name, err)
	}
	wantCluster, ok := authoritative.Clusters[wantName]
	if !ok {
		return fmt.Errorf("kind kubeconfig for %q has no cluster %q", name, wantName)
	}
	wantServer := strings.TrimSpace(wantCluster.Server)
	if wantServer != suppliedServer {
		return fmt.Errorf("refusing to reuse cluster %q: private kubeconfig %q server %q does not match the named cluster's server %q",
			name, kubeconfig, suppliedServer, wantServer)
	}
	return nil
}
