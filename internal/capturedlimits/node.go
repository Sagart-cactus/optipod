package capturedlimits

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/optipod/optipod/internal/nodesizing"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ordinalKey uses the StatefulSet controller reference and stable Pod name.
func ordinalKey(pod *corev1.Pod, uid types.UID) (string, error) {
	ref := metav1.GetControllerOf(pod)
	if ref == nil || ref.UID != uid || ref.Kind != "StatefulSet" || ref.APIVersion != "apps/v1" || ref.Name == "" {
		return "", fmt.Errorf("cannot verify StatefulSet owner")
	}
	key, found := strings.CutPrefix(pod.Name, ref.Name+"-")
	index, err := strconv.ParseUint(key, 10, 31)
	if !found || err != nil || strconv.FormatUint(index, 10) != key {
		return "", fmt.Errorf("invalid StatefulSet Pod name")
	}
	if label, ok := pod.Labels["apps.kubernetes.io/pod-index"]; ok && label != key {
		return "", fmt.Errorf("StatefulSet Pod index differs from its name")
	}
	return key, nil
}

// ForPod returns the baseline for the verified owner and the node targeted by
// the DaemonSet controller. A new node starts under ordinary policy sizing.
func (s *Snapshot) ForPod(ctx context.Context, reader client.Reader, kind string, owner types.UID, pod *corev1.Pod) (*Snapshot, error) {
	if s.WorkloadUID != owner {
		return nil, nil
	}
	if len(s.Ordinals) > 0 {
		if kind != "StatefulSet" {
			return nil, fmt.Errorf("ordinal capture requires a StatefulSet")
		}
		key, err := ordinalKey(pod, owner)
		if err != nil {
			return nil, err
		}
		containers, ok := s.Ordinals[key]
		if !ok {
			return nil, nil
		}
		return &Snapshot{WorkloadUID: owner, Containers: containers, HoldRequestsUntil: s.HoldRequestsUntil}, nil
	}
	if len(s.Nodes) == 0 {
		return s, nil
	}
	if kind != "DaemonSet" {
		return nil, fmt.Errorf("node capture requires a DaemonSet")
	}
	name, err := nodesizing.NodeName(pod)
	if err != nil {
		return nil, err
	}
	captured, found := s.Nodes[name]
	if !found {
		return nil, nil
	}
	node := &corev1.Node{}
	if err := reader.Get(ctx, client.ObjectKey{Name: name}, node); err != nil {
		return nil, fmt.Errorf("cannot verify captured node %s: %w", name, err)
	}
	if captured.UID != node.UID || node.DeletionTimestamp != nil {
		return nil, nil
	}
	return &Snapshot{WorkloadUID: owner, Containers: captured.Containers, HoldRequestsUntil: s.HoldRequestsUntil}, nil
}
