package report

import (
	"testing"

	api "github.com/optipod/optipod/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

func TestReportPairsRawRequestsWithTheirEffectiveRecommendation(t *testing.T) {
	spec := corev1.PodSpec{Containers: []corev1.Container{{Name: "db"}, {Name: "sidecar"}}}
	annotations := map[string]string{
		api.RecommendationAnnotationKey("db", "memory-request"):     "8Gi",
		api.RecommendationAnnotationKey("db", "raw-memory-request"): "9Gi",
		api.RecommendationAnnotationKey("db", "cpu-request"):        "1",
	}
	row := Workload{}
	populateContainers(&row, &spec, annotations, api.ModeAuto)
	db := row.Containers[0].Recommended
	if db["memory-request"] != "8Gi" || db["raw-memory-request"] != "9Gi" || db["cpu-request"] != "1" ||
		db["raw-cpu-request"] != "" || len(row.Containers[1].Recommended) != 0 {
		t.Fatalf("unexpected container recommendations: %+v", row.Containers)
	}
	annotations["optipod.io/memory-request.db"] = "7Gi"
	row = Workload{}
	populateContainers(&row, &spec, annotations, api.ModeAuto)
	if rec := row.Containers[0].Recommended; rec["memory-request"] != "7Gi" || rec["raw-memory-request"] != "" {
		t.Fatalf("paired an old applied value with a new raw request: %v", rec)
	}
}
