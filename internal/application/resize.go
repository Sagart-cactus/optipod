package application

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/discovery"
	"github.com/optipod/optipod/internal/policy"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var podGVR = schema.GroupVersionResource{Version: "v1", Resource: "pods"}

func (e *Engine) ownedPods(ctx context.Context, w *Workload) ([]corev1.Pod, error) {
	list := &corev1.PodList{}
	if err := e.client.List(ctx, list, client.InNamespace(w.Namespace)); err != nil {
		return nil, err
	}
	var pods []corev1.Pod
	for i := range list.Items {
		p := &list.Items[i]
		owner, err := discovery.ResolvePodWorkload(ctx, e.client, p)
		if err != nil || owner.Object.GetUID() != w.Object.GetUID() {
			continue
		}
		if p.DeletionTimestamp != nil || p.Status.Phase == corev1.PodFailed || p.Status.Phase == corev1.PodSucceeded {
			continue
		}
		pods = append(pods, *p)
	}
	return pods, nil
}

func (e *Engine) guardRestart(ctx context.Context, w *Workload) error {
	obj, err := e.convertToClientObject(w)
	if err != nil {
		return err
	}
	if !e.rolloutController.IsWorkloadReady(obj) {
		return fmt.Errorf("restart blocked: workload is not fully ready")
	}
	pods, err := e.ownedPods(ctx, w)
	if err != nil {
		return err
	}
	budgets := &policyv1.PodDisruptionBudgetList{}
	if err = e.client.List(ctx, budgets, client.InNamespace(w.Namespace)); err != nil {
		return err
	}
	for _, b := range budgets.Items {
		selector, err := metav1.LabelSelectorAsSelector(b.Spec.Selector)
		if err != nil {
			return err
		}
		for _, pod := range pods {
			if selector.Matches(labels.Set(pod.Labels)) && (b.Status.ObservedGeneration < b.Generation || b.Status.DisruptionsAllowed < 1) {
				return fmt.Errorf("restart blocked by PodDisruptionBudget %s", b.Name)
			}
		}
	}
	return nil
}

func (e *Engine) applyInPlace(ctx context.Context, w *Workload, recs []ContainerRecommendation, p *api.OptimizationPolicy) (*ApplyResult, error) {
	pods, err := e.ownedPods(ctx, w)
	if err != nil {
		return nil, err
	}
	if len(pods) == 0 {
		return nil, fmt.Errorf("resize deferred: no active pods")
	}
	patches := map[string][]byte{}
	for _, pod := range pods {
		if pod.Status.Phase != corev1.PodRunning {
			return nil, fmt.Errorf("resize deferred: pod %s is not running", pod.Name)
		}
		containerFields := map[string]any{}
		for _, field := range []string{fieldContainers, fieldInitContainers} {
			containerFields[field] = []map[string]any{}
		}
		for _, r := range recs {
			for _, slot := range discovery.ContainerSlots(&pod.Spec) {
				co := slot.Container
				if co.Name != r.Container {
					continue
				}
				resources, err := e.resourceMap(r.Recommendation, p)
				if err != nil {
					return nil, err
				}
				if p.LimitMode(false) == api.LimitAutomatic && !r.Recommendation.PreserveMemory && !isRollback(ctx) {
					limit := api.AutomaticMemoryLimit(r.Recommendation.Memory, r.Recommendation.MinimumMemoryLimit)
					if co.Resources.Limits.Memory().Cmp(limit) > 0 {
						return nil, fmt.Errorf("resize deferred: memory limit changed during planning")
					}
				}
				containerFields[slot.Field] = append(containerFields[slot.Field].([]map[string]any), map[string]any{fieldName: r.Container, fieldResources: resources})
			}
		}
		patch, _ := json.Marshal(map[string]any{fieldMetadata: map[string]any{"uid": pod.UID, "resourceVersion": pod.ResourceVersion}, "spec": containerFields})
		// Validate all replicas before changing any one. The API checks QoS,
		// LimitRange, quota and kubelet resize-policy constraints.
		_, err = e.dynamicClient.Resource(podGVR).Namespace(w.Namespace).Patch(ctx, pod.Name, types.StrategicMergePatchType, patch, metav1.PatchOptions{DryRun: []string{metav1.DryRunAll}}, "resize")
		if err != nil {
			if !p.Spec.UpdateStrategy.AllowRecreate {
				return nil, fmt.Errorf("resize preflight failed for %s: %w", pod.Name, err)
			}
			fallback := p.DeepCopy()
			fallback.Spec.UpdateStrategy.AllowInPlaceResize = false
			if err := e.validateConstraints(ctx, w, recs, fallback); err != nil {
				return nil, err
			}
			if err := e.guardRestart(ctx, w); err != nil {
				return nil, err
			}
			result, err := e.ApplyWithWebhook(ctx, w, recs, p)
			if result != nil {
				result.Method = "GuardedRestart"
			}
			return result, err
		}
		patches[pod.Name] = patch
	}
	// Keep the plan for future admissions without changing the pod template.
	deferred := p.DeepCopy()
	mode := string(api.RolloutOnNextRestart)
	deferred.Spec.UpdateStrategy.RolloutStrategy = &mode
	if _, err = e.ApplyWithWebhook(ctx, w, recs, deferred); err != nil {
		return nil, err
	}
	for _, pod := range pods {
		obj, err := e.convertToClientObject(w)
		if err != nil {
			return nil, err
		}
		if err = e.client.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			return nil, err
		}
		winner, err := policy.NewPolicySelector(e.client).SelectBestPolicy(ctx, &discovery.Workload{Kind: w.Kind, Name: w.Name, Namespace: w.Namespace, Object: obj, Labels: obj.GetLabels()})
		if err != nil {
			return nil, err
		}
		if winner.UID != p.UID || winner.Generation != p.Generation || winner.Spec.Mode != api.ModeAuto {
			return nil, fmt.Errorf("resize blocked: policy changed")
		}
		if _, err = e.dynamicClient.Resource(podGVR).Namespace(w.Namespace).Patch(ctx, pod.Name, types.StrategicMergePatchType, patches[pod.Name], metav1.PatchOptions{}, "resize"); err != nil {
			return nil, fmt.Errorf("resize %s: %w", pod.Name, err)
		}
	}
	// An accepted API patch is not proof of kubelet convergence. Deferred or
	// infeasible allocation remains an explicit error and is retried safely.
	err = wait.PollUntilContextTimeout(ctx, time.Second, 45*time.Second, true, func(ctx context.Context) (bool, error) {
		return e.resizesConverged(ctx, pods, recs, p)
	})
	if err != nil {
		return nil, fmt.Errorf("resize pending kubelet convergence: %w", err)
	}
	return &ApplyResult{Method: "InPlace"}, nil
}

func (e *Engine) resizesConverged(ctx context.Context, pods []corev1.Pod, recs []ContainerRecommendation, p *api.OptimizationPolicy) (bool, error) {
	for _, before := range pods {
		pod := &corev1.Pod{}
		if err := e.client.Get(ctx, client.ObjectKeyFromObject(&before), pod); err != nil {
			return false, err
		}
		if pod.UID != before.UID {
			return false, fmt.Errorf("pod identity changed during resize")
		}
		for _, cond := range pod.Status.Conditions {
			if (cond.Type == corev1.PodResizePending || cond.Type == corev1.PodResizeInProgress) && cond.Status == corev1.ConditionTrue {
				return false, nil
			}
		}
		for _, r := range recs {
			for _, slot := range discovery.ContainerSlots(&pod.Spec) {
				co := slot.Container
				if co.Name != r.Container {
					continue
				}
				found := false
				for _, status := range append(append([]corev1.ContainerStatus{}, pod.Status.ContainerStatuses...), pod.Status.InitContainerStatuses...) {
					if status.Name != co.Name || status.Resources == nil {
						continue
					}
					found = true
					for _, name := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory} {
						if !p.ControlsResource(string(name)) {
							continue
						}
						want := co.Resources.Requests[name]
						actual := status.Resources.Requests[name]
						if want.Cmp(actual) != 0 {
							return false, nil
						}
						limit := co.Resources.Limits[name]
						allocated := status.Resources.Limits[name]
						if limit.Cmp(allocated) != 0 {
							return false, nil
						}
					}
				}
				if !found {
					return false, nil
				}
			}
		}
	}
	return true, nil
}
