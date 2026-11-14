package recommendation

import (
	"fmt"
	"math"
	"sort"
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/metrics"
	"k8s.io/apimachinery/pkg/api/resource"
)

func predictiveCPU(points []metrics.Point, base resource.Quantity, p *api.OptimizationPolicy) (resource.Quantity, string) {
	cfg := p.PredictiveSettings()
	if cfg == nil || !p.ControlsResource("cpu") {
		return base, ""
	}
	if len(points) == 0 {
		return base, "predictive CPU warming up: no time series"
	}
	var latest time.Time
	for _, v := range points {
		if v.Time.After(latest) {
			latest = v.Time
		}
	}
	step := cfg.TrainingWindow.Duration / 120
	if step < time.Second {
		step = time.Second
	}
	// Equal time buckets avoid weighting a heavily scraped replica more strongly.
	// Maximum per bucket keeps the hottest replica represented during scaling.
	buckets := map[int64]int64{}
	for _, v := range points {
		if v.Value < 0 || v.Time.Before(latest.Add(-cfg.TrainingWindow.Duration)) {
			continue
		}
		key := v.Time.UnixNano() / int64(step)
		if old, ok := buckets[key]; !ok || v.Value > old {
			buckets[key] = v.Value
		}
	}
	keys := make([]int64, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	if len(keys) < int(*cfg.MinSamples) || time.Duration(keys[len(keys)-1]-keys[0])*step < cfg.MinimumHistory.Duration {
		return base, "predictive CPU warming up: insufficient distinct history"
	}
	origin := keys[0]
	var sx, sy float64
	for _, key := range keys {
		sx += float64(key-origin) * step.Seconds()
		sy += float64(buckets[key])
	}
	mx, my := sx/float64(len(keys)), sy/float64(len(keys))
	var covariance, variance float64
	for _, key := range keys {
		x := float64(key-origin)*step.Seconds() - mx
		y := float64(buckets[key]) - my
		covariance += x * y
		variance += x * x
	}
	if variance == 0 || covariance <= 0 {
		return base, "predictive CPU: no upward trend; percentile retained"
	}
	slope := covariance / variance
	// Forecast from the larger of fitted/latest usage, then add the configured margin.
	xlast := float64(keys[len(keys)-1]-origin) * step.Seconds()
	anchor := math.Max(float64(buckets[keys[len(keys)-1]]), my+slope*(xlast-mx))
	forecast := (anchor + slope*cfg.Horizon.Seconds()) * *p.Spec.MetricsConfig.ResourceSettings(true).SafetyFactor
	cap := float64(base.MilliValue()) * (1 + float64(*cfg.MaxIncreasePercent)/100)
	desired := math.Max(float64(base.MilliValue()), math.Min(forecast, cap))
	if math.IsNaN(desired) || math.IsInf(desired, 0) || desired >= float64(math.MaxInt64) {
		return base, "predictive CPU: invalid forecast; percentile retained"
	}
	q := *resource.NewMilliQuantity(int64(math.Ceil(desired)), resource.DecimalSI)
	q = clampToBounds(q, p.Spec.ResourceBounds.CPU)
	return q, fmt.Sprintf("predictive CPU upward trend %.2fm/s over %s, horizon %s, cap +%d%% above percentile", slope, time.Duration(keys[len(keys)-1]-origin)*step, cfg.Horizon.Duration, *cfg.MaxIncreasePercent)
}
