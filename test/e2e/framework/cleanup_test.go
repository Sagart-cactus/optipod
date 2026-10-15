package framework

import (
	"bytes"
	"context"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestCleanupContextIsBoundedAndIndependent(t *testing.T) {
	ctx, cancel := CleanupContext()
	defer cancel()
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("cleanup context must carry a deadline")
	}
	// It is derived from Background, so a cancelled parent cannot stop it.
	select {
	case <-ctx.Done():
		t.Fatal("fresh cleanup context should not be done")
	default:
	}
}

func TestSignalGuardRunsCleanupAndExitsNonzero(t *testing.T) {
	dir := t.TempDir()
	f := &fakeRunner{}
	c := &Cluster{Name: ClusterNamePrefix + "sig", owned: true, run: f.run, tmpDir: dir}

	var out bytes.Buffer
	var exitCode int
	g := &SignalGuard{cluster: c, out: &out, exit: func(code int) { exitCode = code }}

	g.handle(syscall.SIGINT)

	if exitCode != 1 {
		t.Errorf("expected nonzero exit on interruption, got %d", exitCode)
	}
	if len(f.calls) == 0 || f.calls[0][1] != "delete" {
		t.Errorf("expected ownership-aware delete on signal, calls=%v", f.calls)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("expected private kubeconfig dir removed after signal cleanup, err=%v", err)
	}
	if out.Len() == 0 {
		t.Error("expected the signal guard to report what it did")
	}
}

func TestSignalGuardRetainsWhenKeepSet(t *testing.T) {
	dir := t.TempDir()
	f := &fakeRunner{}
	c := &Cluster{Name: ClusterNamePrefix + "sigkeep", owned: true, keep: true, run: f.run, tmpDir: dir}

	var out bytes.Buffer
	var exitCode int
	g := &SignalGuard{cluster: c, out: &out, exit: func(code int) { exitCode = code }}

	g.handle(syscall.SIGTERM)

	if exitCode != 1 {
		t.Errorf("expected nonzero exit even when retaining, got %d", exitCode)
	}
	if len(f.calls) != 0 {
		t.Errorf("retained cluster must not be deleted on signal, calls=%v", f.calls)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("retained kubeconfig dir should remain, err=%v", err)
	}
}

func TestSignalGuardStopIsIdempotent(t *testing.T) {
	c := &Cluster{Name: ClusterNamePrefix + "stop", owned: true, run: (&fakeRunner{}).run}
	g := c.InstallSignalGuard(os.Stderr)
	g.Stop()
	g.Stop() // must not panic on a closed channel
	// Give the waiter goroutine a moment to observe the closed channel.
	time.Sleep(10 * time.Millisecond)
	_ = context.Background()
}
