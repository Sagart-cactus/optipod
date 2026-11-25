package capturedlimits

import (
	"context"
	"fmt"
	"time"

	"github.com/optipod/optipod/internal/coexist"
	"github.com/optipod/optipod/internal/discovery"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func tracked(limits corev1.ResourceList) corev1.ResourceList {
	result := corev1.ResourceList{}
	for _, key := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory} {
		if v, ok := limits[key]; ok {
			result[key] = v.DeepCopy()
		}
	}
	return result
}

// Read takes a consensus snapshot of a stable, fully Ready replica set.
// Verify owner UIDs because label selectors alone also match unrelated Pods.
func Read(ctx context.Context, c client.Reader, kind string, key client.ObjectKey, uid types.UID, includeHPARequests bool, nodeHandoff time.Duration) (*Snapshot, error) {
	return readCapture(ctx, c, kind, key, uid, includeHPARequests, nodeHandoff, 0, 0)
}

// ReadDeploymentMaximum records a bounded deferred handoff for interchangeable
// replicas. Missing limits are unbounded; missing requests reserve nothing.
func ReadDeploymentMaximum(ctx context.Context, c client.Reader, key client.ObjectKey, uid types.UID, includeHPARequests bool, duration time.Duration) (*Snapshot, error) {
	if duration <= 0 {
		return nil, fmt.Errorf("maximum replica handoff requires a positive duration")
	}
	return readCapture(ctx, c, "Deployment", key, uid, includeHPARequests, 0, duration, 0)
}

// ReadStatefulOrdinals keeps each existing replica's stable identity and baseline.
func ReadStatefulOrdinals(ctx context.Context, c client.Reader, key client.ObjectKey, uid types.UID, includeHPARequests bool, duration time.Duration) (*Snapshot, error) {
	if duration <= 0 {
		return nil, fmt.Errorf("ordinal handoff requires a positive duration")
	}
	return readCapture(ctx, c, "StatefulSet", key, uid, includeHPARequests, 0, 0, duration)
}
func readCapture(ctx context.Context, c client.Reader, kind string, key client.ObjectKey, uid types.UID, includeHPARequests bool, nodeHandoff, replicaHandoff, ordinalHandoff time.Duration) (*Snapshot, error) {
	if nodeHandoff < 0 || (nodeHandoff > 0 && kind != "DaemonSet") {
		return nil, fmt.Errorf("node handoff requires a DaemonSet and a positive duration")
	}
	obj, err := discovery.WorkloadObject(kind)
	if err != nil {
		return nil, err
	}
	if err = c.Get(ctx, key, obj); err != nil {
		return nil, err
	}
	if obj.GetUID() != uid || uid == "" || obj.GetDeletionTimestamp() != nil {
		return nil, fmt.Errorf("workload identity changed")
	}
	var replicas, ready, updated int32
	var selector *metav1.LabelSelector
	switch o := obj.(type) {
	case *appsv1.Deployment:
		replicas = *o.Spec.Replicas
		ready = o.Status.ReadyReplicas
		updated = o.Status.UpdatedReplicas
		selector = o.Spec.Selector
	case *appsv1.DaemonSet:
		if nodeHandoff > 0 && o.Status.ObservedGeneration != o.Generation {
			return nil, fmt.Errorf("DaemonSet generation has not been observed")
		}
		replicas = o.Status.DesiredNumberScheduled
		ready = o.Status.NumberReady
		updated = o.Status.UpdatedNumberScheduled
		selector = o.Spec.Selector
	case *appsv1.StatefulSet:
		replicas = *o.Spec.Replicas
		ready = o.Status.ReadyReplicas
		updated = o.Status.UpdatedReplicas
		selector = o.Spec.Selector
	default:
		return nil, fmt.Errorf("capture supports Deployment, StatefulSet and DaemonSet")
	}
	if replicas < 1 || ready != replicas || updated != replicas {
		return nil, fmt.Errorf("workload has no stable running replica set")
	}
	labels, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return nil, err
	}
	list := &corev1.PodList{}
	if err = c.List(ctx, list, client.InNamespace(key.Namespace), client.MatchingLabelsSelector{Selector: labels}); err != nil {
		return nil, err
	}
	template, err := discovery.Template(obj)
	if err != nil {
		return nil, err
	}
	declared := map[string]discovery.ContainerSlot{}
	for _, slot := range discovery.ContainerSlots(&template.Spec) {
		declared[slot.Container.Name] = slot
	}
	var restrictions *coexist.Restrictions
	if includeHPARequests {
		restrictions, err = coexist.Read(ctx, c, key.Namespace, kind, key.Name)
		if err != nil {
			return nil, err
		}
	}
	snapshot := &Snapshot{WorkloadUID: uid}
	if nodeHandoff > 0 {
		until := time.Now().UTC().Add(nodeHandoff)
		snapshot.HoldRequestsUntil = &until
		snapshot.Nodes = map[string]NodeCapture{}
	}
	if ordinalHandoff > 0 {
		snapshot.Ordinals = map[string]map[string]Container{}
	}
	if replicaHandoff > 0 || ordinalHandoff > 0 {
		until := time.Now().UTC().Add(max(replicaHandoff, ordinalHandoff))
		snapshot.HoldRequestsUntil = &until
	}
	count := int32(0)
	for i := range list.Items {
		pod := &list.Items[i]
		owner, err := discovery.ResolvePodWorkload(ctx, c, pod)
		if err != nil {
			return nil, err
		}
		if owner.Object.GetUID() != uid {
			continue
		}
		if pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning || !isReady(pod) {
			return nil, fmt.Errorf("pod %s is not stable", pod.Name)
		}
		count++
		current := map[string]Container{}
		for _, slot := range discovery.ContainerSlots(&pod.Spec) {
			name := slot.Container.Name
			entry := Container{Field: slot.Field, Limits: tracked(slot.Container.Resources.Limits)}
			if src, exists := declared[name]; exists {
				if src.Field != slot.Field {
					return nil, fmt.Errorf("container %s changed type", name)
				}
				entry.Template = tracked(src.Container.Resources.Limits)
			}
			for _, dim := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory} {
				if restrictions.Reason(name, string(dim)) == "" {
					continue
				}
				q, ok := slot.Container.Resources.Requests[dim]
				if !ok || q.Sign() <= 0 {
					return nil, fmt.Errorf("pod %s lacks a positive HPA-owned %s request for %s", pod.Name, dim, name)
				}
				if entry.HPARequests == nil {
					entry.HPARequests = corev1.ResourceList{}
				}
				entry.HPARequests[dim] = q.DeepCopy()
				if src, declaredHere := declared[name]; declaredHere {
					if tq, has := src.Container.Resources.Requests[dim]; has {
						if entry.TemplateRequests == nil {
							entry.TemplateRequests = corev1.ResourceList{}
						}
						entry.TemplateRequests[dim] = tq.DeepCopy()
					}
				}
			}
			if nodeHandoff > 0 || replicaHandoff > 0 || ordinalHandoff > 0 {
				running := tracked(slot.Container.Resources.Requests)
				entry.RunningRequests = &running
				if src, ok := declared[name]; ok {
					entry.TemplateRequests = tracked(src.Container.Resources.Requests)
				}
			}
			current[name] = entry
		}
		for name := range declared {
			if _, ok := current[name]; !ok {
				return nil, fmt.Errorf("pod %s lacks template container %s", pod.Name, name)
			}
		}
		if nodeHandoff > 0 {
			name := pod.Spec.NodeName
			if name == "" {
				return nil, fmt.Errorf("pod %s has no node", pod.Name)
			}
			if _, duplicate := snapshot.Nodes[name]; duplicate {
				return nil, fmt.Errorf("multiple stable pods on node %s", name)
			}
			node := &corev1.Node{}
			if err := c.Get(ctx, client.ObjectKey{Name: name}, node); err != nil {
				return nil, err
			}
			if node.UID == "" || node.DeletionTimestamp != nil || node.CreationTimestamp.After(pod.CreationTimestamp.Time) {
				return nil, fmt.Errorf("node %s changed during capture", name)
			}
			snapshot.Nodes[name] = NodeCapture{UID: node.UID, Containers: current}
			continue
		}
		if ordinalHandoff > 0 {
			index, err := ordinalKey(pod, uid)
			if err != nil {
				return nil, err
			}
			if _, exists := snapshot.Ordinals[index]; exists {
				return nil, fmt.Errorf("duplicate StatefulSet ordinal %s", index)
			}
			snapshot.Ordinals[index] = current
			continue
		}
		if snapshot.Containers == nil {
			snapshot.Containers = current
			continue
		}
		if len(snapshot.Containers) != len(current) {
			return nil, fmt.Errorf("replicas have different container sets")
		}
		for name, prev := range snapshot.Containers {
			other, ok := current[name]
			if !ok || other.Field != prev.Field {
				return nil, fmt.Errorf("replicas have different container sets")
			}
			if replicaHandoff > 0 {
				for _, dim := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory} {
					upper, bounded := other.Limits[dim]
					prior, wasBounded := prev.Limits[dim]
					if !bounded {
						delete(prev.Limits, dim)
					} else if wasBounded && prior.Cmp(upper) < 0 {
						prev.Limits[dim] = upper.DeepCopy()
					}
					maximize(*prev.RunningRequests, *other.RunningRequests, dim)
					maximize(prev.HPARequests, other.HPARequests, dim)
				}
				snapshot.Containers[name] = prev
				continue
			}
			if !Same(prev.Limits, other.Limits, corev1.ResourceCPU) || !Same(prev.Limits, other.Limits, corev1.ResourceMemory) || !Same(prev.HPARequests, other.HPARequests, corev1.ResourceCPU) || !Same(prev.HPARequests, other.HPARequests, corev1.ResourceMemory) {
				return nil, fmt.Errorf("replicas disagree on container %s", name)
			}
		}
	}
	if count != replicas {
		return nil, fmt.Errorf("found %d stable owned pods, expected %d", count, replicas)
	}
	generation := obj.GetGeneration()
	if err := c.Get(ctx, key, obj); err != nil {
		return nil, err
	}
	if obj.GetUID() != uid || obj.GetGeneration() != generation || obj.GetDeletionTimestamp() != nil {
		return nil, fmt.Errorf("workload changed during capture")
	}
	return snapshot, nil
}
func isReady(p *corev1.Pod) bool {
	for _, v := range p.Status.Conditions {
		if v.Type == corev1.PodReady {
			return v.Status == corev1.ConditionTrue
		}
	}
	return false
}

func maximize(dst, src corev1.ResourceList, key corev1.ResourceName) {
	q, found := src[key]
	if !found {
		return
	}
	old, exists := dst[key]
	if !exists || old.Cmp(q) < 0 {
		dst[key] = q.DeepCopy()
	}
}
