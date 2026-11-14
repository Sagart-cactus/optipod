package application

import (
	"context"
	"fmt"

	api "github.com/optipod/optipod/api/v1alpha1"
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
	pods, err := e.ownedPods(ctx, w)
	if err != nil {
		return err
	}
	noPods := len(pods) == 0
	if noPods {
		t, err := discovery.Template(w.Object)
		if err != nil {
			return err
		}
		pods = []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Namespace: w.Namespace}, Spec: t.Spec}}
	}
	changes := []constraints.Change{}
	hasChange := false
	for i := range pods {
		next, err := constraints.Overlay(&pods[i], updates)
		if err != nil {
			return err
		}
		hasChange = hasChange || !equality.Semantic.DeepEqual(pods[i].Spec, next.Spec)
		changes = append(changes, constraints.Change{Before: &pods[i], After: next, NewPod: noPods})
	}
	// A zero-unavailable Deployment requires room for at least one surge pod.
	// Without this preflight a template change can leave a rollout stuck forever.
	restarting := hasChange && !noPods && (!p.IsWebhookStrategy() || (p.GetRolloutStrategy() == api.RolloutImmediate && !p.Spec.UpdateStrategy.AllowInPlaceResize))
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
