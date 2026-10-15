package framework

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// FixtureTraceSnapshot is the per-spec failure forensic record: the fixture's
// recorded queries captured immediately after a spec fails, BEFORE any later
// spec's ClearTrace can wipe the failing window (the M3 VAL-CONF-005 forensics
// gap). The anchored timestamps let a reviewer confirm the snapshot covers the
// failed spec's window.
type FixtureTraceSnapshot struct {
	// Spec is the failed spec's full Ginkgo text.
	Spec string `json:"spec"`
	// CapturedAt is when the snapshot was read from the fixture (UTC).
	CapturedAt time.Time `json:"capturedAt"`
	// Queries are the fixture's recorded query_range requests, oldest first.
	Queries []RecordedQuery `json:"queries"`
}

const (
	// snapshotTraceAttempts/snapshotTraceBackoff bound the snapshot's trace
	// read: a failure snapshot must not hang the suite's report path, but a
	// single transient proxy error must not lose the failing window either.
	snapshotTraceAttempts = 3
	snapshotTraceBackoff  = 2 * time.Second

	// snapshotNameMaxLen bounds the sanitized file-name fragment derived from
	// a spec's full text.
	snapshotNameMaxLen = 120
)

// snapshotNameUnsafe matches every character that must not reach an artifact
// file name derived from free-text spec descriptions.
var snapshotNameUnsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// SnapshotFixtureTrace reads the current fixture trace through the dedicated
// fixture client and writes it to <dir>/fixture-trace-on-failure/<name>.json,
// marshaled and redacted through exactly the same pipeline as the diagnostic
// bundle (collector.writeJSON). It returns the written path and the redaction
// count so the caller can print both as evidence. Any failure — including the
// trace read itself — is returned to the caller; this function never panics or
// fails a spec, so a snapshot problem can never mask the original failure.
func SnapshotFixtureTrace(ctx context.Context, clients *Clients, dir, name string) (string, int, error) {
	if clients == nil {
		return "", 0, fmt.Errorf("fixture trace snapshot needs typed clients from the private kubeconfig")
	}
	if dir == "" || strings.TrimSpace(name) == "" {
		return "", 0, fmt.Errorf("fixture trace snapshot needs an output directory and a spec name")
	}
	fixture, err := NewMetricsFixtureClient(clients)
	if err != nil {
		return "", 0, fmt.Errorf("build the fixture client for the failure snapshot: %w", err)
	}
	trace, _, err := fixture.TraceWithRetry(ctx, snapshotTraceAttempts, snapshotTraceBackoff)
	if err != nil {
		return "", 0, fmt.Errorf("read fixture trace for the failure snapshot: %w", err)
	}
	rel := filepath.Join("fixture-trace-on-failure", sanitizeSnapshotName(name)+".json")
	c := &collector{bundle: &DiagnosticBundle{Dir: dir}}
	c.writeJSON(rel, FixtureTraceSnapshot{
		Spec:       name,
		CapturedAt: time.Now().UTC(),
		Queries:    trace,
	})
	if len(c.bundle.CollectionErrors) > 0 {
		return "", c.bundle.Redactions,
			fmt.Errorf("write the fixture trace snapshot: %s", strings.Join(c.bundle.CollectionErrors, "; "))
	}
	return filepath.Join(dir, rel), c.bundle.Redactions, nil
}

// sanitizeSnapshotName turns free text (a spec's full description, with a
// caller-supplied uniqueness prefix) into a safe artifact file-name fragment.
func sanitizeSnapshotName(name string) string {
	s := snapshotNameUnsafe.ReplaceAllString(name, "-")
	s = strings.Trim(s, "-")
	if len(s) > snapshotNameMaxLen {
		s = strings.Trim(s[:snapshotNameMaxLen], "-")
	}
	if s == "" {
		return "spec"
	}
	return s
}
