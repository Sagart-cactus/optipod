package framework

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestNormalizeImageDigest(t *testing.T) {
	const digest = "sha256:4613778f3cfcd10e615029370f5786704559103cf27bef934597ba562b269661"
	cases := map[string]string{
		digest: digest,
		"docker.io/library/optipod-e2e/controller:e2e-1-abcdef12@" + digest: digest,
		"optipod-e2e/controller:e2e-1-abcdef12":                             "",
		"":                                                                  "",
	}
	for in, want := range cases {
		if got := NormalizeImageDigest(in); got != want {
			t.Errorf("NormalizeImageDigest(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUniqueImageTag(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		tag, err := uniqueImageTag()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(tag, "e2e-") {
			t.Fatalf("tag %q lacks e2e- prefix", tag)
		}
		if seen[tag] {
			t.Fatalf("duplicate tag %q", tag)
		}
		seen[tag] = true
	}
}

// fakeRun records commands and returns scripted output.
type fakeRun struct {
	calls [][]string
	out   map[string]string // substring of args joined -> output
	err   error
}

func (f *fakeRun) run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)
	if f.err != nil {
		return []byte("boom"), f.err
	}
	joined := strings.Join(call, " ")
	for needle, out := range f.out {
		if strings.Contains(joined, needle) {
			return []byte(out), nil
		}
	}
	return []byte(""), nil
}

func TestBuildControllerImageBuildsWithUniqueTagAndInspectsID(t *testing.T) {
	const id = "sha256:4613778f3cfcd10e615029370f5786704559103cf27bef934597ba562b269661"
	fr := &fakeRun{out: map[string]string{"image inspect": id + "\n"}}
	b := &ImageBuilder{RepoRoot: "/repo", DockerPath: "docker", Platform: "linux/amd64", Run: fr.run}

	img, err := b.BuildControllerImage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(img.Ref, ControllerImageRepo+":e2e-") {
		t.Fatalf("ref %q lacks pinned repo and unique tag", img.Ref)
	}
	if img.ID != id {
		t.Fatalf("ID = %q, want %q", img.ID, id)
	}
	if len(fr.calls) != 2 {
		t.Fatalf("calls = %v", fr.calls)
	}
	build := strings.Join(fr.calls[0], " ")
	for _, want := range []string{"build", "--platform linux/amd64", "-t " + img.Ref, "-f /repo/Dockerfile", "/repo"} {
		if !strings.Contains(build, want) {
			t.Errorf("build command %q missing %q", build, want)
		}
	}
}

func TestBuildImagePropagatesBuildFailure(t *testing.T) {
	fr := &fakeRun{err: errors.New("docker exploded")}
	b := &ImageBuilder{RepoRoot: "/repo", DockerPath: "docker", Run: fr.run}
	if _, err := b.BuildControllerImage(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "docker exploded") {
		t.Fatalf("expected wrapped build error, got %v", err)
	}
}

func TestLoadImageTargetsOwnedClusterByName(t *testing.T) {
	const nodeDigest = "sha256:33711cdf65017ad4a89c2ec443ee212f767fcdb2eaf33c1244c47f3e9bbef350"
	fr := &fakeRun{out: map[string]string{
		"crictl inspecti": `{"status":{"id":"` + nodeDigest + `","repoTags":["docker.io/library/repo:tag"]}}`,
	}}
	b := &ImageBuilder{KindPath: "kind", DockerPath: "docker", Run: fr.run}
	nodeID, err := b.LoadImage(context.Background(), "optipod-e2e-x", "repo:tag")
	if err != nil {
		t.Fatal(err)
	}
	if nodeID != nodeDigest {
		t.Fatalf("node digest = %q, want %q", nodeID, nodeDigest)
	}
	joined := strings.Join(fr.calls[0], " ")
	if !strings.Contains(joined, "load docker-image repo:tag --name optipod-e2e-x") {
		t.Fatalf("unexpected load command: %q", joined)
	}
	execCall := strings.Join(fr.calls[1], " ")
	if !strings.Contains(execCall, "exec optipod-e2e-x-control-plane crictl inspecti --output json repo:tag") {
		t.Fatalf("unexpected node digest command: %q", execCall)
	}
}

func TestParseCRIImageID(t *testing.T) {
	const digest = "sha256:33711cdf65017ad4a89c2ec443ee212f767fcdb2eaf33c1244c47f3e9bbef350"
	got, err := parseCRIImageID([]byte(`{"status":{"id":"` + digest + `"}}`))
	if err != nil || got != digest {
		t.Fatalf("parseCRIImageID = %q, %v", got, err)
	}
	if _, err := parseCRIImageID([]byte(`{"status":{"id":""}}`)); err == nil {
		t.Fatal("expected error for empty id")
	}
	if _, err := parseCRIImageID([]byte(`not json`)); err == nil {
		t.Fatal("expected error for malformed output")
	}
}

func TestPullAndLoadUsesPinnedReference(t *testing.T) {
	const nodeDigest = "sha256:33711cdf65017ad4a89c2ec443ee212f767fcdb2eaf33c1244c47f3e9bbef350"
	fr := &fakeRun{out: map[string]string{
		"crictl inspecti": `{"status":{"id":"` + nodeDigest + `"}}`,
	}}
	b := &ImageBuilder{DockerPath: "docker", KindPath: "kind", Platform: "linux/arm64", Run: fr.run}
	if err := b.PullAndLoad(context.Background(), "optipod-e2e-x", "example.com/pause:3.10"); err != nil {
		t.Fatal(err)
	}
	if len(fr.calls) != 3 {
		t.Fatalf("calls = %v", fr.calls)
	}
	pull := strings.Join(fr.calls[0], " ")
	if !strings.Contains(pull, "pull --platform linux/arm64 example.com/pause:3.10") {
		t.Fatalf("unexpected pull command: %q", pull)
	}
	if !strings.Contains(WorkloadImage(), WorkloadImageDigest) {
		t.Fatalf("workload image %q is not digest-pinned", WorkloadImage())
	}
}
