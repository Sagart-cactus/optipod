package v1alpha1

import (
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PredictiveCPUConfig enables a bounded upward CPU trend forecast. It never
// lowers the percentile recommendation and does not predict seasonal cycles.
type PredictiveCPUConfig struct {
	// Defaults to 5m. Maximum 1h, and no longer than the training window.
	// +optional
	Horizon metav1.Duration `json:"horizon,omitempty"`
	// Defaults to 1h; uses only observations within this part of the CPU window.
	// +optional
	TrainingWindow metav1.Duration `json:"trainingWindow,omitempty"`
	// Defaults to 15m. Shorter observed history uses the regular percentile.
	// +optional
	MinimumHistory metav1.Duration `json:"minimumHistory,omitempty"`
	// Defaults to 12 distinct time buckets, after taking the maximum across replicas.
	// +kubebuilder:validation:Minimum=3
	// +kubebuilder:validation:Maximum=1000
	// +optional
	MinSamples *int32 `json:"minSamples,omitempty"`
	// Maximum forecast increase over the ordinary percentile plus margin; defaults to 50.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	// +optional
	MaxIncreasePercent *int32 `json:"maxIncreasePercent,omitempty"`
}

func (p *OptimizationPolicy) PredictiveSettings() *PredictiveCPUConfig {
	if p.Spec.MetricsConfig.PredictiveCPU == nil {
		return nil
	}
	n, cap := int32(12), int32(50)
	r := &PredictiveCPUConfig{Horizon: metav1.Duration{Duration: 5 * time.Minute}, TrainingWindow: metav1.Duration{Duration: time.Hour}, MinimumHistory: metav1.Duration{Duration: 15 * time.Minute}, MinSamples: &n, MaxIncreasePercent: &cap}
	s := p.Spec.MetricsConfig.PredictiveCPU
	if s.Horizon.Duration != 0 {
		r.Horizon = s.Horizon
	}
	if s.TrainingWindow.Duration != 0 {
		r.TrainingWindow = s.TrainingWindow
	}
	if s.MinimumHistory.Duration != 0 {
		r.MinimumHistory = s.MinimumHistory
	}
	if s.MinSamples != nil {
		r.MinSamples = s.MinSamples
	}
	if s.MaxIncreasePercent != nil {
		r.MaxIncreasePercent = s.MaxIncreasePercent
	}
	return r
}
func (p *OptimizationPolicy) validatePredictive() error {
	s := p.PredictiveSettings()
	if s == nil {
		return nil
	}
	if s.Horizon.Duration < time.Second || s.Horizon.Duration > time.Hour || s.TrainingWindow.Duration < 10*time.Second || s.TrainingWindow.Duration > 30*24*time.Hour || s.MinimumHistory.Duration < 10*time.Second || s.MinimumHistory.Duration > s.TrainingWindow.Duration || s.Horizon.Duration > s.TrainingWindow.Duration || s.TrainingWindow.Duration > p.Spec.MetricsConfig.ResourceSettings(true).RollingWindow.Duration {
		return fmt.Errorf("invalid predictiveCPU durations: horizon/history must fit training and CPU windows")
	}
	if *s.MinSamples < 3 || *s.MinSamples > 1000 || *s.MaxIncreasePercent < 1 || *s.MaxIncreasePercent > 100 {
		return fmt.Errorf("invalid predictiveCPU samples or increase cap")
	}
	return nil
}
