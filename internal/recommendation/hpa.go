package recommendation

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/optipod/optipod/internal/metrics"
)

type cpuInterval struct {
	at       int64
	workRate float64 // aggregate observed mCPU
	replicas metrics.ReplicaCounts
}

type cpuReplay struct {
	intervals      []cpuInterval
	step           time.Duration
	budget         time.Duration
	eligible       int
	scaleOutTarget int64
}

// Model work arriving uniformly within each observed interval. A change in
// replica count redistributes pending CPU work. Gaps start independent runs.
type candidateStats struct {
	maxDelay, averageReserved, spareWorkRate float64
}

func (r cpuReplay) evaluate(request int64) candidateStats {
	var stats candidateStats
	var backlog float64 // millicore-seconds
	var fleet int32
	seconds := r.step.Seconds()
	for i, v := range r.intervals {
		newSegment := i == 0 || v.at-r.intervals[i-1].at != int64(r.step/time.Second)
		if newSegment {
			backlog = 0
		}
		// Under contention a candidate at or below a sole absolute CPU target
		// cannot trigger a CPU-driven scale-out. Honor observed scale-downs.
		if newSegment || r.scaleOutTarget == 0 || request > r.scaleOutTarget {
			fleet = v.replicas.Current
		} else {
			fleet = min(fleet, v.replicas.Current)
		}
		capacity := float64(min(fleet, v.replicas.Serving)) * float64(request)
		spare := capacity - v.workRate
		backlog = max(0, backlog-spare*seconds)
		stats.spareWorkRate += spare
		if backlog > 0 {
			stats.maxDelay = max(stats.maxDelay, backlog/capacity)
		}
		stats.averageReserved += float64(fleet) * float64(request)
	}
	stats.averageReserved /= float64(len(r.intervals))
	return stats
}

func replayCPU(cpu []metrics.Point, history *metrics.HPAHistory) (cpuReplay, int64, error) {
	r := cpuReplay{step: history.Step, budget: history.DelayBudget, scaleOutTarget: history.ScaleOutTargetMilli}
	if r.step < time.Second || r.step%time.Second != 0 || r.budget < 0 {
		return r, 0, fmt.Errorf("invalid HPA replay interval or delay budget")
	}
	type interval struct {
		sum   float64
		count int
	}
	buckets := map[int64]interval{}
	var latest int64
	for _, point := range cpu {
		if point.Time.Before(history.First) {
			continue
		}
		t := point.Time.Truncate(history.Step).Unix()
		b := buckets[t]
		b.sum += float64(point.Value)
		b.count++
		buckets[t] = b
		latest = max(latest, t)
	}
	r.eligible = len(buckets)
	var peak float64
	for t, b := range buckets {
		c, ok := history.Counts[t]
		if !ok {
			continue
		}
		rate := b.sum / float64(b.count) * float64(c.Current)
		r.intervals = append(r.intervals, cpuInterval{at: t, workRate: rate, replicas: c})
		peak = max(peak, rate)
	}
	n := len(r.intervals)
	if n < 2 || n*2 < r.eligible || history.Counts[latest].Current == 0 {
		return r, 0, fmt.Errorf("insufficient overlapping HPA and CPU history (%d of %d intervals): %w", n, r.eligible, metrics.ErrNoData)
	}
	sort.Slice(r.intervals, func(i, j int) bool { return r.intervals[i].at < r.intervals[j].at })
	if peak >= float64(math.MaxInt64) {
		return r, 0, fmt.Errorf("HPA replay exceeds CPU quantity range")
	}
	// At higher requests, each modeled fleet can only stay the same size or
	// grow. Cost and capacity are monotonic, so bisection finds the cheapest
	// feasible integer candidate. The upper bound can serve peak work alone.
	low, high := int64(1), max(int64(1), int64(math.Ceil(peak)))
	for low < high {
		mid := low + (high-low)/2
		stats := r.evaluate(mid)
		if stats.maxDelay <= r.budget.Seconds() && stats.spareWorkRate >= 0 {
			high = mid
		} else {
			low = mid + 1
		}
	}
	return r, low, nil
}
