// Local console tooling only operates on explicitly named, harness-owned Kind clusters.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/optipod/optipod/internal/console"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func main() {
	action := flag.String("action", "validate", "validate-primary, validate, wait-database or seed")
	flag.Parse()
	if err := run(*action); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(action string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	root := framework.DefaultRepoRoot()
	if action == "wait-database" {
		return waitDatabase(ctx, root)
	}
	data, err := os.ReadFile(filepath.Join(root, ".dev/cluster.json"))
	if err != nil {
		return err
	}
	var state struct{ Name, Kubeconfig string }
	if err = json.Unmarshal(data, &state); err != nil {
		return err
	}
	if state.Name != "optipod-e2e-dev" {
		return fmt.Errorf("expected the isolated optipod-e2e-dev cluster")
	}
	names := []string{state.Name}
	paths := []string{state.Kubeconfig}
	if action != "validate-primary" {
		names = append(names, "optipod-e2e-console-peer")
		paths = append(paths, filepath.Join(root, ".dev/console/peer.kubeconfig"))
	}
	for i, name := range names {
		cluster, e := framework.ReuseOwnedCluster(ctx, name, paths[i], framework.ClusterOptions{})
		if e != nil {
			return e
		}
		if action != "seed" {
			continue
		}
		cfg, e := cluster.RESTConfig()
		if e != nil {
			return e
		}
		c, e := framework.NewClients(cfg)
		if e != nil {
			return e
		}
		if e = seed(ctx, c); e != nil {
			return e
		}
	}
	return nil
}

func seed(ctx context.Context, c *framework.Clients) error {
	const ns = "optipod-console-demo"
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}
	if err := c.Ctrl.Create(ctx, namespace); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	for _, name := range []string{"checkout-api", "payment-worker", "catalog-api"} {
		container := framework.ContainerFixture{Name: "app", CPURequest: "250m", MemoryRequest: "128Mi"}
		dep := framework.NewDeploymentFixture(ns, name, 1, container)
		if err := c.Ctrl.Create(ctx, dep); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
	}
	p := framework.NewPolicyFixture(ns, "observe-demo", api.ModeRecommend)
	if err := c.Ctrl.Create(ctx, p); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// A replaced pod invalidates a kubectl port-forward lazily on its next connection.
// Probe SQL before running tests so reconnecting the test transport is explicit.
func waitDatabase(ctx context.Context, root string) error {
	data, err := os.ReadFile(filepath.Join(root, ".dev/console/database-url"))
	if err != nil {
		return err
	}
	s, err := console.Open(ctx, strings.TrimSpace(string(data)))
	if err != nil {
		return err
	}
	defer s.Pool.Close()
	for {
		probe, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = s.Pool.Ping(probe)
		cancel()
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("local database forward did not become ready")
		case <-time.After(time.Second):
		}
	}
}
