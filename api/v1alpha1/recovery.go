package v1alpha1

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// OOMRecoveryConfig responds to observed OOMKilled terminations, even when the
// metrics backend is unavailable. It never overrides resource bounds or limits
// configured as Preserve. A reset token acknowledges a latched circuit breaker.
type OOMRecoveryConfig struct {
	// Enabled defaults to true; false disables emergency increases.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
	// BumpPercent defaults to 20.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	// +optional
	BumpPercent *int32 `json:"bumpPercent,omitempty"`
	// MinimumIncrease defaults to 64Mi.
	// +optional
	MinimumIncrease *resource.Quantity `json:"minimumIncrease,omitempty"`
	// Window defaults to 1h for counting repeated OOM incidents.
	// +optional
	Window metav1.Duration `json:"window,omitempty"`
	// FreezeDuration defaults to 24h; memory reductions are blocked throughout.
	// +optional
	FreezeDuration metav1.Duration `json:"freezeDuration,omitempty"`
	// MaxRecoveries defaults to 3 per window, then requires a reset token.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=20
	// +optional
	MaxRecoveries *int32 `json:"maxRecoveries,omitempty"`
	// +kubebuilder:validation:MaxLength=64
	// +optional
	ResetToken string `json:"resetToken,omitempty"`
}

func (p *OptimizationPolicy) RecoverySettings() OOMRecoveryConfig {
	enabled := true
	percent, max := int32(20), int32(3)
	minimum := resource.MustParse("64Mi")
	r := OOMRecoveryConfig{Enabled: &enabled, BumpPercent: &percent, MaxRecoveries: &max, MinimumIncrease: &minimum, Window: metav1.Duration{Duration: time.Hour}, FreezeDuration: metav1.Duration{Duration: 24 * time.Hour}}
	if v := p.Spec.UpdateStrategy.OOMRecovery; v != nil {
		if v.Enabled != nil {
			r.Enabled = v.Enabled
		}
		if v.BumpPercent != nil {
			r.BumpPercent = v.BumpPercent
		}
		if v.MaxRecoveries != nil {
			r.MaxRecoveries = v.MaxRecoveries
		}
		if v.MinimumIncrease != nil {
			r.MinimumIncrease = v.MinimumIncrease
		}
		if v.Window.Duration != 0 {
			r.Window = v.Window
		}
		if v.FreezeDuration.Duration != 0 {
			r.FreezeDuration = v.FreezeDuration
		}
		r.ResetToken = v.ResetToken
	}
	return r
}

func (p *OptimizationPolicy) validateRecovery() error {
	r := p.RecoverySettings()
	if *r.BumpPercent < 1 || *r.BumpPercent > 100 || *r.MaxRecoveries < 1 || *r.MaxRecoveries > 20 {
		return fmt.Errorf("invalid oomRecovery bumpPercent or maxRecoveries")
	}
	if r.MinimumIncrease.Sign() < 0 || r.Window.Duration < time.Second || r.Window.Duration > 30*24*time.Hour || r.FreezeDuration.Duration < time.Second || r.FreezeDuration.Duration > 30*24*time.Hour {
		return fmt.Errorf("invalid oomRecovery increase or duration")
	}
	return nil
}
