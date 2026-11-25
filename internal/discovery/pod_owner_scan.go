package discovery

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PodOwnerScan resolves owner chains for one namespace observation. A busy
// namespace typically has many Pods per controller. Read owner metadata once
// per kind instead of sending a GET for every link of every unrelated Pod.
// Admission and final write validation continue to use live object reads.
type PodOwnerScan struct {
	reader    client.Reader
	namespace string
	owners    map[schema.GroupVersionKind]map[string]*metav1.PartialObjectMetadata
}

func NewPodOwnerScan(c client.Reader, namespace string) *PodOwnerScan {
	return &PodOwnerScan{reader: c, namespace: namespace, owners: make(map[schema.GroupVersionKind]map[string]*metav1.PartialObjectMetadata)}
}

func (s *PodOwnerScan) get(ctx context.Context, kind string, key client.ObjectKey, obj client.Object) error {
	gvr, err := GVR(kind)
	if err != nil {
		return err
	}
	gvk := gvr.GroupVersion().WithKind(kind)
	index, loaded := s.owners[gvk]
	if !loaded {
		list := &metav1.PartialObjectMetadataList{}
		list.SetGroupVersionKind(gvk.GroupVersion().WithKind(kind + "List"))
		if err := s.reader.List(ctx, list, client.InNamespace(s.namespace)); err == nil {
			index = make(map[string]*metav1.PartialObjectMetadata, len(list.Items))
			for i := range list.Items {
				item := &list.Items[i]
				index[item.Name] = item
			}
		}
		s.owners[gvk] = index
	}
	if m := index[key.Name]; m != nil {
		obj.SetName(m.Name)
		obj.SetNamespace(m.Namespace)
		obj.SetUID(m.UID)
		obj.SetOwnerReferences(m.OwnerReferences)
		return nil
	}
	return s.reader.Get(ctx, key, obj)
}

func (s *PodOwnerScan) Matches(ctx context.Context, pod *corev1.Pod, w *Workload, allowIntermediate bool) bool {
	if pod.Namespace != s.namespace || w.Namespace != s.namespace || w.Object.GetUID() == "" {
		return false
	}
	var stop types.UID
	if allowIntermediate {
		stop = w.Object.GetUID()
	}
	found, err := walkPodOwners(ctx, s.get, pod, stop)
	return err == nil && found.Object.GetUID() == w.Object.GetUID() && found.Kind == w.Kind && found.Name == w.Name
}
