/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package metrics

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// These tests drive the production PrometheusProvider (the metrics path the
// deployed controller uses) against a local HTTP server speaking the real
// Prometheus /api/v1/query_range protocol (VAL-FAST-007). They capture the
// provider's actual requests (query text, window, step) and assert exact
// percentile outputs for valid windows plus error decisions for missing,
// empty, malformed and unavailable responses, followed by recovery.

const (
	providerNamespace  = "metrics-fault-ns"
	providerPod        = "fault-pod"
	providerContainer  = "app"
	cpuMetricSubstring = "container_cpu_usage_seconds_total"
	memMetricSubstring = "container_memory_working_set_bytes"
)

// capturedRangeQuery records one range-query request the provider issued.
type capturedRangeQuery struct {
	Query string
	Start time.Time
	End   time.Time
	Step  time.Duration
}

// promTestServer serves the Prometheus range-query API and records every
// request the production provider makes. respond maps a query to an HTTP
// status and response body.
type promTestServer struct {
	*httptest.Server

	mu       sync.Mutex
	captured []capturedRangeQuery
	failNext int // number of upcoming requests to answer with 503
	respond  func(query string) (int, string)
}

func newPromTestServer(t *testing.T, respond func(query string) (int, string)) *promTestServer {
	t.Helper()

	server := &promTestServer{respond: respond}
	server.Server = httptest.NewServer(server)
	t.Cleanup(server.Close)
	return server
}

func (s *promTestServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/v1/query_range" {
		http.Error(w, "unexpected path", http.StatusNotFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	if s.failNext > 0 {
		s.failNext--
		s.mu.Unlock()
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("metrics backend temporarily unavailable"))
		return
	}
	s.mu.Unlock()

	query := r.Form.Get("query")
	start := parsePromTimestamp(r.Form.Get("start"))
	end := parsePromTimestamp(r.Form.Get("end"))
	step := parsePromStep(r.Form.Get("step"))

	s.mu.Lock()
	s.captured = append(s.captured, capturedRangeQuery{Query: query, Start: start, End: end, Step: step})
	s.mu.Unlock()

	code, body := s.respond(query)
	w.WriteHeader(code)
	_, _ = w.Write([]byte(body))
}

func (s *promTestServer) queries() []capturedRangeQuery {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]capturedRangeQuery(nil), s.captured...)
}

func (s *promTestServer) failNextRequests(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failNext = n
}

func parsePromTimestamp(value string) time.Time {
	seconds, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(int64(seconds), int64((seconds-float64(int64(seconds)))*1e9))
}

func parsePromStep(value string) time.Duration {
	seconds, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return time.Duration(seconds * float64(time.Second))
}

// promMatrixResponse renders a successful Prometheus matrix result.
func promMatrixResponse(values string) (int, string) {
	return http.StatusOK, fmt.Sprintf(
		`{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"__name__":"m"},"values":[%s]}]}}`,
		values,
	)
}

func newTestPrometheusProvider(t *testing.T, server *promTestServer) *PrometheusProvider {
	t.Helper()

	provider, err := NewPrometheusProvider(PrometheusConfig{URL: server.URL})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	return provider
}

// TestPrometheusProviderValidRangeQueryWindow proves the provider issues a
// correct range query for the requested window and computes exact percentiles
// from the returned samples (VAL-FAST-007).
func TestPrometheusProviderValidRangeQueryWindow(t *testing.T) {
	window := 2 * time.Hour
	// CPU samples in cores: [0.2, 0.25, 0.3, 0.35, 0.4] -> millicores
	// [200, 250, 300, 350, 400] -> P50=300m, P90=380m, P99=398m.
	// Memory samples in bytes: [104857600, 209715200, 262144000] -> P50=200Mi,
	// P90=251658240, P99=261095424.
	server := newPromTestServer(t, func(query string) (int, string) {
		switch {
		case strings.Contains(query, cpuMetricSubstring):
			return promMatrixResponse(`[0,"0.2"],[1,"0.25"],[2,"0.3"],[3,"0.35"],[4,"0.4"]`)
		case strings.Contains(query, memMetricSubstring):
			return promMatrixResponse(`[0,"104857600"],[1,"209715200"],[2,"262144000"]`)
		default:
			return http.StatusOK, `{"status":"success","data":{"resultType":"matrix","result":[]}}`
		}
	})
	provider := newTestPrometheusProvider(t, server)

	containerMetrics, err := provider.GetContainerMetrics(context.Background(), providerNamespace, providerPod, providerContainer, window)
	if err != nil {
		t.Fatalf("GetContainerMetrics returned an error: %v", err)
	}

	// Exact CPU percentiles.
	if got := containerMetrics.CPU.P50.MilliValue(); got != 300 {
		t.Errorf("CPU P50 = %d milli, want 300 milli", got)
	}
	if got := containerMetrics.CPU.P90.MilliValue(); got != 380 {
		t.Errorf("CPU P90 = %d milli, want 380 milli", got)
	}
	if got := containerMetrics.CPU.P99.MilliValue(); got != 398 {
		t.Errorf("CPU P99 = %d milli, want 398 milli", got)
	}

	// Exact memory percentiles.
	if got := containerMetrics.Memory.P50.Value(); got != 209715200 {
		t.Errorf("memory P50 = %d bytes, want 209715200", got)
	}
	if got := containerMetrics.Memory.P90.Value(); got != 251658240 {
		t.Errorf("memory P90 = %d bytes, want 251658240", got)
	}
	if got := containerMetrics.Memory.P99.Value(); got != 261095424 {
		t.Errorf("memory P99 = %d bytes, want 261095424", got)
	}

	// The provider must have issued exactly two range queries (CPU then
	// memory) with the expected PromQL and window.
	queries := server.queries()
	if len(queries) != 2 {
		t.Fatalf("expected 2 captured queries, got %d: %+v", len(queries), queries)
	}
	wantCPUQuery := fmt.Sprintf(
		`rate(container_cpu_usage_seconds_total{namespace="%s",pod="%s",container="%s"}[2h])`,
		providerNamespace, providerPod, providerContainer,
	)
	if queries[0].Query != wantCPUQuery {
		t.Errorf("CPU query = %q, want %q", queries[0].Query, wantCPUQuery)
	}
	wantMemoryQuery := fmt.Sprintf(
		`container_memory_working_set_bytes{namespace="%s",pod="%s",container="%s"}`,
		providerNamespace, providerPod, providerContainer,
	)
	if queries[1].Query != wantMemoryQuery {
		t.Errorf("memory query = %q, want %q", queries[1].Query, wantMemoryQuery)
	}
	for i, query := range queries {
		observedWindow := query.End.Sub(query.Start)
		if observedWindow < window-5*time.Second || observedWindow > window+5*time.Second {
			t.Errorf("query %d window = %s, want %s", i, observedWindow, window)
		}
		if query.Step != 30*time.Second {
			t.Errorf("query %d step = %s, want 30s", i, query.Step)
		}
	}
}

// TestPrometheusProviderMissingAndEmptySeries proves the exact error decision
// for absent series and present-but-empty series (VAL-FAST-007).
func TestPrometheusProviderMissingAndEmptySeries(t *testing.T) {
	t.Run("no series at all is an error, not a zero recommendation", func(t *testing.T) {
		server := newPromTestServer(t, func(string) (int, string) {
			// A completely empty matrix result: the series does not exist.
			return http.StatusOK, `{"status":"success","data":{"resultType":"matrix","result":[]}}`
		})
		provider := newTestPrometheusProvider(t, server)

		_, err := provider.GetContainerMetrics(context.Background(), providerNamespace, providerPod, providerContainer, time.Hour)
		if err == nil {
			t.Fatal("expected an error for a missing series")
		}
		if !strings.Contains(err.Error(), "no data returned from Prometheus") {
			t.Errorf("unexpected error for a missing series: %v", err)
		}
	})

	t.Run("a series without samples is an error, not a zero recommendation", func(t *testing.T) {
		server := newPromTestServer(t, func(string) (int, string) {
			// Matrix result with an entry carrying no values.
			return http.StatusOK, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"__name__":"m"},"values":[]}]}}`
		})
		provider := newTestPrometheusProvider(t, server)

		_, err := provider.GetContainerMetrics(context.Background(), providerNamespace, providerPod, providerContainer, time.Hour)
		if err == nil {
			t.Fatal("expected an error for an empty series")
		}
		if !strings.Contains(err.Error(), "no samples in result") {
			t.Errorf("unexpected error for an empty series: %v", err)
		}
	})

	t.Run("a non-matrix result is rejected with the observed type", func(t *testing.T) {
		server := newPromTestServer(t, func(string) (int, string) {
			return http.StatusOK, `{"status":"success","data":{"resultType":"vector","result":[]}}`
		})
		provider := newTestPrometheusProvider(t, server)

		_, err := provider.GetContainerMetrics(context.Background(), providerNamespace, providerPod, providerContainer, time.Hour)
		if err == nil {
			t.Fatal("expected an error for a vector result")
		}
		if !strings.Contains(err.Error(), "unexpected result type") {
			t.Errorf("unexpected error for a vector result: %v", err)
		}
	})
}

// TestPrometheusProviderMalformedResponses proves malformed bodies and
// Prometheus-level errors surface as provider errors (VAL-FAST-007).
func TestPrometheusProviderMalformedResponses(t *testing.T) {
	t.Run("invalid JSON is an error", func(t *testing.T) {
		server := newPromTestServer(t, func(string) (int, string) {
			return http.StatusOK, "this is not json"
		})
		provider := newTestPrometheusProvider(t, server)

		_, err := provider.GetContainerMetrics(context.Background(), providerNamespace, providerPod, providerContainer, time.Hour)
		if err == nil {
			t.Fatal("expected an error for a malformed response")
		}
	})

	t.Run("a Prometheus error response is surfaced to the caller", func(t *testing.T) {
		server := newPromTestServer(t, func(string) (int, string) {
			return http.StatusBadRequest, `{"status":"error","errorType":"bad_data","error":"query is malformed"}`
		})
		provider := newTestPrometheusProvider(t, server)

		_, err := provider.GetContainerMetrics(context.Background(), providerNamespace, providerPod, providerContainer, time.Hour)
		if err == nil {
			t.Fatal("expected an error for a Prometheus error response")
		}
		if !strings.Contains(err.Error(), "query is malformed") {
			t.Errorf("error does not carry the Prometheus cause: %v", err)
		}
	})
}

// TestPrometheusProviderUnavailableThenRecovers proves an unavailable backend
// yields an error (never a fabricated recommendation) and the same provider
// resumes normal exact processing once valid samples return (VAL-FAST-007).
func TestPrometheusProviderUnavailableThenRecovers(t *testing.T) {
	server := newPromTestServer(t, func(query string) (int, string) {
		switch {
		case strings.Contains(query, cpuMetricSubstring):
			return promMatrixResponse(`[0,"0.2"],[1,"0.25"],[2,"0.3"],[3,"0.35"],[4,"0.4"]`)
		case strings.Contains(query, memMetricSubstring):
			return promMatrixResponse(`[0,"104857600"],[1,"209715200"],[2,"262144000"]`)
		default:
			return http.StatusOK, `{"status":"success","data":{"resultType":"matrix","result":[]}}`
		}
	})
	provider := newTestPrometheusProvider(t, server)

	// Backend outage: the CPU range query is answered with 503 and the
	// provider must surface the error without fabricating a recommendation
	// (only one failure is scripted because the memory query never runs
	// once the CPU query errors).
	server.failNextRequests(1)
	_, err := provider.GetContainerMetrics(context.Background(), providerNamespace, providerPod, providerContainer, time.Hour)
	if err == nil {
		t.Fatal("expected an error while the backend is unavailable")
	}

	// Recovery: valid samples return and the provider computes the same
	// exact percentiles as the healthy path.
	containerMetrics, err := provider.GetContainerMetrics(context.Background(), providerNamespace, providerPod, providerContainer, time.Hour)
	if err != nil {
		t.Fatalf("GetContainerMetrics did not recover after valid samples returned: %v", err)
	}
	if got := containerMetrics.CPU.P90.MilliValue(); got != 380 {
		t.Errorf("recovered CPU P90 = %d milli, want 380 milli", got)
	}
	if got := containerMetrics.Memory.P90.Value(); got != 251658240 {
		t.Errorf("recovered memory P90 = %d bytes, want 251658240", got)
	}
}

// TestPrometheusProviderHealthCheckAgainstBuildinfo proves the production
// health probe distinguishes a healthy backend from an unreachable one.
func TestPrometheusProviderHealthCheckAgainstBuildinfo(t *testing.T) {
	buildInfoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/status/buildinfo" {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","data":{"version":"2.53.0"}}`))
	}))
	defer buildInfoServer.Close()

	healthy, err := NewPrometheusProvider(PrometheusConfig{URL: buildInfoServer.URL})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	if err := healthy.HealthCheck(context.Background()); err != nil {
		t.Errorf("health check against a healthy backend failed: %v", err)
	}

	// A closed port is an unavailable backend and must be reported as such.
	unreachable, err := NewPrometheusProvider(PrometheusConfig{URL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	if err := unreachable.HealthCheck(context.Background()); err == nil {
		t.Error("health check against an unreachable backend must fail")
	}
}

// TestFormatDuration pins the window formatting used inside the PromQL range
// selector for seconds, minutes, hours and days.
func TestFormatDuration(t *testing.T) {
	tests := []struct {
		window time.Duration
		want   string
	}{
		{45 * time.Second, "45s"},
		{90 * time.Second, "1m"},
		{2 * time.Hour, "2h"},
		{25 * time.Hour, "1d"},
	}
	for _, tt := range tests {
		if got := formatDuration(tt.window); got != tt.want {
			t.Errorf("formatDuration(%s) = %q, want %q", tt.window, got, tt.want)
		}
	}
}
