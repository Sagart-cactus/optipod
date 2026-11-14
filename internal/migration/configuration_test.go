package migration

import (
	"os"
	"strings"
	"testing"
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
)

func TestMigrationRejectsSilentSemanticLoss(t *testing.T) {
	data, err := os.ReadFile("../../examples/migration/qa-stability-1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{Name: "test", Namespace: "test", StartupMode: "ignore", CoveragePercent: 80, MinimumHistory: time.Hour}
	result, err := Translate(data, opts)
	if err != nil {
		t.Fatal(err)
	}
	if result.Policy.Spec.Mode != api.ModeRecommend || result.Policy.Spec.MetricsConfig.MinCoveragePercent != 80 {
		t.Fatal("migration must be Recommend with independently supplied coverage")
	}
	if result.Policy.Spec.MetricsConfig.CPU.Percentile != "P99" || result.Policy.LimitMode(true) != "None" {
		t.Fatal("lost explicit settings")
	}
	for _, bad := range []string{string(data) + "horizontal: {optimization: on}\n", strings.Replace(string(data), "target: p99", "target: p81", 1), strings.Replace(string(data), "type: noLimit", "type: automatic", 1)} {
		if _, err := Translate([]byte(bad), opts); err == nil {
			t.Fatal("silently accepted unsupported behavior")
		}
	}
	opts.StartupMode = ""
	if _, err := Translate(data, opts); err == nil {
		t.Fatal("ambiguous startup mode accepted")
	}
}
