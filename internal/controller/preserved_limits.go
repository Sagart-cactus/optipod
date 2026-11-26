package controller

import (
	"context"
	"fmt"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/capturedlimits"
	"github.com/optipod/optipod/internal/coexist"
	"github.com/optipod/optipod/internal/discovery"
	"github.com/optipod/optipod/internal/safety"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func (wp *WorkloadProcessor) capPreservedLimits(ctx context.Context, workload *discovery.Workload, policy *api.OptimizationPolicy, state *safety.State, restrictions *coexist.Restrictions, recs []api.ContainerRecommendation, requestFloors map[string]corev1.ResourceList) error {
	ceilings, err := wp.preservedCeilings(ctx, workload, policy, state)
	if err != nil {
		return err
	}

	for i := range recs {
		rec := &recs[i]
		limits := ceilings[rec.Container]
		floors := requestFloors[rec.Container]
		if limit, bounded := limits[corev1.ResourceCPU]; bounded && restrictions.Reason(rec.Container, "cpu") == "" {
			floor := policy.Spec.ResourceBounds.CPU.Min
			if required := floors[corev1.ResourceCPU]; required.Cmp(floor) > 0 {
				floor = required
			}
			capPreservedRequest(rec, "CPU", &rec.CPU, &rec.RawCPU, limit, floor)
		}
		if limit, bounded := limits[corev1.ResourceMemory]; bounded && rec.Memory != nil && rec.Memory.Cmp(limit) > 0 && restrictions.Reason(rec.Container, "memory") == "" {
			floor := policy.Spec.ResourceBounds.Memory.Min
			if required := floors[corev1.ResourceMemory]; required.Cmp(floor) > 0 {
				floor = required
			}
			if rec.MinimumMemoryLimit != nil && rec.MinimumMemoryLimit.Cmp(floor) > 0 {
				floor = *rec.MinimumMemoryLimit
			}
			if state != nil {
				safe := state.GuardMemoryRequest(rec.Container, &limit, policy)
				if safe.Cmp(floor) > 0 {
					floor = *safe
				}
			}
			capPreservedRequest(rec, "memory", &rec.Memory, &rec.RawMemory, limit, floor)
			if rec.Memory == nil {
				rec.Emergency = false
				rec.MinimumMemoryLimit = nil
			}
		}
	}
	return nil
}

func (wp *WorkloadProcessor) preservedCeilings(ctx context.Context, workload *discovery.Workload, policy *api.OptimizationPolicy, state *safety.State) (map[string]corev1.ResourceList, error) {
	if policy.LimitMode(true) != api.LimitPreserve && policy.LimitMode(false) != api.LimitPreserve {
		return nil, nil
	}
	template, err := discovery.Template(workload.Object)
	if err != nil {
		return nil, err
	}
	if template.Spec.Resources != nil {
		return nil, nil // Whole-Pod budgets are projected separately.
	}
	if state != nil {
		for i := range state.Pods {
			pod := &state.Pods[i]
			if activeLimitPod(pod) && pod.Spec.Resources != nil {
				return nil, nil
			}
		}
	}
	snapshot, err := capturedlimits.PolicySnapshot(policy, workload.Object)
	if err != nil {
		return nil, err
	}
	declared := capturedlimits.DeclaredSlots(&template.Spec)
	ceilings := make(map[string]corev1.ResourceList, len(declared))

	// New targets start with the template except where a workload-wide
	// capture also applies to them. Record the lowest finite ceiling for a
	// recommendation shared by targets, without turning an unset limit into zero.
	addPreservedCeilings(ceilings, &template.Spec, declared, snapshot, policy)
	if state != nil {
		for i := range state.Pods {
			pod := &state.Pods[i]
			if !activeLimitPod(pod) {
				continue
			}
			var podCapture *capturedlimits.Snapshot
			if snapshot != nil {
				podCapture, err = snapshot.ForPod(ctx, wp.client, workload.Kind, workload.Object.GetUID(), pod)
				if err != nil {
					return nil, err
				}
			}
			if podCapture != nil && policy.GetRolloutStrategy() != api.RolloutOnNextRestart {
				addPreservedCeilings(ceilings, &pod.Spec, declared, nil, policy)
			}
			addPreservedCeilings(ceilings, &pod.Spec, declared, podCapture, policy)
		}
	}
	return ceilings, nil
}

func activeLimitPod(pod *corev1.Pod) bool {
	return pod.DeletionTimestamp == nil && pod.Status.Phase != corev1.PodFailed && pod.Status.Phase != corev1.PodSucceeded
}

func addPreservedCeilings(ceilings map[string]corev1.ResourceList, spec *corev1.PodSpec, declared map[string]discovery.ContainerSlot, snapshot *capturedlimits.Snapshot, policy *api.OptimizationPolicy) {
	for _, slot := range discovery.ContainerSlots(spec) {
		name := slot.Container.Name
		if policy.ExcludesContainer(name) || (slot.Field == nativeSidecarField && !policy.IncludesNativeSidecars()) {
			continue
		}
		capture, original, captured := snapshot.ContainerForSlot(slot, declared, policy)
		for _, dim := range [...]corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory} {
			if !policy.ControlsResource(string(dim)) || policy.LimitMode(dim == corev1.ResourceCPU) != api.LimitPreserve {
				continue
			}
			limits := slot.Container.Resources.Limits
			if captured && capture.AppliesToLimit(original, dim) {
				limits = capture.Limits
			}
			limit, bounded := limits[dim]
			if !bounded {
				continue
			}
			if ceilings[name] == nil {
				ceilings[name] = corev1.ResourceList{}
			}
			if old, present := ceilings[name][dim]; !present || limit.Cmp(old) < 0 {
				ceilings[name][dim] = limit
			}
		}
	}
}

func capPreservedRequest(rec *api.ContainerRecommendation, dimension string, request, raw **resource.Quantity, limit, minimum resource.Quantity) {
	if *request == nil || (*request).Cmp(limit) <= 0 {
		return
	}
	before := (*request).DeepCopy()
	*raw = &before
	switch {
	case limit.Sign() <= 0:
		*request = nil
		rec.Explanation += fmt.Sprintf("; %s request %s left unchanged: preserved limit %s allows no positive request", dimension, before.String(), limit.String())
	case minimum.Cmp(limit) > 0:
		*request = nil
		rec.Explanation += fmt.Sprintf("; %s request %s left unchanged: preserved limit %s is below required minimum %s", dimension, before.String(), limit.String(), minimum.String())
	default:
		effective := limit.DeepCopy()
		*request = &effective
		rec.Explanation += fmt.Sprintf("; %s request capped from %s to %s by preserved limit %s", dimension, before.String(), effective.String(), limit.String())
	}
}
