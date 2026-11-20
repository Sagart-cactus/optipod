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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
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
	observedAnnotation string
	Pods               []corev1.Pod                           `json:"-"`
	Containers         map[string]*ContainerState             `json:"containers"`
	LastChange         time.Time                              `json:"lastChange,omitempty"`
	PlanHash           string                                 `json:"planHash,omitempty"`
	Current            map[string]corev1.ResourceRequirements `json:"-"`
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
	s, err := storedState(obj)
	if err != nil {
		return nil, err
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

func storedState(obj client.Object) (*State, error) {
	s := &State{Containers: map[string]*ContainerState{}, Current: map[string]corev1.ResourceRequirements{}}
	s.observedAnnotation = obj.GetAnnotations()[StateAnnotation]
	if s.observedAnnotation != "" {
		if err := json.Unmarshal([]byte(s.observedAnnotation), s); err != nil {
			return nil, fmt.Errorf("invalid safety state: %w", err)
		}
	}
	if s.Containers == nil {
		s.Containers = map[string]*ContainerState{}
	}
	return s, nil
}

// Accept records the plan only after the workload mutation succeeds. Rebase
// the accepted quantities on each read to preserve concurrent OOM observations.
func (s *State) Accept(ctx context.Context, c client.Client, w *discovery.Workload, recs []api.ContainerRecommendation, now time.Time) error {
	return retry.RetryOnConflict(wait.Backoff{Steps: 4, Duration: 200 * time.Millisecond, Factor: 2, Jitter: 0.1}, func() error {
		obj := w.Object.DeepCopyObject().(client.Object)
		if err := c.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			return err
		}
		if obj.GetUID() != w.Object.GetUID() {
			return fmt.Errorf("workload identity changed before recording accepted plan")
		}
		latest, err := storedState(obj)
		if err != nil {
			return err
		}
		prior := latest.PlanHash
		if err := latest.acceptPlan(recs, now); err != nil {
			return err
		}
		if prior != s.PlanHash && prior != latest.PlanHash {
			return fmt.Errorf("a different accepted plan was recorded during application")
		}
		if err := latest.saveTo(ctx, c, obj); err != nil {
			return err
		}
		s.Containers, s.PlanHash, s.LastChange, s.observedAnnotation = latest.Containers, latest.PlanHash, latest.LastChange, latest.observedAnnotation
		return nil
	})
}

func (s *State) acceptPlan(recs []api.ContainerRecommendation, now time.Time) error {
	plan := map[string]ContainerState{}
	for _, r := range recs {
		v := s.Containers[r.Container]
		if v == nil {
			return fmt.Errorf("missing observed safety state for container %s", r.Container)
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
	return nil
}

func (s *State) Save(ctx context.Context, c client.Client, w *discovery.Workload) error {
	obj := w.Object.DeepCopyObject().(client.Object)
	if err := c.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
		return err
	}
	if obj.GetUID() != w.Object.GetUID() {
		return fmt.Errorf("workload identity changed before saving safety state")
	}
	if s.observedAnnotation != obj.GetAnnotations()[StateAnnotation] {
		gvr, err := discovery.GVR(w.Kind)
		if err != nil {
			return err
		}
		return apierrors.NewConflict(gvr.GroupResource(), w.Name, fmt.Errorf("concurrent safety state update"))
	}
	return s.saveTo(ctx, c, obj)
}

func (s *State) saveTo(ctx context.Context, c client.Client, obj client.Object) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if len(data) > 64*1024 {
		return fmt.Errorf("safety state exceeds 64KiB")
	}
	if string(data) == obj.GetAnnotations()[StateAnnotation] {
		return nil
	}
	patch, _ := json.Marshal(map[string]any{"metadata": map[string]any{"uid": obj.GetUID(), "resourceVersion": obj.GetResourceVersion(), "annotations": map[string]string{StateAnnotation: string(data)}}})
	if err := c.Patch(ctx, obj, client.RawPatch(types.MergePatchType, patch)); err != nil {
		return err
	}
	s.observedAnnotation = string(data)
	return nil
}
