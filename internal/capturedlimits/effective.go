package capturedlimits

import (
	"fmt"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/discovery"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PolicySnapshot reads the capture format used by request-only webhook policies.
func PolicySnapshot(policy *api.OptimizationPolicy, owner client.Object) (*Snapshot, error) {
	if !policy.IsWebhookStrategy() || !policy.Spec.UpdateStrategy.UpdateRequestsOnly || policy.Spec.DaemonSetSizing != nil {
		return nil, nil
	}
	uid := owner.GetUID()
	raw := ForOwner(policy.Annotations, uid)
	if raw == "" {
		return nil, nil
	}
	snapshot, err := Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid captured limits: %w", err)
	}
	if _, keyed := policy.Annotations["optipod.io/capture-"+string(uid)]; keyed && snapshot.WorkloadUID != uid {
		return nil, fmt.Errorf("captured resource owner UID differs from the policy key")
	}
	if snapshot.WorkloadUID != uid {
		return nil, nil
	}
	return snapshot, nil
}

// ContainerForSlot returns a capture only when the Pod slot and declaration
// still match the recorded container identity.
func (s *Snapshot) ContainerForSlot(slot discovery.ContainerSlot, declared map[string]discovery.ContainerSlot, policy *api.OptimizationPolicy) (Container, *corev1.ResourceRequirements, bool) {
	if s == nil {
		return Container{}, nil, false
	}
	name := slot.Container.Name
	capture, ok := s.Containers[name]
	if !ok || slot.Field != capture.Field || policy.ExcludesContainer(name) || (slot.Field == "initContainers" && !policy.IncludesNativeSidecars()) {
		return Container{}, nil, false
	}
	original, present := declared[name]
	if present != (capture.Template != nil) || (present && original.Field != slot.Field) || (!present && !policy.IncludesInjectedSidecars()) {
		return Container{}, nil, false
	}
	if present {
		return capture, &original.Container.Resources, true
	}
	return capture, nil, true
}

// AppliesToLimit lets a deliberate edit of a template dimension supersede
// its capture, independently of the other dimensions.
func (c Container) AppliesToLimit(original *corev1.ResourceRequirements, key corev1.ResourceName) bool {
	return original == nil || Same(original.Limits, c.Template, key)
}

func DeclaredSlots(template *corev1.PodSpec) map[string]discovery.ContainerSlot {
	slots := map[string]discovery.ContainerSlot{}
	for _, slot := range discovery.ContainerSlots(template) {
		slots[slot.Container.Name] = slot
	}
	return slots
}
