package framework

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const defaultBuildTimeout = 20 * time.Minute

// BuiltImage describes one locally built (or pulled) image loaded into the
// owned cluster. Ref is the name:tag reference; ID is the normalized
// "sha256:<hex>" image digest reported by the local Docker daemon; NodeID is
// the image digest recorded by the cluster node's containerd after
// LoadImage, which is what a running pod's imageID reports (the local daemon
// and the node store compute different digests for the same loaded content).
type BuiltImage struct {
	Ref    string
	ID     string
	NodeID string
}

// ImageBuilder builds the checkout's images once per suite invocation and loads
// them once into the owned Kind cluster. All external commands are injectable
// for isolated tests.
type ImageBuilder struct {
	RepoRoot     string
	DockerPath   string
	KindPath     string
	Run          commandRunner
	BuildTimeout time.Duration
	// Platform overrides the target build platform; defaults to linux/<GOARCH>
	// so the built image matches the Kind node architecture.
	Platform string
}

// NewImageBuilder wires an ImageBuilder to the real environment.
func NewImageBuilder(repoRoot string) *ImageBuilder {
	return &ImageBuilder{
		RepoRoot:     repoRoot,
		DockerPath:   "docker",
		KindPath:     "kind",
		Run:          execRunner,
		BuildTimeout: defaultBuildTimeout,
	}
}

func (b *ImageBuilder) platform() string {
	if b.Platform != "" {
		return b.Platform
	}
	return "linux/" + runtime.GOARCH
}

func uniqueImageTag() (string, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate image tag suffix: %w", err)
	}
	return fmt.Sprintf("e2e-%d-%s", time.Now().Unix(), hex.EncodeToString(buf)), nil
}

// BuildControllerImage builds the production controller image from the
// checkout's root Dockerfile with a unique per-run tag.
func (b *ImageBuilder) BuildControllerImage(ctx context.Context) (*BuiltImage, error) {
	return b.build(ctx, "Dockerfile", ControllerImageRepo)
}

// BuildFixtureImage builds the deterministic metrics fixture image from
// test/e2e/metricsfixture with a unique per-run tag.
func (b *ImageBuilder) BuildFixtureImage(ctx context.Context) (*BuiltImage, error) {
	return b.build(ctx, "test/e2e/metricsfixture/Dockerfile", MetricsFixtureImageRepo)
}

func (b *ImageBuilder) build(ctx context.Context, dockerfile, repo string) (*BuiltImage, error) {
	tag, err := uniqueImageTag()
	if err != nil {
		return nil, err
	}
	ref := repo + ":" + tag

	// The checkout's .dockerignore excludes everything but Go sources, so a
	// relative -f resolves against the client's working directory and fails;
	// pass the absolute Dockerfile path, which docker reads directly from disk.
	dockerfilePath := filepath.Join(b.RepoRoot, dockerfile)

	cctx, cancel := context.WithTimeout(ctx, b.BuildTimeout)
	defer cancel()
	out, err := b.Run(cctx, b.DockerPath, "build",
		"--platform", b.platform(),
		"-t", ref,
		"-f", dockerfilePath,
		b.RepoRoot)
	if err != nil {
		return nil, fmt.Errorf("build image %s: %w (%s)", ref, err, tailOutput(string(out)))
	}

	idCtx, idCancel := context.WithTimeout(ctx, 30*time.Second)
	defer idCancel()
	idOut, err := b.Run(idCtx, b.DockerPath, "image", "inspect", "--format", "{{.Id}}", ref)
	if err != nil {
		return nil, fmt.Errorf("inspect built image %s: %w (%s)", ref, err, tailOutput(string(idOut)))
	}
	id := NormalizeImageDigest(strings.TrimSpace(string(idOut)))
	if id == "" {
		return nil, fmt.Errorf("could not determine image ID for %s from %q", ref, strings.TrimSpace(string(idOut)))
	}
	return &BuiltImage{Ref: ref, ID: id}, nil
}

// LoadImage loads a locally present image into the owned Kind cluster exactly
// once per call and returns the image digest as recorded by the node's
// containerd (the value a pod's imageID will report). It uses the Kind CLI with
// the cluster name; no kubeconfig is involved.
func (b *ImageBuilder) LoadImage(ctx context.Context, clusterName, ref string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	out, err := b.Run(cctx, b.KindPath, "load", "docker-image", ref, "--name", clusterName)
	if err != nil {
		return "", fmt.Errorf("load image %s into cluster %s: %w (%s)", ref, clusterName, err, tailOutput(string(out)))
	}
	nodeID, err := b.nodeImageDigest(ctx, clusterName, ref)
	if err != nil {
		return "", fmt.Errorf("resolve node image digest for %s on %s: %w", ref, clusterName, err)
	}
	return nodeID, nil
}

// nodeImageDigest asks the owned cluster's node CRI for the image ID of ref,
// which is exactly the value a pod's containerStatuses[].imageID reports.
// (containerd's `ctr images ls` instead shows the OCI index digest, which
// differs from the CRI image ID.)
func (b *ImageBuilder) nodeImageDigest(ctx context.Context, clusterName, ref string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	out, err := b.Run(cctx, b.DockerPath, "exec", clusterName+"-control-plane",
		"crictl", "inspecti", "--output", "json", ref)
	if err != nil {
		return "", fmt.Errorf("inspect node image: %w (%s)", err, tailOutput(string(out)))
	}
	return parseCRIImageID(out)
}

// parseCRIImageID extracts status.id from `crictl inspecti -o json` output.
func parseCRIImageID(inspectJSON []byte) (string, error) {
	var parsed struct {
		Status struct {
			ID string `json:"id"`
		} `json:"status"`
	}
	if err := json.Unmarshal(inspectJSON, &parsed); err != nil {
		return "", fmt.Errorf("decode crictl inspecti output: %w", err)
	}
	id := NormalizeImageDigest(parsed.Status.ID)
	if id == "" {
		return "", fmt.Errorf("no image digest in crictl status.id %q", parsed.Status.ID)
	}
	return id, nil
}

// PullAndLoad pulls a pinned external image for the build platform and loads
// it into the owned cluster. Note: kind cannot load a digest reference to a
// multi-architecture manifest list (the local store holds only the pulled
// platform's content, and ctr import --all-platforms then fails); use this only
// for plain name:tag references. Digest-pinned multi-arch images such as the
// workload pause image are instead referenced by digest in pod specs and pulled
// directly by the node's containerd, which is content-deterministic.
func (b *ImageBuilder) PullAndLoad(ctx context.Context, clusterName, pinnedRef string) error {
	pctx, pcancel := context.WithTimeout(ctx, 5*time.Minute)
	defer pcancel()
	out, err := b.Run(pctx, b.DockerPath, "pull", "--platform", b.platform(), pinnedRef)
	if err != nil {
		return fmt.Errorf("pull pinned image %s: %w (%s)", pinnedRef, err, tailOutput(string(out)))
	}
	_, err = b.LoadImage(ctx, clusterName, pinnedRef)
	return err
}

var digestPattern = regexp.MustCompile(`sha256:[0-9a-f]{64}`)

// NormalizeImageDigest extracts the bare "sha256:<hex>" digest from any image
// reference form: Docker's "sha256:..." ID, or a containerd-style
// "registry/name@sha256:..." pullable ID as reported in pod status.
func NormalizeImageDigest(ref string) string {
	return digestPattern.FindString(ref)
}

// tailOutput keeps command errors actionable without flooding diagnostics.
func tailOutput(out string) string {
	const maxLen = 4096
	trimmed := strings.TrimSpace(out)
	if len(trimmed) > maxLen {
		return "..." + trimmed[len(trimmed)-maxLen:]
	}
	return trimmed
}
