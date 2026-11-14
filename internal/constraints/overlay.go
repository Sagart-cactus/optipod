package constraints

import (
	"fmt"

	"github.com/optipod/optipod/internal/discovery"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func Overlay(pod *corev1.Pod, updates map[string]map[string]any) (*corev1.Pod, error) {
	next := pod.DeepCopy()
	for _, slot := range discovery.ContainerSlots(&next.Spec) {
		update, ok := updates[slot.Container.Name]
		if !ok {
			continue
		}
		var co *corev1.Container
		if slot.Field == "initContainers" {
			co = &next.Spec.InitContainers[slot.Index]
		} else {
			co = &next.Spec.Containers[slot.Index]
		}
		for field, raw := range update {
			values, ok := raw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("invalid resource map")
			}
			target := &co.Resources.Requests
			if field == "limits" {
				target = &co.Resources.Limits
			}
			if *target == nil {
				*target = corev1.ResourceList{}
			}
			for name, raw := range values {
				key := corev1.ResourceName(name)
				if raw == nil {
					delete(*target, key)
					continue
				}
				value, ok := raw.(string)
				if !ok {
					return nil, fmt.Errorf("invalid resource quantity")
				}
				q, err := resource.ParseQuantity(value)
				if err != nil {
					return nil, err
				}
				(*target)[key] = q
			}
		}
	}
	return next, nil
}
