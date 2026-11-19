package recommendation

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/optipod/optipod/internal/metrics"
	"k8s.io/apimachinery/pkg/api/resource"
)

// Replay the observed HPA decisions on the container CPU timeline. Each
// evaluation interval has one vote, regardless of how many replicas existed.
func calibrateCPU(base resource.Quantity, percentile string, cpu []metrics.Point, history *metrics.HPAHistory) (resource.Quantity, string, error) {
	q, err := strconv.Atoi(strings.TrimPrefix(percentile, "P"))
	if err != nil || q < 1 || q > 100 || history.Step <= 0 {
		return base, "", fmt.Errorf("invalid HPA calibration percentile or interval")
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
	demand := make([]float64, 0, len(buckets))
	for t, b := range buckets {
		if f, ok := history.Factors[t]; ok {
			demand = append(demand, b.sum/float64(b.count)*f)
		}
	}
	// Sparse or stale HPA data can hide entire scaling episodes. Require a
	// coverage of at least half of the eligible CPU timeline plus the latest interval to be observable.
	if len(demand) < 2 || len(demand)*2 < len(buckets) || history.Factors[latest] == 0 {
		return base, "", fmt.Errorf("insufficient overlapping HPA and CPU history (%d of %d intervals)", len(demand), len(buckets))
	}
	sort.Float64s(demand)
	// Expected shortfall at the configured percentile. The fractional boundary
	// gives exactly the desired tail mass even for short, irregular histories.
	width := max(1.0, float64(len(demand))*float64(100-q)/100)
	var sum float64
	for i := 0; i < int(math.Ceil(width)); i++ {
		sum += demand[len(demand)-1-i] * min(1.0, width-float64(i))
	}
	value := math.Ceil(sum / width)
	if value >= float64(math.MaxInt64) {
		return base, "", fmt.Errorf("HPA calibration exceeds CPU quantity range")
	}
	calibrated := max(base.MilliValue(), int64(value))
	return *resource.NewMilliQuantity(calibrated, resource.DecimalSI), fmt.Sprintf("HPA CPU calibration: %dm base, %dm before headroom, %d/%d aligned intervals", base.MilliValue(), calibrated, len(demand), len(buckets)), nil
}
