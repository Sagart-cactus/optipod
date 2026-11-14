package metrics

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
)

func TestCustomMetricScopeScalingAndInvalidSamples(t *testing.T) {
	value := "20"
	server := newPromTestServer(t, func(query string) (int, string) {
		if !strings.Contains(query, `namespace="ns"`) || !strings.Contains(query, `pod="pod"`) || !strings.Contains(query, `container="app"`) {
			t.Errorf("unscoped query: %s", query)
		}
		return 200, fmt.Sprintf(`{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1700000000,%q]]}]}}`, value)
	})
	prom, err := NewPrometheusProvider(PrometheusConfig{URL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	factor := .02
	wrapped, err := ConfigureSeries(prom, api.MetricsConfig{CPU: &api.ResourceMetricsConfig{Source: &api.MetricSource{Name: "queue_depth", Multiplier: &factor}}})
	if err != nil {
		t.Fatal(err)
	}
	points, err := wrapped.GetResourceSamples(context.Background(), "ns", "pod", "app", time.Hour, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 || points[0].Value != 400 {
		t.Fatalf("incorrect gauge scaling: %v", points)
	}
	for _, bad := range []string{"NaN", "+Inf", "-1", "1e100"} {
		value = bad
		if _, err := wrapped.GetResourceSamples(context.Background(), "ns", "pod", "app", time.Hour, true); err == nil {
			t.Fatalf("accepted unsafe value %s", bad)
		}
	}
	if _, err := metricSelector(&api.MetricSource{Name: "metric", Labels: map[string]string{"namespace": "foreign"}}, "ns", "pod", "app"); err == nil {
		t.Fatal("scope override allowed")
	}
	value = fmt.Sprint(time.Now().Add(time.Minute).Unix())
	if _, err := wrapped.(ObservationClock).SourceTimestamp(context.Background(), "ns", "pod", "app", time.Now(), true); err == nil {
		t.Fatal("future scrape timestamp accepted")
	}
}
