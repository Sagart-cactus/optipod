package webhook

import (
	"fmt"
	"sort"

	"k8s.io/apimachinery/pkg/api/resource"
)

func FromResourcePlan(plan map[string]map[string]any) ([]ResourceRecommendation, error) {
	result := []ResourceRecommendation{}
	for name, sections := range plan {
		r := ResourceRecommendation{ContainerName: name}
		for section, raw := range sections {
			fields, ok := raw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("invalid resource section")
			}
			for name, value := range fields {
				var target **resource.Quantity
				var remove *bool
				switch section + "." + name {
				case "requests.cpu":
					target = &r.CPU
					remove = &r.RemoveCPURequest
				case "requests.memory":
					target = &r.Memory
					remove = &r.RemoveMemoryRequest
				case "limits.cpu":
					target = &r.CPULimit
					remove = &r.RemoveCPULimit
				case "limits.memory":
					target = &r.MemoryLimit
					remove = &r.RemoveMemoryLimit
				default:
					return nil, fmt.Errorf("unknown resource field %s.%s", section, name)
				}
				if value == nil {
					*remove = true
					continue
				}
				text, ok := value.(string)
				if !ok {
					return nil, fmt.Errorf("invalid quantity")
				}
				q, err := resource.ParseQuantity(text)
				if err != nil {
					return nil, err
				}
				*target = &q
			}
		}
		result = append(result, r)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ContainerName < result[j].ContainerName })
	return result, nil
}
