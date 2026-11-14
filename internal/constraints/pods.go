// Package constraints validates proposed pod resources before storing a plan
// and again at admission. Kubernetes remains the final admission authority.
package constraints

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	podresource "k8s.io/component-helpers/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Change struct {
	Before, After *corev1.Pod
	NewPod        bool
}

func QoS(p *corev1.Pod) corev1.PodQOSClass {
	guaranteed, anyResource := true, false
	for _, c := range append(append([]corev1.Container{}, p.Spec.Containers...), p.Spec.InitContainers...) {
		for _, name := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory} {
			r, l := c.Resources.Requests[name], c.Resources.Limits[name]
			anyResource = anyResource || r.Sign() > 0 || l.Sign() > 0
			guaranteed = guaranteed && r.Sign() > 0 && l.Sign() > 0 && r.Cmp(l) == 0
		}
	}
	if !anyResource {
		return corev1.PodQOSBestEffort
	}
	if guaranteed {
		return corev1.PodQOSGuaranteed
	}
	return corev1.PodQOSBurstable
}

func Validate(ctx context.Context, c client.Reader, namespace string, changes []Change, preserveQoS bool) error {
	ranges := &corev1.LimitRangeList{}
	if err := c.List(ctx, ranges, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("read LimitRanges: %w", err)
	}
	for _, change := range changes {
		p := change.After
		if p.Spec.Resources != nil {
			return fmt.Errorf("pod-level resources require a separate policy; container-only optimization is blocked")
		}
		if preserveQoS && change.Before != nil && QoS(change.Before) != QoS(p) {
			return fmt.Errorf("QoS preservation blocks %s to %s", QoS(change.Before), QoS(p))
		}
		for _, co := range append(append([]corev1.Container{}, p.Spec.Containers...), p.Spec.InitContainers...) {
			for _, name := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory} {
				req := co.Resources.Requests[name]
				lim, has := co.Resources.Limits[name]
				if req.Sign() < 0 || lim.Sign() < 0 {
					return fmt.Errorf("negative resources for %s", co.Name)
				}
				if has && req.Cmp(lim) > 0 {
					return fmt.Errorf("container %s %s request exceeds preserved limit", co.Name, name)
				}
			}
		}
		for _, lr := range ranges.Items {
			for _, item := range lr.Spec.Limits {
				switch item.Type {
				case corev1.LimitTypeContainer:
					for _, co := range append(append([]corev1.Container{}, p.Spec.Containers...), p.Spec.InitContainers...) {
						if err := validateRange(co.Resources.Requests, co.Resources.Limits, item); err != nil {
							return fmt.Errorf("LimitRange %s container %s: %w", lr.Name, co.Name, err)
						}
					}
				case corev1.LimitTypePod:
					if err := validateRange(podresource.PodRequests(p, podresource.PodResourcesOptions{}), podresource.PodLimits(p, podresource.PodResourcesOptions{}), item); err != nil {
						return fmt.Errorf("LimitRange %s pod: %w", lr.Name, err)
					}
				}
			}
		}
	}
	return validateQuotas(ctx, c, namespace, changes)
}

func validateRange(requests, limits corev1.ResourceList, item corev1.LimitRangeItem) error {
	for _, name := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory} {
		r, rOK := requests[name]
		l, lOK := limits[name]
		if min, ok := item.Min[name]; ok && (!rOK || r.Cmp(min) < 0 || (lOK && l.Cmp(min) < 0)) {
			return fmt.Errorf("%s below minimum %s", name, min.String())
		}
		if max, ok := item.Max[name]; ok && (!lOK || l.Cmp(max) > 0 || (rOK && r.Cmp(max) > 0)) {
			return fmt.Errorf("%s exceeds maximum %s or has no limit", name, max.String())
		}
		if ratio, ok := item.MaxLimitRequestRatio[name]; ok && (r.IsZero() || l.IsZero() || l.AsApproximateFloat64()/r.AsApproximateFloat64() > ratio.AsApproximateFloat64()) {
			return fmt.Errorf("%s exceeds maximum limit/request ratio %s", name, ratio.String())
		}
	}
	return nil
}
func quotaResource(name corev1.ResourceName) (corev1.ResourceName, bool, bool) {
	s := string(name)
	limits := strings.HasPrefix(s, "limits.")
	s = strings.TrimPrefix(strings.TrimPrefix(s, "limits."), "requests.")
	return corev1.ResourceName(s), limits, s == "cpu" || s == "memory"
}
func quotaCharge(p *corev1.Pod) corev1.ResourceList {
	result := corev1.ResourceList{corev1.ResourcePods: resource.MustParse("1")}
	requests := podresource.PodRequests(p, podresource.PodResourcesOptions{})
	limits := podresource.PodLimits(p, podresource.PodResourcesOptions{})
	for _, name := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory} {
		result[name] = requests[name]
		result[corev1.ResourceName("requests."+string(name))] = requests[name]
		result[corev1.ResourceName("limits."+string(name))] = limits[name]
	}
	return result
}
func matchesQuota(q corev1.ResourceQuota, p *corev1.Pod) (bool, error) {
	match := func(scope corev1.ResourceQuotaScope) (bool, error) {
		switch scope {
		case corev1.ResourceQuotaScopeTerminating:
			return p.Spec.ActiveDeadlineSeconds != nil, nil
		case corev1.ResourceQuotaScopeNotTerminating:
			return p.Spec.ActiveDeadlineSeconds == nil, nil
		case corev1.ResourceQuotaScopeBestEffort:
			return QoS(p) == corev1.PodQOSBestEffort, nil
		case corev1.ResourceQuotaScopeNotBestEffort:
			return QoS(p) != corev1.PodQOSBestEffort, nil
		case corev1.ResourceQuotaScopePriorityClass:
			return p.Spec.PriorityClassName != "", nil
		default:
			return false, fmt.Errorf("ResourceQuota %s has unsupported pod scope %s", q.Name, scope)
		}
	}
	// PVC-only and object-count quotas have no effect on these changes.
	relevant := false
	for name := range q.Spec.Hard {
		_, _, ok := quotaResource(name)
		relevant = relevant || ok || name == corev1.ResourcePods
	}
	if !relevant {
		return false, nil
	}
	for _, scope := range q.Spec.Scopes {
		ok, err := match(scope)
		if err != nil || !ok {
			return false, err
		}
	}
	if q.Spec.ScopeSelector != nil {
		for _, e := range q.Spec.ScopeSelector.MatchExpressions {
			exists, err := match(e.ScopeName)
			if err != nil {
				return false, err
			}
			in := false
			for _, v := range e.Values {
				if v == p.Spec.PriorityClassName {
					in = true
				}
			}
			switch e.Operator {
			case corev1.ScopeSelectorOpExists:
				if !exists {
					return false, nil
				}
			case corev1.ScopeSelectorOpDoesNotExist:
				if exists {
					return false, nil
				}
			case corev1.ScopeSelectorOpIn:
				if e.ScopeName != corev1.ResourceQuotaScopePriorityClass || !in {
					return false, nil
				}
			case corev1.ScopeSelectorOpNotIn:
				if e.ScopeName != corev1.ResourceQuotaScopePriorityClass || in {
					return false, nil
				}
			default:
				return false, fmt.Errorf("unsupported quota scope operator %s", e.Operator)
			}
		}
	}
	return true, nil
}

func validateQuotas(ctx context.Context, c client.Reader, namespace string, changes []Change) error {
	quotas := &corev1.ResourceQuotaList{}
	if err := c.List(ctx, quotas, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("read ResourceQuotas: %w", err)
	}
	for _, q := range quotas.Items {
		delta := corev1.ResourceList{}
		for _, change := range changes {
			for _, v := range []struct {
				p    *corev1.Pod
				sign int64
			}{{change.After, 1}, {change.Before, -1}} {
				if v.p == nil || (v.sign < 0 && change.NewPod) {
					continue
				}
				matches, err := matchesQuota(q, v.p)
				if err != nil {
					return err
				}
				if !matches {
					continue
				}
				charge := quotaCharge(v.p)
				for name, value := range charge {
					if v.sign < 0 {
						value.Neg()
					}
					sum := delta[name]
					sum.Add(value)
					delta[name] = sum
				}
				// Compute quotas require each container to carry the charged resource.
				if v.sign > 0 {
					for name := range q.Spec.Hard {
						resourceName, limits, ok := quotaResource(name)
						if !ok {
							continue
						}
						for _, co := range append(append([]corev1.Container{}, v.p.Spec.Containers...), v.p.Spec.InitContainers...) {
							values := co.Resources.Requests
							if limits {
								values = co.Resources.Limits
							}
							if _, present := values[resourceName]; !present {
								return fmt.Errorf("ResourceQuota %s requires %s for container %s", q.Name, name, co.Name)
							}
						}
					}
				}
			}
		}
		for name, change := range delta {
			hard, tracked := q.Spec.Hard[name]
			if !tracked || change.Sign() <= 0 {
				continue
			}
			used, present := q.Status.Used[name]
			if !present {
				return fmt.Errorf("ResourceQuota %s usage for %s is not initialized", q.Name, name)
			}
			used.Add(change)
			if used.Cmp(hard) > 0 {
				return fmt.Errorf("ResourceQuota %s exceeds %s: projected %s, hard %s", q.Name, name, used.String(), hard.String())
			}
		}
	}
	return nil
}
