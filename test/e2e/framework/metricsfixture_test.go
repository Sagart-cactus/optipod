package framework

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// fixtureAPIServer fakes the API-server service-proxy hop to the fixture and
// records the requests the harness client makes.
type fixtureAPIServer struct {
	t        *testing.T
	lastPath string
	lastBody []byte
	trace    []RecordedQuery
}

func (f *fixtureAPIServer) handler() http.Handler {
	mux := http.NewServeMux()
	prefix := "/api/v1/namespaces/" + FixtureNamespace + "/services/" + FixtureServiceName + ":9090/proxy/-/"
	mux.HandleFunc(prefix+"series", func(w http.ResponseWriter, r *http.Request) {
		f.lastPath = r.URL.Path
		body, err := io.ReadAll(r.Body)
		if err != nil {
			f.t.Errorf("read body: %v", err)
		}
		f.lastBody = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success"}`))
	})
	mux.HandleFunc(prefix+"scrape-metrics", func(w http.ResponseWriter, r *http.Request) {
		f.lastPath = r.URL.Path
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte("optipod_workloads_monitored 1\n"))
	})
	mux.HandleFunc(prefix+"trace", func(w http.ResponseWriter, r *http.Request) {
		f.lastPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodDelete {
			f.trace = nil
			_, _ = w.Write([]byte(`{"status":"success"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"queries": f.trace})
	})
	return mux
}

func newFixtureTestClient(t *testing.T, srv *httptest.Server) *MetricsFixtureClient {
	t.Helper()
	kube, err := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	return &MetricsFixtureClient{kube: kube, namespace: FixtureNamespace, serviceName: FixtureServiceName, port: FixturePort}
}

func TestSetSeriesPostsThroughServiceProxy(t *testing.T) {
	fake := &fixtureAPIServer{t: t}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)
	client := newFixtureTestClient(t, srv)

	err := client.SetSeries(context.Background(), []FixtureSeries{{
		Namespace: "shop", Pod: "web-abc", Container: "app",
		CPUCores: []float64{0.1, 0.2}, MemoryBytes: []float64{104857600},
	}})
	if err != nil {
		t.Fatal(err)
	}
	wantPath := "/api/v1/namespaces/optipod-e2e-metrics/services/optipod-metrics-fixture:9090/proxy/-/series"
	if fake.lastPath != wantPath {
		t.Fatalf("proxy path = %q, want %q", fake.lastPath, wantPath)
	}
	var payload struct {
		Series []struct {
			Namespace   string    `json:"namespace"`
			Pod         string    `json:"pod"`
			Container   string    `json:"container"`
			CPUCores    []float64 `json:"cpuCores"`
			MemoryBytes []float64 `json:"memoryBytes"`
		} `json:"series"`
	}
	if err := json.Unmarshal(fake.lastBody, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Series) != 1 || payload.Series[0].Namespace != "shop" ||
		payload.Series[0].CPUCores[1] != 0.2 || payload.Series[0].MemoryBytes[0] != 104857600 {
		t.Fatalf("unexpected payload: %s", fake.lastBody)
	}
}

func TestTraceAndClearTrace(t *testing.T) {
	fake := &fixtureAPIServer{t: t}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)
	client := newFixtureTestClient(t, srv)

	fake.trace = []RecordedQuery{{Query: `rate(container_cpu_usage_seconds_total{namespace="n",pod="p",container="c"}[24h])`}}
	queries, err := client.Trace(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) != 1 || !strings.Contains(queries[0].Query, "container_cpu_usage_seconds_total") {
		t.Fatalf("trace = %+v", queries)
	}

	if err := client.ClearTrace(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fake.trace != nil {
		t.Fatalf("trace not cleared: %+v", fake.trace)
	}
}

// TestScrapeControllerMetricsThroughServiceProxy pins the harness-side path of
// the in-cluster controller metrics scrape: the fixture's scrape-through
// endpoint behind the API server's service proxy.
func TestScrapeControllerMetricsThroughServiceProxy(t *testing.T) {
	fake := &fixtureAPIServer{t: t}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)
	client := newFixtureTestClient(t, srv)

	exposition, err := client.ScrapeControllerMetrics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantPath := "/api/v1/namespaces/optipod-e2e-metrics/services/optipod-metrics-fixture:9090/proxy/-/scrape-metrics"
	if fake.lastPath != wantPath {
		t.Fatalf("proxy path = %q, want %q", fake.lastPath, wantPath)
	}
	if !strings.Contains(exposition, "optipod_workloads_monitored 1") {
		t.Fatalf("exposition = %q", exposition)
	}
}

// TestScrapeControllerMetricsPropagatesFailures pins that a fixture-side
// failure (including a relayed 401/403 from the controller's filtered
// endpoint) reaches the caller instead of being mistaken for a scrape.
func TestScrapeControllerMetricsPropagatesFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"status":"error","error":"the controller metrics endpoint answered HTTP 403"}`))
	}))
	t.Cleanup(srv.Close)
	client := newFixtureTestClient(t, srv)
	if _, err := client.ScrapeControllerMetrics(context.Background()); err == nil {
		t.Fatal("expected propagated scrape failure")
	}
}

func TestSetSeriesPropagatesAPIErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"status":"error","error":"boom"}`))
	}))
	t.Cleanup(srv.Close)
	client := newFixtureTestClient(t, srv)
	if err := client.SetSeries(context.Background(), nil); err == nil {
		t.Fatal("expected propagated API error")
	}
}
