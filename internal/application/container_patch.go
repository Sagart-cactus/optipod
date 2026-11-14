package application

import (
	"encoding/json"
	"fmt"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/discovery"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func (e *Engine) containerResourcePatch(w *Workload, recs []ContainerRecommendation, p *api.OptimizationPolicy, ssa bool) ([]byte, error) {
	t, err := discovery.Template(w.Object)
	if err != nil {
		return nil, err
	}
	fields := map[string]string{}
	for _, slot := range discovery.ContainerSlots(&t.Spec) {
		fields[slot.Container.Name] = slot.Field
	}
	entries := map[string][]map[string]any{fieldContainers: {}, fieldInitContainers: {}}
	for _, rec := range recs {
		field := fields[rec.Container]
		if field == "" {
			return nil, fmt.Errorf("container %s not found in workload template; injected containers require webhook strategy", rec.Container)
		}
		resources, err := e.resourceMap(rec.Recommendation, p)
		if err != nil {
			return nil, err
		}
		entries[field] = append(entries[field], map[string]any{fieldName: rec.Container, fieldResources: resources})
	}
	gvr, err := discovery.GVR(w.Kind)
	if err != nil {
		return nil, err
	}
	patch := map[string]any{"apiVersion": gvr.GroupVersion().String(), "kind": w.Kind, fieldMetadata: map[string]any{fieldName: w.Name, "namespace": w.Namespace}}
	for _, field := range []string{fieldContainers, fieldInitContainers} {
		containers := entries[field]
		if ssa {
			if err := e.preserveOwnedResources(w, &containers, field); err != nil {
				return nil, err
			}
		}
		if len(containers) == 0 {
			continue
		}
		raw := make([]any, len(containers))
		for i := range containers {
			raw[i] = containers[i]
		}
		if err := unstructured.SetNestedSlice(patch, raw, append(discovery.PodSpecPath(w.Kind), field)...); err != nil {
			return nil, err
		}
	}
	addMutationPreconditions(patch, w)
	return json.Marshal(patch)
}
