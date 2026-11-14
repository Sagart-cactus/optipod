package webhook

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const readySpecHash = "optipod.io/ready-spec-hash"

func specHash(obj client.Object) string {
	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return ""
	}
	data, err := json.Marshal(u["spec"])
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func observedGeneration(obj client.Object) int64 {
	switch o := obj.(type) {
	case *appsv1.Deployment:
		return o.Status.ObservedGeneration
	case *appsv1.StatefulSet:
		return o.Status.ObservedGeneration
	case *appsv1.DaemonSet:
		return o.Status.ObservedGeneration
	case *appsv1.ReplicaSet:
		return o.Status.ObservedGeneration
	case *unstructured.Unstructured:
		if o.GetKind() == kindRollout {
			value, _, _ := unstructured.NestedString(o.Object, "status", "observedGeneration")
			n, err := strconv.ParseInt(value, 10, 64)
			if err == nil {
				return n
			}
		}
	}
	return -1
}

func specObserved(obj client.Object) bool {
	return observedGeneration(obj) >= obj.GetGeneration() || (obj.GetAnnotations()[readySpecHash] != "" && obj.GetAnnotations()[readySpecHash] == specHash(obj))
}

// Deployment generation changes on metadata annotations too. Record the exact
// ready spec before writing OptiPod annotations, so our own bookkeeping cannot
// indefinitely prevent a restart. An external spec edit invalidates this proof.
func MarkReadySpec(ctx context.Context, c client.Client, obj client.Object) error {
	current := obj.DeepCopyObject().(client.Object)
	if err := c.Get(ctx, client.ObjectKeyFromObject(obj), current); err != nil {
		return err
	}
	if current.GetUID() != obj.GetUID() {
		return fmt.Errorf("workload identity changed")
	}
	if observedGeneration(current) < current.GetGeneration() || !NewRolloutController(c).IsWorkloadReady(current) {
		return nil
	}
	hash := specHash(current)
	if hash == "" || current.GetAnnotations()[readySpecHash] == hash {
		return nil
	}
	patch, _ := json.Marshal(map[string]any{"metadata": map[string]any{"uid": current.GetUID(), "resourceVersion": current.GetResourceVersion(), "annotations": map[string]string{readySpecHash: hash}}})
	return c.Patch(ctx, current, client.RawPatch(types.MergePatchType, patch))
}
