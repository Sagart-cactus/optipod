package discovery

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ResolvePodWorkload follows controlling owner references, validating UIDs so a
// deleted/recreated owner cannot donate stale recommendations to another pod.
func ResolvePodWorkload(ctx context.Context, c client.Reader, pod *corev1.Pod) (*Workload, error) {
	return resolvePodWorkload(ctx, c, pod, "")
}

// PodBelongsTo checks the chain up to the selected workload. An operator above
// a verified workload does not change which pods supply its observations.
func PodBelongsTo(ctx context.Context, c client.Reader, pod *corev1.Pod, w *Workload) bool {
	if w.Object.GetUID() == "" || w.Namespace != pod.Namespace {
		return false
	}
	owner, err := resolvePodWorkload(ctx, c, pod, w.Object.GetUID())
	return err == nil && owner.Kind == w.Kind && owner.Name == w.Name && owner.Object.GetUID() == w.Object.GetUID()
}

func resolvePodWorkload(ctx context.Context, c client.Reader, pod *corev1.Pod, stop types.UID) (*Workload, error) {
	return walkPodOwners(ctx, func(ctx context.Context, _ string, key client.ObjectKey, obj client.Object) error {
		return c.Get(ctx, key, obj)
	}, pod, stop)
}
func walkPodOwners(ctx context.Context, get func(context.Context, string, client.ObjectKey, client.Object) error, pod *corev1.Pod, stop types.UID) (*Workload, error) {
	var obj client.Object = pod
	kind := "Pod"
	for depth := 0; depth < 8; depth++ {
		owner := metav1.GetControllerOf(obj)
		// Operators may own a native workload without providing a supported
		// optimization target of their own. The native controller was already
		// fetched and UID-checked on the preceding iteration.
		native := kind == kindDeployment || kind == kindStatefulSet || kind == kindDaemonSet
		var operatorOwned bool
		if owner != nil && native {
			_, err := GVR(owner.Kind)
			operatorOwned = err != nil
		}
		if owner == nil || operatorOwned || (stop != "" && obj.GetUID() == stop) {
			return &Workload{Kind: kind, Namespace: obj.GetNamespace(), Name: obj.GetName(), Labels: obj.GetLabels(), Object: obj}, nil
		}
		parent, err := WorkloadObject(owner.Kind)
		if err != nil {
			return nil, err
		}
		gvr, err := GVR(owner.Kind)
		if err != nil || owner.APIVersion != gvr.GroupVersion().String() {
			return nil, fmt.Errorf("unsupported owner API %s", owner.APIVersion)
		}

		if err := get(ctx, owner.Kind, client.ObjectKey{Namespace: pod.Namespace, Name: owner.Name}, parent); err != nil {
			return nil, err
		}
		if parent.GetUID() != owner.UID {
			return nil, fmt.Errorf("owner UID mismatch for %s/%s", owner.Kind, owner.Name)
		}
		obj, kind = parent, owner.Kind
	}
	return nil, fmt.Errorf("owner chain exceeds maximum depth")
}
