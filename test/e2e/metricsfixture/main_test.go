package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestServer(t *testing.T) (*server, *httptest.Server) {
	t.Helper()
	s := newServer()
	ts := httptest.NewServer(s.mux())
	t.Cleanup(ts.Close)
	return s, ts
}

func setSeries(t *testing.T, ts *httptest.Server, body string) {
	t.Helper()
	resp, err := http.Post(ts.URL+"/-/series", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /-/series: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /-/series status = %d, want 200", resp.StatusCode)
	}
}

type queryRangeReply struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Values [][2]interface{}  `json:"values"`
		} `json:"result"`
	} `json:"data"`
	Error string `json:"error"`
}

func doQueryRange(t *testing.T, ts *httptest.Server, query string) (int, queryRangeReply) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/query_range", strings.NewReader(
		"query="+query+"&start=1000&end=1300&step=30"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("query_range: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var reply queryRangeReply
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		t.Fatalf("decode query_range reply: %v", err)
	}
	return resp.StatusCode, reply
}

const cpuQuery = `rate(container_cpu_usage_seconds_total{namespace="shop",pod="web-abc",container="app"}[24h])`
const memQuery = `container_memory_working_set_bytes{namespace="shop",pod="web-abc",container="app"}`

func TestQueryRangePresentReturnsConfiguredMatrixSamples(t *testing.T) {
	_, ts := newTestServer(t)
	setSeries(t, ts, `{"series":[{"namespace":"shop","pod":"web-abc","container":"app",
		"cpuCores":[0.1,0.2,0.3],"memoryBytes":[104857600,209715200]}]}`)

	code, reply := doQueryRange(t, ts, cpuQuery)
	if code != http.StatusOK || reply.Status != "success" {
		t.Fatalf("code=%d status=%q", code, reply.Status)
	}
	if reply.Data.ResultType != "matrix" {
		t.Fatalf("resultType = %q, want matrix", reply.Data.ResultType)
	}
	if len(reply.Data.Result) != 1 {
		t.Fatalf("result series = %d, want 1", len(reply.Data.Result))
	}
	values := reply.Data.Result[0].Values
	if len(values) != 3 {
		t.Fatalf("values = %d, want 3", len(values))
	}
	// Timestamps follow start+i*step; values carry the configured samples in order.
	if values[0][0].(float64) != 1000 || values[1][0].(float64) != 1030 {
		t.Fatalf("unexpected timestamps: %v", values)
	}
	if values[2][1].(string) != "0.3" {
		t.Fatalf("unexpected sample value: %v", values[2])
	}

	_, memReply := doQueryRange(t, ts, memQuery)
	if got := memReply.Data.Result[0].Values; len(got) != 2 || got[1][1].(string) != "209715200" {
		t.Fatalf("memory values = %v, want 2 configured samples", got)
	}
}

func TestQueryRangeUnknownSeriesIsMissing(t *testing.T) {
	_, ts := newTestServer(t)
	code, reply := doQueryRange(t, ts, cpuQuery)
	if code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if reply.Data.ResultType != "matrix" || len(reply.Data.Result) != 0 {
		t.Fatalf("missing series should return empty matrix, got %+v", reply.Data)
	}
}

func TestQueryRangeModes(t *testing.T) {
	_, ts := newTestServer(t)
	setSeries(t, ts, `{"series":[
		{"namespace":"a","pod":"p","container":"c","mode":"missing"},
		{"namespace":"b","pod":"p","container":"c","mode":"invalid"},
		{"namespace":"c","pod":"p","container":"c","mode":"unavailable"}]}`)

	queryFor := func(ns string) string {
		return `rate(container_cpu_usage_seconds_total{namespace="` + ns + `",pod="p",container="c"}[24h])`
	}

	_, missing := doQueryRange(t, ts, queryFor("a"))
	if missing.Data.ResultType != "matrix" || len(missing.Data.Result) != 0 {
		t.Fatalf("missing mode: %+v", missing.Data)
	}

	_, invalid := doQueryRange(t, ts, queryFor("b"))
	if invalid.Data.ResultType != "vector" {
		t.Fatalf("invalid mode should yield non-matrix resultType, got %q", invalid.Data.ResultType)
	}

	code, unavailable := doQueryRange(t, ts, queryFor("c"))
	if code != http.StatusInternalServerError || unavailable.Status != "error" {
		t.Fatalf("unavailable mode: code=%d reply=%+v", code, unavailable)
	}
}

func TestSeriesReplaceIsWholesale(t *testing.T) {
	_, ts := newTestServer(t)
	setSeries(t, ts, `{"series":[{"namespace":"shop","pod":"web-abc","container":"app","cpuCores":[0.5]}]}`)
	setSeries(t, ts, `{"series":[]}`)
	_, reply := doQueryRange(t, ts, cpuQuery)
	if len(reply.Data.Result) != 0 {
		t.Fatalf("replace should drop old series, got %+v", reply.Data)
	}
}

func TestTraceRecordsQueriesAndClears(t *testing.T) {
	s, ts := newTestServer(t)
	for i := 0; i < 2; i++ {
		doQueryRange(t, ts, fmt.Sprintf(`container_memory_working_set_bytes{namespace="n%d",pod="p",container="c"}`, i))
	}

	resp, err := http.Get(ts.URL + "/-/trace")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var trace struct {
		Queries []recordedQuery `json:"queries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&trace); err != nil {
		t.Fatal(err)
	}
	if len(trace.Queries) != 2 {
		t.Fatalf("trace entries = %d, want 2", len(trace.Queries))
	}
	if !strings.Contains(trace.Queries[0].Query, `namespace="n0"`) {
		t.Fatalf("trace lost query text: %q", trace.Queries[0].Query)
	}

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/-/trace", nil)
	delResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = delResp.Body.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.trace) != 0 {
		t.Fatalf("trace not cleared: %d entries", len(s.trace))
	}
}

func TestBuildinfoSucceeds(t *testing.T) {
	_, ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/api/v1/status/buildinfo")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var reply struct {
		Status string            `json:"status"`
		Data   map[string]string `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		t.Fatal(err)
	}
	if reply.Status != "success" || reply.Data["version"] == "" {
		t.Fatalf("unexpected buildinfo reply: %+v", reply)
	}
}

func TestParseLabelsHandlesProviderQueryShapes(t *testing.T) {
	k := parseLabels(cpuQuery)
	if k.namespace != "shop" || k.pod != "web-abc" || k.container != "app" {
		t.Fatalf("parseLabels = %+v", k)
	}
	if !isCPUQuery(cpuQuery) {
		t.Fatal("cpu query not detected")
	}
	if isCPUQuery(memQuery) {
		t.Fatal("memory query misdetected as cpu")
	}
}

// writeTokenFile materializes a fake projected service-account token for the
// scrape-through tests.
func writeTokenFile(t *testing.T, token string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(token), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func doScrapeMetrics(t *testing.T, ts *httptest.Server) (int, string) {
	t.Helper()
	resp, err := http.Get(ts.URL + "/-/scrape-metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(body)
}

// TestScrapeMetricsRelaysAuthenticatedExposition proves the scrape-through
// endpoint presents the pod's bearer token to the controller metrics endpoint
// over HTTPS (the shipped ServiceMonitor shape) and relays the exposition.
func TestScrapeMetricsRelaysAuthenticatedExposition(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte("optipod_workloads_monitored{namespace=\"n\",policy=\"p\"} 1\n"))
	}))
	t.Cleanup(upstream.Close)

	s, ts := newTestServer(t)
	s.scrapeTarget = upstream.URL
	s.tokenPath = writeTokenFile(t, "fixture-sa-token\n")

	code, body := doScrapeMetrics(t, ts)
	if code != http.StatusOK {
		t.Fatalf("code = %d, want 200 (body %q)", code, body)
	}
	if gotAuth != "Bearer fixture-sa-token" {
		t.Fatalf("upstream Authorization = %q, want the projected bearer token", gotAuth)
	}
	if !strings.Contains(body, `optipod_workloads_monitored{namespace="n",policy="p"} 1`) {
		t.Fatalf("exposition not relayed verbatim: %q", body)
	}
}

// TestScrapeMetricsRelaysUpstreamFailureStatus proves a filtered-endpoint
// rejection (e.g. 401/403 from the authentication/authorization filter) is
// surfaced with its status rather than masked as a fixture success.
func TestScrapeMetricsRelaysUpstreamFailureStatus(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(upstream.Close)

	s, ts := newTestServer(t)
	s.scrapeTarget = upstream.URL
	s.tokenPath = writeTokenFile(t, "fixture-sa-token")

	code, body := doScrapeMetrics(t, ts)
	if code != http.StatusForbidden {
		t.Fatalf("code = %d, want the upstream 403 relayed", code)
	}
	if !strings.Contains(body, "HTTP 403") {
		t.Fatalf("error envelope should name the upstream status, got %q", body)
	}
}

// TestScrapeMetricsWithoutToken proves the endpoint fails closed when the
// projected service-account token is absent, instead of scraping anonymously.
func TestScrapeMetricsWithoutToken(t *testing.T) {
	s, ts := newTestServer(t)
	s.scrapeTarget = "https://127.0.0.1:1/metrics"
	s.tokenPath = filepath.Join(t.TempDir(), "absent-token")

	code, body := doScrapeMetrics(t, ts)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503", code)
	}
	if !strings.Contains(body, "token is not readable") {
		t.Fatalf("error envelope should explain the missing token, got %q", body)
	}
}

// TestScrapeMetricsRejectsNonGet pins the read-only contract of the
// scrape-through endpoint.
func TestScrapeMetricsRejectsNonGet(t *testing.T) {
	_, ts := newTestServer(t)
	resp, err := http.Post(ts.URL+"/-/scrape-metrics", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("code = %d, want 405", resp.StatusCode)
	}
}
