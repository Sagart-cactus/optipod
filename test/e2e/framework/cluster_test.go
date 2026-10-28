package framework

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// fakeRunner records invocations and simulates the pinned Kind CLI without a real
// cluster. For "create cluster" it writes the private kubeconfig file so that the
// production chmod path can run, and it answers `kind get kubeconfig --name`
// from kindKubeconfigs so the reuse-binding check can be exercised hermetically.
type fakeRunner struct {
	calls       [][]string
	clustersOut string
	deleteErr   error
	deleteOut   string
	// kindKubeconfigs maps a cluster name to the kubeconfig Kind would report
	// for it via `kind get kubeconfig --name <name>`.
	kindKubeconfigs map[string]string
}

func (f *fakeRunner) run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	switch {
	case len(args) >= 2 && args[0] == "create" && args[1] == "cluster":
		if kc := flagValue(args, "--kubeconfig"); kc != "" {
			if err := os.WriteFile(kc, []byte("apiVersion: v1\nkind: Config\n"), 0o600); err != nil {
				return nil, err
			}
		}
		return []byte("created"), nil
	case len(args) >= 2 && args[0] == "delete" && args[1] == "cluster":
		return []byte(f.deleteOut), f.deleteErr
	case len(args) >= 2 && args[0] == "get" && args[1] == "clusters":
		return []byte(f.clustersOut), nil
	case len(args) >= 2 && args[0] == "get" && args[1] == "kubeconfig":
		cluster := flagValue(args, "--name")
		if cfg, ok := f.kindKubeconfigs[cluster]; ok {
			return []byte(cfg), nil
		}
		return nil, fmt.Errorf("no kind cluster named %q", cluster)
	default:
		return []byte("ok"), nil
	}
}

// kindShapedKubeconfig returns the YAML a Kind-written kubeconfig carries for a
// cluster: a kind-<name> context/cluster with the given server endpoint and no
// auth material, which is all the reuse-binding check inspects.
func kindShapedKubeconfig(t *testing.T, cluster, server string) []byte {
	t.Helper()
	entry := KindContextName(cluster)
	data, err := clientcmd.Write(clientcmdapi.Config{
		CurrentContext: entry,
		Contexts:       map[string]*clientcmdapi.Context{entry: {Cluster: entry}},
		Clusters:       map[string]*clientcmdapi.Cluster{entry: {Server: server}},
	})
	if err != nil {
		t.Fatalf("encode kind-shaped kubeconfig: %v", err)
	}
	return data
}

// writeKubeconfigFile writes data to dir/kubeconfig and returns its path.
func writeKubeconfigFile(t *testing.T, dir string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	return path
}

// assertNoMutation proves a refused reuse touched no cluster: the simulated Kind
// CLI must only have been asked to read state.
func assertNoMutation(t *testing.T, f *fakeRunner) {
	t.Helper()
	for _, call := range f.calls {
		if len(call) >= 2 && (call[1] == "create" || call[1] == "delete") {
			t.Fatalf("refused reuse mutated state via %v", call)
		}
	}
}

func flagValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func TestUniqueClusterNameIsPrefixedAndDistinct(t *testing.T) {
	a, err := UniqueClusterName()
	if err != nil {
		t.Fatal(err)
	}
	b, err := UniqueClusterName()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(a, ClusterNamePrefix) || !strings.HasPrefix(b, ClusterNamePrefix) {
		t.Fatalf("names missing prefix: %q %q", a, b)
	}
	if a == b {
		t.Fatalf("expected distinct names, got %q twice", a)
	}
}

func TestCreateClusterUsesPrivateKubeconfigAndPins(t *testing.T) {
	f := &fakeRunner{}
	c, err := CreateCluster(context.Background(), ClusterOptions{Run: f.run})
	if err != nil {
		t.Fatalf("CreateCluster: %v", err)
	}
	t.Cleanup(func() { _ = c.Delete(context.Background()) })

	if !c.Owned() {
		t.Error("expected created cluster to be owned")
	}
	if !strings.HasPrefix(c.Name, ClusterNamePrefix) {
		t.Errorf("cluster name %q lacks harness prefix", c.Name)
	}
	// The private kubeconfig must live under a temp dir, not the default location.
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(c.Kubeconfig, filepath.Join(home, ".kube")) {
		t.Errorf("kubeconfig %q points at the default location", c.Kubeconfig)
	}
	if _, err := os.Stat(c.Kubeconfig); err != nil {
		t.Errorf("private kubeconfig not written: %v", err)
	}

	create := f.calls[0]
	if got := flagValue(create, "--image"); got != NodeImage() {
		t.Errorf("expected pinned node image %q, got %q", NodeImage(), got)
	}
	if got := flagValue(create, "--kubeconfig"); got != c.Kubeconfig {
		t.Errorf("create did not use the private kubeconfig: %q", got)
	}
	if got := flagValue(create, "--name"); got != c.Name {
		t.Errorf("create used name %q, expected %q", got, c.Name)
	}
}

func TestCreateClusterRejectsForeignName(t *testing.T) {
	_, err := CreateCluster(context.Background(), ClusterOptions{Name: "kind", Run: (&fakeRunner{}).run})
	if err == nil {
		t.Fatal("expected refusal to create a cluster without the harness prefix")
	}
}

func TestDeleteRefusesUnownedCluster(t *testing.T) {
	c := &Cluster{Name: ClusterNamePrefix + "x", run: (&fakeRunner{}).run}
	if err := c.Delete(context.Background()); err == nil {
		t.Fatal("expected refusal to delete an unowned cluster")
	}
}

func TestDeleteRefusesForeignName(t *testing.T) {
	c := &Cluster{Name: "kind", owned: true, run: (&fakeRunner{}).run}
	if err := c.Delete(context.Background()); err == nil {
		t.Fatal("expected refusal to delete a cluster without the harness prefix")
	}
}

func TestDeleteIsIdempotentOnNotFound(t *testing.T) {
	dir := t.TempDir()
	f := &fakeRunner{deleteErr: errors.New("exit status 1"), deleteOut: "ERROR: cluster not found"}
	c := &Cluster{Name: ClusterNamePrefix + "gone", owned: true, run: f.run, tmpDir: dir}
	if err := c.Delete(context.Background()); err != nil {
		t.Fatalf("not-found delete should succeed, got %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("expected temp kubeconfig dir removed, stat err=%v", err)
	}
}

func TestKeepClusterSkipsDeletion(t *testing.T) {
	dir := t.TempDir()
	f := &fakeRunner{}
	c := &Cluster{Name: ClusterNamePrefix + "keep", owned: true, keep: true, run: f.run, tmpDir: dir}
	if err := c.Delete(context.Background()); err != nil {
		t.Fatalf("Delete with keep should be a no-op, got %v", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("expected no kind calls when retaining, got %v", f.calls)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("retained kubeconfig dir should remain, stat err=%v", err)
	}
}

func TestReuseRejectsForeignName(t *testing.T) {
	_, err := ReuseOwnedCluster(context.Background(), "kind", "/tmp/does-not-matter", ClusterOptions{Run: (&fakeRunner{}).run})
	if err == nil {
		t.Fatal("expected refusal to reuse a non-harness cluster")
	}
}

func TestReuseRequiresExistingKubeconfig(t *testing.T) {
	_, err := ReuseOwnedCluster(context.Background(), ClusterNamePrefix+"abc", "/nonexistent/kubeconfig", ClusterOptions{Run: (&fakeRunner{}).run})
	if err == nil {
		t.Fatal("expected refusal when the private kubeconfig is missing")
	}
}

func TestReuseRejectsAbsentCluster(t *testing.T) {
	isolateKubeconfigEnv(t)
	name := ClusterNamePrefix + "missing"
	kc := writeKubeconfigFile(t, t.TempDir(), kindShapedKubeconfig(t, name, "https://127.0.0.1:1"))
	f := &fakeRunner{clustersOut: "kind\nsome-other\n"}
	_, err := ReuseOwnedCluster(context.Background(), name, kc, ClusterOptions{Run: f.run})
	if err == nil {
		t.Fatal("expected refusal to reuse a cluster absent from kind")
	}
	assertNoMutation(t, f)
}

// isolateKubeconfigEnv clears ambient kubeconfig state so the default-path
// detection sees only what the test sets up, and points HOME at a fresh temp
// dir so no test can accidentally collide with the developer's own config.
func isolateKubeconfigEnv(t *testing.T) {
	t.Helper()
	t.Setenv("KUBECONFIG", "")
	t.Setenv("HOME", t.TempDir())
}

// reuseFixture pairs a harness-prefixed retained cluster name with a Kind-shaped
// private kubeconfig whose server matches the endpoint Kind reports for it.
func reuseFixture(t *testing.T) (name, kubeconfig string, f *fakeRunner) {
	t.Helper()
	name = ClusterNamePrefix + "retained"
	data := kindShapedKubeconfig(t, name, "https://127.0.0.1:39443")
	kubeconfig = writeKubeconfigFile(t, t.TempDir(), data)
	f = &fakeRunner{
		clustersOut:     "kind\n" + name + "\n",
		kindKubeconfigs: map[string]string{name: string(data)},
	}
	return name, kubeconfig, f
}

func TestReuseAttachesRetainedCluster(t *testing.T) {
	isolateKubeconfigEnv(t)
	name, kc, f := reuseFixture(t)
	c, err := ReuseOwnedCluster(context.Background(), name, kc, ClusterOptions{Run: f.run})
	if err != nil {
		t.Fatalf("ReuseOwnedCluster: %v", err)
	}
	if !c.Owned() || !c.Keep() {
		t.Errorf("reused cluster should be owned and retained, owned=%v keep=%v", c.Owned(), c.Keep())
	}
	// A reused, retained cluster must not be auto-deleted.
	if err := c.Delete(context.Background()); err != nil {
		t.Fatalf("Delete on reused cluster should be a no-op, got %v", err)
	}
	if _, err := os.Stat(kc); err != nil {
		t.Errorf("reused kubeconfig should remain, stat err=%v", err)
	}
}

func TestReuseRejectsKubeconfigForAnotherCluster(t *testing.T) {
	isolateKubeconfigEnv(t)
	requested := ClusterNamePrefix + "requested"
	// The supplied file is a valid Kind kubeconfig, but for a different cluster.
	kc := writeKubeconfigFile(t, t.TempDir(),
		kindShapedKubeconfig(t, ClusterNamePrefix+"other", "https://127.0.0.1:39443"))
	f := &fakeRunner{
		clustersOut:     "kind\n" + requested + "\n",
		kindKubeconfigs: map[string]string{requested: string(kindShapedKubeconfig(t, requested, "https://127.0.0.1:39443"))},
	}
	cluster, err := ReuseOwnedCluster(context.Background(), requested, kc, ClusterOptions{Run: f.run})
	if err == nil {
		t.Fatal("expected refusal when the kubeconfig targets another cluster")
	}
	if cluster != nil {
		t.Fatalf("a refused reuse must attach to nothing, got %+v", cluster)
	}
	if !strings.Contains(err.Error(), "refusing to reuse") || !strings.Contains(err.Error(), KindContextName(requested)) {
		t.Errorf("the refusal should stay identifiable and name the expected target, got %q", err.Error())
	}
	assertNoMutation(t, f)
}

func TestReuseRejectsMismatchedKubeconfigEndpoint(t *testing.T) {
	isolateKubeconfigEnv(t)
	name, kc, f := reuseFixture(t)
	// Same cluster/context name, but the endpoint Kind reports is different, so
	// the private file cannot be the named cluster's kubeconfig.
	f.kindKubeconfigs[name] = string(kindShapedKubeconfig(t, name, "https://127.0.0.1:65535"))
	cluster, err := ReuseOwnedCluster(context.Background(), name, kc, ClusterOptions{Run: f.run})
	if err == nil {
		t.Fatal("expected refusal when the kubeconfig endpoint does not match the named cluster")
	}
	if cluster != nil {
		t.Fatalf("a refused reuse must attach to nothing, got %+v", cluster)
	}
	if !strings.Contains(err.Error(), "server") {
		t.Errorf("the endpoint mismatch refusal should say so, got %q", err.Error())
	}
	assertNoMutation(t, f)
}

func TestReuseRejectsKubeconfigWithoutCurrentContext(t *testing.T) {
	isolateKubeconfigEnv(t)
	name := ClusterNamePrefix + "bare"
	// A readable file that merely exists (the pre-fix false positive).
	kc := writeKubeconfigFile(t, t.TempDir(), []byte("apiVersion: v1\nkind: Config\n"))
	f := &fakeRunner{
		clustersOut:     "kind\n" + name + "\n",
		kindKubeconfigs: map[string]string{name: string(kindShapedKubeconfig(t, name, "https://127.0.0.1:39443"))},
	}
	cluster, err := ReuseOwnedCluster(context.Background(), name, kc, ClusterOptions{Run: f.run})
	if err == nil {
		t.Fatal("expected refusal for a kubeconfig that targets no cluster")
	}
	if cluster != nil {
		t.Fatalf("a refused reuse must attach to nothing, got %+v", cluster)
	}
	assertNoMutation(t, f)
}

func TestReuseRejectsDefaultKubeconfigPath(t *testing.T) {
	isolateKubeconfigEnv(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("resolve HOME: %v", err)
	}
	name, _, f := reuseFixture(t)
	defaultPath := filepath.Join(home, ".kube", "config")
	if mkErr := os.MkdirAll(filepath.Dir(defaultPath), 0o700); mkErr != nil {
		t.Fatal(mkErr)
	}
	if wErr := os.WriteFile(defaultPath, kindShapedKubeconfig(t, name, "https://127.0.0.1:39443"), 0o600); wErr != nil {
		t.Fatal(wErr)
	}
	cluster, err := ReuseOwnedCluster(context.Background(), name, defaultPath, ClusterOptions{Run: f.run})
	if err == nil {
		t.Fatal("expected refusal when reuse names the default kubeconfig")
	}
	if cluster != nil {
		t.Fatalf("a refused reuse must attach to nothing, got %+v", cluster)
	}
	if !strings.Contains(err.Error(), "default kubeconfig") {
		t.Errorf("the default-path refusal should say so, got %q", err.Error())
	}
	// The default path is rejected before any Kind command runs.
	if len(f.calls) != 0 {
		t.Errorf("default-path refusal should not run any kind command, got %v", f.calls)
	}
}

func TestReuseRejectsKubeconfigFromEnvPath(t *testing.T) {
	isolateKubeconfigEnv(t)
	name, _, f := reuseFixture(t)
	shared := writeKubeconfigFile(t, t.TempDir(), kindShapedKubeconfig(t, name, "https://127.0.0.1:39443"))
	t.Setenv("KUBECONFIG", shared)
	cluster, err := ReuseOwnedCluster(context.Background(), name, shared, ClusterOptions{Run: f.run})
	if err == nil {
		t.Fatal("expected refusal when reuse names a $KUBECONFIG entry")
	}
	if cluster != nil {
		t.Fatalf("a refused reuse must attach to nothing, got %+v", cluster)
	}
	if !strings.Contains(err.Error(), "default kubeconfig") {
		t.Errorf("the $KUBECONFIG refusal should identify the default path, got %q", err.Error())
	}
	if len(f.calls) != 0 {
		t.Errorf("$KUBECONFIG refusal should not run any kind command, got %v", f.calls)
	}
}

func TestInspectCommandsReferencePathNotContents(t *testing.T) {
	c := &Cluster{Name: ClusterNamePrefix + "insp", Kubeconfig: "/tmp/private/kubeconfig", kindPath: "kind"}
	cmds := strings.Join(c.InspectCommands(), "\n")
	if !strings.Contains(cmds, c.Kubeconfig) {
		t.Errorf("inspect commands should reference the kubeconfig path, got %q", cmds)
	}
	if !strings.Contains(cmds, "delete cluster --name "+c.Name) {
		t.Errorf("inspect commands should include disposal, got %q", cmds)
	}
	if !strings.Contains(cmds, ReuseClusterEnvVar+"="+c.Name) ||
		!strings.Contains(cmds, ReuseKubeconfigEnvVar+"="+c.Kubeconfig) {
		t.Errorf("inspect commands should include the explicit focused-reuse invocation, got %q", cmds)
	}
}

func TestKeepClusterRequestedParsing(t *testing.T) {
	for _, v := range []string{"1", "true", "TRUE", "yes"} {
		t.Setenv("KEEP_CLUSTER", v)
		if !OptionsFromEnv().Keep {
			t.Errorf("KEEP_CLUSTER=%q should request retention", v)
		}
	}
	t.Setenv("KEEP_CLUSTER", "")
	if OptionsFromEnv().Keep {
		t.Error("empty KEEP_CLUSTER should not request retention")
	}
}

func TestReuseFromEnvNotRequestedReturnsNil(t *testing.T) {
	t.Setenv(ReuseClusterEnvVar, "")
	t.Setenv(ReuseKubeconfigEnvVar, "")
	cluster, err := ReuseFromEnv(context.Background(), ClusterOptions{Run: (&fakeRunner{}).run})
	if err != nil {
		t.Fatalf("no reuse requested should never error, got %v", err)
	}
	if cluster != nil {
		t.Fatalf("no reuse requested must not attach to anything, got %+v", cluster)
	}
}

func TestReuseFromEnvAttachesRetainedHarnessCluster(t *testing.T) {
	isolateKubeconfigEnv(t)
	name, kc, f := reuseFixture(t)
	t.Setenv(ReuseClusterEnvVar, name)
	t.Setenv(ReuseKubeconfigEnvVar, kc)
	cluster, err := ReuseFromEnv(context.Background(), ClusterOptions{Run: f.run})
	if err != nil {
		t.Fatalf("ReuseFromEnv: %v", err)
	}
	if cluster == nil {
		t.Fatal("explicit reuse should attach to the retained cluster")
	}
	if cluster.Name != name || cluster.Kubeconfig != kc {
		t.Errorf("attached to %q/%q, want %q/%q", cluster.Name, cluster.Kubeconfig, name, kc)
	}
	if !cluster.Owned() || !cluster.Keep() {
		t.Errorf("reused cluster should be owned and retained, owned=%v keep=%v", cluster.Owned(), cluster.Keep())
	}
}

func TestReuseFromEnvRefusesUserCluster(t *testing.T) {
	dir := t.TempDir()
	kc := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kc, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(ReuseClusterEnvVar, "kind")
	t.Setenv(ReuseKubeconfigEnvVar, kc)
	cluster, err := ReuseFromEnv(context.Background(), ClusterOptions{Run: (&fakeRunner{}).run})
	if err == nil {
		t.Fatal("expected refusal to reuse a user cluster named by the reuse variables")
	}
	if cluster != nil {
		t.Fatalf("a refused reuse must attach to nothing, got %+v", cluster)
	}
	if !strings.Contains(err.Error(), "refused") {
		t.Errorf("the refusal should stay identifiable, got %q", err.Error())
	}
}

func TestReuseFromEnvRefusesMismatchedKubeconfig(t *testing.T) {
	isolateKubeconfigEnv(t)
	requested := ClusterNamePrefix + "requested"
	kc := writeKubeconfigFile(t, t.TempDir(),
		kindShapedKubeconfig(t, ClusterNamePrefix+"other", "https://127.0.0.1:39443"))
	t.Setenv(ReuseClusterEnvVar, requested)
	t.Setenv(ReuseKubeconfigEnvVar, kc)
	f := &fakeRunner{
		clustersOut:     "kind\n" + requested + "\n",
		kindKubeconfigs: map[string]string{requested: string(kindShapedKubeconfig(t, requested, "https://127.0.0.1:39443"))},
	}
	cluster, err := ReuseFromEnv(context.Background(), ClusterOptions{Run: f.run})
	if err == nil {
		t.Fatal("expected refusal when the reuse kubeconfig targets another cluster")
	}
	if cluster != nil {
		t.Fatalf("a refused reuse must attach to nothing, got %+v", cluster)
	}
	assertNoMutation(t, f)
}

func TestReuseFromEnvRefusesMissingKubeconfig(t *testing.T) {
	isolateKubeconfigEnv(t)
	t.Setenv(ReuseClusterEnvVar, ClusterNamePrefix+"gone")
	t.Setenv(ReuseKubeconfigEnvVar, filepath.Join(t.TempDir(), "does-not-exist"))
	cluster, err := ReuseFromEnv(context.Background(), ClusterOptions{Run: (&fakeRunner{}).run})
	if err == nil {
		t.Fatal("expected refusal when the retained cluster's kubeconfig is missing")
	}
	if cluster != nil {
		t.Fatalf("a refused reuse must attach to nothing, got %+v", cluster)
	}
}
