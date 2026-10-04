package framework

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const defaultCreateTimeout = 5 * time.Minute

// Cluster is a Kind cluster scoped to a single harness run. It always carries a
// private kubeconfig path; the harness never reads or mutates the developer's
// default kubeconfig or current context, and it only deletes a cluster this run
// owns.
type Cluster struct {
	Name       string
	Kubeconfig string // path to a private kubeconfig file, never the default

	kindPath string
	owned    bool // true only when this run created (or verifiably attached to) the cluster
	keep     bool // retain on cleanup (KEEP_CLUSTER or focused reuse)
	tmpDir   string
	run      commandRunner
}

// ClusterOptions configures cluster creation and reuse.
type ClusterOptions struct {
	Name          string
	CreateTimeout time.Duration
	Keep          bool
	KindPath      string
	NodeImage     string
	Run           commandRunner
}

// OptionsFromEnv derives cluster options from the environment. KEEP_CLUSTER
// opts into retention; everything else uses safe defaults.
func OptionsFromEnv() ClusterOptions {
	return ClusterOptions{Keep: keepClusterRequested()}
}

func keepClusterRequested() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("KEEP_CLUSTER"))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

// UniqueClusterName returns a collision-resistant, DNS-safe cluster name carrying
// the harness prefix.
func UniqueClusterName() (string, error) {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate cluster suffix: %w", err)
	}
	return fmt.Sprintf("%s%d-%s", ClusterNamePrefix, time.Now().Unix(), hex.EncodeToString(buf)), nil
}

func (o ClusterOptions) runner() commandRunner {
	if o.Run != nil {
		return o.Run
	}
	return execRunner
}

func (o ClusterOptions) kind() string {
	if o.KindPath != "" {
		return o.KindPath
	}
	return "kind"
}

// CreateCluster provisions a uniquely named owned Kind cluster with a private
// kubeconfig, created with the pinned Kind CLI and pinned node image. On failure
// it best-effort removes any partially created cluster.
func CreateCluster(ctx context.Context, opts ClusterOptions) (*Cluster, error) {
	name := opts.Name
	if name == "" {
		var err error
		if name, err = UniqueClusterName(); err != nil {
			return nil, err
		}
	}
	if !strings.HasPrefix(name, ClusterNamePrefix) {
		return nil, fmt.Errorf("refusing to create cluster %q without harness prefix %q", name, ClusterNamePrefix)
	}

	tmpDir, err := os.MkdirTemp("", "optipod-e2e-kubeconfig-")
	if err != nil {
		return nil, fmt.Errorf("create private kubeconfig dir: %w", err)
	}

	nodeImage := opts.NodeImage
	if nodeImage == "" {
		nodeImage = NodeImage()
	}
	c := &Cluster{
		Name:       name,
		Kubeconfig: filepath.Join(tmpDir, "kubeconfig"),
		kindPath:   opts.kind(),
		keep:       opts.Keep,
		tmpDir:     tmpDir,
		run:        opts.runner(),
	}

	timeout := opts.CreateTimeout
	if timeout <= 0 {
		timeout = defaultCreateTimeout
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{
		"create", "cluster",
		"--name", name,
		"--kubeconfig", c.Kubeconfig,
		"--image", nodeImage,
		"--wait", "120s",
	}
	if out, err := c.run(cctx, c.kindPath, args...); err != nil {
		// Attempting creation establishes ownership; tear down any partial cluster
		// with the private kubeconfig so a failed create leaves nothing behind.
		c.owned = true
		_ = c.Delete(context.Background())
		return nil, fmt.Errorf("create kind cluster %q: %w (%s)", name, err, strings.TrimSpace(string(out)))
	}
	c.owned = true

	if err := os.Chmod(c.Kubeconfig, 0o600); err != nil {
		return nil, fmt.Errorf("secure private kubeconfig: %w", err)
	}
	return c, nil
}

// ReuseOwnedCluster attaches to a previously retained harness cluster for focused
// reruns. It accepts only a cluster that carries the harness prefix, has a
// readable private kubeconfig, and is actually present among Kind clusters. A
// reused cluster is treated as user-managed and is never auto-deleted.
func ReuseOwnedCluster(ctx context.Context, name, kubeconfig string, opts ClusterOptions) (*Cluster, error) {
	if !strings.HasPrefix(name, ClusterNamePrefix) {
		return nil, fmt.Errorf("refusing to reuse cluster %q: missing harness prefix %q", name, ClusterNamePrefix)
	}
	if kubeconfig == "" {
		return nil, fmt.Errorf("refusing to reuse cluster %q: no private kubeconfig provided", name)
	}
	if _, err := os.Stat(kubeconfig); err != nil {
		return nil, fmt.Errorf("private kubeconfig %q unavailable: %w", kubeconfig, err)
	}

	run := opts.runner()
	kindPath := opts.kind()
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := run(cctx, kindPath, "get", "clusters")
	if err != nil {
		return nil, fmt.Errorf("list kind clusters: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	if !clusterListed(string(out), name) {
		return nil, fmt.Errorf("refusing to reuse cluster %q: not present among kind clusters", name)
	}
	return &Cluster{
		Name:       name,
		Kubeconfig: kubeconfig,
		kindPath:   kindPath,
		owned:      true,
		keep:       true,
		run:        run,
	}, nil
}

// Delete removes the owned cluster and private kubeconfig. It is ownership-aware
// (refusing any cluster this run does not own or that lacks the harness prefix),
// idempotent (treating an already-absent cluster as success), and a no-op when
// retention is requested.
func (c *Cluster) Delete(ctx context.Context) error {
	if c.keep {
		return nil
	}
	if !c.owned {
		return fmt.Errorf("refusing to delete cluster %q: not owned by this run", c.Name)
	}
	if !strings.HasPrefix(c.Name, ClusterNamePrefix) {
		return fmt.Errorf("refusing to delete cluster %q: missing harness prefix", c.Name)
	}

	cctx, cancel := context.WithTimeout(ctx, defaultCleanupTimeout)
	defer cancel()
	out, err := c.run(cctx, c.kindPath, "delete", "cluster", "--name", c.Name, "--kubeconfig", c.Kubeconfig)
	if err != nil && !isNotFound(string(out)) {
		return fmt.Errorf("delete kind cluster %q: %w (%s)", c.Name, err, strings.TrimSpace(string(out)))
	}
	if c.tmpDir != "" {
		if rmErr := os.RemoveAll(c.tmpDir); rmErr != nil {
			return fmt.Errorf("remove private kubeconfig dir: %w", rmErr)
		}
	}
	return nil
}

// RESTConfig derives a client-go configuration from the private kubeconfig only.
func (c *Cluster) RESTConfig() (*rest.Config, error) {
	cfg, err := clientcmd.BuildConfigFromFlags("", c.Kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("build rest config from private kubeconfig: %w", err)
	}
	return cfg, nil
}

// Owned reports whether this run owns the cluster.
func (c *Cluster) Owned() bool { return c.owned }

// Keep reports whether the cluster is retained on cleanup.
func (c *Cluster) Keep() bool { return c.keep }

// InspectCommands returns copy-pasteable commands to inspect and dispose of a
// retained owned cluster. It references the private kubeconfig by path only and
// never prints its contents.
func (c *Cluster) InspectCommands() []string {
	return []string{
		fmt.Sprintf("export KUBECONFIG=%s", c.Kubeconfig),
		fmt.Sprintf("kubectl --kubeconfig %s get pods -A", c.Kubeconfig),
		fmt.Sprintf("%s delete cluster --name %s --kubeconfig %s", c.kindPath, c.Name, c.Kubeconfig),
	}
}

// PrintRetentionNotice writes the retained cluster name, private kubeconfig path,
// and inspection/disposal commands.
func (c *Cluster) PrintRetentionNotice(w io.Writer) {
	_, _ = fmt.Fprintf(w, "KEEP_CLUSTER set: retaining owned Kind cluster %q\n", c.Name)
	_, _ = fmt.Fprintf(w, "private kubeconfig: %s\n", c.Kubeconfig)
	for _, cmd := range c.InspectCommands() {
		_, _ = fmt.Fprintf(w, "  %s\n", cmd)
	}
}

func clusterListed(output, name string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == name {
			return true
		}
	}
	return false
}

func isNotFound(output string) bool {
	o := strings.ToLower(output)
	return strings.Contains(o, "not exist") ||
		strings.Contains(o, "no such") ||
		strings.Contains(o, "not found")
}
