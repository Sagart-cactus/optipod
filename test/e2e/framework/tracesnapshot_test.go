package framework

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// snapshotTestClients builds a Clients whose rest.Config points at the given
// test server, so SnapshotFixtureTrace exercises its real constructor path
// (dedicated clientset included).
func snapshotTestClients(t *testing.T, srv *httptest.Server) *Clients {
	t.Helper()
	cfg := &rest.Config{Host: srv.URL}
	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("build clientset: %v", err)
	}
	return &Clients{Kube: kube, restConfig: cfg}
}

// snapshotTraceServer serves one recorded query carrying canary-shaped
// sensitive content, so the snapshot proves it passes through the diagnostic
// redaction pipeline rather than being written raw.
const snapshotCanaryToken = "Bearer fake-snapshot-token-0123456789abcdef"

func snapshotTraceServer() http.Handler {
	prefix := "/api/v1/namespaces/" + FixtureNamespace + "/services/" + FixtureServiceName + ":9090/proxy/-/trace"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != prefix || r.Method != http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"queries": []RecordedQuery{
				{Query: `rate(container_cpu_usage_seconds_total{namespace="shop",pod="web-0",container="app"}[24h])`},
				{Query: `rate(container_cpu_usage_seconds_total{namespace="shop",pod="web-1",container="` + snapshotCanaryToken + `"}[24h])`},
			},
		})
	})
}

func TestSnapshotFixtureTraceWritesRedactedSnapshot(t *testing.T) {
	srv := httptest.NewServer(snapshotTraceServer())
	t.Cleanup(srv.Close)
	dir := t.TempDir()

	path, redactions, err := SnapshotFixtureTrace(context.Background(), snapshotTestClients(t, srv), dir,
		"01-Deliberate failure diagnostics validation fails a deliberate assertion")
	if err != nil {
		t.Fatalf("SnapshotFixtureTrace: %v", err)
	}
	wantSuffix := filepath.Join("fixture-trace-on-failure",
		"01-Deliberate-failure-diagnostics-validation-fails-a-deliberate-assertion.json")
	if !strings.HasSuffix(path, wantSuffix) {
		t.Fatalf("path = %q, want suffix %q", path, wantSuffix)
	}
	if redactions < 1 {
		t.Fatalf("redactions = %d, want at least one (the canary bearer token in a query)", redactions)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	content := string(raw)
	if strings.Contains(content, snapshotCanaryToken) {
		t.Fatalf("the snapshot leaked the canary token verbatim:\n%s", content)
	}
	if !strings.Contains(content, RedactPlaceholder) {
		t.Fatalf("the snapshot does not show the redaction placeholder:\n%s", content)
	}
	var snapshot FixtureTraceSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	if snapshot.Spec == "" || snapshot.CapturedAt.IsZero() {
		t.Fatalf("snapshot missing spec or capture time: %+v", snapshot)
	}
	if len(snapshot.Queries) != 2 {
		t.Fatalf("snapshot recorded %d queries, want the fixture's 2", len(snapshot.Queries))
	}
}

func TestSnapshotFixtureTracePropagatesReadFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"status":"error","error":"proxy down"}`))
	}))
	t.Cleanup(srv.Close)

	_, _, err := SnapshotFixtureTrace(context.Background(), snapshotTestClients(t, srv), t.TempDir(), "01-spec")
	if err == nil {
		t.Fatal("expected the trace read failure to propagate")
	}
	if !strings.Contains(err.Error(), "read fixture trace for the failure snapshot") {
		t.Fatalf("error = %v, want the snapshot read context in the message", err)
	}
}

func TestSnapshotFixtureTraceValidatesInputs(t *testing.T) {
	if _, _, err := SnapshotFixtureTrace(context.Background(), nil, t.TempDir(), "01-spec"); err == nil {
		t.Fatal("expected an error for nil clients")
	}
	srv := httptest.NewServer(snapshotTraceServer())
	t.Cleanup(srv.Close)
	if _, _, err := SnapshotFixtureTrace(context.Background(), snapshotTestClients(t, srv), "", "01-spec"); err == nil {
		t.Fatal("expected an error for an empty output directory")
	}
	if _, _, err := SnapshotFixtureTrace(context.Background(), snapshotTestClients(t, srv), t.TempDir(), "  "); err == nil {
		t.Fatal("expected an error for a blank spec name")
	}
}

func TestSanitizeSnapshotName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"02-spec text (with) [chars]", "02-spec-text-with-chars"},
		{"", "spec"},
		{"---", "spec"},
		{"already-safe_1.2", "already-safe_1.2"},
	}
	for _, tc := range cases {
		if got := sanitizeSnapshotName(tc.in); got != tc.want {
			t.Errorf("sanitizeSnapshotName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	long := strings.Repeat("a", snapshotNameMaxLen+50)
	if got := sanitizeSnapshotName(long); len(got) > snapshotNameMaxLen {
		t.Errorf("sanitizeSnapshotName did not truncate: len = %d", len(got))
	}
}
