package recommendation

import (
	"strings"
	"testing"
	"time"

	"github.com/optipod/optipod/internal/metrics"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestCalibrateCPUTail(t *testing.T) {
	points := []metrics.Point{}
	factors := map[int64]float64{}
	for i, pair := range [][]int64{{10, 10}, {10}, {30, 50}, {100}} {
		stamp := time.Unix(int64(60+i*30), 0)
		for _, v := range pair {
			points = append(points, metrics.Point{Time: stamp, Value: v})
		}
		factors[stamp.Unix()] = 1
	}
	factors[120] = 2 // Pending scale-in doubles the projected per-replica demand to 80m.
	h := &metrics.HPAHistory{Step: 30 * time.Second, Factors: factors}
	for _, tc := range []struct {
		base int64
		q    string
		want int64
	}{{5, "P50", 90}, {5, "P80", 100}, {5, "P100", 100}, {110, "P50", 110}} {
		got, why, err := calibrateCPU(*resource.NewMilliQuantity(tc.base, resource.DecimalSI), tc.q, points, h)
		if err != nil || got.MilliValue() != tc.want || !strings.Contains(why, "4/4") {
			t.Fatalf("%+v got %s reason %q error %v", tc, got.String(), why, err)
		}
	}
	delete(factors, 150)
	if _, _, err := calibrateCPU(resource.MustParse("5m"), "P95", points, h); err == nil {
		t.Fatal("calibrated with stale HPA observations")
	}
}
