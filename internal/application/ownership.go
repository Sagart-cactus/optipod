package application

import (
	"encoding/json"
	"fmt"
	"strings"

	api "github.com/optipod/optipod/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Limit removal uses strategic merge because SSA treats a null Quantity as
// zero. Enforce the same ownership permission before that alternate patch path.
func (e *Engine) guardMergeOwnership(w *Workload, recs []ContainerRecommendation, p *api.OptimizationPolicy) error {
	if p.Spec.UpdateStrategy.AllowFieldOwnershipTakeover {
		return nil
	}
	current, err := e.getCurrentResources(w)
	if err != nil {
		return err
	}
	changes := map[string]map[string]any{}
	for _, rec := range recs {
		values, err := e.resourceMap(rec.Recommendation, p)
		if err != nil {
			return err
		}
		changes[rec.Container] = values
	}
	for _, entry := range w.Object.GetManagedFields() {
		if entry.Manager == FieldManagerName || entry.FieldsV1 == nil || entry.Subresource != "" {
			continue
		}
		var fields map[string]any
		if err := json.Unmarshal(entry.FieldsV1.GetRawBytes(), &fields); err != nil {
			return err
		}
		for _, field := range []string{fieldContainers, fieldInitContainers} {
			owned, _, _ := unstructured.NestedMap(fields, "f:spec", "f:template", "f:spec", "f:"+field)
			for key, raw := range owned {
				if !strings.HasPrefix(key, "k:") {
					continue
				}
				var id struct {
					Name string `json:"name"`
				}
				if err := json.Unmarshal([]byte(key[2:]), &id); err != nil {
					return err
				}
				cf, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				for section, rawValues := range changes[id.Name] {
					values := rawValues.(map[string]any)
					old := current[id.Name].Requests
					if section == fieldLimits {
						old = current[id.Name].Limits
					}
					for name, value := range values {
						before, exists := old[corev1.ResourceName(name)]
						changed := exists
						if value != nil {
							q, err := resource.ParseQuantity(value.(string))
							if err != nil {
								return err
							}
							changed = !exists || before.Cmp(q) != 0
						}
						if !changed {
							continue
						}
						if _, owns, _ := unstructured.NestedFieldNoCopy(cf, "f:resources", "f:"+section, "f:"+name); owns {
							return fmt.Errorf("SSA conflict: manager %s owns %s %s.%s; allowFieldOwnershipTakeover is required", entry.Manager, id.Name, section, name)
						}
					}
				}
			}
		}
	}
	return nil
}

// SSA's omission means deletion for fields we already own. Preserve their
// current values when optimization is disabled for a dimension or container.
// This retains ownership until an external manager explicitly takes it over.
func (e *Engine) preserveOwnedResources(w *Workload, containers *[]map[string]any, containerField ...string) error {
	field := fieldContainers
	if len(containerField) > 0 {
		field = containerField[0]
	}
	current, err := e.getCurrentResources(w)
	if err != nil {
		return err
	}
	for _, entry := range w.Object.GetManagedFields() {
		if entry.Manager != FieldManagerName || entry.FieldsV1 == nil {
			continue
		}
		var fields map[string]any
		if err := json.Unmarshal(entry.FieldsV1.GetRawBytes(), &fields); err != nil {
			return err
		}
		owned, _, _ := unstructured.NestedMap(fields, "f:spec", "f:template", "f:spec", "f:"+field)
		for key, raw := range owned {
			if !strings.HasPrefix(key, "k:") {
				continue
			}
			var id struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal([]byte(key[2:]), &id); err != nil {
				return err
			}
			cf, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if _, exists := current[id.Name]; !exists {
				continue
			}
			if _, ownsResources := cf["f:resources"]; !ownsResources {
				continue
			}
			var target map[string]any
			for _, c := range *containers {
				if c[fieldName] == id.Name {
					target = c
					break
				}
			}
			if target == nil {
				target = map[string]any{fieldName: id.Name, fieldResources: map[string]any{}}
				*containers = append(*containers, target)
			}
			resources := target[fieldResources].(map[string]any)
			for _, section := range []string{fieldRequests, fieldLimits} {
				ownedResources, _, _ := unstructured.NestedMap(cf, "f:resources", "f:"+section)
				actual := current[id.Name].Requests
				if section == fieldLimits {
					actual = current[id.Name].Limits
				}
				planned, ok := resources[section].(map[string]any)
				if !ok {
					planned = map[string]any{}
				}
				for name, q := range actual {
					if name != corev1.ResourceCPU && name != corev1.ResourceMemory {
						continue
					}
					if _, owns := ownedResources["f:"+string(name)]; owns {
						if _, set := planned[string(name)]; !set {
							planned[string(name)] = q.String()
						}
					}
				}
				if len(planned) > 0 {
					resources[section] = planned
				}
			}
		}
	}
	return nil
}
