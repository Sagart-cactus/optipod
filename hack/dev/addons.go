package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/optipod/optipod/test/e2e/framework"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Install only after ReuseOwnedCluster has verified the isolated test target.
func installAddons(ctx context.Context, root string, s state) error {
	for key, dir := range map[string]string{"HELM_CACHE_HOME": "cache",
		"HELM_CONFIG_HOME": "config",
		"HELM_DATA_HOME":   "data"} {
		if err := os.Setenv(key, filepath.Join(root, ".dev/helm", dir)); err != nil {
			return err
		}
	}
	if err := command(ctx,
		"helm",
		"upgrade",
		"--install",
		"optipod-e2e-keda",
		"keda",
		"--repo",
		"https://kedacore.github.io/charts",
		"--version",
		"2.21.0",
		"--namespace",
		"optipod-e2e-keda",
		"--create-namespace",
		"--kubeconfig",
		s.Kubeconfig,
		"--wait",
		"--timeout",
		"5m"); err != nil {
		return err
	}
	cluster, err := framework.ReuseOwnedCluster(ctx, s.Name, s.Kubeconfig, framework.ClusterOptions{})
	if err != nil {
		return err
	}
	cfg, err := cluster.RESTConfig()
	if err != nil {
		return err
	}
	clients, err := framework.NewClients(cfg)
	if err != nil {
		return err
	}
	_,
		err = clients.Kube.CoreV1().Namespaces().Create(ctx,
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "argo-rollouts"}},
		metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	req,
		err := http.NewRequestWithContext(ctx,
		http.MethodGet,
		"https://github.com/argoproj/argo-rollouts/releases/download/v1.10.2/install.yaml",
		nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("argo manifest HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 10*1024*1024))
	if err != nil {
		return err
	}
	hash := sha256.Sum256(data)
	// SHA256 of the public release manifest.
	const expectedSHA256 = "b8280994791eaee7eb91c8b84450efcc6e0292fca5aafc18c1b0ebc5d18dbefe" // pragma: allowlist secret
	if hex.EncodeToString(hash[:]) != expectedSHA256 {
		return fmt.Errorf("argo release manifest digest mismatch")
	}
	path := filepath.Join(root, ".dev/argo-rollouts-v1.10.2.yaml")
	if err = os.WriteFile(path, data, 0600); err != nil {
		return err
	}
	if err = command(ctx,
		"kubectl",
		"--kubeconfig",
		s.Kubeconfig,
		"apply",
		"--server-side",
		"-n",
		"argo-rollouts",
		"-f",
		path); err != nil {
		return err
	}
	return command(ctx,
		"kubectl",
		"--kubeconfig",
		s.Kubeconfig,
		"-n",
		"argo-rollouts",
		"rollout",
		"status",
		"deployment/argo-rollouts",
		"--timeout=300s")
}
