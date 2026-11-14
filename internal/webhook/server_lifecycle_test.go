package webhook

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestServerRestartStopsPreviousRun(t *testing.T) {
	dir := t.TempDir()
	cert, key := filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key")
	if err := createMockCertificates(cert, key); err != nil {
		t.Fatal(err)
	}
	s := NewServer(createFakeClient(), cert, key, 0, nil)
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- s.Start(ctx) }()
		deadline := time.After(5 * time.Second)
		ticker := time.NewTicker(time.Millisecond)
		for s.CheckReady(nil) != nil {
			select {
			case err := <-done:
				cancel()
				ticker.Stop()
				t.Fatalf("server exited before readiness: %v", err)
			case <-deadline:
				cancel()
				ticker.Stop()
				t.Fatal("server did not become ready")
			case <-ticker.C:
			}
		}
		ticker.Stop()
		if err := s.Stop(ctx); err != nil {
			cancel()
			t.Fatal(err)
		}
		if s.CheckReady(nil) == nil {
			cancel()
			t.Fatal("stopped listener still reports ready")
		}
		select {
		case err := <-done:
			if err != nil {
				cancel()
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			cancel()
			t.Fatal("Stop left Start waiting for context cancellation")
		}
		cancel()
	}
}
