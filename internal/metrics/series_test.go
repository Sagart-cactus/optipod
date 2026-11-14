package metrics

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestPrometheusMergesDuplicateScrapesConservatively(t *testing.T) {
	s := newPromTestServer(t, func(string) (int, string) {
		return http.StatusOK, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"job":"a"},"values":[[1,"0.1"],[2,"0.2"]]},{"metric":{"job":"b"},"values":[[1,"0.9"],[2,"0.1"],[3,"0.5"]]}]}}`
	})
	p := newTestPrometheusProvider(t, s)
	points, err := p.GetResourceSamples(context.Background(), "ns", "pod", "c", 7*24*time.Hour, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 3 || points[0].Value != 900 || points[1].Value != 200 || points[2].Value != 500 {
		t.Fatalf("incorrect merged samples: %+v", points)
	}
	q := s.queries()[0]
	if q.Step < time.Minute || 7*24*time.Hour/q.Step > 10000 {
		t.Fatalf("unsafe seven-day resolution: %s", q.Step)
	}
}

func TestDistinctReplicaSamplingAndDuplicateTimestamps(t *testing.T) {
	s := newTimeSeriesStore(10)
	a := TargetKey{Namespace: "n", WorkloadKind: "Deployment", WorkloadName: "w", Container: "c", PodName: "a"}
	b := a
	b.PodName = "b"
	s.registerTarget(a, "a")
	s.registerTarget(b, "b")
	now := time.Now()
	s.appendSample(a, Sample{PodName: "a", Timestamp: now, CPUMilli: 100})
	s.appendSample(a, Sample{PodName: "a", Timestamp: now, CPUMilli: 100})
	s.appendSample(b, Sample{PodName: "b", Timestamp: now, CPUMilli: 900})
	if len(s.listTargets()) != 2 || len(s.samplesSince(a, now.Add(-time.Second))) != 1 {
		t.Fatal("replica lost or duplicate scrape inflated coverage")
	}
	if s.samplesSince(b, now.Add(-time.Second))[0].CPUMilli != 900 {
		t.Fatal("replica samples mixed")
	}
}
