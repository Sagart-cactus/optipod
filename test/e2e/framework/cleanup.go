package framework

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

const defaultCleanupTimeout = 2 * time.Minute

// CleanupContext returns a bounded context that is independent of any run or test
// context. Teardown must proceed even after the main context is cancelled by a
// timeout or interruption, so this is derived from context.Background rather than
// from a parent that may already be done.
func CleanupContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), defaultCleanupTimeout)
}

// SignalGuard runs ownership-aware cleanup when the process is interrupted, using
// an independent bounded context, and then exits nonzero so an interrupted run is
// never mistaken for success.
type SignalGuard struct {
	cluster  *Cluster
	out      io.Writer
	exit     func(int)
	ch       chan os.Signal
	stopOnce sync.Once
}

// InstallSignalGuard registers a handler for SIGINT and SIGTERM that tears down
// the owned cluster and exits nonzero. Call Stop to remove it on normal exit.
func (c *Cluster) InstallSignalGuard(out io.Writer) *SignalGuard {
	g := &SignalGuard{
		cluster: c,
		out:     out,
		exit:    os.Exit,
		ch:      make(chan os.Signal, 1),
	}
	signal.Notify(g.ch, syscall.SIGINT, syscall.SIGTERM)
	go g.wait()
	return g
}

func (g *SignalGuard) wait() {
	sig, ok := <-g.ch
	if !ok {
		return // Stop closed the channel on normal exit
	}
	g.handle(sig)
}

func (g *SignalGuard) handle(sig os.Signal) {
	_, _ = fmt.Fprintf(g.out, "received signal %v: running bounded ownership-aware cleanup\n", sig)
	ctx, cancel := CleanupContext()
	defer cancel()
	if err := g.cluster.Delete(ctx); err != nil {
		_, _ = fmt.Fprintf(g.out, "cleanup after signal failed: %v\n", err)
	}
	// Exit nonzero so a signal-interrupted run cannot report success.
	g.exit(1)
}

// Stop removes the signal handler. It is safe to call multiple times.
func (g *SignalGuard) Stop() {
	g.stopOnce.Do(func() {
		signal.Stop(g.ch)
		close(g.ch)
	})
}
