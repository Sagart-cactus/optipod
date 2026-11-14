package application

import (
	"context"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/discovery"
	"github.com/optipod/optipod/internal/recommendation"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// Refresh the non-decreasing limit floor from both the final template read and
// live replicas, including mixed revisions and injected sidecars.
func (e *Engine) preserveAutomaticLimits(ctx context.Context, w *Workload, recs []ContainerRecommendation, p *api.OptimizationPolicy) error {
	if p.LimitMode(false) != api.LimitAutomatic || isRollback(ctx) {
		return nil
	}
	template, err := discovery.Template(w.Object)
	if err != nil {
		return err
	}
	pods, err := e.ownedPods(ctx, w)
	if err != nil {
		return err
	}
	pods = append(pods, corev1.Pod{Spec: template.Spec})
	for _, pod := range pods {
		for _, slot := range discovery.ContainerSlots(&pod.Spec) {
			for _, rec := range recs {
				if rec.Container != slot.Container.Name || rec.Recommendation.PreserveMemory {
					continue
				}
				floor := slot.Container.Resources.Limits.Memory()
				if rec.Recommendation.MinimumMemoryLimit == nil || floor.Cmp(*rec.Recommendation.MinimumMemoryLimit) > 0 {
					value := floor.DeepCopy()
					rec.Recommendation.MinimumMemoryLimit = &value
				}
			}
		}
	}
	return nil
}

func addMutationPreconditions(patch map[string]interface{}, w *Workload) {
	if w.Object.GetUID() == "" {
		return
	}
	meta, ok := patch[fieldMetadata].(map[string]interface{})
	if !ok {
		meta = map[string]interface{}{}
		patch[fieldMetadata] = meta
	}
	meta["uid"] = string(w.Object.GetUID())
	if rv := w.Object.GetResourceVersion(); rv != "" {
		meta["resourceVersion"] = rv
	}
}

func (e *Engine) guardRestartIfNeeded(ctx context.Context, w *Workload, needed bool) error {
	if !needed {
		return nil
	}
	return e.guardRestart(ctx, w)
}

func (e *Engine) resourceChangeNeeded(w *Workload, recs []ContainerRecommendation, p *api.OptimizationPolicy) (bool, error) {
	current, err := e.getCurrentResources(w)
	if err != nil {
		return false, err
	}
	for _, rec := range recs {
		desired, err := e.resourceMap(rec.Recommendation, p)
		if err != nil {
			return false, err
		}
		for field, values := range desired {
			existing := current[rec.Container].Requests
			if field == fieldLimits {
				existing = current[rec.Container].Limits
			}
			for key, value := range values.(map[string]any) {
				old, present := existing[corev1.ResourceName(key)]
				if value == nil {
					if present {
						return true, nil
					}
					continue
				}
				q, err := resource.ParseQuantity(value.(string))
				if err != nil {
					return false, err
				}
				if !present || q.Cmp(old) != 0 {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

// resourceMap is shared by SSA, strategic merge and admission-plan storage.
// A null limit explicitly removes it; an omitted limit preserves it.
func (e *Engine) resourceMap(rec *recommendation.Recommendation, p *api.OptimizationPolicy) (map[string]any, error) {
	if rec.ResourceOverride != nil {
		result := map[string]any{}
		for field, raw := range rec.ResourceOverride {
			values := map[string]any{}
			for name, value := range raw.(map[string]any) {
				if (name == resourceCPU && (!p.ControlsResource(name) || rec.PreserveCPU)) || (name == "memory" && (!p.ControlsResource(name) || rec.PreserveMemory)) {
					continue
				}
				values[name] = value
			}
			if len(values) > 0 {
				result[field] = values
			}
		}
		return result, nil
	}
	requests := corev1.ResourceList{}
	if p.ControlsResource(resourceCPU) && !rec.PreserveCPU {
		requests[corev1.ResourceCPU] = rec.CPU
	}
	if p.ControlsResource("memory") && !rec.PreserveMemory {
		requests[corev1.ResourceMemory] = rec.Memory
	}
	rm := map[string]any{}
	for name, q := range requests {
		rm[string(name)] = q.String()
	}
	result := map[string]any{fieldRequests: rm}
	limits := corev1.ResourceList{}
	if p.LimitMode(true) == limitMultiplier || p.LimitMode(false) == limitMultiplier {
		var err error
		limits, err = e.CalculateLimitsWithDefaults(requests, p.Spec.UpdateStrategy.LimitConfig)
		if err != nil {
			return nil, err
		}
	}
	lm := map[string]any{}
	for name := range requests {
		switch p.LimitMode(name == corev1.ResourceCPU) {
		case limitNone:
			lm[string(name)] = nil
		case limitMultiplier:
			q := limits[name]
			if name == corev1.ResourceMemory && rec.MinimumMemoryLimit != nil && q.Cmp(*rec.MinimumMemoryLimit) < 0 {
				q = rec.MinimumMemoryLimit.DeepCopy()
			}
			lm[string(name)] = q.String()
		case api.LimitAutomatic:
			q := api.AutomaticMemoryLimit(requests[name], rec.MinimumMemoryLimit)
			lm[string(name)] = q.String()
		}
	}
	if len(lm) > 0 {
		result[fieldLimits] = lm
	}
	return result, nil
}

// ResourcePlan returns the exact partial CPU/memory updates, including explicit
// limit removals, used by all mutation methods and the durable journal.
func ResourcePlan(recs []ContainerRecommendation, p *api.OptimizationPolicy) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	engine := &Engine{}
	for _, r := range recs {
		value, err := engine.resourceMap(r.Recommendation, p)
		if err != nil {
			return nil, err
		}
		out[r.Container] = value
	}
	return out, nil
}
