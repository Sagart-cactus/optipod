package controller

import (
	"testing"
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
)

func TestAdmissionRefreshSchedule(t *testing.T) {
	ssa := string(api.StrategySSA)
	webhook := string(api.StrategyWebhook)
	for _, tc := range []struct {
		name                     string
		mode                     api.PolicyMode
		strategy                 *string
		ttl, age, interval, want time.Duration
		admission                bool
	}{
		{"default webhook", api.ModeAuto, nil, 0, 0, time.Hour, 150 * time.Second, true},
		{"short plan", api.ModeAuto, &webhook, 24 * time.Second, time.Minute, time.Hour, 12 * time.Second, true},
		{"short sample lifetime", api.ModeAuto, &webhook, time.Hour, time.Minute, time.Hour, 30 * time.Second, true},
		{"short configured interval", api.ModeAuto, &webhook, time.Minute, time.Minute, time.Second, time.Second, true},
		{"default plan lifetime", api.ModeAuto, &webhook, 0, time.Hour, time.Hour, 450 * time.Second, true},
		{"recommendation only", api.ModeRecommend, &webhook, time.Second, time.Minute, time.Hour, 30 * time.Second, false},
		{"server side apply", api.ModeAuto, &ssa, time.Second, time.Minute, time.Hour, 30 * time.Second, false},
		{"disabled", api.ModeDisabled, nil, time.Second, time.Minute, time.Hour, time.Hour, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &api.OptimizationPolicy{}
			p.Spec.Mode = tc.mode
			p.Spec.UpdateStrategy.Strategy = tc.strategy
			p.Spec.UpdateStrategy.RecommendationTTL.Duration = tc.ttl
			p.Spec.MetricsConfig.MaxSampleAge.Duration = tc.age
			if got := needsAdmissionRefresh(p); got != tc.admission {
				t.Errorf("admission worker = %v, want %v", got, tc.admission)
			}
			if got := metricsRefreshInterval(p, tc.interval); got != tc.want {
				t.Errorf("refresh interval = %s, want %s", got, tc.want)
			}
		})
	}
}
