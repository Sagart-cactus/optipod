package safety

import (
	"fmt"
	"math"
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type RecoveryState struct {
	Seen        map[string]time.Time `json:"seen,omitempty"`
	Count       int32                `json:"count"`
	WindowStart time.Time            `json:"windowStart"`
	FreezeUntil time.Time            `json:"freezeUntil"`
	Floor       resource.Quantity    `json:"floor"`
	CircuitOpen bool                 `json:"circuitOpen,omitempty"`
	Reason      string               `json:"reason,omitempty"`
	ResetToken  string               `json:"resetToken,omitempty"`
}

// ObserveOOM persists evidence before any application, so retries and operator
// restarts cannot count the same termination twice or repeatedly raise memory.
func (s *State) ObserveOOM(p *api.OptimizationPolicy, now time.Time) {
	cfg := p.RecoverySettings()
	if !*cfg.Enabled || !p.ControlsResource("memory") {
		return
	}
	for name, state := range s.Containers {
		if p.ExcludesContainer(name) {
			continue
		}
		if state.Recovery == nil {
			state.Recovery = &RecoveryState{Seen: map[string]time.Time{}}
		}
		r := state.Recovery
		if r.Seen == nil {
			r.Seen = map[string]time.Time{}
		}
		if r.ResetToken != cfg.ResetToken {
			if r.CircuitOpen {
				r.Seen = map[string]time.Time{}
			}
			r.CircuitOpen = false
			r.Reason = ""
			r.Count = 0
			r.WindowStart = now
			r.ResetToken = cfg.ResetToken
		}
		if now.Sub(r.WindowStart) > cfg.Window.Duration {
			r.Count = 0
			r.WindowStart = now
		}
		for id, t := range r.Seen {
			if now.Sub(t) > cfg.Window.Duration {
				delete(r.Seen, id)
			}
		}
		newIncident := false
		for _, pod := range s.Pods {
			for _, status := range append(append([]corev1.ContainerStatus{}, pod.Status.ContainerStatuses...), pod.Status.InitContainerStatuses...) {
				if status.Name != name {
					continue
				}
				term := status.LastTerminationState.Terminated
				if status.State.Terminated != nil {
					term = status.State.Terminated
				}
				if term == nil || term.Reason != "OOMKilled" || term.FinishedAt.IsZero() || now.Sub(term.FinishedAt.Time) > cfg.Window.Duration {
					continue
				}
				id := fmt.Sprintf("%s/%s/%d", pod.UID, term.ContainerID, term.FinishedAt.UnixNano())
				if _, ok := r.Seen[id]; ok {
					continue
				}
				r.Seen[id] = term.FinishedAt.Time
				newIncident = true
			}
		}
		if !newIncident {
			continue
		}
		r.Count++
		r.FreezeUntil = now.Add(cfg.FreezeDuration.Duration)
		if r.Count > *cfg.MaxRecoveries {
			r.CircuitOpen = true
			r.Reason = "repeated OOM incidents exceeded maxRecoveries"
			continue
		}
		if r.CircuitOpen {
			continue
		}
		current := s.Current[name]
		base := current.Requests.Memory().Value()
		if current.Limits.Memory().Value() > base {
			base = current.Limits.Memory().Value()
		}
		if r.Floor.Value() > base {
			base = r.Floor.Value()
		}
		next := int64(math.Ceil(float64(base) * (100 + float64(*cfg.BumpPercent)) / 100))
		if base+cfg.MinimumIncrease.Value() > next {
			next = base + cfg.MinimumIncrease.Value()
		}
		if next > p.Spec.ResourceBounds.Memory.Max.Value() {
			r.CircuitOpen = true
			r.Reason = "OOM recovery would exceed memory maximum"
			continue
		}
		r.Floor = *resource.NewQuantity(next, resource.BinarySI)
	}
}

func (s *State) RecoveryError(p *api.OptimizationPolicy) error {
	if !*p.RecoverySettings().Enabled || !p.ControlsResource("memory") {
		return nil
	}
	for name, state := range s.Containers {
		if p.ExcludesContainer(name) || state.Recovery == nil {
			continue
		}
		r := state.Recovery
		if r.CircuitOpen {
			return fmt.Errorf("OOM circuit breaker for %s: %s; change oomRecovery.resetToken after investigation", name, r.Reason)
		}
		if time.Now().Before(r.FreezeUntil) && p.LimitMode(false) == "Preserve" {
			current := s.Current[name]
			limit := current.Limits.Memory()
			if limit.Sign() > 0 && r.Floor.Cmp(*limit) > 0 {
				return fmt.Errorf("OOM recovery for %s blocked by preserved memory limit; configure memoryMode Multiplier or None", name)
			}
		}
	}
	return nil
}

// EmergencyRecommendation allows a bounded memory increase despite lost metrics.
// CPU is held at its current allocation; no decrease is inferred from missing data.
func (s *State) EmergencyRecommendation(name string, p *api.OptimizationPolicy, now time.Time) *api.ContainerRecommendation {
	state := s.Containers[name]
	if state == nil || state.Recovery == nil || !now.Before(state.Recovery.FreezeUntil) || !p.ControlsResource("memory") || !*p.RecoverySettings().Enabled {
		return nil
	}
	current := s.Current[name]
	r := state.Recovery
	if r.Floor.Cmp(*current.Requests.Memory()) <= 0 {
		return nil
	}
	expires := metav1.NewTime(now.Add(time.Minute))
	rec := &api.ContainerRecommendation{Container: name, Memory: copyQ(&r.Floor), MinimumMemoryLimit: copyQ(&r.Floor), Emergency: true, ValidUntil: &expires, Explanation: "bounded recovery from observed OOMKilled termination"}
	if p.ControlsResource("cpu") {
		rec.CPU = copyQ(current.Requests.Cpu())
	}
	return rec
}

func (s *State) ApplyRecovery(recs []api.ContainerRecommendation, p *api.OptimizationPolicy, now time.Time) {
	if !*p.RecoverySettings().Enabled || !p.ControlsResource("memory") {
		return
	}
	for i := range recs {
		rec := &recs[i]
		state := s.Containers[rec.Container]
		if state == nil || state.Recovery == nil || !now.Before(state.Recovery.FreezeUntil) || rec.Memory == nil {
			continue
		}
		r := state.Recovery
		current := s.Current[rec.Container]
		floor := r.Floor
		if current.Requests.Memory().Cmp(floor) > 0 {
			floor = current.Requests.Memory().DeepCopy()
		}
		if rec.Memory.Cmp(floor) < 0 {
			rec.Memory = copyQ(&floor)
		}
		limitFloor := floor
		if current.Limits.Memory().Cmp(limitFloor) > 0 {
			limitFloor = current.Limits.Memory().DeepCopy()
		}
		rec.MinimumMemoryLimit = copyQ(&limitFloor)
		if r.Floor.Cmp(*current.Requests.Memory()) > 0 {
			rec.Emergency = true
			rec.Explanation += "; OOM recovery bypasses routine cooldown"
		} else {
			rec.Explanation += "; OOM memory decrease freeze"
		}
	}
}
