package webhook

import (
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/capturedlimits"
	"github.com/optipod/optipod/internal/coexist"
	"github.com/optipod/optipod/internal/discovery"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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
		var original *corev1.ResourceRequirements
		if present {
			original = &orig.Container.Resources
		}
		applied := false
		for _, key := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory} {
			if p.ControlsResource(string(key)) && restoreCapturedDimension(&r, s, c, original, p, restrictions, key) {
				applied = true
			}
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

func restoreCapturedDimension(r *ResourceRecommendation, s *capturedlimits.Snapshot, capture capturedlimits.Container,
	original *corev1.ResourceRequirements, p *api.OptimizationPolicy, restrictions *coexist.Restrictions, key corev1.ResourceName,
) bool {
	cpu := key == corev1.ResourceCPU
	request, removeRequest := &r.Memory, &r.RemoveMemoryRequest
	limit, removeLimit := &r.MemoryLimit, &r.RemoveMemoryLimit
	if cpu {
		request, removeRequest = &r.CPU, &r.RemoveCPURequest
		limit, removeLimit = &r.CPULimit, &r.RemoveCPULimit
	}
	now := time.Now()
	deferred := p.GetRolloutStrategy() == api.RolloutOnNextRestart
	baseline := original == nil || capturedlimits.Same(original.Requests, capture.TemplateRequests, key)
	var changed bool
	if restrictions.Reason(r.ContainerName, string(key)) != "" {
		q, captured := capture.HPARequests[key]
		if !captured || !deferred || !baseline {
			return false
		}
		*request, *removeRequest = &q, false
		changed = true
	} else if capture.RunningRequests != nil && baseline {
		hold := deferred && s.HoldRequestsUntil != nil && now.Before(*s.HoldRequestsUntil)
		// Prefer each fresh dimension when it becomes available.
		missing := *request == nil && !*removeRequest
		fallback := missing && s.FallbackRequestsUntil != nil && now.Before(*s.FallbackRequestsUntil)
		if hold || fallback {
			*request, *removeRequest = capturedQuantity(*capture.RunningRequests, key)
			changed = true
		}
	}
	if p.LimitMode(cpu) == api.LimitPreserve && (original == nil || capturedlimits.Same(original.Limits, capture.Template, key)) {
		*limit, *removeLimit = capturedQuantity(capture.Limits, key)
		return true
	}
	return changed
}

func capturedQuantity(values corev1.ResourceList, key corev1.ResourceName) (*resource.Quantity, bool) {
	q, exists := values[key]
	if !exists {
		return nil, true
	}
	return &q, false
}
