package history

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/discovery"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const decisionLabel = "optipod.io/decision-history"
const decisionIndexLabel = "optipod.io/decision-index"

type DecisionSnapshot struct {
	Outcome         string                        `json:"outcome"`
	Reason          string                        `json:"reason"`
	Recommendations []api.ContainerRecommendation `json:"recommendations,omitempty"`
	AppliedRecord   string                        `json:"appliedRecord,omitempty"`
}

// ObserveDecision records outcome transitions, including missing metrics,
// constraints and Disabled mode. A separate index avoids touching the workload
// or displacing its guarded rollback target. Repeated identical outcomes do not
// create records. Immutable data is written before advancing the index.
func ObserveDecision(ctx context.Context, c client.Client, w *discovery.Workload, p *api.OptimizationPolicy, status *api.WorkloadStatus, processingErr error) error {
	current := w.Object.DeepCopyObject().(client.Object)
	if err := c.Get(ctx, client.ObjectKeyFromObject(w.Object), current); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if current.GetUID() != w.Object.GetUID() {
		return fmt.Errorf("decision workload identity changed")
	}
	decision := &DecisionSnapshot{Outcome: status.Status, Reason: status.Reason, Recommendations: status.Recommendations}
	if processingErr != nil {
		decision.Outcome = "Error"
		decision.Reason = processingErr.Error()
	}
	if status.Status == "Applied" {
		decision.AppliedRecord = current.GetAnnotations()[Latest]
	}
	// Expiry renewals are operational freshness, not new decisions. Keep the
	// original expiry in the record but omit it from the transition fingerprint.
	stable := *decision
	stable.Recommendations = append([]api.ContainerRecommendation(nil), decision.Recommendations...)
	for i := range stable.Recommendations {
		stable.Recommendations[i].ValidUntil = nil
	}
	fingerprint := hash(struct {
		Policy   Reference
		Spec     api.OptimizationPolicySpec
		Decision DecisionSnapshot
	}{
		Reference{UID: p.UID, Generation: p.Generation}, p.Spec, stable,
	})
	key := client.ObjectKey{Namespace: w.Namespace, Name: "optipod-decisions-" + hash(string(current.GetUID()))[:32]}
	index := &corev1.ConfigMap{}
	create := false
	if err := c.Get(ctx, key, index); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		create = true
		index = &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace, Labels: map[string]string{decisionIndexLabel: labelTrue, WorkloadLabel: string(current.GetUID())}}, Data: map[string]string{}}
	}
	if index.Labels[decisionIndexLabel] != labelTrue || index.Labels[WorkloadLabel] != string(current.GetUID()) {
		return fmt.Errorf("decision index identity changed")
	}
	if index.Data["fingerprint"] == fingerprint {
		return nil
	}
	previous := index.Data["latest"]
	name := "optipod-decision-" + hash(string(current.GetUID()) + previous + fingerprint)[:32]
	r := &Record{Version: 1, Name: name, Workload: Reference{Kind: w.Kind, Namespace: w.Namespace, Name: w.Name, UID: current.GetUID(), Generation: current.GetGeneration()}, Policy: Reference{Kind: policyKind, Namespace: p.Namespace, Name: p.Name, UID: p.UID, Generation: p.Generation}, Mode: p.Spec.Mode, PolicySpec: p.Spec.DeepCopy(), Decision: decision, PlanHash: fingerprint, Previous: previous, Phase: decision.Outcome, Method: "Decision", Created: time.Now().UTC(), Updated: time.Now().UTC()}
	if err := persistDecision(ctx, c, r); err != nil {
		return err
	}
	index.Data = map[string]string{"latest": name, "fingerprint": fingerprint}
	var err error
	if create {
		err = c.Create(ctx, index)
	} else {
		err = c.Update(ctx, index)
	}
	if err != nil {
		return err
	}
	return pruneDecisions(ctx, c, r)
}

func persistDecision(ctx context.Context, c client.Client, r *Record) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if len(data) > 512*1024 {
		return fmt.Errorf("decision snapshot exceeds 512KiB")
	}
	immutable := true
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: r.Name, Namespace: r.Workload.Namespace, Labels: map[string]string{Label: labelTrue, WorkloadLabel: string(r.Workload.UID), decisionLabel: labelTrue}}, Immutable: &immutable, Data: map[string]string{DataKey: string(data)}}
	if err := c.Create(ctx, cm); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func pruneDecisions(ctx context.Context, c client.Client, latest *Record) error {
	list := &corev1.ConfigMapList{}
	if err := c.List(ctx, list, client.InNamespace(latest.Workload.Namespace), client.MatchingLabels{decisionLabel: labelTrue, WorkloadLabel: string(latest.Workload.UID)}); err != nil {
		return err
	}
	sort.Slice(list.Items, func(i, j int) bool {
		return list.Items[i].CreationTimestamp.After(list.Items[j].CreationTimestamp.Time)
	})
	for i := range list.Items {
		cm := &list.Items[i]
		if cm.Name == latest.Name || (i < 64 && time.Since(cm.CreationTimestamp.Time) <= 30*24*time.Hour) {
			continue
		}
		if err := c.Delete(ctx, cm, client.Preconditions{UID: &cm.UID, ResourceVersion: &cm.ResourceVersion}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}
