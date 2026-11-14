package application

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/discovery"
	"github.com/optipod/optipod/internal/history"
	"github.com/optipod/optipod/internal/nodesizing"
	"github.com/optipod/optipod/internal/policy"
	"github.com/optipod/optipod/internal/safety"
	"github.com/optipod/optipod/internal/webhook"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ReconcileNodeSizing records per-node previews and arms admission. Existing
// pods remain untouched; each new pod is sized against its actual destination.
func (e *Engine) ReconcileNodeSizing(ctx context.Context, w *discovery.Workload, p *api.OptimizationPolicy) error {
	if w.Kind != "DaemonSet" {
		return fmt.Errorf("node sizing requires a DaemonSet")
	}
	if err := p.ValidateCreate(); err != nil {
		return err
	}
	if e.dryRun {
		return nil
	}
	current := w.Object.DeepCopyObject().(client.Object)
	if err := e.client.Get(ctx, client.ObjectKeyFromObject(w.Object), current); err != nil {
		return err
	}
	if current.GetUID() != w.Object.GetUID() {
		return fmt.Errorf("node sizing workload identity changed")
	}
	winner, err := policy.NewPolicySelector(e.client).SelectBestPolicy(ctx, &discovery.Workload{Kind: w.Kind, Name: w.Name, Namespace: w.Namespace, Object: current})
	if err != nil {
		return err
	}
	if winner == nil || winner.UID != p.UID || winner.Generation != p.Generation {
		return fmt.Errorf("node sizing policy changed")
	}
	if current.GetAnnotations()[history.Paused] == valueTrue || current.GetAnnotations()[history.RollbackTo] != "" {
		return fmt.Errorf("node sizing workload is paused")
	}
	template, err := discovery.Template(current)
	if err != nil {
		return err
	}
	containers := []corev1.Container{}
	for _, slot := range discovery.ContainerSlots(&template.Spec) {
		containers = append(containers, slot.Container)
	}
	state, err := safety.Load(ctx, e.client, w, containers)
	if err != nil {
		return err
	}
	state.ObserveOOM(p, time.Now())
	if err := state.Save(ctx, e.client, w); err != nil {
		return err
	}
	if err := state.RecoveryError(p); err != nil {
		return err
	}
	if err := e.client.Get(ctx, client.ObjectKeyFromObject(w.Object), current); err != nil {
		return err
	}
	previews := map[string]any{}
	pods := &corev1.PodList{}
	if err := e.client.List(ctx, pods, client.InNamespace(w.Namespace)); err != nil {
		return err
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		owner, err := discovery.ResolvePodWorkload(ctx, e.client, pod)
		if err != nil || owner.Object.GetUID() != current.GetUID() {
			continue
		}
		if pod.Spec.NodeName != "" {
			if err := history.ObserveNodeAdmission(ctx, e.client, w, pod); err != nil {
				return err
			}
		}
		name, plan, err := nodesizing.Plan(ctx, e.client, pod, p)
		if err != nil {
			return err
		}
		previews[name] = plan
	}
	data, err := json.Marshal(previews)
	if err != nil {
		return err
	}
	if len(data) > 128*1024 {
		return fmt.Errorf("node sizing preview exceeds annotation limit")
	}
	before := current.DeepCopyObject().(client.Object)
	a := current.GetAnnotations()
	if a == nil {
		a = map[string]string{}
	}
	a[nodesizing.SummaryAnnotation] = string(data)
	if p.Spec.Mode == api.ModeAuto {
		a[api.AnnotationWebhookEnabled] = valueTrue
		a[api.AnnotationStrategy] = "webhook"
		delete(a, webhook.RecommendationExpiry)
	}
	current.SetAnnotations(a)
	return e.client.Patch(ctx, current, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
}
