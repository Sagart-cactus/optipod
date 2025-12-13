package helpers

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// DefaultNginxImage is the default nginx image used in tests
	DefaultNginxImage = "nginx:1.25-alpine"
)

// WorkloadHelper provides utilities for managing workload resources in tests
type WorkloadHelper struct {
	client    client.Client
	namespace string
}

// NewWorkloadHelper creates a new WorkloadHelper instance
func NewWorkloadHelper(c client.Client, namespace string) *WorkloadHelper {
	return &WorkloadHelper{
		client:    c,
		namespace: namespace,
	}
}

// WorkloadType defines the type of workload to create
type WorkloadType string

const (
	WorkloadTypeDeployment  WorkloadType = "Deployment"
	WorkloadTypeStatefulSet WorkloadType = "StatefulSet"
	WorkloadTypeDaemonSet   WorkloadType = "DaemonSet"
)

// WorkloadConfig defines configuration for creating workload resources
type WorkloadConfig struct {
	Name       string
	Namespace  string
	Type       WorkloadType
	Labels     map[string]string
	Resources  ResourceRequirements
	Replicas   int32
	Image      string
	Containers []ContainerConfig
}

// ContainerConfig defines configuration for a container
type ContainerConfig struct {
	Name      string
	Image     string
	Resources ResourceRequirements
}

// ResourceRequirements defines CPU and memory requirements
type ResourceRequirements struct {
	Requests ResourceList
	Limits   ResourceList
}

// ResourceList defines resource quantities
type ResourceList struct {
	CPU    string
	Memory string
}

// CreateDeployment creates a Deployment with the specified configuration
func (h *WorkloadHelper) CreateDeployment(config WorkloadConfig) (*appsv1.Deployment, error) {
	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.Name,
			Namespace: h.namespace,
			Labels:    config.Labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &config.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": config.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": config.Name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: h.buildContainers(config),
					Volumes:    h.buildVolumes(config),
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: &[]bool{true}[0],
						RunAsUser:    &[]int64{1000}[0],
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
				},
			},
		},
	}

	err := h.client.Create(context.TODO(), deployment)
	if err != nil {
		return nil, fmt.Errorf("failed to create Deployment %s: %w", config.Name, err)
	}

	// Restore TypeMeta fields that may be cleared by the client
	deployment.Kind = "Deployment"
	deployment.APIVersion = "apps/v1"

	return deployment, nil
}

// CreateStatefulSet creates a StatefulSet with the specified configuration
func (h *WorkloadHelper) CreateStatefulSet(config WorkloadConfig) (*appsv1.StatefulSet, error) {
	statefulSet := &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "StatefulSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.Name,
			Namespace: h.namespace,
			Labels:    config.Labels,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &config.Replicas,
			ServiceName: config.Name + "-svc",
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": config.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": config.Name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: h.buildContainers(config),
					Volumes:    h.buildVolumes(config),
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: &[]bool{true}[0],
						RunAsUser:    &[]int64{1000}[0],
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
				},
			},
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 1},
				},
			},
		},
	}

	err := h.client.Create(context.TODO(), statefulSet)
	if err != nil {
		return nil, fmt.Errorf("failed to create StatefulSet %s: %w", config.Name, err)
	}

	// Restore TypeMeta fields that may be cleared by the client
	statefulSet.Kind = "StatefulSet"
	statefulSet.APIVersion = "apps/v1"

	return statefulSet, nil
}

// CreateDaemonSet creates a DaemonSet with the specified configuration
func (h *WorkloadHelper) CreateDaemonSet(config WorkloadConfig) (*appsv1.DaemonSet, error) {
	daemonSet := &appsv1.DaemonSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DaemonSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.Name,
			Namespace: h.namespace,
			Labels:    config.Labels,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": config.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": config.Name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: h.buildContainers(config),
					Volumes:    h.buildVolumes(config),
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: &[]bool{true}[0],
						RunAsUser:    &[]int64{1000}[0],
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Tolerations: []corev1.Toleration{
						{
							Key:      "node-role.kubernetes.io/control-plane",
							Operator: corev1.TolerationOpExists,
							Effect:   corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
			UpdateStrategy: appsv1.DaemonSetUpdateStrategy{
				Type: appsv1.RollingUpdateDaemonSetStrategyType,
			},
		},
	}

	err := h.client.Create(context.TODO(), daemonSet)
	if err != nil {
		return nil, fmt.Errorf("failed to create DaemonSet %s: %w", config.Name, err)
	}

	// Restore TypeMeta fields that may be cleared by the client
	daemonSet.Kind = "DaemonSet"
	daemonSet.APIVersion = "apps/v1"

	return daemonSet, nil
}

// buildContainers builds container specifications from config
func (h *WorkloadHelper) buildContainers(config WorkloadConfig) []corev1.Container {
	var containers []corev1.Container

	// If specific containers are defined, use them
	if len(config.Containers) > 0 {
		for _, containerConfig := range config.Containers {
			container := corev1.Container{
				Name:  containerConfig.Name,
				Image: containerConfig.Image,
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: &[]bool{false}[0],
					Capabilities: &corev1.Capabilities{
						Drop: []corev1.Capability{"ALL"},
					},
					ReadOnlyRootFilesystem: &[]bool{false}[0],
					RunAsNonRoot:           &[]bool{true}[0],
					RunAsUser:              &[]int64{1000}[0],
					SeccompProfile: &corev1.SeccompProfile{
						Type: corev1.SeccompProfileTypeRuntimeDefault,
					},
				},
			}

			// Set resources if specified
			if containerConfig.Resources.Requests.CPU != "" || containerConfig.Resources.Requests.Memory != "" ||
				containerConfig.Resources.Limits.CPU != "" || containerConfig.Resources.Limits.Memory != "" {
				container.Resources = h.buildResourceRequirements(containerConfig.Resources)
			}

			containers = append(containers, container)
		}
	} else {
		// Default single container
		image := config.Image
		if image == "" {
			image = DefaultNginxImage
		}

		container := corev1.Container{
			Name:  "app",
			Image: image,
			SecurityContext: &corev1.SecurityContext{
				AllowPrivilegeEscalation: &[]bool{false}[0],
				Capabilities: &corev1.Capabilities{
					Drop: []corev1.Capability{"ALL"},
				},
				ReadOnlyRootFilesystem: &[]bool{false}[0],
				RunAsNonRoot:           &[]bool{true}[0],
				RunAsUser:              &[]int64{1000}[0],
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
		}

		// Set resources if specified
		if config.Resources.Requests.CPU != "" || config.Resources.Requests.Memory != "" ||
			config.Resources.Limits.CPU != "" || config.Resources.Limits.Memory != "" {
			container.Resources = h.buildResourceRequirements(config.Resources)
		}

		// Add volume mounts for nginx
		if image == DefaultNginxImage {
			container.VolumeMounts = []corev1.VolumeMount{
				{
					Name:      "cache",
					MountPath: "/var/cache/nginx",
				},
				{
					Name:      "run",
					MountPath: "/var/run",
				},
			}
		}

		containers = append(containers, container)
	}

	return containers
}

// buildVolumes builds volume specifications from config
func (h *WorkloadHelper) buildVolumes(config WorkloadConfig) []corev1.Volume {
	var volumes []corev1.Volume

	// Add volumes for nginx if using nginx image
	image := config.Image
	if image == "" {
		image = DefaultNginxImage
	}

	if image == DefaultNginxImage {
		volumes = append(volumes, []corev1.Volume{
			{
				Name: "cache",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			},
			{
				Name: "run",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			},
		}...)
	}

	return volumes
}

// buildResourceRequirements builds Kubernetes ResourceRequirements from config
func (h *WorkloadHelper) buildResourceRequirements(resources ResourceRequirements) corev1.ResourceRequirements {
	req := corev1.ResourceRequirements{}

	// Set requests
	if resources.Requests.CPU != "" || resources.Requests.Memory != "" {
		req.Requests = make(corev1.ResourceList)
		if resources.Requests.CPU != "" {
			req.Requests[corev1.ResourceCPU] = resource.MustParse(resources.Requests.CPU)
		}
		if resources.Requests.Memory != "" {
			req.Requests[corev1.ResourceMemory] = resource.MustParse(resources.Requests.Memory)
		}
	}

	// Set limits
	if resources.Limits.CPU != "" || resources.Limits.Memory != "" {
		req.Limits = make(corev1.ResourceList)
		if resources.Limits.CPU != "" {
			req.Limits[corev1.ResourceCPU] = resource.MustParse(resources.Limits.CPU)
		}
		if resources.Limits.Memory != "" {
			req.Limits[corev1.ResourceMemory] = resource.MustParse(resources.Limits.Memory)
		}
	}

	return req
}

// WaitForWorkloadReady waits for the workload to be ready
func (h *WorkloadHelper) WaitForWorkloadReady(
	workloadName string, workloadType WorkloadType, timeout time.Duration,
) error {
	return wait.PollUntilContextTimeout(
		context.TODO(), 5*time.Second, timeout, true,
		func(ctx context.Context) (bool, error) {
			switch workloadType {
			case WorkloadTypeDeployment:
				deployment := &appsv1.Deployment{}
				err := h.client.Get(context.TODO(), types.NamespacedName{
					Name:      workloadName,
					Namespace: h.namespace,
				}, deployment)
				if err != nil {
					return false, err
				}
				return deployment.Status.ReadyReplicas == *deployment.Spec.Replicas, nil

			case WorkloadTypeStatefulSet:
				statefulSet := &appsv1.StatefulSet{}
				err := h.client.Get(context.TODO(), types.NamespacedName{
					Name:      workloadName,
					Namespace: h.namespace,
				}, statefulSet)
				if err != nil {
					return false, err
				}
				return statefulSet.Status.ReadyReplicas == *statefulSet.Spec.Replicas, nil

			case WorkloadTypeDaemonSet:
				daemonSet := &appsv1.DaemonSet{}
				err := h.client.Get(context.TODO(), types.NamespacedName{
					Name:      workloadName,
					Namespace: h.namespace,
				}, daemonSet)
				if err != nil {
					return false, err
				}
				return daemonSet.Status.NumberReady == daemonSet.Status.DesiredNumberScheduled, nil

			default:
				return false, fmt.Errorf("unsupported workload type: %s", workloadType)
			}
		})
}

// GetWorkloadAnnotations retrieves OptipPod annotations from a workload
func (h *WorkloadHelper) GetWorkloadAnnotations(
	workloadName string, workloadType WorkloadType,
) (map[string]string, error) {
	var annotations map[string]string

	switch workloadType {
	case WorkloadTypeDeployment:
		deployment := &appsv1.Deployment{}
		err := h.client.Get(context.TODO(), types.NamespacedName{
			Name:      workloadName,
			Namespace: h.namespace,
		}, deployment)
		if err != nil {
			return nil, err
		}
		annotations = deployment.Annotations

	case WorkloadTypeStatefulSet:
		statefulSet := &appsv1.StatefulSet{}
		err := h.client.Get(context.TODO(), types.NamespacedName{
			Name:      workloadName,
			Namespace: h.namespace,
		}, statefulSet)
		if err != nil {
			return nil, err
		}
		annotations = statefulSet.Annotations

	case WorkloadTypeDaemonSet:
		daemonSet := &appsv1.DaemonSet{}
		err := h.client.Get(context.TODO(), types.NamespacedName{
			Name:      workloadName,
			Namespace: h.namespace,
		}, daemonSet)
		if err != nil {
			return nil, err
		}
		annotations = daemonSet.Annotations

	default:
		return nil, fmt.Errorf("unsupported workload type: %s", workloadType)
	}

	// Filter OptipPod annotations
	optipodAnnotations := make(map[string]string)
	for key, value := range annotations {
		if len(key) > 10 && key[:10] == "optipod.io" {
			optipodAnnotations[key] = value
		}
	}

	return optipodAnnotations, nil
}

// DeleteWorkload deletes a workload by name and type
func (h *WorkloadHelper) DeleteWorkload(workloadName string, workloadType WorkloadType) error {
	switch workloadType {
	case WorkloadTypeDeployment:
		deployment := &appsv1.Deployment{}
		err := h.client.Get(context.TODO(), types.NamespacedName{
			Name:      workloadName,
			Namespace: h.namespace,
		}, deployment)
		if err != nil {
			return err
		}
		return h.client.Delete(context.TODO(), deployment)

	case WorkloadTypeStatefulSet:
		statefulSet := &appsv1.StatefulSet{}
		err := h.client.Get(context.TODO(), types.NamespacedName{
			Name:      workloadName,
			Namespace: h.namespace,
		}, statefulSet)
		if err != nil {
			return err
		}
		return h.client.Delete(context.TODO(), statefulSet)

	case WorkloadTypeDaemonSet:
		daemonSet := &appsv1.DaemonSet{}
		err := h.client.Get(context.TODO(), types.NamespacedName{
			Name:      workloadName,
			Namespace: h.namespace,
		}, daemonSet)
		if err != nil {
			return err
		}
		return h.client.Delete(context.TODO(), daemonSet)

	default:
		return fmt.Errorf("unsupported workload type: %s", workloadType)
	}
}
