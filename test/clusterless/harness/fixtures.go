package harness

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
)

// PolicyBuilder provides a fluent API for constructing OptimizationPolicy objects
type PolicyBuilder struct {
	policy *optipodv1alpha1.OptimizationPolicy
}

// NewPolicy creates a new PolicyBuilder with basic metadata
func NewPolicy(name, namespace string) *PolicyBuilder {
	return &PolicyBuilder{
		policy: &optipodv1alpha1.OptimizationPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: optipodv1alpha1.OptimizationPolicySpec{
				Mode:     optipodv1alpha1.ModeRecommend, // Safe default
				Selector: optipodv1alpha1.WorkloadSelector{},
				MetricsConfig: optipodv1alpha1.MetricsConfig{
					Provider: "prometheus", // Default provider
				},
				ResourceBounds: optipodv1alpha1.ResourceBounds{
					CPU: optipodv1alpha1.ResourceBound{
						Min: resource.MustParse("10m"),
						Max: resource.MustParse("4000m"),
					},
					Memory: optipodv1alpha1.ResourceBound{
						Min: resource.MustParse("64Mi"),
						Max: resource.MustParse("8Gi"),
					},
				},
				UpdateStrategy: optipodv1alpha1.UpdateStrategy{},
			},
		},
	}
}

// WithMode sets the optimization mode
func (b *PolicyBuilder) WithMode(mode optipodv1alpha1.PolicyMode) *PolicyBuilder {
	b.policy.Spec.Mode = mode
	return b
}

// WithStrategy sets the update strategy
func (b *PolicyBuilder) WithStrategy(strategy optipodv1alpha1.UpdateStrategyType) *PolicyBuilder {
	strategyStr := string(strategy)
	b.policy.Spec.UpdateStrategy.Strategy = &strategyStr
	return b
}

// WithLabelSelector sets the workload selector
func (b *PolicyBuilder) WithLabelSelector(labels map[string]string) *PolicyBuilder {
	b.policy.Spec.Selector.WorkloadSelector = &metav1.LabelSelector{
		MatchLabels: labels,
	}
	return b
}

// WithMetricsConfig sets the metrics configuration
func (b *PolicyBuilder) WithMetricsConfig(provider string, percentile string, safetyFactor float64) *PolicyBuilder {
	b.policy.Spec.MetricsConfig = optipodv1alpha1.MetricsConfig{
		Provider:     provider,
		Percentile:   percentile,
		SafetyFactor: &safetyFactor,
	}
	return b
}

// WithResourceBounds sets CPU and memory bounds
func (b *PolicyBuilder) WithResourceBounds(cpuMin, cpuMax, memMin, memMax string) *PolicyBuilder {
	b.policy.Spec.ResourceBounds = optipodv1alpha1.ResourceBounds{
		CPU: optipodv1alpha1.ResourceBound{
			Min: resource.MustParse(cpuMin),
			Max: resource.MustParse(cpuMax),
		},
		Memory: optipodv1alpha1.ResourceBound{
			Min: resource.MustParse(memMin),
			Max: resource.MustParse(memMax),
		},
	}
	return b
}

// WithWeight sets the policy weight for multi-policy selection
func (b *PolicyBuilder) WithWeight(weight int32) *PolicyBuilder {
	b.policy.Spec.Weight = &weight
	return b
}

// WithSafetyConfig sets safety configuration
func (b *PolicyBuilder) WithSafetyConfig(preventMemDecrease bool, gradualDecrease *float64) *PolicyBuilder {
	allowUnsafe := !preventMemDecrease
	b.policy.Spec.UpdateStrategy.AllowUnsafeMemoryDecrease = &allowUnsafe

	if gradualDecrease != nil {
		percentage := int(*gradualDecrease * 100) // Convert 0.1 to 10
		b.policy.Spec.UpdateStrategy.GradualDecreaseConfig = &optipodv1alpha1.GradualDecreaseConfig{
			Enabled:                  true,
			MemoryDecreasePercentage: &percentage,
		}
	}
	return b
}

// WithAnnotations sets annotations on the policy
func (b *PolicyBuilder) WithAnnotations(annotations map[string]string) *PolicyBuilder {
	if b.policy.Annotations == nil {
		b.policy.Annotations = make(map[string]string)
	}
	for k, v := range annotations {
		b.policy.Annotations[k] = v
	}
	return b
}

// Build returns the constructed OptimizationPolicy
func (b *PolicyBuilder) Build() *optipodv1alpha1.OptimizationPolicy {
	return b.policy
}

// DeploymentBuilder provides a fluent API for constructing Deployment objects
type DeploymentBuilder struct {
	deployment *appsv1.Deployment
}

// NewDeployment creates a new DeploymentBuilder with basic metadata
func NewDeployment(name, namespace string) *DeploymentBuilder {
	return &DeploymentBuilder{
		deployment: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": name},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{"app": name},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{},
					},
				},
			},
		},
	}
}

// WithLabels sets labels on the deployment and pod template
func (b *DeploymentBuilder) WithLabels(labels map[string]string) *DeploymentBuilder {
	if b.deployment.Labels == nil {
		b.deployment.Labels = make(map[string]string)
	}
	for k, v := range labels {
		b.deployment.Labels[k] = v
	}

	// Update pod template labels
	if b.deployment.Spec.Template.Labels == nil {
		b.deployment.Spec.Template.Labels = make(map[string]string)
	}
	for k, v := range labels {
		b.deployment.Spec.Template.Labels[k] = v
	}

	// Update selector
	b.deployment.Spec.Selector.MatchLabels = labels
	return b
}

// WithContainer adds a container to the deployment
func (b *DeploymentBuilder) WithContainer(name, image, cpuRequest, memRequest string) *DeploymentBuilder {
	container := corev1.Container{
		Name:  name,
		Image: image,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(cpuRequest),
				corev1.ResourceMemory: resource.MustParse(memRequest),
			},
		},
	}
	b.deployment.Spec.Template.Spec.Containers = append(
		b.deployment.Spec.Template.Spec.Containers, container)
	return b
}

// WithContainerLimits adds limits to the last added container
func (b *DeploymentBuilder) WithContainerLimits(cpuLimit, memLimit string) *DeploymentBuilder {
	if len(b.deployment.Spec.Template.Spec.Containers) > 0 {
		lastIdx := len(b.deployment.Spec.Template.Spec.Containers) - 1
		container := &b.deployment.Spec.Template.Spec.Containers[lastIdx]
		if container.Resources.Limits == nil {
			container.Resources.Limits = make(corev1.ResourceList)
		}
		container.Resources.Limits[corev1.ResourceCPU] = resource.MustParse(cpuLimit)
		container.Resources.Limits[corev1.ResourceMemory] = resource.MustParse(memLimit)
	}
	return b
}

// WithAnnotations sets annotations on the deployment
func (b *DeploymentBuilder) WithAnnotations(annotations map[string]string) *DeploymentBuilder {
	if b.deployment.Annotations == nil {
		b.deployment.Annotations = make(map[string]string)
	}
	for k, v := range annotations {
		b.deployment.Annotations[k] = v
	}
	return b
}

// WithPodAnnotations sets annotations on the pod template
func (b *DeploymentBuilder) WithPodAnnotations(annotations map[string]string) *DeploymentBuilder {
	if b.deployment.Spec.Template.Annotations == nil {
		b.deployment.Spec.Template.Annotations = make(map[string]string)
	}
	for k, v := range annotations {
		b.deployment.Spec.Template.Annotations[k] = v
	}
	return b
}

// WithReplicas sets the number of replicas
func (b *DeploymentBuilder) WithReplicas(replicas int32) *DeploymentBuilder {
	b.deployment.Spec.Replicas = &replicas
	return b
}

// Build returns the constructed Deployment
func (b *DeploymentBuilder) Build() *appsv1.Deployment {
	return b.deployment
}

// PodBuilder provides a fluent API for constructing Pod objects
type PodBuilder struct {
	pod *corev1.Pod
}

// NewPod creates a new PodBuilder with basic metadata
func NewPod(name, namespace string) *PodBuilder {
	return &PodBuilder{
		pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{},
			},
		},
	}
}

// WithLabels sets labels on the pod
func (b *PodBuilder) WithLabels(labels map[string]string) *PodBuilder {
	if b.pod.Labels == nil {
		b.pod.Labels = make(map[string]string)
	}
	for k, v := range labels {
		b.pod.Labels[k] = v
	}
	return b
}

// WithAnnotations sets annotations on the pod
func (b *PodBuilder) WithAnnotations(annotations map[string]string) *PodBuilder {
	if b.pod.Annotations == nil {
		b.pod.Annotations = make(map[string]string)
	}
	for k, v := range annotations {
		b.pod.Annotations[k] = v
	}
	return b
}

// WithContainer adds a container to the pod
func (b *PodBuilder) WithContainer(name, image, cpuRequest, memRequest string) *PodBuilder {
	container := corev1.Container{
		Name:  name,
		Image: image,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(cpuRequest),
				corev1.ResourceMemory: resource.MustParse(memRequest),
			},
		},
	}
	b.pod.Spec.Containers = append(b.pod.Spec.Containers, container)
	return b
}

// WithOwnerReference sets an owner reference
func (b *PodBuilder) WithOwnerReference(owner metav1.Object, gvk metav1.GroupVersionKind) *PodBuilder {
	ownerRef := metav1.OwnerReference{
		APIVersion: gvk.Group + "/" + gvk.Version,
		Kind:       gvk.Kind,
		Name:       owner.GetName(),
		UID:        owner.GetUID(),
	}
	b.pod.OwnerReferences = append(b.pod.OwnerReferences, ownerRef)
	return b
}

// Build returns the constructed Pod
func (b *PodBuilder) Build() *corev1.Pod {
	return b.pod
}
