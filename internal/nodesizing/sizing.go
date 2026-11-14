// Package nodesizing resolves admission-time DaemonSet requests from real node
// allocatable resources. Unknown or ambiguous destination nodes fail closed.
package nodesizing

import (
	"context"
	"fmt"
	"math"
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/discovery"
	"github.com/optipod/optipod/internal/safety"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const SummaryAnnotation = "optipod.io/node-sizing-preview"

func NodeName(pod *corev1.Pod) (string, error) {
	if pod.Spec.NodeName != "" {
		return pod.Spec.NodeName, nil
	}
	if pod.Spec.Affinity == nil || pod.Spec.Affinity.NodeAffinity == nil || pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		return "", fmt.Errorf("DaemonSet destination node is unknown")
	}
	terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	name := ""
	for _, term := range terms {
		target := ""
		for _, field := range term.MatchFields {
			if field.Key == "metadata.name" {
				if field.Operator != corev1.NodeSelectorOpIn || len(field.Values) != 1 {
					return "", fmt.Errorf("ambiguous destination node")
				}
				if target != "" && target != field.Values[0] {
					return "", fmt.Errorf("conflicting destination node")
				}
				target = field.Values[0]
			}
		}
		if target == "" || (name != "" && name != target) {
			return "", fmt.Errorf("ambiguous destination node")
		}
		name = target
	}
	if name == "" {
		return "", fmt.Errorf("destination node is unknown")
	}
	return name, nil
}
func Plan(ctx context.Context, c client.Client, pod *corev1.Pod, p *api.OptimizationPolicy) (string, map[string]map[string]any, error) {
	name, err := NodeName(pod)
	if err != nil {
		return "", nil, err
	}
	node := &corev1.Node{}
	if err := c.Get(ctx, client.ObjectKey{Name: name}, node); err != nil {
		return name, nil, err
	}
	if node.DeletionTimestamp != nil {
		return name, nil, fmt.Errorf("destination node is deleting")
	}
	plan, err := ForNode(pod, p, node)
	if err != nil {
		return name, nil, err
	}
	owner, err := discovery.ResolvePodWorkload(ctx, c, pod)
	if err != nil || owner.Kind != "DaemonSet" {
		return name, nil, fmt.Errorf("node sizing requires a verified DaemonSet owner")
	}
	containers := []corev1.Container{}
	for _, slot := range discovery.ContainerSlots(&pod.Spec) {
		containers = append(containers, slot.Container)
	}
	state, err := safety.Load(ctx, c, owner, containers)
	if err != nil {
		return name, nil, err
	}
	state.ObserveOOM(p, time.Now())
	if err := state.RecoveryError(p); err != nil {
		return name, nil, err
	}
	if *p.RecoverySettings().Enabled {
		for container, sections := range plan {
			cs := state.Containers[container]
			if cs == nil || cs.Recovery == nil || !time.Now().Before(cs.Recovery.FreezeUntil) {
				continue
			}
			requests := sections["requests"].(map[string]any)
			raw, changes := requests["memory"]
			if !changes {
				continue
			}
			floor := cs.Recovery.Floor
			current := state.Current[container]
			if current.Requests.Memory().Cmp(floor) > 0 {
				floor = current.Requests.Memory().DeepCopy()
			}
			if floor.Cmp(p.Spec.ResourceBounds.Memory.Max) > 0 {
				return name, nil, fmt.Errorf("OOM floor exceeds node-sizing memory maximum")
			}
			q, err := resource.ParseQuantity(raw.(string))
			if err != nil {
				return name, nil, err
			}
			if q.Cmp(floor) < 0 {
				requests["memory"] = floor.String()
			}
			limits := sections["limits"].(map[string]any)
			if raw := limits["memory"]; raw != nil {
				limit, err := resource.ParseQuantity(raw.(string))
				if err != nil {
					return name, nil, err
				}
				if current.Limits.Memory().Cmp(floor) > 0 {
					floor = current.Limits.Memory().DeepCopy()
				}
				if limit.Cmp(floor) < 0 {
					limits["memory"] = floor.String()
				}
			}
		}
	}
	return name, plan, nil
}
func ForNode(pod *corev1.Pod, p *api.OptimizationPolicy, node *corev1.Node) (map[string]map[string]any, error) {
	if p.Spec.DaemonSetSizing == nil {
		return nil, fmt.Errorf("node sizing is not configured")
	}
	result := map[string]map[string]any{}
	for _, slot := range discovery.ContainerSlots(&pod.Spec) {
		co := slot.Container
		if p.ExcludesContainer(co.Name) || (slot.Field == "initContainers" && !p.IncludesNativeSidecars()) {
			continue
		}
		percentages, explicit := p.Spec.DaemonSetSizing.Containers[co.Name]
		if !explicit {
			_, hasCPU := co.Resources.Requests[corev1.ResourceCPU]
			_, hasMemory := co.Resources.Requests[corev1.ResourceMemory]
			if p.Spec.DaemonSetSizing.Default == nil || (!hasCPU && !hasMemory) {
				continue
			}
			percentages = *p.Spec.DaemonSetSizing.Default
		}
		requests, limits := map[string]any{}, map[string]any{}
		for i, percent := range []*float64{percentages.CPUPercent, percentages.MemoryPercent} {
			key := corev1.ResourceCPU
			bounds := p.Spec.ResourceBounds.CPU
			multiplier := 1.0
			if i == 1 {
				key = corev1.ResourceMemory
				bounds = p.Spec.ResourceBounds.Memory
				multiplier = 1.1
			}
			if percent == nil || !p.ControlsResource(string(key)) {
				continue
			}
			alloc := node.Status.Allocatable[key]
			if alloc.Sign() <= 0 {
				return nil, fmt.Errorf("node %s has no allocatable %s", node.Name, key)
			}
			base := alloc.Value()
			if i == 0 {
				base = alloc.MilliValue()
			}
			value := int64(math.Ceil(float64(base) * *percent / 100))
			q := resource.NewQuantity(value, resource.BinarySI)
			if i == 0 {
				q = resource.NewMilliQuantity(value, resource.DecimalSI)
			}
			if q.Cmp(bounds.Min) < 0 {
				*q = bounds.Min.DeepCopy()
			}
			if q.Cmp(bounds.Max) > 0 {
				*q = bounds.Max.DeepCopy()
			}
			requests[string(key)] = q.String()
			switch p.LimitMode(i == 0) {
			case "None":
				limits[string(key)] = nil
			case api.LimitAutomatic:
				limit := api.AutomaticMemoryLimit(*q, co.Resources.Limits.Memory())
				limits[string(key)] = limit.String()
			case "Multiplier":
				if cfg := p.Spec.UpdateStrategy.LimitConfig; cfg != nil {
					if i == 0 && cfg.CPULimitMultiplier != nil {
						multiplier = *cfg.CPULimitMultiplier
					}
					if i == 1 && cfg.MemoryLimitMultiplier != nil {
						multiplier = *cfg.MemoryLimitMultiplier
					}
				}
				val := q.Value()
				if i == 0 {
					val = q.MilliValue()
				}
				v := int64(math.Ceil(float64(val) * multiplier))
				l := resource.NewQuantity(v, resource.BinarySI)
				if i == 0 {
					l = resource.NewMilliQuantity(v, resource.DecimalSI)
				}
				limits[string(key)] = l.String()
			default:
				return nil, fmt.Errorf("node sizing cannot preserve %s limits", key)
			}
		}
		if len(requests) > 0 {
			result[co.Name] = map[string]any{"requests": requests, "limits": limits}
		}
	}
	return result, nil
}
