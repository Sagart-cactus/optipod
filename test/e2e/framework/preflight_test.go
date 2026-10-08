package framework

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// okPreflighter returns a Preflighter whose injected dependencies all succeed,
// so individual tests can disable exactly one prerequisite.
func okPreflighter() *Preflighter {
	return &Preflighter{
		LookPath: func(string) (string, error) { return "/usr/local/bin/tool", nil },
		Run: func(_ context.Context, name string, _ ...string) ([]byte, error) {
			if name == "kind" {
				return []byte("kind " + KindCLIVersion + " go1.25.5 darwin/amd64"), nil
			}
			return []byte("ok"), nil
		},
		GOARCH:       "amd64",
		DiskPath:     "/",
		MinDiskBytes: 1, // trivially satisfiable in tests
		ProbePort:    func() error { return nil },
	}
}

func TestPreflightSucceedsWhenReady(t *testing.T) {
	if err := okPreflighter().Check(context.Background()); err != nil {
		t.Fatalf("expected ready host, got %v", err)
	}
}

func TestPreflightBlocksUnsupportedArch(t *testing.T) {
	p := okPreflighter()
	p.GOARCH = "mips"
	assertBlocker(t, p.Check(context.Background()), "architecture")
}

func TestPreflightBlocksMissingDocker(t *testing.T) {
	p := okPreflighter()
	p.LookPath = func(name string) (string, error) {
		if name == "docker" {
			return "", errors.New("not found")
		}
		return "/usr/local/bin/tool", nil
	}
	assertBlocker(t, p.Check(context.Background()), "docker-binary")
}

func TestPreflightBlocksDaemonDown(t *testing.T) {
	p := okPreflighter()
	p.Run = func(_ context.Context, name string, _ ...string) ([]byte, error) {
		if name == "docker" {
			return []byte("Cannot connect to the Docker daemon"), errors.New("exit status 1")
		}
		return []byte("kind " + KindCLIVersion), nil
	}
	assertBlocker(t, p.Check(context.Background()), "docker-daemon")
}

func TestPreflightBlocksKindVersionMismatch(t *testing.T) {
	p := okPreflighter()
	p.Run = func(_ context.Context, name string, _ ...string) ([]byte, error) {
		if name == "kind" {
			return []byte("kind v0.30.0 go1.25.5 darwin/amd64"), nil
		}
		return []byte("ok"), nil
	}
	assertBlocker(t, p.Check(context.Background()), "kind-cli")
}

func TestPreflightBlocksMissingKustomize(t *testing.T) {
	p := okPreflighter()
	p.LookPath = func(name string) (string, error) {
		if name == "kustomize" {
			return "", errors.New("not found")
		}
		return "/usr/local/bin/tool", nil
	}
	assertBlocker(t, p.Check(context.Background()), "kustomize-binary")
}

func TestPreflightBlocksInsufficientDisk(t *testing.T) {
	p := okPreflighter()
	p.MinDiskBytes = 1 << 62 // larger than any real free space
	assertBlocker(t, p.Check(context.Background()), "disk")
}

func TestPreflightBlocksUnavailablePort(t *testing.T) {
	p := okPreflighter()
	p.ProbePort = func() error { return errors.New("no ephemeral ports") }
	assertBlocker(t, p.Check(context.Background()), "dynamic-port")
}

func assertBlocker(t *testing.T, err error, wantCheck string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected a blocker for check %q, got nil", wantCheck)
	}
	var b *Blocker
	if !errors.As(err, &b) {
		t.Fatalf("expected *Blocker, got %T: %v", err, err)
	}
	if b.Check != wantCheck {
		t.Fatalf("expected blocker check %q, got %q (%v)", wantCheck, b.Check, err)
	}
	if strings.TrimSpace(b.Remedy) == "" {
		t.Errorf("blocker %q has no actionable remedy", b.Check)
	}
}

// TestExecRunnerReturnsWhenOrphanHoldsOutputPipe reproduces the hang the runner
// must bound: a cancelled command (e.g. docker build on its timeout) can leave
// a descendant holding the output pipe open. Without the WaitDelay bound, the
// cancelled build's Wait would block until the orphan exits and the suite would
// stall with an owned cluster left behind.
func TestExecRunnerReturnsWhenOrphanHoldsOutputPipe(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("bash not available: %v", err)
	}

	oldDelay := commandWaitDelay
	commandWaitDelay = 300 * time.Millisecond
	t.Cleanup(func() { commandWaitDelay = oldDelay })

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	// The background sleep inherits bash's stdout (the runner's pipe) and
	// survives bash's exit, exactly like an orphaned buildx session proxy.
	_, err = execRunner(ctx, bash, "-c", "sleep 30 & exit 0")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("a cancelled command must return an error, got nil")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("runner stayed blocked for %s waiting on an orphaned pipe holder; the WaitDelay bound did not apply", elapsed)
	}
}
