// Package safety applies durable workload-level safety rules before application.
package safety

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/discovery"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const StateAnnotation = "optipod.io/safety-state"

type ContainerState struct {
	Recovery       *RecoveryState     `json:"recovery,omitempty"`
	InitialMemory  resource.Quantity  `json:"initialMemory"`
	InitialLimit   resource.Quantity  `json:"initialLimit"`
	AcceptedCPU    *resource.Quantity `json:"acceptedCPU,omitempty"`
	AcceptedMemory *resource.Quantity `json:"acceptedMemory,omitempty"`
	AcceptedLimit  *resource.Quantity `json:"acceptedLimit,omitempty"`
}
type State struct {
	Pods       []corev1.Pod                           `json:"-"`
	Containers map[string]*ContainerState             `json:"containers"`
	LastChange time.Time                              `json:"lastChange,omitempty"`
	PlanHash   string                                 `json:"planHash,omitempty"`
	Current    map[string]corev1.ResourceRequirements `json:"-"`
}

// Load considers actual owned pods, including mixed revisions. Taking the
// maximum memory request avoids accelerating a decrease during a rollout.
func Load(ctx context.Context, c client.Client, w *discovery.Workload, containers []corev1.Container) (*State, error) {
	obj := w.Object.DeepCopyObject().(client.Object)
	if err := c.Get(ctx, client.ObjectKeyFromObject(w.Object), obj); err != nil {
		return nil, err
	}
	if obj.GetUID() != w.Object.GetUID() {
		return nil, fmt.Errorf("workload identity changed")
	}
	s := &State{Containers: map[string]*ContainerState{}, Current: map[string]corev1.ResourceRequirements{}}
	if raw := obj.GetAnnotations()[StateAnnotation]; raw != "" {
		if err := json.Unmarshal([]byte(raw), s); err != nil {
			return nil, fmt.Errorf("invalid safety state: %w", err)
		}
	}
	if s.Containers == nil {
		s.Containers = map[string]*ContainerState{}
	}
	for _, co := range containers {
		s.Current[co.Name] = *co.Resources.DeepCopy()
	}
	pods := &corev1.PodList{}
	if err := c.List(ctx, pods, client.InNamespace(w.Namespace)); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for i := range pods.Items {
		pod := &pods.Items[i]
		owner, err := discovery.ResolvePodWorkload(ctx, c, pod)
		if err != nil || owner.Object.GetUID() != w.Object.GetUID() {
			continue
		}
		s.Pods = append(s.Pods, *pod)
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		for _, co := range append(append([]corev1.Container{}, pod.Spec.Containers...), pod.Spec.InitContainers...) {
			prev, ok := s.Current[co.Name]
			if !ok {
				continue
			}
			if !seen[co.Name] {
				s.Current[co.Name] = *co.Resources.DeepCopy()
				seen[co.Name] = true
				continue
			}
			if prev.Requests == nil {
				prev.Requests = corev1.ResourceList{}
			}
			if prev.Limits == nil {
				prev.Limits = corev1.ResourceList{}
			}
			for k, v := range co.Resources.Requests {
				q := prev.Requests[k]
				if v.Cmp(q) > 0 {
					prev.Requests[k] = v
				}
			}
			for k, v := range co.Resources.Limits {
				q := prev.Limits[k]
				if v.Cmp(q) > 0 {
					prev.Limits[k] = v
				}
			}
			s.Current[co.Name] = prev
		}
	}
	for name, current := range s.Current {
		if s.Containers[name] == nil {
			s.Containers[name] = &ContainerState{InitialMemory: current.Requests.Memory().DeepCopy(), InitialLimit: current.Limits.Memory().DeepCopy()}
		}
	}
	return s, nil
}

func (s *State) Guard(recs []api.ContainerRecommendation, p *api.OptimizationPolicy, now time.Time) {
	cooldown := time.Duration(0)
	if p.Spec.UpdateStrategy.GradualDecreaseConfig != nil && p.Spec.UpdateStrategy.GradualDecreaseConfig.Enabled {
		cooldown = time.Hour
	}
	if cfg := p.Spec.UpdateStrategy.Stability; cfg != nil {
		cooldown = cfg.Cooldown.Duration
	}
	for i := range recs {
		r := &recs[i]
		current, ok := s.Current[r.Container]
		if !ok {
			continue
		}
		state := s.Containers[r.Container]
		if r.CPU != nil {
			r.CPU = threshold(r.CPU, current.Requests.Cpu(), p, true)
		}
		if r.Memory != nil {
			r.Memory = threshold(r.Memory, current.Requests.Memory(), p, false)
			r.Memory = memoryFloor(r.Memory, current.Requests.Memory(), &state.InitialMemory, p)
			if p.LimitMode(false) == "Multiplier" {
				zero := resource.MustParse("0")
				r.MinimumMemoryLimit = memoryFloor(&zero, current.Limits.Memory(), &state.InitialLimit, p)
			}
		}
		if !s.LastChange.IsZero() && now.Sub(s.LastChange) < cooldown {
			if r.CPU != nil && state.AcceptedCPU != nil {
				r.CPU = copyQ(state.AcceptedCPU)
			}
			if r.Memory != nil && state.AcceptedMemory != nil {
				r.Memory = copyQ(state.AcceptedMemory)
				r.MinimumMemoryLimit = copyQ(state.AcceptedLimit)
			}
			r.Explanation += "; cooldown holds last accepted resources"
		} else {
			r.Explanation += "; memory safety and change thresholds applied"
		}
		if r.Memory != nil && p.LimitMode(false) == api.LimitAutomatic {
			limit := api.AutomaticMemoryLimit(*r.Memory, current.Limits.Memory())
			r.MinimumMemoryLimit = &limit
		}
	}
}

func threshold(target, current *resource.Quantity, p *api.OptimizationPolicy, cpu bool) *resource.Quantity {
	s := p.Spec.UpdateStrategy.Stability
	if s == nil {
		return target
	}
	value := func(q *resource.Quantity) float64 {
		if cpu {
			return float64(q.MilliValue())
		}
		return float64(q.Value())
	}
	diff := math.Abs(value(target) - value(current))
	minimum := s.MinimumMemoryChange
	if cpu {
		minimum = s.MinimumCPUChange
	}
	if (minimum != nil && diff < value(minimum)) || (value(current) > 0 && diff*100/value(current) < float64(s.MinimumChangePercent)) {
		return copyQ(current)
	}
	return target
}

func memoryFloor(target, current, initial *resource.Quantity, p *api.OptimizationPolicy) *resource.Quantity {
	if target.Cmp(*current) >= 0 {
		return copyQ(target)
	}
	if p.Spec.UpdateStrategy.AllowUnsafeMemoryDecrease != nil && *p.Spec.UpdateStrategy.AllowUnsafeMemoryDecrease {
		return copyQ(target)
	}
	g := p.Spec.UpdateStrategy.GradualDecreaseConfig
	if g == nil || !g.Enabled {
		return copyQ(current)
	}
	percent, total := 10, 70
	if g.MemoryDecreasePercentage != nil {
		percent = *g.MemoryDecreasePercentage
	}
	if g.MaximumTotalDecrease != nil {
		total = *g.MaximumTotalDecrease
	}
	floor := int64(math.Ceil(float64(initial.Value()) * (100 - float64(total)) / 100))
	// Small changes may proceed in one step, still subject to the total floor.
	diff := current.Value() - target.Value()
	if g.MinimumDecreaseThreshold == nil || diff >= g.MinimumDecreaseThreshold.Value() {
		step := int64(math.Ceil(float64(current.Value()) * (100 - float64(percent)) / 100))
		if step > floor {
			floor = step
		}
	}
	if target.Value() < floor {
		return resource.NewQuantity(floor, resource.BinarySI)
	}
	return copyQ(target)
}

func copyQ(q *resource.Quantity) *resource.Quantity {
	if q == nil {
		return nil
	}
	v := q.DeepCopy()
	return &v
}

// Accept is called only after successful application. Renewing freshness does
// not reset cooldowns. State is stored with a workload UID precondition.
func (s *State) Accept(ctx context.Context, c client.Client, w *discovery.Workload, recs []api.ContainerRecommendation, now time.Time) error {
	plan := map[string]ContainerState{}
	for _, r := range recs {
		v := s.Containers[r.Container]
		if v == nil {
			continue
		}
		v.AcceptedCPU = copyQ(r.CPU)
		v.AcceptedMemory = copyQ(r.Memory)
		v.AcceptedLimit = copyQ(r.MinimumMemoryLimit)
		plan[r.Container] = ContainerState{AcceptedCPU: v.AcceptedCPU, AcceptedMemory: v.AcceptedMemory, AcceptedLimit: v.AcceptedLimit}
	}
	data, _ := json.Marshal(plan)
	hash := sha256.Sum256(data)
	next := hex.EncodeToString(hash[:])
	if next != s.PlanHash {
		s.LastChange = now
		s.PlanHash = next
	}
	return s.Save(ctx, c, w)
}

func (s *State) Save(ctx context.Context, c client.Client, w *discovery.Workload) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if len(data) > 64*1024 {
		return fmt.Errorf("safety state exceeds 64KiB")
	}
	obj := w.Object.DeepCopyObject().(client.Object)
	if err := c.Get(ctx, client.ObjectKeyFromObject(w.Object), obj); err != nil {
		return err
	}
	if string(data) == obj.GetAnnotations()[StateAnnotation] {
		return nil
	}
	patch, _ := json.Marshal(map[string]any{"metadata": map[string]any{"uid": w.Object.GetUID(), "resourceVersion": obj.GetResourceVersion(), "annotations": map[string]string{StateAnnotation: string(data)}}})
	return c.Patch(ctx, obj, client.RawPatch(types.MergePatchType, patch))
}
