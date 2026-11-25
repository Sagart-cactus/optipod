package metrics

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestMemoryPeakUsesAdjacentLifetimeBoundedBuckets(t *testing.T) {
	// The first eligible scrape is at 100.001s. The 90s bucket ends at
	// 190s. The remaining 20s are a partial bucket ending at 210s.
	widths := map[float64]int{190: 90000, 210: 20000}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		at, err := strconv.ParseFloat(r.FormValue("end"), 64)
		if err != nil {
			t.Error(err)
		}
		width := widths[at]
		want := fmt.Sprintf("max_over_time(container_memory_working_set_bytes{namespace=%q,pod=%q,container=%q}[%dms])", "ns", "p", "c", width)
		if width == 0 || r.FormValue("start") != r.FormValue("end") || r.FormValue("query") != want {
			t.Errorf("unexpected bucket at %v: %s", at, r.FormValue("query"))
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := fmt.Fprintf(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[%v,"42"]]}]}}`, at); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	provider, err := NewPrometheusProvider(PrometheusConfig{URL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	points, err := provider.MemoryPeakSamples(context.Background(), "ns", "p", "c",
		time.Unix(100, 100000), time.Unix(210, 900000), 7*24*time.Hour)
	if err != nil || len(points) != 2 || points[0].Time.UnixMilli() != 190000 || points[1].Time.UnixMilli() != 210000 {
		t.Fatalf("points = %v, err = %v", points, err)
	}
}
