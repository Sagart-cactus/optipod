// Package main is the deterministic in-cluster Prometheus-compatible metrics
// fixture used by the OptiPod e2e harness. It serves the subset of the
// Prometheus HTTP API that the production Prometheus metrics provider consumes
// (/api/v1/query_range and /api/v1/status/buildinfo) with fully deterministic,
// test-controlled series, plus a small admin API the harness uses to replace
// series and inspect the queries the deployed controller actually issued.
//
// The fixture intentionally uses only the standard library so its image builds
// quickly and hermetically from the repository checkout.
package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Series behaviour modes. They model the fault cases the harness must drive the
// deployed controller through: present data, missing data (successful empty
// result), invalid data (well-formed envelope with an unexpected result type),
// and an unavailable backend (HTTP error).
const (
	ModePresent     = "present"
	ModeMissing     = "missing"
	ModeInvalid     = "invalid"
	ModeUnavailable = "unavailable"
)

// Prometheus API envelope vocabulary.
const (
	statusSuccess     = "success"
	statusError       = "error"
	errorTypeBadData  = "bad_data"
	errorTypeInternal = "internal"
	resultTypeMatrix  = "matrix"
	resultTypeVector  = "vector"
)

// Series describes the deterministic CPU (cores) and memory (bytes) samples for
// one namespace/pod/container target.
type Series struct {
	Namespace   string    `json:"namespace"`
	Pod         string    `json:"pod"`
	Container   string    `json:"container"`
	Mode        string    `json:"mode"` // empty means present
	CPUCores    []float64 `json:"cpuCores,omitempty"`
	MemoryBytes []float64 `json:"memoryBytes,omitempty"`
}

type seriesKey struct {
	namespace string
	pod       string
	container string
}

func keyOf(namespace, pod, container string) seriesKey {
	return seriesKey{namespace: namespace, pod: pod, container: container}
}

// recordedQuery captures one query_range request issued against the fixture so
// tests can prove what the deployed controller actually requested.
type recordedQuery struct {
	Time  time.Time `json:"time"`
	Query string    `json:"query"`
}

const maxTraceEntries = 4096

// Controller-metrics scrape-through configuration. The target defaults to the
// shipped, name-prefixed metrics Service of the deployed controller
// (config/default/metrics_service.yaml) in the harness-owned cluster; main()
// lets the fixture manifest pin it explicitly through CONTROLLER_METRICS_URL.
// The token path is the projected service-account token, the same
// bearerTokenFile the shipped ServiceMonitor (config/prometheus/monitor.yaml)
// configures for a real Prometheus.
const (
	defaultControllerMetricsURL = "https://optipod-controller-manager-metrics-service.optipod-system.svc:8443/metrics"
	defaultTokenPath            = "/var/run/secrets/kubernetes.io/serviceaccount/token"
)

type server struct {
	mu     sync.Mutex
	series map[seriesKey]Series
	trace  []recordedQuery

	// scrapeTarget and tokenPath are configuration, not request input, so the
	// scrape-through endpoint is not an open proxy. scrapeClient mirrors the
	// shipped ServiceMonitor's tlsConfig: the deployed controller serves
	// metrics with a self-signed certificate, so the shipped monitor scrapes
	// with insecureSkipVerify and the bearer token is the identity anchor.
	scrapeTarget string
	tokenPath    string
	scrapeClient *http.Client
}

func newServer() *server {
	return &server{
		series:       map[seriesKey]Series{},
		scrapeTarget: defaultControllerMetricsURL,
		tokenPath:    defaultTokenPath,
		scrapeClient: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // mirrors shipped ServiceMonitor tlsConfig
			},
		},
	}
}

func (s *server) mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/query_range", s.handleQueryRange)
	mux.HandleFunc("/api/v1/status/buildinfo", s.handleBuildinfo)
	mux.HandleFunc("/-/series", s.handleSeries)
	mux.HandleFunc("/-/trace", s.handleTrace)
	mux.HandleFunc("/-/scrape-metrics", s.handleScrapeMetrics)
	mux.HandleFunc("/healthz", handleOK)
	mux.HandleFunc("/readyz", handleOK)
	return mux
}

func handleOK(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

var labelPattern = regexp.MustCompile(`(namespace|pod|container)="([^"]*)"`)

// parseLabels extracts the namespace/pod/container label matchers from a PromQL
// query string produced by the production provider.
func parseLabels(query string) seriesKey {
	k := seriesKey{}
	for _, m := range labelPattern.FindAllStringSubmatch(query, -1) {
		switch m[1] {
		case "namespace":
			k.namespace = m[2]
		case "pod":
			k.pod = m[2]
		case "container":
			k.container = m[2]
		}
	}
	return k
}

type apiResponse struct {
	Status    string   `json:"status"`
	Data      *apiData `json:"data,omitempty"`
	ErrorType string   `json:"errorType,omitempty"`
	Error     string   `json:"error,omitempty"`
	Warnings  []string `json:"warnings,omitempty"`
}

type apiData struct {
	ResultType string          `json:"resultType"`
	Result     json.RawMessage `json:"result"`
}

type matrixEntry struct {
	Metric map[string]string `json:"metric"`
	Values [][2]interface{}  `json:"values"`
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *server) handleQueryRange(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, apiResponse{
			Status: statusError, ErrorType: errorTypeBadData, Error: fmt.Sprintf("parse form: %v", err),
		})
		return
	}
	query := r.FormValue("query")

	s.mu.Lock()
	if len(s.trace) < maxTraceEntries {
		s.trace = append(s.trace, recordedQuery{Time: time.Now().UTC(), Query: query})
	}
	k := parseLabels(query)
	series, ok := s.series[k]
	s.mu.Unlock()

	if !ok {
		series = Series{Mode: ModeMissing}
	}

	switch series.Mode {
	case ModeUnavailable:
		writeJSON(w, http.StatusInternalServerError, apiResponse{
			Status: statusError, ErrorType: errorTypeInternal,
			Error: "fixture configured to be unavailable",
		})
		return
	case ModeInvalid:
		// A well-formed envelope with the wrong result type: the production
		// provider fails its matrix assertion on this.
		writeJSON(w, http.StatusOK, apiResponse{
			Status: statusSuccess,
			Data:   &apiData{ResultType: resultTypeVector, Result: json.RawMessage(`[]`)},
		})
		return
	case ModeMissing:
		writeJSON(w, http.StatusOK, apiResponse{
			Status: statusSuccess,
			Data:   &apiData{ResultType: resultTypeMatrix, Result: json.RawMessage(`[]`)},
		})
		return
	}

	samples := series.MemoryBytes
	if isCPUQuery(query) {
		samples = series.CPUCores
	}
	if len(samples) == 0 {
		writeJSON(w, http.StatusOK, apiResponse{
			Status: statusSuccess,
			Data:   &apiData{ResultType: resultTypeMatrix, Result: json.RawMessage(`[]`)},
		})
		return
	}

	start, end, step := parseRange(r)
	values := make([][2]interface{}, 0, len(samples))
	for i, v := range samples {
		ts := start + float64(i)*step
		if ts > end {
			break
		}
		values = append(values, [2]interface{}{ts, strconv.FormatFloat(v, 'f', -1, 64)})
	}
	result, err := json.Marshal([]matrixEntry{{
		Metric: map[string]string{
			"namespace": k.namespace,
			"pod":       k.pod,
			"container": k.container,
		},
		Values: values,
	}})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, apiResponse{
			Status: statusError, ErrorType: errorTypeInternal, Error: err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, apiResponse{
		Status: statusSuccess,
		Data:   &apiData{ResultType: resultTypeMatrix, Result: result},
	})
}

var cpuMetricPattern = regexp.MustCompile(`container_cpu_usage_seconds_total`)

func isCPUQuery(query string) bool {
	return cpuMetricPattern.MatchString(query)
}

// parseRange reads Prometheus query_range start/end/step parameters, falling
// back to a one-hour window at a 30s step when absent or malformed.
func parseRange(r *http.Request) (start, end, step float64) {
	now := float64(time.Now().Unix())
	start, err1 := strconv.ParseFloat(r.FormValue("start"), 64)
	end, err2 := strconv.ParseFloat(r.FormValue("end"), 64)
	step, err3 := strconv.ParseFloat(r.FormValue("step"), 64)
	if err1 != nil || err2 != nil || err3 != nil || step <= 0 || end <= start {
		return now - 3600, now, 30
	}
	return start, end, step
}

func (s *server) handleBuildinfo(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": statusSuccess,
		"data": map[string]string{
			"version":  "optipod-e2e-fixture",
			"revision": "pinned",
		},
	})
}

type seriesRequest struct {
	Series []Series `json:"series"`
}

// handleSeries replaces the entire configured series set on POST.
func (s *server) handleSeries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, apiResponse{
			Status: statusError, ErrorType: errorTypeBadData, Error: "POST required",
		})
		return
	}
	var req seriesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, apiResponse{
			Status: statusError, ErrorType: errorTypeBadData, Error: fmt.Sprintf("decode series: %v", err),
		})
		return
	}
	next := make(map[seriesKey]Series, len(req.Series))
	for _, sr := range req.Series {
		mode := sr.Mode
		if mode == "" {
			mode = ModePresent
		}
		sr.Mode = mode
		next[keyOf(sr.Namespace, sr.Pod, sr.Container)] = sr
	}
	s.mu.Lock()
	s.series = next
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, apiResponse{Status: statusSuccess})
}

func (s *server) handleTrace(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	trace := make([]recordedQuery, len(s.trace))
	copy(trace, s.trace)
	s.mu.Unlock()

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]interface{}{"queries": trace})
	case http.MethodDelete:
		s.mu.Lock()
		s.trace = nil
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, apiResponse{Status: statusSuccess})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, apiResponse{
			Status: statusError, ErrorType: errorTypeBadData, Error: "GET or DELETE required",
		})
	}
}

// handleScrapeMetrics scrapes the deployed controller's metrics endpoint from
// inside the cluster and relays the Prometheus exposition verbatim. It mirrors
// the shipped ServiceMonitor (config/prometheus/monitor.yaml): HTTPS against
// the shipped metrics Service, the pod's projected service-account token as
// the bearer credential, and no serving-certificate verification (the endpoint
// presents a self-signed certificate; authentication and authorization happen
// in the endpoint's own filter against the token identity). An out-of-cluster
// caller cannot present that bearer token through the API server's service
// proxy — the API server strips the caller's Authorization header after
// authenticating it — so a Prometheus-shaped scrape of the filtered endpoint
// can only happen in-cluster, exactly where a real Prometheus agent runs. The
// token is read from the projected volume per request and never logged.
func (s *server) handleScrapeMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, apiResponse{
			Status: statusError, ErrorType: errorTypeBadData, Error: "GET required",
		})
		return
	}
	token, err := os.ReadFile(s.tokenPath)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, apiResponse{
			Status: statusError, ErrorType: errorTypeInternal,
			Error: "the fixture's projected service-account token is not readable",
		})
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, s.scrapeTarget, nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, apiResponse{
			Status: statusError, ErrorType: errorTypeInternal,
			Error: "build the metrics scrape request",
		})
		return
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	req.Header.Set("Accept", "text/plain")
	resp, err := s.scrapeClient.Do(req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, apiResponse{
			Status: statusError, ErrorType: errorTypeInternal,
			Error: fmt.Sprintf("scrape the controller metrics endpoint: %v", err),
		})
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		writeJSON(w, resp.StatusCode, apiResponse{
			Status: statusError, ErrorType: errorTypeInternal,
			Error: fmt.Sprintf("the controller metrics endpoint answered HTTP %d", resp.StatusCode),
		})
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, resp.Body)
}

func main() {
	port := os.Getenv("FIXTURE_PORT")
	if port == "" {
		port = "9090"
	}
	srv := newServer()
	if target := os.Getenv("CONTROLLER_METRICS_URL"); target != "" {
		srv.scrapeTarget = target
	}
	httpSrv := &http.Server{
		Addr:              ":" + port,
		Handler:           srv.mux(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("optipod metrics fixture listening on :%s", port)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("fixture server failed: %v", err)
	}
}
