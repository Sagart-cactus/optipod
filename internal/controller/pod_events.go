package controller

import (
	"context"
	"reflect"
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/discovery"
	"github.com/optipod/optipod/internal/policy"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var podLifecycleEvents = predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
	old, ok := e.ObjectOld.(*corev1.Pod)
	if !ok {
		return false
	}
	next, ok := e.ObjectNew.(*corev1.Pod)
	if !ok {
		return false
	}
	return old.Status.Phase != next.Status.Phase || !reflect.DeepEqual(old.Status.ContainerStatuses, next.Status.ContainerStatuses) || !reflect.DeepEqual(old.Status.InitContainerStatuses, next.Status.InitContainerStatuses)
}}

func (r *OptimizationPolicyReconciler) policyForPod(ctx context.Context, obj client.Object) []reconcile.Request {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return nil
	}
	owner, err := discovery.ResolvePodWorkload(ctx, r.Client, pod)
	if err != nil {
		return nil
	}
	p, err := policy.NewPolicySelector(r.Client).SelectBestPolicy(ctx, owner)
	if err != nil || p == nil {
		return nil
	}
	return []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(p)}}
}

func metricsRefreshInterval(p *api.OptimizationPolicy, interval time.Duration) time.Duration {
	if p.Spec.Mode == api.ModeDisabled {
		return interval
	}
	age := p.Spec.MetricsConfig.MaxSampleAge.Duration
	if age <= 0 {
		age = 5 * time.Minute
	}
	if cap := age / 2; interval > cap {
		return cap
	}
	return interval
}
