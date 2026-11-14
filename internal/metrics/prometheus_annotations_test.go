package metrics

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
)

func TestPrometheusQueryAnnotations(t *testing.T) {
	for _, test := range []struct {
		name        string
		annotations string
		wantError   bool
	}{
		{"infos", `"infos":["query evaluation info"]`, false},
		{"warnings", `"warnings":["partial series"]`, true},
		{"both", `"warnings":["partial series"],"infos":["query evaluation info"]`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			at := time.Now().Truncate(time.Second)
			server := newPromTestServer(t, func(query string) (int, string) {
				value := "0.25"
				if strings.HasPrefix(query, "timestamp(") {
					value = strconv.FormatInt(at.Unix()-1, 10)
				}
				_, body := promMatrixResponse(fmt.Sprintf(`[%d,%q]`, at.Unix(), value))
				return http.StatusOK, strings.Replace(body, `"data":`, test.annotations+`,"data":`, 1)
			})
			provider := newTestPrometheusProvider(t, server)
			custom, err := ConfigureSeries(provider, api.MetricsConfig{
				CPU: &api.ResourceMetricsConfig{Source: &api.MetricSource{Name: "custom_cpu"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, source := range []struct {
				name     string
				provider SeriesProvider
			}{
				{"standard", provider},
				{"custom", custom},
			} {
				t.Run(source.name, func(t *testing.T) {
					points, err := source.provider.GetResourceSamples(context.Background(), "ns", "pod", "app", time.Hour, true)
					if (err != nil) != test.wantError {
						t.Fatalf("sample query error = %v, want error = %t", err, test.wantError)
					}
					if !test.wantError && (len(points) != 1 || points[0].Value != 250) {
						t.Fatalf("informational annotation changed samples: %+v", points)
					}
					clock := source.provider.(ObservationClock)
					stamp, err := clock.SourceTimestamp(context.Background(), "ns", "pod", "app", at, true)
					if (err != nil) != test.wantError {
						t.Fatalf("timestamp query error = %v, want error = %t", err, test.wantError)
					}
					if !test.wantError && !stamp.Equal(at.Add(-time.Second)) {
						t.Fatalf("informational annotation changed timestamp: %s", stamp)
					}
				})
			}
		})
	}
}
