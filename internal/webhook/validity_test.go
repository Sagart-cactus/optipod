package webhook

import (
	"testing"
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRecommendationValidityRejectsExpiredAndReplacedPolicy(t *testing.T) {
	p := &api.OptimizationPolicy{ObjectMeta: metav1.ObjectMeta{UID: "original", Generation: 4}}
	now := time.Now()
	a := map[string]string{}
	StampRecommendations(a, p, now)
	if !ValidRecommendations(a, p, now) {
		t.Fatal("fresh plan rejected")
	}
	if ValidRecommendations(a, p, now.Add(16*time.Minute)) {
		t.Fatal("expired plan accepted")
	}
	p.Generation++
	if ValidRecommendations(a, p, now) {
		t.Fatal("old policy generation accepted")
	}
	p.Generation--
	p.UID = "replacement"
	if ValidRecommendations(a, p, now) {
		t.Fatal("recreated policy accepted old plan")
	}
	if ValidRecommendations(nil, p, now) {
		t.Fatal("unstamped plan accepted")
	}
}
