package discovery

import corev1 "k8s.io/api/core/v1"

type ContainerSlot struct {
	Container corev1.Container
	Field     string
	Index     int
}

// ContainerSlots excludes one-shot init and ephemeral containers. Native
// sidecars are restartable init containers and retain their own API path.
func ContainerSlots(spec *corev1.PodSpec) []ContainerSlot {
	slots := make([]ContainerSlot, 0, len(spec.Containers)+len(spec.InitContainers))
	for i, c := range spec.Containers {
		slots = append(slots, ContainerSlot{c, "containers", i})
	}
	for i, c := range spec.InitContainers {
		if c.RestartPolicy != nil && *c.RestartPolicy == corev1.ContainerRestartPolicyAlways {
			slots = append(slots, ContainerSlot{c, "initContainers", i})
		}
	}
	return slots
}
