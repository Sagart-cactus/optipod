package framework

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

// WaitOptions bounds a condition wait. Every wait in the harness is bounded by
// an explicit timeout and polls on a fixed interval; there are no unexplained
// fixed sleeps.
type WaitOptions struct {
	// Timeout is the overall deadline for the condition to become true.
	Timeout time.Duration
	// Interval is the poll interval. Defaults to 2s when unset.
	Interval time.Duration
	// Description names the awaited condition for timeout diagnostics.
	Description string
}

func (o WaitOptions) interval() time.Duration {
	if o.Interval > 0 {
		return o.Interval
	}
	return 2 * time.Second
}

// ConditionFunc reports whether the awaited condition holds. A returned error
// is treated as a transient observation (poll continues) unless it is the final
// one, in which case it is included in the timeout error.
type ConditionFunc func(ctx context.Context) (done bool, err error)

// WaitForCondition polls cond until it reports done, the timeout elapses, or
// the context is cancelled. The returned error names the awaited condition, the
// elapsed deadline, and the last observed state, so a bounded wait failure is
// self-explanatory in diagnostics.
func WaitForCondition(ctx context.Context, opts WaitOptions, cond ConditionFunc) error {
	lastNote := "condition never became true"
	err := wait.PollUntilContextTimeout(ctx, opts.interval(), opts.Timeout, true,
		func(ctx context.Context) (bool, error) {
			done, err := cond(ctx)
			if err != nil {
				lastNote = fmt.Sprintf("last observation error: %v", err)
				return false, nil
			}
			if !done {
				lastNote = "condition not yet true"
			}
			return done, nil
		})
	if err != nil {
		return fmt.Errorf("timed out after %s waiting for %s: %w (%s)",
			opts.Timeout, opts.Description, err, lastNote)
	}
	return nil
}

// WaitForCRDEstablished waits until the named CRD reports the Established
// condition, so API operations against it cannot race CRD registration.
func WaitForCRDEstablished(ctx context.Context, c *Clients, name string, opts WaitOptions) error {
	if opts.Description == "" {
		opts.Description = fmt.Sprintf("CRD %s Established", name)
	}
	return WaitForCondition(ctx, opts, func(ctx context.Context) (bool, error) {
		crd, err := c.Ext.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, cond := range crd.Status.Conditions {
			if cond.Type == apiextv1.Established && cond.Status == apiextv1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	})
}

// WaitForDeploymentAvailable waits until the named Deployment reports the
// Available condition with its full replica count.
func WaitForDeploymentAvailable(ctx context.Context, c *Clients, namespace, name string, opts WaitOptions) error {
	if opts.Description == "" {
		opts.Description = fmt.Sprintf("Deployment %s/%s Available", namespace, name)
	}
	return WaitForCondition(ctx, opts, func(ctx context.Context) (bool, error) {
		dep, err := c.Kube.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		desired := int32(1)
		if dep.Spec.Replicas != nil {
			desired = *dep.Spec.Replicas
		}
		for _, cond := range dep.Status.Conditions {
			if cond.Type == appsv1.DeploymentAvailable && cond.Status == corev1.ConditionTrue &&
				dep.Status.UpdatedReplicas == desired {
				return true, nil
			}
		}
		return false, nil
	})
}

// WaitForServiceEndpoints waits until the named Service has at least one ready
// endpoint address, proving a backend pod is serving behind it.
func WaitForServiceEndpoints(ctx context.Context, c *Clients, namespace, name string, opts WaitOptions) error {
	if opts.Description == "" {
		opts.Description = fmt.Sprintf("Service %s/%s endpoints ready", namespace, name)
	}
	return WaitForCondition(ctx, opts, func(ctx context.Context) (bool, error) {
		ep, err := c.Kube.CoreV1().Endpoints(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, subset := range ep.Subsets {
			if len(subset.Addresses) > 0 {
				return true, nil
			}
		}
		return false, nil
	})
}

// WaitForPodCountReady waits until at least min pods matching the label
// selector in the namespace report Ready. It returns the observed pods so
// callers can assert on identities.
func WaitForPodCountReady(ctx context.Context, c *Clients, namespace, labelSelector string, min int, opts WaitOptions) ([]corev1.Pod, error) {
	if opts.Description == "" {
		opts.Description = fmt.Sprintf("at least %d ready pod(s) matching %q in %s", min, labelSelector, namespace)
	}
	var observed []corev1.Pod
	err := WaitForCondition(ctx, opts, func(ctx context.Context) (bool, error) {
		list, err := c.Kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			return false, err
		}
		ready := make([]corev1.Pod, 0, len(list.Items))
		for _, pod := range list.Items {
			if podReady(&pod) {
				ready = append(ready, pod)
			}
		}
		observed = ready
		return len(ready) >= min, nil
	})
	if err != nil {
		return nil, err
	}
	return observed, nil
}

func podReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// DeleteNamespace idempotently deletes a spec-owned namespace. A missing
// namespace is success; any other error propagates.
func DeleteNamespace(ctx context.Context, c *Clients, name string) error {
	err := c.Kube.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete namespace %s: %w", name, err)
	}
	return nil
}
