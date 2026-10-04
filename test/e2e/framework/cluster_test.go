package framework

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRunner records invocations and simulates the pinned Kind CLI without a real
// cluster. For "create cluster" it writes the private kubeconfig file so that the
// production chmod path can run.
type fakeRunner struct {
	calls       [][]string
	clustersOut string
	deleteErr   error
	deleteOut   string
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
	default:
		return []byte("ok"), nil
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

func TestReuseAttachesRetainedCluster(t *testing.T) {
	dir := t.TempDir()
	kc := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kc, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	name := ClusterNamePrefix + "retained"
	f := &fakeRunner{clustersOut: "kind\n" + name + "\n"}
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

func TestReuseRejectsAbsentCluster(t *testing.T) {
	dir := t.TempDir()
	kc := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kc, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &fakeRunner{clustersOut: "kind\nsome-other\n"}
	_, err := ReuseOwnedCluster(context.Background(), ClusterNamePrefix+"missing", kc, ClusterOptions{Run: f.run})
	if err == nil {
		t.Fatal("expected refusal to reuse a cluster absent from kind")
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
