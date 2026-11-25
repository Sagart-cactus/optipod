package webhook

import (
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/capturedlimits"
	"github.com/optipod/optipod/internal/coexist"
	"github.com/optipod/optipod/internal/discovery"
	corev1 "k8s.io/api/core/v1"
)

// Restore only controlled dimensions whose declared template limit still
// matches the capture. A deliberate template edit takes effect independently.
func restoreCapturedLimits(pod *corev1.Pod, template *corev1.PodTemplateSpec, recs []ResourceRecommendation, s *capturedlimits.Snapshot, p *api.OptimizationPolicy, restrictions *coexist.Restrictions) []ResourceRecommendation {
	declared := map[string]discovery.ContainerSlot{}
	for _, slot := range discovery.ContainerSlots(&template.Spec) {
		declared[slot.Container.Name] = slot
	}
	index := map[string]int{}
	for i, r := range recs {
		index[r.ContainerName] = i
	}
	for _, slot := range discovery.ContainerSlots(&pod.Spec) {
		name := slot.Container.Name
		c, ok := s.Containers[name]
		if !ok || slot.Field != c.Field || p.ExcludesContainer(name) || (slot.Field == "initContainers" && !p.IncludesNativeSidecars()) {
			continue
		}
		orig, present := declared[name]
		if present != (c.Template != nil) || (present && orig.Field != slot.Field) || (!present && !p.IncludesInjectedSidecars()) {
			continue
		}
		i, exists := index[name]
		r := ResourceRecommendation{ContainerName: name}
		if exists {
			r = recs[i]
		}
		applied := false
		for _, key := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory} {
			cpu := key == corev1.ResourceCPU
			if !p.ControlsResource(string(key)) {
				continue
			}
			hold := c.RunningRequests != nil && s.HoldRequestsUntil != nil && time.Now().Before(*s.HoldRequestsUntil) && p.GetRolloutStrategy() == api.RolloutOnNextRestart
			// A handoff may temporarily have no plan for the new policy UID.
			// Prefer each fresh dimension as soon as it becomes available.
			missing := (cpu && r.CPU == nil && !r.RemoveCPURequest) || (!cpu && r.Memory == nil && !r.RemoveMemoryRequest)
			fallback := missing && c.RunningRequests != nil && s.FallbackRequestsUntil != nil && time.Now().Before(*s.FallbackRequestsUntil)
			if (hold || fallback) && restrictions.Reason(name, string(key)) == "" && (!present || capturedlimits.Same(orig.Container.Resources.Requests, c.TemplateRequests, key)) {
				q, found := (*c.RunningRequests)[key]
				if cpu {
					r.CPU = nil
					r.RemoveCPURequest = !found
					if found {
						r.CPU = &q
					}
				} else {
					r.Memory = nil
					r.RemoveMemoryRequest = !found
					if found {
						r.Memory = &q
					}
				}
				applied = true
			}
			if restrictions.Reason(name, string(key)) != "" {
				q, captured := c.HPARequests[key]
				if !captured || p.GetRolloutStrategy() != api.RolloutOnNextRestart || (present && !capturedlimits.Same(orig.Container.Resources.Requests, c.TemplateRequests, key)) {
					continue
				}
				if cpu {
					r.CPU = &q
					r.RemoveCPURequest = false
				} else {
					r.Memory = &q
					r.RemoveMemoryRequest = false
				}
				applied = true
			}
			if p.LimitMode(cpu) != api.LimitPreserve {
				continue
			}
			if present && !capturedlimits.Same(orig.Container.Resources.Limits, c.Template, key) {
				continue
			}
			q, has := c.Limits[key]
			if cpu {
				r.CPULimit = nil
				r.RemoveCPULimit = !has
				if has {
					r.CPULimit = &q
				}
			} else {
				r.MemoryLimit = nil
				r.RemoveMemoryLimit = !has
				if has {
					r.MemoryLimit = &q
				}
			}
			applied = true
		}
		if applied {
			if exists {
				recs[i] = r
			} else {
				recs = append(recs, r)
			}
		}
	}
	return recs
}
