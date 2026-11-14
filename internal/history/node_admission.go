package history

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/constraints"
	"github.com/optipod/optipod/internal/discovery"
	"github.com/optipod/optipod/internal/nodesizing"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const nodeObservation = "optipod.io/node-admission-history"

// ObserveNodeAdmission journals a persisted admission receipt once its pod is
// observed. It is never a rollback target: per-node plans have no uniform inverse.
func ObserveNodeAdmission(ctx context.Context, c client.Client, w *discovery.Workload, pod *corev1.Pod) error {
	raw := pod.Annotations[nodesizing.ReceiptAnnotation]
	if raw == "" {
		return nil
	}
	var receipt nodesizing.Receipt
	if err := json.Unmarshal([]byte(raw), &receipt); err != nil {
		return fmt.Errorf("invalid node admission receipt: %w", err)
	}
	if receipt.WorkloadUID != w.Object.GetUID() || receipt.Node != pod.Spec.NodeName || receipt.PolicyUID == "" || receipt.PolicyName == "" || receipt.Created.IsZero() {
		return fmt.Errorf("node admission receipt identity is invalid")
	}
	owner, err := discovery.ResolvePodWorkload(ctx, c, pod)
	if err != nil || owner.Object.GetUID() != w.Object.GetUID() {
		return fmt.Errorf("node admission owner changed")
	}
	// A receipt describes resources admitted earlier; later resize/edit activity
	// must not be misreported as a successful admission of that older plan.
	candidate, err := constraints.Overlay(pod, receipt.Plan)
	if err != nil {
		return err
	}
	if hash(Resources(&candidate.Spec)) != hash(Resources(&pod.Spec)) {
		return nil
	}
	name := "optipod-node-record-" + hash(string(pod.UID) + raw)[:32]
	cm := &corev1.ConfigMap{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: name}, cm); err == nil {
		return nil
	} else if !apierrors.IsNotFound(err) {
		return err
	}
	record := Record{PolicySpec: receipt.PolicySpec, Version: 1, Name: name, Workload: Reference{Kind: w.Kind, Namespace: w.Namespace, Name: w.Name, UID: w.Object.GetUID(), Generation: w.Object.GetGeneration()}, Policy: Reference{Kind: policyKind, Namespace: receipt.PolicyNamespace, Name: receipt.PolicyName, UID: receipt.PolicyUID, Generation: receipt.PolicyGeneration}, Mode: api.ModeAuto, PlanHash: hash(receipt.Plan), Plan: receipt.Plan, BeforeTemplate: receipt.Before, Phase: "Observed", Method: "NodeAdmission", Created: receipt.Created, Updated: time.Now().UTC(), Node: receipt.Node, AdmissionPod: pod.Name, AdmissionPodUID: pod.UID}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	immutable := true
	cm = &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: pod.Namespace, Labels: map[string]string{Label: labelTrue, WorkloadLabel: string(w.Object.GetUID()), nodeObservation: labelTrue}}, Immutable: &immutable, Data: map[string]string{DataKey: string(data)}}
	if err := c.Create(ctx, cm); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	list := &corev1.ConfigMapList{}
	if err := c.List(ctx, list, client.InNamespace(pod.Namespace), client.MatchingLabels{nodeObservation: labelTrue, WorkloadLabel: string(w.Object.GetUID())}); err != nil {
		return err
	}
	sort.Slice(list.Items, func(i, j int) bool {
		return list.Items[i].CreationTimestamp.After(list.Items[j].CreationTimestamp.Time)
	})
	for i := range list.Items {
		cm := &list.Items[i]
		if i < 128 && time.Since(cm.CreationTimestamp.Time) <= 30*24*time.Hour {
			continue
		}
		if err := c.Delete(ctx, cm); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}
