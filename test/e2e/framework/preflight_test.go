package framework

import (
	"context"
	"errors"
	"strings"
	"testing"
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
