// Package history persists bounded, per-workload optimization journals. Records
// are namespaced ConfigMaps so they survive controller and workload deletion.
package history

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/discovery"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	Label         = "optipod.io/history"
	WorkloadLabel = "optipod.io/workload-uid"
	Latest        = "optipod.io/latest-record"
	LatestDigest  = "optipod.io/latest-record-digest"
	Paused        = "optipod.io/paused"
	RollbackTo    = "optipod.io/rollback-to"
	DataKey       = "record.json"
)

type Reference struct {
	Kind, Namespace, Name string
	UID                   types.UID
	Generation            int64
}
type PodSnapshot struct {
	Name      string
	UID       types.UID
	Resources map[string]corev1.ResourceRequirements
}
type Record struct {
	PolicySpec          *api.OptimizationPolicySpec            `json:"policySpec,omitempty"`
	EffectivePolicySpec *api.OptimizationPolicySpec            `json:"effectivePolicySpec,omitempty"`
	Decision            *DecisionSnapshot                      `json:"decision,omitempty"`
	Node                string                                 `json:"node,omitempty"`
	AdmissionPod        string                                 `json:"admissionPod,omitempty"`
	AdmissionPodUID     types.UID                              `json:"admissionPodUID,omitempty"`
	Digest              string                                 `json:"-"`
	Version             int                                    `json:"version"`
	Name                string                                 `json:"name"`
	Workload            Reference                              `json:"workload"`
	Policy              Reference                              `json:"policy"`
	Mode                api.PolicyMode                         `json:"mode"`
	PlanHash            string                                 `json:"planHash"`
	Previous            string                                 `json:"previous,omitempty"`
	Plan                map[string]map[string]any              `json:"plan"`
	BeforeTemplate      map[string]corev1.ResourceRequirements `json:"beforeTemplate"`
	TemplateIdentity    string                                 `json:"templateIdentity"`
	BeforeAdmission     map[string]string                      `json:"beforeAdmission,omitempty"`
	BeforePods          []PodSnapshot                          `json:"beforePods,omitempty"`
	Phase               string                                 `json:"phase"`
	Method              string                                 `json:"method,omitempty"`
	Error               string                                 `json:"error,omitempty"`
	Created             time.Time                              `json:"created"`
	Updated             time.Time                              `json:"updated"`
	RollbackError       string                                 `json:"rollbackError,omitempty"`
}

func Resources(spec *corev1.PodSpec) map[string]corev1.ResourceRequirements {
	out := map[string]corev1.ResourceRequirements{}
	for _, slot := range discovery.ContainerSlots(spec) {
		out[slot.Container.Name] = *slot.Container.Resources.DeepCopy()
	}
	return out
}
func hash(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func Read(ctx context.Context, c client.Reader, namespace, name string) (*Record, error) {
	cm := &corev1.ConfigMap{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, cm); err != nil {
		return nil, err
	}
	if cm.Labels[Label] != labelTrue {
		return nil, fmt.Errorf("%s is not an OptiPod history record", name)
	}
	var r Record
	if err := json.Unmarshal([]byte(cm.Data[DataKey]), &r); err != nil {
		return nil, err
	}
	if r.Version != 1 || r.Name != cm.Name || r.Workload.Namespace != namespace || string(r.Workload.UID) != cm.Labels[WorkloadLabel] {
		return nil, fmt.Errorf("history record identity is invalid")
	}
	r.Digest = hash(cm.Data[DataKey])
	if phase := cm.Annotations["optipod.io/history-phase"]; phase != "" {
		r.Phase = phase
		r.Method = cm.Annotations["optipod.io/history-method"]
		r.Error = cm.Annotations["optipod.io/history-error"]
		r.RollbackError = cm.Annotations["optipod.io/rollback-error"]
		r.Updated, _ = time.Parse(time.RFC3339Nano, cm.Annotations["optipod.io/history-updated"])
	}
	return &r, nil
}

// Prepare is durable before a mutation starts. Deterministic IDs resume an
// interrupted attempt; freshness-only renewals reuse the same completed record.
func Prepare(ctx context.Context, c client.Client, w *discovery.Workload, p *api.OptimizationPolicy, plan map[string]map[string]any) (*Record, error) {
	current := w.Object.DeepCopyObject().(client.Object)
	if err := c.Get(ctx, client.ObjectKeyFromObject(w.Object), current); err != nil {
		return nil, err
	}
	if current.GetUID() != w.Object.GetUID() {
		return nil, fmt.Errorf("workload identity changed before journal preparation")
	}
	fingerprint := hash(struct {
		Plan       map[string]map[string]any
		Policy     types.UID
		Generation int64
		Mode       api.PolicyMode
	}{plan, p.UID, p.Generation, p.Spec.Mode})
	previous := current.GetAnnotations()[Latest]
	if previous != "" {
		last, err := Read(ctx, c, w.Namespace, previous)
		if err != nil {
			return nil, fmt.Errorf("read latest history: %w", err)
		}
		if last.Workload.UID != w.Object.GetUID() || last.Digest != current.GetAnnotations()[LatestDigest] {
			return nil, fmt.Errorf("latest history binding is invalid")
		}
		if last.PlanHash == fingerprint && last.Phase != "RolledBack" {
			return last, nil
		}
	}
	name := "optipod-record-" + hash(string(w.Object.GetUID()) + previous + fingerprint)[:32]
	if r, err := Read(ctx, c, w.Namespace, name); err == nil {
		return r, nil
	} else if !apierrors.IsNotFound(err) {
		return nil, err
	}
	template, err := discovery.Template(current)
	if err != nil {
		return nil, err
	}
	r := &Record{Version: 1, Name: name, Workload: Reference{Kind: w.Kind, Namespace: w.Namespace, Name: w.Name, UID: w.Object.GetUID(), Generation: current.GetGeneration()}, Policy: Reference{Kind: policyKind, Namespace: p.Namespace, Name: p.Name, UID: p.UID, Generation: p.Generation}, Mode: p.Spec.Mode, PlanHash: fingerprint, Previous: previous, Plan: plan, BeforeTemplate: Resources(&template.Spec), Phase: "Prepared", Created: time.Now().UTC(), Updated: time.Now().UTC()}
	r.TemplateIdentity = TemplateIdentity(template)
	if err := snapshotPolicy(ctx, c, p, r); err != nil {
		return nil, err
	}
	r.BeforeAdmission = map[string]string{}
	for key, value := range current.GetAnnotations() {
		if strings.HasPrefix(key, "optipod.io/cpu-") || strings.HasPrefix(key, "optipod.io/memory-") || strings.HasPrefix(key, "optipod.io/applied-policy-") || key == "optipod.io/recommendation-expires-at" {
			r.BeforeAdmission[key] = value
		}
	}
	pods := &corev1.PodList{}
	if err := c.List(ctx, pods, client.InNamespace(w.Namespace)); err != nil {
		return nil, err
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.DeletionTimestamp != nil || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		owner, err := discovery.ResolvePodWorkload(ctx, c, pod)
		if err != nil || owner.Object.GetUID() != w.Object.GetUID() {
			continue
		}
		r.BeforePods = append(r.BeforePods, PodSnapshot{Name: pod.Name, UID: pod.UID, Resources: Resources(&pod.Spec)})
	}
	data, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	if len(data) > 512*1024 {
		return nil, fmt.Errorf("history snapshot exceeds 512KiB; optimization deferred")
	}
	immutable := true
	cm := &corev1.ConfigMap{Immutable: &immutable, ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: w.Namespace, Labels: map[string]string{Label: labelTrue, WorkloadLabel: string(w.Object.GetUID())}}, Data: map[string]string{DataKey: string(data)}}
	if err := c.Create(ctx, cm); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return Read(ctx, c, w.Namespace, name)
		}
		return nil, err
	}
	r.Digest = hash(string(data))
	return r, nil
}

func snapshotPolicy(ctx context.Context, c client.Client, p *api.OptimizationPolicy, r *Record) error {
	configured := &api.OptimizationPolicy{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(p), configured); err != nil {
		return err
	}
	if configured.UID != p.UID || configured.Generation != p.Generation {
		return fmt.Errorf("policy changed before history snapshot")
	}
	r.PolicySpec = configured.Spec.DeepCopy()
	r.EffectivePolicySpec = p.Spec.DeepCopy()
	return nil
}

func Save(ctx context.Context, c client.Client, r *Record) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cm := &corev1.ConfigMap{}
		if err := c.Get(ctx, client.ObjectKey{Namespace: r.Workload.Namespace, Name: r.Name}, cm); err != nil {
			return err
		}
		if cm.Labels[Label] != labelTrue || cm.Labels[WorkloadLabel] != string(r.Workload.UID) {
			return fmt.Errorf("history record identity changed")
		}
		if hash(cm.Data[DataKey]) != r.Digest {
			return fmt.Errorf("immutable history snapshot changed")
		}
		r.Updated = time.Now().UTC()
		if cm.Annotations == nil {
			cm.Annotations = map[string]string{}
		}
		cm.Annotations["optipod.io/history-phase"] = r.Phase
		cm.Annotations["optipod.io/history-method"] = r.Method
		cm.Annotations["optipod.io/history-error"] = r.Error
		cm.Annotations["optipod.io/rollback-error"] = r.RollbackError
		cm.Annotations["optipod.io/history-updated"] = r.Updated.Format(time.RFC3339Nano)
		return c.Update(ctx, cm)
	})
}

func Complete(ctx context.Context, c client.Client, w *discovery.Workload, r *Record, method string, applyErr error) error {
	if r.Phase == "RolledBack" {
		return fmt.Errorf("cannot complete a reverted record")
	}
	if applyErr != nil {
		if r.Phase != phaseApplied {
			r.Phase = "Failed"
		}
		r.Error = applyErr.Error()
		return Save(ctx, c, r)
	}
	if (r.Phase == phaseApplied || r.Phase == "Recommended") && r.Error == "" && w.Object.GetAnnotations()[Latest] == r.Name && w.Object.GetAnnotations()[LatestDigest] == r.Digest {
		return nil
	}
	r.Phase = phaseApplied
	if r.Mode == api.ModeRecommend {
		r.Phase = "Recommended"
	}
	r.Method = method
	r.Error = ""
	if err := Save(ctx, c, r); err != nil {
		return err
	}
	patch, _ := json.Marshal(map[string]any{"metadata": map[string]any{"uid": r.Workload.UID, "annotations": map[string]string{Latest: r.Name, LatestDigest: r.Digest}}})
	if err := c.Patch(ctx, w.Object, client.RawPatch(types.MergePatchType, patch)); err != nil {
		return err
	}
	return prune(ctx, c, r)
}

// Keep the latest 32 records and 30 days, always protecting the latest and
// in-progress records. Deleting a workload does not erase its audit history.
func prune(ctx context.Context, c client.Client, latest *Record) error {
	list := &corev1.ConfigMapList{}
	if err := c.List(ctx, list, client.InNamespace(latest.Workload.Namespace), client.MatchingLabels{Label: labelTrue, WorkloadLabel: string(latest.Workload.UID)}); err != nil {
		return err
	}
	sort.Slice(list.Items, func(i, j int) bool {
		return list.Items[i].CreationTimestamp.After(list.Items[j].CreationTimestamp.Time)
	})
	retained := 0
	for i := range list.Items {
		cm := &list.Items[i]
		if cm.Labels[decisionLabel] == labelTrue || cm.Labels[nodeObservation] == labelTrue {
			continue
		}
		phase := cm.Annotations["optipod.io/history-phase"]
		if phase == "" || phase == "Prepared" || phase == "RollbackPending" {
			continue
		}
		var r Record
		if json.Unmarshal([]byte(cm.Data[DataKey]), &r) != nil {
			continue
		}
		retained++
		if cm.Name == latest.Name || (retained <= 32 && time.Since(r.Created) <= 30*24*time.Hour) {
			continue
		}
		if err := c.Delete(ctx, cm, client.Preconditions{UID: &cm.UID, ResourceVersion: &cm.ResourceVersion}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// TemplateIdentity ignores resource changes and our rollout timestamp, but
// detects image, command, environment, volume and scheduling changes.
func TemplateIdentity(template *corev1.PodTemplateSpec) string {
	copy := template.DeepCopy()
	for i := range copy.Spec.Containers {
		copy.Spec.Containers[i].Resources = corev1.ResourceRequirements{}
	}
	for i := range copy.Spec.InitContainers {
		copy.Spec.InitContainers[i].Resources = corev1.ResourceRequirements{}
	}
	for key := range copy.Annotations {
		if strings.HasPrefix(key, "optipod.io/") {
			delete(copy.Annotations, key)
		}
	}
	return hash(copy)
}
