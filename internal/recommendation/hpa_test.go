package recommendation

import (
	"errors"
	"testing"
	"time"

	"github.com/optipod/optipod/internal/metrics"
)

func TestReplayCPUDelayAndCost(t *testing.T) {
	// Same histogram, different arrival order. Consecutive bursts need more
	// reserved CPU than bursts separated by enough time to drain the backlog.
	for _, tc := range []struct {
		demand []int64
		budget time.Duration
		want   int64
	}{{[]int64{200, 0, 200, 0}, 30 * time.Second, 100}, {[]int64{200, 200, 0, 0}, 30 * time.Second, 134}, {[]int64{200, 0, 200, 0}, 0, 200}} {
		h := &metrics.HPAHistory{Step: 30 * time.Second, DelayBudget: tc.budget, Counts: map[int64]metrics.ReplicaCounts{}}
		var points []metrics.Point
		for i, v := range tc.demand {
			at := int64(60 + i*30)
			h.Counts[at] = metrics.ReplicaCounts{Current: 2, Serving: 2}
			points = append(points, metrics.Point{Time: time.Unix(at, 0), Value: v})
		}
		r, got, err := replayCPU(points, h)
		if err != nil || got != tc.want {
			t.Fatalf("candidate %d, want %d, err %v", got, tc.want, err)
		}
		stats := r.evaluate(got)
		if stats.maxDelay > tc.budget.Seconds() || stats.averageReserved != float64(got*2) {
			t.Fatalf("delay %v, cost %v", stats.maxDelay, stats.averageReserved)
		}
		delete(h.Counts, 150)
		if _, _, err := replayCPU(points, h); !errors.Is(err, metrics.ErrNoData) {
			t.Fatalf("stale HPA %v", err)
		}
	}
}

func TestCPUReplayGapAndReplicaChange(t *testing.T) {
	r := cpuReplay{step: 30 * time.Second, intervals: []cpuInterval{
		{at: 0, workRate: 200, replicas: metrics.ReplicaCounts{Current: 1, Serving: 1}},
		{at: 60, workRate: 400, replicas: metrics.ReplicaCounts{Current: 2, Serving: 2}},
	}}
	stats := r.evaluate(100)
	if stats.maxDelay != 30 || stats.averageReserved != 150 {
		t.Fatalf("gap replay %v %v", stats.maxDelay, stats.averageReserved)
	}
	r.intervals[1].at = 30
	stats = r.evaluate(100)
	if stats.maxDelay != 45 {
		t.Fatalf("replica transition delay %v", stats.maxDelay)
	}
}

func TestReplayCannotCreditUnreachableCPUScaleOut(t *testing.T) {
	h := &metrics.HPAHistory{Step: 30 * time.Second, DelayBudget: 0, Counts: map[int64]metrics.ReplicaCounts{
		60: {Current: 1, Serving: 1}, 90: {Current: 3, Serving: 3},
	}, ScaleOutTargetMilli: 150}
	points := []metrics.Point{{Time: time.Unix(60, 0), Value: 10}, {Time: time.Unix(90, 0), Value: 100}}
	r, got, err := replayCPU(points, h)
	if err != nil || got != 151 {
		t.Fatalf("request %d, error %v", got, err)
	}
	if r.evaluate(150).maxDelay != 30 || r.evaluate(151).maxDelay != 0 {
		t.Fatal("CPU scale-out boundary not reflected in delay")
	}
}
