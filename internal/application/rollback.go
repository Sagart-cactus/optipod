package application

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/coexist"
	"github.com/optipod/optipod/internal/discovery"
	"github.com/optipod/optipod/internal/history"
	"github.com/optipod/optipod/internal/policy"
	"github.com/optipod/optipod/internal/recommendation"
	"github.com/optipod/optipod/internal/safety"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type rollbackContextKey struct{}

func isRollback(ctx context.Context) bool { return ctx.Value(rollbackContextKey{}) != nil }

func (e *Engine) Rollback(ctx context.Context, w *discovery.Workload, p *api.OptimizationPolicy, name string) (*ApplyResult, error) {
	if e.dryRun || p.Spec.Mode != api.ModeAuto {
		return nil, fmt.Errorf("rollback requires Auto mode and disabled global dry-run")
	}
	current, record, err := e.loadRollbackRecord(ctx, w, p, name)
	if err != nil {
		return nil, err
	}
	if record.Phase == "RolledBack" {
		return &ApplyResult{Method: "RollbackNoChange"}, nil
	}
	if record.Phase != "Applied" && record.Phase != phaseRollbackPending {
		return nil, fmt.Errorf("record phase %s is not eligible for rollback", record.Phase)
	}
	winner, err := policy.NewPolicySelector(e.client).SelectBestPolicy(ctx, w)
	if err != nil || winner.UID != p.UID || winner.Generation != p.Generation || winner.Spec.Mode != api.ModeAuto {
		return nil, fmt.Errorf("rollback blocked: winning policy changed")
	}
	template, err := discovery.Template(current)
	if err != nil {
		return nil, err
	}
	if history.TemplateIdentity(template) != record.TemplateIdentity {
		return nil, fmt.Errorf("rollback blocked: workload image or non-resource template changed")
	}
	restore, err := history.Restoration(record)
	if err != nil {
		return nil, err
	}
	if err := validateRollbackRecovery(current, restore); err != nil {
		return nil, err
	}
	scalers, err := coexist.Read(ctx, e.client, w.Namespace, w.Kind, w.Name)
	if err != nil {
		return nil, err
	}
	for name, sections := range restore {
		for _, raw := range sections {
			for resourceName := range raw.(map[string]any) {
				if reason := scalers.Reason(name, resourceName); reason != "" {
					return nil, fmt.Errorf("rollback blocked by %s for %s.%s", reason, name, resourceName)
				}
			}
		}
	}
	raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(current)
	if err != nil {
		return nil, err
	}
	app := &Workload{Kind: w.Kind, Namespace: w.Namespace, Name: w.Name, Object: &unstructured.Unstructured{Object: raw}}
	if err := e.validateRollbackDrift(ctx, app, template, record, restore); err != nil {
		return nil, err
	}
	recs, err := restorationRecommendations(restore)
	if err != nil {
		return nil, err
	}
	restorePolicy := emergencyPolicy(p)
	if err := e.validateConstraints(ctx, app, recs, restorePolicy); err != nil {
		return nil, err
	}
	// Persist intent before mutation. A retry may observe either the recorded
	// resources or the restored resources, and continues the same rollback.
	record.Phase = phaseRollbackPending
	if err := history.Save(ctx, e.client, record); err != nil {
		return nil, err
	}
	patch, _ := json.Marshal(map[string]any{fieldMetadata: map[string]any{"uid": current.GetUID(), "resourceVersion": current.GetResourceVersion(), "annotations": map[string]string{history.Paused: valueTrue}}})
	if err := e.client.Patch(ctx, current, client.RawPatch(types.MergePatchType, patch)); err != nil {
		return nil, err
	}
	raw, err = runtime.DefaultUnstructuredConverter.ToUnstructured(current)
	if err != nil {
		return nil, err
	}
	app.Object = &unstructured.Unstructured{Object: raw}
	result, applyErr := e.Apply(context.WithValue(ctx, rollbackContextKey{}, name), app, recs, restorePolicy)
	if applyErr != nil {
		record.RollbackError = applyErr.Error()
		_ = history.Save(ctx, e.client, record)
		return nil, applyErr
	}
	record.Phase = "RolledBack"
	record.RollbackError = ""
	if err := history.Save(ctx, e.client, record); err != nil {
		return nil, err
	}
	if result != nil {
		result.Method = "Rollback" + result.Method
	}
	return result, nil
}

func validateRollbackRecovery(current client.Object, restore map[string]map[string]any) error {
	var state safety.State
	if raw := current.GetAnnotations()[safety.StateAnnotation]; raw != "" {
		if err := json.Unmarshal([]byte(raw), &state); err != nil {
			return fmt.Errorf("rollback blocked: invalid safety state: %w", err)
		}
	}
	for name, sections := range restore {
		container := state.Containers[name]
		if container == nil || container.Recovery == nil {
			continue
		}
		recovery := container.Recovery
		for section, raw := range sections {
			value, changesMemory := raw.(map[string]any)["memory"]
			if !changesMemory || (!recovery.CircuitOpen && !time.Now().Before(recovery.FreezeUntil)) {
				continue
			}
			// Removing a memory limit does not reduce headroom, but removing a
			// request or shrinking either allocation can undo OOM protection.
			if value == nil && section == fieldLimits {
				continue
			}
			if value == nil {
				return fmt.Errorf("rollback blocked by active OOM recovery for %s", name)
			}
			q, err := resource.ParseQuantity(value.(string))
			if err != nil || q.Cmp(recovery.Floor) < 0 {
				return fmt.Errorf("rollback blocked by active OOM recovery floor for %s", name)
			}
		}
	}
	return nil
}

func restorationRecommendations(restore map[string]map[string]any) ([]ContainerRecommendation, error) {
	recs := []ContainerRecommendation{}
	for name, values := range restore {
		rec := &recommendation.Recommendation{ResourceOverride: values, PreserveCPU: true, PreserveMemory: true}
		for section, raw := range values {
			for key, value := range raw.(map[string]any) {
				if key == resourceCPU {
					rec.PreserveCPU = false
				} else {
					rec.PreserveMemory = false
				}
				if section == fieldRequests && value != nil {
					q, err := resource.ParseQuantity(value.(string))
					if err != nil {
						return nil, err
					}
					if key == resourceCPU {
						rec.CPU = q
					} else {
						rec.Memory = q
					}
				}
			}
		}
		recs = append(recs, ContainerRecommendation{Container: name, Recommendation: rec})
	}
	return recs, nil
}

func (e *Engine) loadRollbackRecord(ctx context.Context, w *discovery.Workload, p *api.OptimizationPolicy, name string) (client.Object, *history.Record, error) {
	current := w.Object.DeepCopyObject().(client.Object)
	if err := e.client.Get(ctx, client.ObjectKeyFromObject(w.Object), current); err != nil {
		return nil, nil, err
	}
	if current.GetUID() != w.Object.GetUID() || current.GetAnnotations()[history.RollbackTo] != name {
		return nil, nil, fmt.Errorf("rollback request or workload identity changed")
	}
	record, err := history.Read(ctx, e.client, w.Namespace, name)
	if err != nil {
		return nil, nil, err
	}
	if record.Workload.UID != current.GetUID() || record.Workload.Name != w.Name || record.Workload.Kind != w.Kind || record.Name != current.GetAnnotations()[history.Latest] || record.Digest != current.GetAnnotations()[history.LatestDigest] {
		return nil, nil, fmt.Errorf("rollback must reference the latest bound record for this workload UID")
	}
	if record.Mode != api.ModeAuto || record.Policy.UID != p.UID || record.Policy.Generation != p.Generation {
		return nil, nil, fmt.Errorf("rollback policy identity or generation differs from the recorded action")
	}
	return current, record, nil
}

func (e *Engine) validateRollbackDrift(ctx context.Context, app *Workload, template *corev1.PodTemplateSpec, record *history.Record, restore map[string]map[string]any) error {
	if record.Method == "ServerSideApply" || record.Method == methodStrategicMerge {
		if resources := history.Resources(&template.Spec); !history.Matches(resources, record.Plan) && (record.Phase != phaseRollbackPending || !history.Matches(resources, restore)) {
			return fmt.Errorf("rollback blocked: template resources changed after the recorded action")
		}
	}
	pods, err := e.ownedPods(ctx, app)
	if err != nil {
		return err
	}
	for _, pod := range pods {
		resources := history.Resources(&pod.Spec)
		if !history.Matches(resources, record.Plan) && !history.Matches(resources, restore) {
			return fmt.Errorf("rollback blocked: pod %s resources changed after the recorded action", pod.Name)
		}
	}
	return nil
}
