package controller

import (
	"context"
	"fmt"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/discovery"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (wp *WorkloadProcessor) selectedContainers(ctx context.Context, w *discovery.Workload, p *api.OptimizationPolicy) ([]corev1.Container, error) {
	t, err := discovery.Template(w.Object)
	if err != nil {
		return nil, err
	}
	result := []corev1.Container{}
	seen := map[string]bool{}
	for _, slot := range discovery.ContainerSlots(&t.Spec) {
		seen[slot.Container.Name] = true
		if slot.Field == "initContainers" && !p.IncludesNativeSidecars() {
			continue
		}
		result = append(result, slot.Container)
	}
	if wp.client == nil || !p.IncludesInjectedSidecars() {
		return result, nil
	}
	pods := &corev1.PodList{}
	if err := wp.client.List(ctx, pods, client.InNamespace(w.Namespace)); err != nil {
		return nil, err
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		owner, err := discovery.ResolvePodWorkload(ctx, wp.client, pod)
		if err != nil || owner.Object.GetUID() != w.Object.GetUID() {
			continue
		}
		for _, slot := range discovery.ContainerSlots(&pod.Spec) {
			if seen[slot.Container.Name] || p.ExcludesContainer(slot.Container.Name) || (slot.Field == "initContainers" && !p.IncludesNativeSidecars()) {
				continue
			}
			if !p.IsWebhookStrategy() {
				return nil, fmt.Errorf("injected container %s requires webhook strategy; it is absent from the workload template", slot.Container.Name)
			}
			result = append(result, slot.Container)
			seen[slot.Container.Name] = true
		}
	}
	return result, nil
}
