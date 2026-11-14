// Package horizontal manages explicitly opted-in native HPAs and never adopts
// an existing scaler. CPU/memory utilization requests are protected by coexist.
package horizontal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/coexist"
	"github.com/optipod/optipod/internal/discovery"
	"github.com/optipod/optipod/internal/history"
	"github.com/optipod/optipod/internal/policy"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const Managed = "optipod.io/managed-hpa"
const policyLabel = "optipod.io/hpa-policy"
const workloadLabel = "optipod.io/hpa-workload-uid"
const specHash = "optipod.io/hpa-spec-hash"

func hash(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func Name(uid types.UID) string { return "optipod-" + hash(string(uid))[:24] }
func active(p *api.OptimizationPolicy, w client.Object) bool {
	return p.Spec.Mode == api.ModeAuto && p.Spec.HorizontalScaling != nil && w.GetDeletionTimestamp() == nil && w.GetAnnotations()[history.Paused] != labelTrue && w.GetAnnotations()[history.RollbackTo] == ""
}
func owned(h *autoscalingv2.HorizontalPodAutoscaler) bool {
	o := metav1.GetControllerOf(h)
	return h.Labels[Managed] == labelTrue && o != nil && string(o.UID) == h.Labels[workloadLabel] && h.Name == Name(o.UID)
}
func remove(ctx context.Context, c client.Client, h *autoscalingv2.HorizontalPodAutoscaler) error {
	uid, rv := h.UID, h.ResourceVersion
	err := c.Delete(ctx, h, &client.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &rv}})
	return client.IgnoreNotFound(err)
}
func Reconcile(ctx context.Context, c client.Client, w *discovery.Workload, p *api.OptimizationPolicy, dryRun bool) error {
	if dryRun {
		return nil
	}
	current := w.Object.DeepCopyObject().(client.Object)
	if err := c.Get(ctx, client.ObjectKeyFromObject(w.Object), current); err != nil {
		return err
	}
	if current.GetUID() != w.Object.GetUID() {
		return fmt.Errorf("horizontal target identity changed")
	}
	winner, err := policy.NewPolicySelector(c).SelectBestPolicy(ctx, &discovery.Workload{Kind: w.Kind, Name: w.Name, Namespace: w.Namespace, Object: current})
	if err != nil {
		return err
	}
	if winner == nil || winner.UID != p.UID || winner.Generation != p.Generation {
		return fmt.Errorf("horizontal winning policy changed")
	}
	list := &autoscalingv2.HorizontalPodAutoscalerList{}
	if err := c.List(ctx, list, client.InNamespace(w.Namespace)); err != nil {
		return err
	}
	var ours *autoscalingv2.HorizontalPodAutoscaler
	for i := range list.Items {
		h := &list.Items[i]
		if h.Spec.ScaleTargetRef.Kind == w.Kind && h.Spec.ScaleTargetRef.Name == w.Name && owned(h) && h.Labels[workloadLabel] == string(current.GetUID()) {
			ours = h
		}
	}
	if !active(p, current) {
		if ours != nil {
			return remove(ctx, c, ours)
		}
		return nil
	}
	if err := p.ValidateCreate(); err != nil {
		return err
	}
	gvr, err := discovery.GVR(w.Kind)
	if err != nil {
		return err
	}
	restrictions, err := coexist.Read(ctx, c, w.Namespace, w.Kind, w.Name)
	if err != nil {
		return err
	}
	if err := yieldToExistingScaler(ctx, c, ours, restrictions.Targets); err != nil {
		return err
	}
	h := p.Spec.HorizontalScaling
	desired := autoscalingv2.HorizontalPodAutoscalerSpec{ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{APIVersion: gvr.GroupVersion().String(), Kind: w.Kind, Name: w.Name}, MinReplicas: &h.MinReplicas, MaxReplicas: h.MaxReplicas, Metrics: h.Metrics, Behavior: h.Behavior}
	if ours != nil {
		if ours.Annotations == nil || ours.Annotations["optipod.io/hpa-desired-hash"] == "" {
			return fmt.Errorf("managed HPA ownership metadata was edited externally")
		}
		// HPA defaulting is included in the saved spec hash after creation/update.
		if ours.Annotations[specHash] == "" || ours.Annotations[specHash] != hash(ours.Spec) {
			return fmt.Errorf("managed HPA was edited externally; refusing to overwrite drift")
		}
		if ours.Annotations["optipod.io/hpa-desired-hash"] == hash(desired) && ours.Labels["optipod.io/hpa-policy-uid"] == string(p.UID) {
			return nil
		}
		ours.Spec = *desired.DeepCopy()
		ours.Annotations["optipod.io/hpa-desired-hash"] = hash(desired)
		ours.Annotations[specHash] = ""
		ours.Labels[policyLabel] = hash(client.ObjectKeyFromObject(p).String())[:32]
		ours.Labels["optipod.io/hpa-policy-uid"] = string(p.UID)
		return persist(ctx, c, ours, false)
	}
	object := &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: Name(current.GetUID()), Namespace: w.Namespace, Labels: map[string]string{Managed: labelTrue, workloadLabel: string(current.GetUID()), policyLabel: hash(client.ObjectKeyFromObject(p).String())[:32], "optipod.io/hpa-policy-uid": string(p.UID)}, Annotations: map[string]string{"optipod.io/hpa-desired-hash": hash(desired)}, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(current, gvr.GroupVersion().WithKind(w.Kind))}}, Spec: desired}
	return persist(ctx, c, object, true)
}

// Default through the API before persisting so the drift fingerprint is atomic
// with the spec, including native HPA defaults. A crash cannot leave an
// unprotected interval between creating the scaler and recording its fingerprint.
func persist(ctx context.Context, c client.Client, h *autoscalingv2.HorizontalPodAutoscaler, create bool) error {
	probe := h.DeepCopy()
	var err error
	if create {
		err = c.Create(ctx, probe, client.DryRunAll)
	} else {
		err = c.Update(ctx, probe, client.DryRunAll)
	}
	if err != nil {
		return err
	}
	h.Spec = *probe.Spec.DeepCopy()
	h.Annotations[specHash] = hash(h.Spec)
	if create {
		return c.Create(ctx, h)
	}
	return c.Update(ctx, h)
}

// Cleanup removes only verified managed HPAs whose original policy no longer
// selects/enables that workload. Called even for deleted or Disabled policies.
func Cleanup(ctx context.Context, c client.Client, key client.ObjectKey, dryRun bool) error {
	if dryRun {
		return nil
	}
	list := &autoscalingv2.HorizontalPodAutoscalerList{}
	if err := c.List(ctx, list, client.MatchingLabels{Managed: labelTrue, policyLabel: hash(key.String())[:32]}); err != nil {
		return err
	}
	for i := range list.Items {
		h := &list.Items[i]
		if !owned(h) {
			continue
		}
		owner := metav1.GetControllerOf(h)
		obj, err := discovery.WorkloadObject(owner.Kind)
		if err != nil {
			return err
		}
		err = c.Get(ctx, client.ObjectKey{Namespace: h.Namespace, Name: owner.Name}, obj)
		if apierrors.IsNotFound(err) {
			if err := remove(ctx, c, h); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if obj.GetUID() != owner.UID {
			if err := remove(ctx, c, h); err != nil {
				return err
			}
			continue
		}
		// Verify namespace reads before evaluating selectors; failures must not be
		// mistaken for an intentional policy deselection.
		if err := c.Get(ctx, client.ObjectKey{Name: h.Namespace}, &corev1.Namespace{}); err != nil {
			return err
		}
		matches, err := policy.NewPolicySelector(c).GetMatchingPolicies(ctx, &discovery.Workload{Kind: owner.Kind, Namespace: h.Namespace, Name: owner.Name, Object: obj})
		if err != nil {
			return err
		}
		keep := false
		if len(matches) > 0 {
			p := matches[0].Policy
			keep = string(p.UID) == h.Labels["optipod.io/hpa-policy-uid"] && active(p, obj)
		}
		if !keep {
			if err := remove(ctx, c, h); err != nil {
				return err
			}
		}
	}
	return nil
}

func yieldToExistingScaler(ctx context.Context, c client.Client, ours *autoscalingv2.HorizontalPodAutoscaler, targets []string) error {
	for _, target := range targets {
		if ours != nil && target == "HPA "+ours.Name {
			continue
		}
		if ours != nil {
			if err := remove(ctx, c, ours); err != nil {
				return err
			}
		}
		return fmt.Errorf("managed HPA blocked by existing %s", target)
	}
	return nil
}
