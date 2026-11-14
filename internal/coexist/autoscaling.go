// Package coexist protects the request denominators used by horizontal scalers.
package coexist

import (
	"context"
	"fmt"

	api "github.com/optipod/optipod/api/v1alpha1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Restrictions struct {
	Resources map[string]map[string]string
	Targets   []string
}

func (r *Restrictions) add(container, resource, reason string) {
	if container == "" {
		container = "*"
	}
	if r.Resources[container] == nil {
		r.Resources[container] = map[string]string{}
	}
	r.Resources[container][resource] = reason
}

func (r *Restrictions) Reason(container, resource string) string {
	if r == nil {
		return ""
	}
	if reason := r.Resources["*"][resource]; reason != "" {
		return reason
	}
	return r.Resources[container][resource]
}

// ContainerPolicy removes dimensions whose utilization depends on requests.
// Absolute AverageValue and external metrics do not reserve a denominator.
func (r *Restrictions) ContainerPolicy(p *api.OptimizationPolicy, container string) *api.OptimizationPolicy {
	result := p.DeepCopy()
	if result.Spec.ResourcePolicy == nil {
		result.Spec.ResourcePolicy = &api.ResourcePolicy{}
	}
	resources := []api.ControlledResource{}
	for _, name := range []string{"cpu", "memory"} {
		if p.ControlsResource(name) && r.Reason(container, name) == "" {
			resources = append(resources, api.ControlledResource(name))
		}
	}
	if len(resources) == 0 {
		return nil
	}
	result.Spec.ResourcePolicy.ControlledResources = resources
	return result
}

func Read(ctx context.Context, c client.Reader, namespace, kind, name string) (*Restrictions, error) {
	r := &Restrictions{Resources: map[string]map[string]string{}}
	list := &autoscalingv2.HorizontalPodAutoscalerList{}
	if err := c.List(ctx, list, client.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("inspect HPA coexistence: %w", err)
	}
	for _, hpa := range list.Items {
		if hpa.Spec.ScaleTargetRef.Name != name || hpa.Spec.ScaleTargetRef.Kind != kind {
			continue
		}
		reason := "HPA " + hpa.Name
		r.Targets = append(r.Targets, reason)
		if len(hpa.Spec.Metrics) == 0 {
			r.add("*", "cpu", reason)
		}
		for _, metric := range hpa.Spec.Metrics {
			if m := metric.Resource; m != nil && (m.Target.Type == autoscalingv2.UtilizationMetricType || m.Target.AverageUtilization != nil) {
				r.add("*", string(m.Name), reason)
			}
			if m := metric.ContainerResource; m != nil && (m.Target.Type == autoscalingv2.UtilizationMetricType || m.Target.AverageUtilization != nil) {
				r.add(m.Container, string(m.Name), reason)
			}
		}
	}
	keda := &unstructured.UnstructuredList{}
	keda.SetGroupVersionKind(schema.GroupVersionKind{Group: "keda.sh", Version: "v1alpha1", Kind: "ScaledObjectList"})
	if err := c.List(ctx, keda, client.InNamespace(namespace)); err != nil {
		if meta.IsNoMatchError(err) || apierrors.IsNotFound(err) {
			return r, nil
		}
		return nil, fmt.Errorf("inspect KEDA coexistence: %w", err)
	}
	for _, scaled := range keda.Items {
		target, _, _ := unstructured.NestedString(scaled.Object, "spec", "scaleTargetRef", "name")
		targetKind, _, _ := unstructured.NestedString(scaled.Object, "spec", "scaleTargetRef", "kind")
		if targetKind == "" {
			targetKind = "Deployment"
		}
		if target != name || targetKind != kind {
			continue
		}
		reason := "KEDA ScaledObject " + scaled.GetName()
		r.Targets = append(r.Targets, reason)
		triggers, _, _ := unstructured.NestedSlice(scaled.Object, "spec", "triggers")
		for _, entry := range triggers {
			trigger, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			typ, _, _ := unstructured.NestedString(trigger, "type")
			if typ != "cpu" && typ != "memory" {
				continue
			}
			metricType, _, _ := unstructured.NestedString(trigger, "metricType")
			if metricType == "" {
				metricType, _, _ = unstructured.NestedString(trigger, "metadata", "type")
			}
			if metricType == "AverageValue" {
				continue
			}
			container, _, _ := unstructured.NestedString(trigger, "metadata", "containerName")
			r.add(container, typ, reason)
		}
	}
	return r, nil
}
