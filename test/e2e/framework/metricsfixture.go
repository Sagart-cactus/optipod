package framework

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"k8s.io/client-go/kubernetes"
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

// NewMetricsFixtureClient targets the fixture installed by the harness.
func NewMetricsFixtureClient(clients *Clients) *MetricsFixtureClient {
	return &MetricsFixtureClient{
		kube:        clients.Kube,
		namespace:   FixtureNamespace,
		serviceName: FixtureServiceName,
		port:        FixturePort,
	}
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
