package application

import (
	"context"
	"fmt"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/capturedlimits"
	"github.com/optipod/optipod/internal/constraints"
	"github.com/optipod/optipod/internal/discovery"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func (e *Engine) validateConstraints(ctx context.Context, w *Workload, recs []ContainerRecommendation, p *api.OptimizationPolicy) error {
	// A typed object converted from the informer cache can have empty TypeMeta.
	w.Object.SetKind(w.Kind)
	updates := map[string]map[string]any{}
	for _, r := range recs {
		values, err := e.resourceMap(r.Recommendation, p)
		if err != nil {
			return err
		}
		updates[r.Container] = values
	}
	changes, hasChange, err := e.constraintChanges(ctx, w, updates, p)
	if err != nil {
		return err
	}
	// A zero-unavailable Deployment requires room for at least one surge pod.
	// Without this preflight a template change can leave a rollout stuck forever.
	restarting := hasChange && !changes[0].NewPod && (!p.IsWebhookStrategy() || (p.GetRolloutStrategy() == api.RolloutImmediate && !p.Spec.UpdateStrategy.AllowInPlaceResize))
	if restarting && w.Kind == "Deployment" {
		d := &appsv1.Deployment{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(w.Object.Object, d); err != nil {
			return err
		}
		if d.Spec.Strategy.Type != appsv1.RecreateDeploymentStrategyType && d.Spec.Strategy.RollingUpdate != nil && d.Spec.Strategy.RollingUpdate.MaxUnavailable != nil {
			n := int32(1)
			if d.Spec.Replicas != nil {
				n = *d.Spec.Replicas
			}
			unavailable, err := intstr.GetScaledValueFromIntOrPercent(d.Spec.Strategy.RollingUpdate.MaxUnavailable, int(n), false)
			if err != nil {
				return err
			}
			if unavailable == 0 {
				changes = append(changes, constraints.Change{After: changes[0].After, NewPod: true})
			}
		}
	}
	if err := constraints.Validate(ctx, e.client, w.Namespace, changes, p.Spec.UpdateStrategy.PreserveQoS); err != nil {
		return fmt.Errorf("constraint preflight: %w", err)
	}
	return nil
}

func (e *Engine) constraintChanges(ctx context.Context, w *Workload, updates map[string]map[string]any, p *api.OptimizationPolicy) ([]constraints.Change, bool, error) {
	pods, err := e.ownedPods(ctx, w)
	if err != nil {
		return nil, false, err
	}
	var snapshot *capturedlimits.Snapshot
	if p.GetRolloutStrategy() == api.RolloutOnNextRestart && !isRollback(ctx) {
		snapshot, err = capturedlimits.PolicySnapshot(p, w.Object)
		if err != nil {
			return nil, false, err
		}
	}
	var declared map[string]discovery.ContainerSlot
	noPods := len(pods) == 0
	if noPods || snapshot != nil {
		t, err := discovery.Template(w.Object)
		if err != nil {
			return nil, false, err
		}
		if noPods {
			pods = []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Namespace: w.Namespace}, Spec: t.Spec}}
		}
		if snapshot != nil {
			declared = capturedlimits.DeclaredSlots(&t.Spec)
		}
	}
	changes := make([]constraints.Change, 0, len(pods))
	hasChange := false
	for i := range pods {
		whole := constraints.WholePodRequests(&pods[i], p)
		project := constraints.Overlay
		if whole {
			project = constraints.BudgetCandidate
		}
		next, err := project(&pods[i], updates)
		if err != nil {
			return nil, false, err
		}
		// Deferred admission restores a captured preserved limit before
		// validation. Project the same limit here for the changed dimensions.
		if snapshot != nil && !whole {
			podSnapshot := snapshot
			if !noPods {
				podSnapshot, err = snapshot.ForPod(ctx, e.client, w.Kind, w.Object.GetUID(), &pods[i])
				if err != nil {
					return nil, false, err
				}
			}
			projectCapturedLimits(next, updates, declared, podSnapshot, p)
		}
		hasChange = hasChange || !equality.Semantic.DeepEqual(pods[i].Spec, next.Spec)
		changes = append(changes, constraints.Change{Before: &pods[i], After: next, NewPod: noPods, WholePodRequests: whole})
	}
	return changes, hasChange, nil
}

func projectCapturedLimits(pod *corev1.Pod, updates map[string]map[string]any, declared map[string]discovery.ContainerSlot, snapshot *capturedlimits.Snapshot, policy *api.OptimizationPolicy) {
	for _, slot := range discovery.ContainerSlots(&pod.Spec) {
		requests, _ := updates[slot.Container.Name]["requests"].(map[string]any)
		if len(requests) == 0 {
			continue
		}
		capture, original, applies := snapshot.ContainerForSlot(slot, declared, policy)
		if !applies {
			continue
		}
		var container *corev1.Container
		if slot.Field == "initContainers" {
			container = &pod.Spec.InitContainers[slot.Index]
		} else {
			container = &pod.Spec.Containers[slot.Index]
		}
		for _, dim := range [...]corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory} {
			if requests[string(dim)] == nil || policy.LimitMode(dim == corev1.ResourceCPU) != api.LimitPreserve || !capture.AppliesToLimit(original, dim) {
				continue
			}
			if limit, hasLimit := capture.Limits[dim]; hasLimit {
				if container.Resources.Limits == nil {
					container.Resources.Limits = corev1.ResourceList{}
				}
				container.Resources.Limits[dim] = limit.DeepCopy()
			} else {
				delete(container.Resources.Limits, dim)
			}
		}
	}
}
