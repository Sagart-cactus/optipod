package framework

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// kindCLIID names both the preflight check and the pin manifest entry for the
// Kind CLI.
const kindCLIID = "kind-cli"

// Blocker is an actionable preflight failure. It names the failed check, the
// reason, and a concrete remedy. A Blocker is always returned before any cluster
// is owned, so receiving one implies that no cluster was created.
type Blocker struct {
	Check  string
	Reason string
	Remedy string
}

func (b *Blocker) Error() string {
	return fmt.Sprintf("preflight check %q failed: %s; remedy: %s", b.Check, b.Reason, b.Remedy)
}

// commandRunner runs a command bounded by ctx and returns its combined output.
type commandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// commandWaitDelay bounds how long a cancelled command's Wait may keep waiting
// once its output pipes are held open by an orphaned descendant. docker build
// (buildx) can leave a session proxy or credential helper holding the output
// pipe after the CLI itself exits; without this bound, cancelling a hung build
// would leave the whole suite blocked forever with an owned cluster behind it.
var commandWaitDelay = 10 * time.Second

func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = commandWaitDelay
	return cmd.CombinedOutput()
}

// Preflighter verifies host prerequisites before any cluster is owned. Every
// external dependency is injectable so the checks can be exercised in isolation
// without a real Docker daemon, Kind binary, or controlled PATH.
type Preflighter struct {
	LookPath     func(string) (string, error)
	Run          commandRunner
	GOARCH       string
	DiskPath     string
	MinDiskBytes uint64
	ProbePort    func() error
	Timeout      time.Duration
}

// NewPreflighter builds a Preflighter wired to the real environment.
func NewPreflighter() *Preflighter {
	return &Preflighter{
		LookPath:     exec.LookPath,
		Run:          execRunner,
		GOARCH:       runtime.GOARCH,
		DiskPath:     os.TempDir(),
		MinDiskBytes: 10 << 30, // 10 GiB headroom for node image, build, and cluster
		ProbePort:    probeDynamicPort,
		Timeout:      30 * time.Second,
	}
}

// Check runs all preflight checks in order and returns the first Blocker
// encountered, or nil when the host is ready. It never creates a cluster.
func (p *Preflighter) Check(ctx context.Context) error {
	checks := []func(context.Context) *Blocker{
		p.checkArch,
		p.checkDockerDaemon,
		p.checkKindCLI,
		p.checkKubectl,
		p.checkKustomize,
		p.checkNodeImagePin,
		p.checkDisk,
		p.checkDynamicPort,
	}
	for _, c := range checks {
		if b := c(ctx); b != nil {
			return b
		}
	}
	return nil
}

func (p *Preflighter) requireBinary(name string) *Blocker {
	if _, err := p.LookPath(name); err != nil {
		return &Blocker{
			Check:  name + "-binary",
			Reason: fmt.Sprintf("%q was not found on PATH", name),
			Remedy: fmt.Sprintf("install %s and ensure it is on PATH", name),
		}
	}
	return nil
}

func (p *Preflighter) checkArch(_ context.Context) *Blocker {
	if !SupportedArches[p.GOARCH] {
		return &Blocker{
			Check:  "architecture",
			Reason: fmt.Sprintf("host architecture %q is not supported", p.GOARCH),
			Remedy: "run the harness on an amd64 or arm64 host",
		}
	}
	return nil
}

func (p *Preflighter) checkDockerDaemon(ctx context.Context) *Blocker {
	if b := p.requireBinary("docker"); b != nil {
		return b
	}
	cctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()
	if out, err := p.Run(cctx, "docker", "info", "--format", "{{.ServerVersion}}"); err != nil {
		return &Blocker{
			Check:  "docker-daemon",
			Reason: fmt.Sprintf("docker daemon is not reachable: %v (%s)", err, strings.TrimSpace(string(out))),
			Remedy: "start Docker (Docker Desktop or the engine) and ensure the current user can reach it",
		}
	}
	return nil
}

func (p *Preflighter) checkKindCLI(ctx context.Context) *Blocker {
	if b := p.requireBinary("kind"); b != nil {
		return b
	}
	cctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()
	out, err := p.Run(cctx, "kind", "version")
	if err != nil {
		return &Blocker{
			Check:  kindCLIID,
			Reason: fmt.Sprintf("could not run kind version: %v", err),
			Remedy: "install the pinned Kind " + KindCLIVersion,
		}
	}
	if !strings.Contains(string(out), KindCLIVersion) {
		return &Blocker{
			Check:  kindCLIID,
			Reason: fmt.Sprintf("kind version %q does not match pinned %s", strings.TrimSpace(string(out)), KindCLIVersion),
			Remedy: "install the pinned Kind " + KindCLIVersion,
		}
	}
	return nil
}

func (p *Preflighter) checkKubectl(ctx context.Context) *Blocker {
	return p.checkToolRuns(ctx, "kubectl", "kubectl",
		"install kubectl and ensure it is on PATH", "version", "--client")
}

func (p *Preflighter) checkKustomize(ctx context.Context) *Blocker {
	return p.checkToolRuns(ctx, "kustomize", "kustomize",
		"install the pinned kustomize "+KustomizeVersion, "version")
}

func (p *Preflighter) checkToolRuns(ctx context.Context, check, bin, remedy string, args ...string) *Blocker {
	if b := p.requireBinary(bin); b != nil {
		return b
	}
	cctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()
	if out, err := p.Run(cctx, bin, args...); err != nil {
		return &Blocker{
			Check:  check,
			Reason: fmt.Sprintf("%s is not usable: %v (%s)", bin, err, strings.TrimSpace(string(out))),
			Remedy: remedy,
		}
	}
	return nil
}

func (p *Preflighter) checkNodeImagePin(_ context.Context) *Blocker {
	if !strings.Contains(NodeImage(), "@sha256:") {
		return &Blocker{
			Check:  "node-image-pin",
			Reason: "the Kind node image is not pinned by digest",
			Remedy: "pin the Kind node image by its sha256 digest",
		}
	}
	return nil
}

func (p *Preflighter) checkDisk(_ context.Context) *Blocker {
	free, err := freeDiskBytes(p.DiskPath)
	if err != nil {
		return &Blocker{
			Check:  "disk",
			Reason: fmt.Sprintf("could not stat %s: %v", p.DiskPath, err),
			Remedy: "ensure the temporary directory is accessible",
		}
	}
	if free < p.MinDiskBytes {
		return &Blocker{
			Check:  "disk",
			Reason: fmt.Sprintf("only %d bytes free on %s, need at least %d", free, p.DiskPath, p.MinDiskBytes),
			Remedy: "free disk space before running the Kind suite",
		}
	}
	return nil
}

func probeDynamicPort() error {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	return l.Close()
}

func (p *Preflighter) checkDynamicPort(_ context.Context) *Blocker {
	if err := p.ProbePort(); err != nil {
		return &Blocker{
			Check:  "dynamic-port",
			Reason: fmt.Sprintf("could not bind an ephemeral localhost port: %v", err),
			Remedy: "ensure localhost ephemeral ports are available (no exhausted port range)",
		}
	}
	return nil
}
