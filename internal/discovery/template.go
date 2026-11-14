package discovery

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func WorkloadObject(kind string) (client.Object, error) {
	switch kind {
	case kindRollout:
		return &unstructured.Unstructured{Object: map[string]any{"apiVersion": "argoproj.io/v1alpha1", "kind": kindRollout}}, nil
	case kindDeployment:
		return &appsv1.Deployment{}, nil
	case kindStatefulSet:
		return &appsv1.StatefulSet{}, nil
	case kindDaemonSet:
		return &appsv1.DaemonSet{}, nil
	case kindReplicaSet:
		return &appsv1.ReplicaSet{}, nil
	case kindJob:
		return &batchv1.Job{}, nil
	case kindCronJob:
		return &batchv1.CronJob{}, nil
	}
	return nil, fmt.Errorf("unsupported workload kind %s", kind)
}

func GVR(kind string) (schema.GroupVersionResource, error) {
	if kind == kindRollout {
		return schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "rollouts"}, nil
	}
	resource := map[string]string{kindDeployment: "deployments", kindStatefulSet: "statefulsets", kindDaemonSet: "daemonsets", kindReplicaSet: "replicasets", kindJob: "jobs", kindCronJob: "cronjobs"}[kind]
	if resource == "" {
		return schema.GroupVersionResource{}, fmt.Errorf("unsupported workload kind %s", kind)
	}
	group := "apps"
	if IsBatch(kind) {
		group = "batch"
	}
	return schema.GroupVersionResource{Group: group, Version: "v1", Resource: resource}, nil
}
func IsBatch(kind string) bool { return kind == kindJob || kind == kindCronJob }
func TemplatePath(kind string) []string {
	if kind == kindCronJob {
		return []string{fieldSpec, "jobTemplate", fieldSpec, "template"}
	}
	return []string{fieldSpec, "template"}
}
func PodSpecPath(kind string) []string { return append(TemplatePath(kind), fieldSpec) }
func Template(obj client.Object) (*corev1.PodTemplateSpec, error) {
	switch o := obj.(type) {
	case *appsv1.Deployment:
		return o.Spec.Template.DeepCopy(), nil
	case *appsv1.StatefulSet:
		return o.Spec.Template.DeepCopy(), nil
	case *appsv1.DaemonSet:
		return o.Spec.Template.DeepCopy(), nil
	case *appsv1.ReplicaSet:
		return o.Spec.Template.DeepCopy(), nil
	case *batchv1.Job:
		return o.Spec.Template.DeepCopy(), nil
	case *batchv1.CronJob:
		return o.Spec.JobTemplate.Spec.Template.DeepCopy(), nil
	case *unstructured.Unstructured:
		value, found, err := unstructured.NestedMap(o.Object, TemplatePath(o.GetKind())...)
		if err != nil || !found {
			return nil, fmt.Errorf("missing pod template for %s", o.GetKind())
		}
		t := &corev1.PodTemplateSpec{}
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(value, t)
		return t, err
	}
	return nil, fmt.Errorf("unsupported workload %T", obj)
}
