package webhook

import (
	"context"
	"fmt"
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/discovery"
	"github.com/optipod/optipod/internal/history"
	"github.com/optipod/optipod/internal/policy"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func rolloutReady(obj *unstructured.Unstructured) bool {
	if obj.GetAPIVersion() != "argoproj.io/v1alpha1" || obj.GetKind() != kindRollout || !specObserved(obj) {
		return false
	}
	paused, _, _ := unstructured.NestedBool(obj.Object, "spec", "paused")
	aborted, _, _ := unstructured.NestedBool(obj.Object, "status", "abort")
	pauses, _, _ := unstructured.NestedSlice(obj.Object, "status", "pauseConditions")
	phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
	want, found, _ := unstructured.NestedInt64(obj.Object, "spec", "replicas")
	if !found {
		want = 1
	}
	ready, _, _ := unstructured.NestedInt64(obj.Object, "status", "readyReplicas")
	updated, _, _ := unstructured.NestedInt64(obj.Object, "status", "updatedReplicas")
	total, _, _ := unstructured.NestedInt64(obj.Object, "status", "replicas")
	return !paused && !aborted && len(pauses) == 0 && phase == "Healthy" && want > 0 && ready == want && updated == want && total == want
}

// ReplicaSets do not replace existing pods after a template edit. Evict one
// mismatching pod at a time, using the API's PDB enforcement and UID precondition.
func (rc *RolloutController) restartReplicaSet(ctx context.Context, rs *appsv1.ReplicaSet) error {
	list := &corev1.PodList{}
	if err := rc.client.List(ctx, list, client.InNamespace(rs.Namespace)); err != nil {
		return err
	}
	desired, err := rc.parseStoredPlan(ctx, rs)
	if err != nil {
		return err
	}
	for _, pod := range list.Items {
		owner := metav1.GetControllerOf(&pod)
		if owner == nil || owner.UID != rs.UID || pod.DeletionTimestamp != nil {
			continue
		}
		if history.Matches(history.Resources(&pod.Spec), desired) {
			continue
		}
		current := &appsv1.ReplicaSet{}
		if err := rc.client.Get(ctx, client.ObjectKeyFromObject(rs), current); err != nil {
			return err
		}
		if current.UID != rs.UID || current.Annotations[RecommendationHash] != rs.Annotations[RecommendationHash] || !rc.IsWorkloadReady(current) {
			return fmt.Errorf("ReplicaSet changed or is not ready before eviction")
		}
		winner, err := policy.NewPolicySelector(rc.client).SelectBestPolicy(ctx, &discovery.Workload{Kind: "ReplicaSet", Namespace: rs.Namespace, Name: rs.Name, Object: current})
		if err != nil {
			return err
		}
		if winner.Spec.Mode != api.ModeAuto || !ValidRecommendations(current.Annotations, winner, time.Now()) || !winner.Spec.UpdateStrategy.AllowRecreate {
			return fmt.Errorf("ReplicaSet eviction policy is no longer valid")
		}
		uid := pod.UID
		eviction := &policyv1.Eviction{ObjectMeta: metav1.ObjectMeta{Name: pod.Name, Namespace: pod.Namespace}, DeleteOptions: &metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}}
		if err := rc.client.SubResource("eviction").Create(ctx, &pod, eviction); err != nil {
			return err
		}
		if err := wait.PollUntilContextTimeout(ctx, time.Second, 90*time.Second, true, func(ctx context.Context) (bool, error) {
			return rc.replicaSetReplacementReady(ctx, rs, uid)
		}); err != nil {
			return fmt.Errorf("ReplicaSet replacement not ready: %w", err)
		}
	}
	// Persist idempotency only after every replacement is ready.
	current := &appsv1.ReplicaSet{}
	if err := rc.client.Get(ctx, client.ObjectKeyFromObject(rs), current); err != nil {
		return err
	}
	if current.UID != rs.UID || current.Annotations[RecommendationHash] != rs.Annotations[RecommendationHash] {
		return fmt.Errorf("ReplicaSet recommendation changed")
	}
	current.Annotations[RolloutHash] = rs.Annotations[RolloutHash]
	return rc.client.Update(ctx, current)
}
func (rc *RolloutController) parseStoredPlan(ctx context.Context, rs *appsv1.ReplicaSet) (map[string]map[string]any, error) {
	if rs.Annotations[history.Paused] == trueValue {
		record, err := history.Read(ctx, rc.client, rs.Namespace, rs.Annotations[history.RollbackTo])
		if err != nil {
			return nil, err
		}
		if record.Workload.UID != rs.UID || record.Digest != rs.Annotations[history.LatestDigest] {
			return nil, fmt.Errorf("invalid rollback journal binding")
		}
		return history.Restoration(record)
	}
	winner, err := policy.NewPolicySelector(rc.client).SelectBestPolicy(ctx, &discovery.Workload{Kind: "ReplicaSet", Namespace: rs.Namespace, Name: rs.Name, Object: rs})
	if err != nil {
		return nil, err
	}
	m := &Mutator{}
	recs := m.parseRecommendationAnnotations(&corev1.Pod{Spec: rs.Spec.Template.Spec}, rs.Annotations)
	plan := map[string]map[string]any{}
	for _, r := range recs {
		requests, limits := map[string]any{}, map[string]any{}
		if r.CPU != nil {
			requests["cpu"] = r.CPU.String()
			if winner.LimitMode(true) == limitNone {
				limits["cpu"] = nil
			}
		}
		if r.Memory != nil {
			requests["memory"] = r.Memory.String()
			if winner.LimitMode(false) == limitNone {
				limits["memory"] = nil
			}
		}
		if r.CPULimit != nil {
			limits["cpu"] = r.CPULimit.String()
		}
		if r.MemoryLimit != nil {
			limits["memory"] = r.MemoryLimit.String()
		}
		plan[r.ContainerName] = map[string]any{"requests": requests, "limits": limits}
	}
	if len(plan) == 0 {
		return nil, fmt.Errorf("no resource plan available for ReplicaSet restart")
	}
	return plan, nil
}

func (rc *RolloutController) replicaSetReplacementReady(ctx context.Context, rs *appsv1.ReplicaSet, uid types.UID) (bool, error) {
	pods := &corev1.PodList{}
	if err := rc.client.List(ctx, pods, client.InNamespace(rs.Namespace)); err != nil {
		return false, err
	}
	n := int32(0)
	for _, p := range pods.Items {
		owner := metav1.GetControllerOf(&p)
		if owner == nil || owner.UID != rs.UID {
			continue
		}
		if p.UID == uid || p.DeletionTimestamp != nil {
			return false, nil
		}
		for _, c := range p.Status.Conditions {
			if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
				n++
			}
		}
	}
	want := int32(1)
	if rs.Spec.Replicas != nil {
		want = *rs.Spec.Replicas
	}
	return n == want, nil
}
