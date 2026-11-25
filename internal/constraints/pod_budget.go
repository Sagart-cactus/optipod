package constraints

import (
	"fmt"

	api "github.com/optipod/optipod/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	podresource "k8s.io/component-helpers/resource"
)

// WholePodRequests is limited to deferred, request-only admission. Other
// strategies keep the original constraint against changing a Pod budget.
func WholePodRequests(pod *corev1.Pod, policy *api.OptimizationPolicy) bool {
	return pod.Spec.Resources != nil && policy.IsWebhookStrategy() && policy.GetRolloutStrategy() == api.RolloutOnNextRestart && policy.Spec.UpdateStrategy.UpdateRequestsOnly && policy.Spec.DaemonSetSizing == nil
}

// BudgetCandidate projects complete container recommendations onto the Pod's
// declared request dimensions. Kubernetes aggregates concurrent sidecars and
// sequential init containers. Overhead is accounted for by the scheduler.
func BudgetCandidate(pod *corev1.Pod, updates map[string]map[string]any) (*corev1.Pod, error) {
	next := pod.DeepCopy()
	for _, key := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory} {
		if _, declared := pod.Spec.Resources.Requests[key]; !declared {
			continue
		}
		usage := pod.DeepCopy()
		complete := true
		project := func(c *corev1.Container) error {
			requests, _ := updates[c.Name]["requests"].(map[string]any)
			value, ok := requests[string(key)].(string)
			if !ok {
				complete = false
				return nil
			}
			q, err := resource.ParseQuantity(value)
			if err != nil || q.Sign() <= 0 {
				return fmt.Errorf("invalid %s recommendation for %s", key, c.Name)
			}
			if floor, exists := c.Resources.Requests[key]; exists && floor.Cmp(q) > 0 {
				q = floor.DeepCopy()
			}
			if c.Resources.Requests == nil {
				c.Resources.Requests = corev1.ResourceList{}
			}
			c.Resources.Requests[key] = q
			return nil
		}
		for i := range usage.Spec.Containers {
			if err := project(&usage.Spec.Containers[i]); err != nil {
				return nil, err
			}
		}
		for i := range usage.Spec.InitContainers {
			c := &usage.Spec.InitContainers[i]
			if c.RestartPolicy != nil && *c.RestartPolicy == corev1.ContainerRestartPolicyAlways {
				if err := project(c); err != nil {
					return nil, err
				}
			} else if _, reserved := c.Resources.Requests[key]; !reserved {
				complete = false
			}
		}
		if !complete {
			continue
		}
		target := podresource.AggregateContainerRequests(usage, podresource.PodResourcesOptions{})[key]
		if limit, bounded := pod.Spec.Resources.Limits[key]; bounded && target.Cmp(limit) > 0 {
			target = limit.DeepCopy()
		}
		next.Spec.Resources.Requests[key] = target
	}
	return next, nil
}
