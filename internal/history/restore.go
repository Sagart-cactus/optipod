package history

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// Restoration returns only fields controlled by the recorded action. A mixed
// pre-change replica baseline is rejected instead of choosing an arbitrary pod.
func Restoration(r *Record) (map[string]map[string]any, error) {
	result := map[string]map[string]any{}
	for container, sections := range r.Plan {
		baseline, exists := r.BeforeTemplate[container]
		for i, pod := range r.BeforePods {
			resources, ok := pod.Resources[container]
			if !ok {
				return nil, fmt.Errorf("container %s missing from a pre-change pod", container)
			}
			if i == 0 {
				baseline = resources
				exists = true
				continue
			}
			if !equalControlled(baseline, resources, sections) {
				return nil, fmt.Errorf("rollback requires a uniform resource baseline; replicas differed for %s", container)
			}
		}
		if !exists {
			return nil, fmt.Errorf("no original resources for %s", container)
		}
		restored := map[string]any{}
		for section, raw := range sections {
			fields, ok := raw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("invalid history resource plan")
			}
			values := baseline.Requests
			if section == fieldLimits {
				values = baseline.Limits
			}
			changes := map[string]any{}
			for name := range fields {
				if name != "cpu" && name != "memory" {
					return nil, fmt.Errorf("unsupported rollback resource %s", name)
				}
				q, ok := values[corev1.ResourceName(name)]
				if ok {
					changes[name] = q.String()
				} else {
					changes[name] = nil
				}
			}
			restored[section] = changes
		}
		result[container] = restored
	}
	return result, nil
}
func equalControlled(a, b corev1.ResourceRequirements, sections map[string]any) bool {
	for section, raw := range sections {
		fields, ok := raw.(map[string]any)
		if !ok {
			return false
		}
		av, bv := a.Requests, b.Requests
		if section == fieldLimits {
			av, bv = a.Limits, b.Limits
		}
		for name := range fields {
			aq, aOK := av[corev1.ResourceName(name)]
			bq, bOK := bv[corev1.ResourceName(name)]
			if aOK != bOK || aq.Cmp(bq) != 0 {
				return false
			}
		}
	}
	return true
}

// Matches checks exact presence and quantities for the recorded partial plan.
func Matches(resources map[string]corev1.ResourceRequirements, plan map[string]map[string]any) bool {
	for name, sections := range plan {
		co, ok := resources[name]
		if !ok {
			return false
		}
		for section, raw := range sections {
			fields, ok := raw.(map[string]any)
			if !ok {
				return false
			}
			values := co.Requests
			if section == fieldLimits {
				values = co.Limits
			}
			for key, value := range fields {
				q, present := values[corev1.ResourceName(key)]
				if value == nil {
					if present {
						return false
					}
					continue
				}
				text, ok := value.(string)
				if !ok {
					return false
				}
				desired, err := resource.ParseQuantity(text)
				if err != nil || !present || q.Cmp(desired) != 0 {
					return false
				}
			}
		}
	}
	return true
}
