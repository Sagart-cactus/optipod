package framework

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Fixture behavior modes, mirroring test/e2e/metricsfixture.
const (
	// FixtureModePresent serves the configured matrix samples.
	FixtureModePresent = "present"
	// FixtureModeMissing serves a successful empty matrix result.
	FixtureModeMissing = "missing"
	// FixtureModeInvalid serves a well-formed envelope with the wrong result type.
	FixtureModeInvalid = "invalid"
	// FixtureModeUnavailable serves an HTTP 500 error envelope.
	FixtureModeUnavailable = "unavailable"
)

// FixtureSeries configures deterministic CPU (cores) and memory (bytes) matrix
// samples for one namespace/pod/container target of the in-cluster fixture.
// The JSON tags are the fixture server's wire format.
type FixtureSeries struct {
	Namespace   string    `json:"namespace"`
	Pod         string    `json:"pod"`
	Container   string    `json:"container"`
	Mode        string    `json:"mode,omitempty"` // empty means FixtureModePresent
	CPUCores    []float64 `json:"cpuCores,omitempty"`
	MemoryBytes []float64 `json:"memoryBytes,omitempty"`
}

// RecordedQuery is one query_range request the fixture observed, proving what
// the deployed controller actually requested.
type RecordedQuery struct {
	Time  time.Time `json:"time"`
	Query string    `json:"query"`
}

// MetricsFixtureClient drives the in-cluster metrics fixture through the
// private API server's service-proxy subresource, so no host port-forward or
// fixed host port is ever needed.
type MetricsFixtureClient struct {
	kube        kubernetes.Interface
	namespace   string
	serviceName string
	port        int
}

const (
	// fixtureClientQPS/fixtureClientBurst give the fixture-proxy path its own
	// client-go token bucket, deliberately raised above client-go's default
	// (rest.DefaultQPS/rest.DefaultBurst, 5/10). The suite's shared clientset
	// keeps the default limiter; a transient proxy-read failure can make
	// client-go's internal request retry loop burn through a shared bucket
	// and starve unrelated waits (the M3 VAL-CONF-005 incident: "client rate
	// limiter Wait returned an error" during a fixture trace read). A
	// dedicated, larger bucket makes that starvation structurally impossible
	// on this path.
	fixtureClientQPS   = 20
	fixtureClientBurst = 40
)

// NewMetricsFixtureClient targets the fixture installed by the harness. It
// builds a dedicated clientset from the private rest.Config with its own
// raised rate limiter (see fixtureClientQPS), so Trace/SetSeries/ClearTrace
// and the scrape-through endpoint never share the suite-wide CoreV1 token
// bucket.
func NewMetricsFixtureClient(clients *Clients) (*MetricsFixtureClient, error) {
	if clients == nil || clients.RESTConfig() == nil {
		return nil, fmt.Errorf("metrics fixture client: typed clients from the private kubeconfig are required")
	}
	cfg := rest.CopyConfig(clients.RESTConfig())
	cfg.QPS = fixtureClientQPS
	cfg.Burst = fixtureClientBurst
	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build the dedicated fixture-proxy clientset: %w", err)
	}
	return &MetricsFixtureClient{
		kube:        kube,
		namespace:   FixtureNamespace,
		serviceName: FixtureServiceName,
		port:        FixturePort,
	}, nil
}

// URL is the in-cluster service URL the deployed controller is configured with.
func (m *MetricsFixtureClient) URL() string { return FixtureServiceURL() }

// SetSeries atomically replaces the fixture's entire series set. Any error —
// including the fixture's own error envelope — propagates to the caller.
func (m *MetricsFixtureClient) SetSeries(ctx context.Context, series []FixtureSeries) error {
	payload := struct {
		Series []FixtureSeries `json:"series"`
	}{Series: series}
	if payload.Series == nil {
		payload.Series = []FixtureSeries{}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode fixture series: %w", err)
	}
	return m.kube.CoreV1().RESTClient().Post().
		Namespace(m.namespace).
		Resource("services").
		Name(fmt.Sprintf("%s:%d", m.serviceName, m.port)).
		SubResource("proxy").
		Suffix("-", "series").
		SetHeader("Content-Type", "application/json").
		Body(bytes.NewReader(body)).
		Do(ctx).Error()
}

// Trace returns the query_range requests the fixture has recorded, oldest first.
func (m *MetricsFixtureClient) Trace(ctx context.Context) ([]RecordedQuery, error) {
	raw, err := m.kube.CoreV1().RESTClient().Get().
		Namespace(m.namespace).
		Resource("services").
		Name(fmt.Sprintf("%s:%d", m.serviceName, m.port)).
		SubResource("proxy").
		Suffix("-", "trace").
		DoRaw(ctx)
	if err != nil {
		return nil, fmt.Errorf("read fixture trace: %w", err)
	}
	var reply struct {
		Queries []RecordedQuery `json:"queries"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return nil, fmt.Errorf("decode fixture trace: %w", err)
	}
	return reply.Queries, nil
}

// TraceWithRetry reads the query trace with a small, explicit retry budget
// for transient transport failures on the API-server service-proxy hop. It
// returns the trace, the number of attempts used, and — when the budget is
// exhausted — the LAST real error, so a caller can surface an honest "trace
// read failed after N attempts" failure instead of re-polling for minutes and
// losing the cause. attempts below 1 is treated as 1 (a single read).
func (m *MetricsFixtureClient) TraceWithRetry(ctx context.Context, attempts int, backoff time.Duration) ([]RecordedQuery, int, error) {
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		trace, err := m.Trace(ctx)
		if err == nil {
			return trace, attempt, nil
		}
		lastErr = err
		if attempt == attempts {
			return nil, attempt, lastErr
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, attempt, fmt.Errorf("context ended during fixture trace read backoff after attempt %d (last error: %v): %w",
				attempt, lastErr, ctx.Err())
		case <-timer.C:
		}
	}
	return nil, attempts, lastErr
}

// ScrapeControllerMetrics returns the deployed controller's Prometheus
// exposition, scraped in-cluster by the fixture through the shipped metrics
// access path (HTTPS against the shipped metrics service with the fixture's
// service-account token bound to the shipped metrics-reader ClusterRole — the
// shipped ServiceMonitor's shape). Any fixture-side failure, including an
// authentication/authorization rejection relayed from the controller's
// metrics endpoint, propagates to the caller.
func (m *MetricsFixtureClient) ScrapeControllerMetrics(ctx context.Context) (string, error) {
	raw, err := m.kube.CoreV1().RESTClient().Get().
		Namespace(m.namespace).
		Resource("services").
		Name(fmt.Sprintf("%s:%d", m.serviceName, m.port)).
		SubResource("proxy").
		Suffix("-", "scrape-metrics").
		DoRaw(ctx)
	if err != nil {
		return "", fmt.Errorf("scrape controller metrics through the in-cluster fixture (%s): %w",
			ControllerMetricsURL(), err)
	}
	return string(raw), nil
}

// ClearTrace resets the fixture's recorded queries so per-spec traces are
// attributable to the spec that caused them.
func (m *MetricsFixtureClient) ClearTrace(ctx context.Context) error {
	return m.kube.CoreV1().RESTClient().Delete().
		Namespace(m.namespace).
		Resource("services").
		Name(fmt.Sprintf("%s:%d", m.serviceName, m.port)).
		SubResource("proxy").
		Suffix("-", "trace").
		Do(ctx).Error()
}
