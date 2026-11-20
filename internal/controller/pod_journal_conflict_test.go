package controller

import (
	"context"
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/discovery"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type interveningClient struct {
	client.Client
	beforePatch func() error
	writes      int
}

func (c *interveningClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	c.writes++
	if c.writes == 1 {
		if err := c.beforePatch(); err != nil {
			return err
		}
	}
	return c.Client.Patch(ctx, obj, patch, opts...)
}

var _ = It("preserves concurrent journal edits and ignores unrelated resource-version changes", func(ctx context.Context) {
	verifyPodJournalConcurrency(GinkgoT(), ctx, k8sClient)
})

func verifyPodJournalConcurrency(t interface {
	Helper()
	Fatal(...any)
	Fatalf(string, ...any)
	Error(...any)
}, ctx context.Context, c client.Client) {
	t.Helper()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "pod-journal-cas-"}}
	if err := c.Create(ctx, ns); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := c.Delete(ctx, ns); err != nil {
			t.Error(err)
		}
	}()
	for _, scenario := range []string{"other-metadata", "existing-journal", "first-insertion"} {
		meta := metav1.ObjectMeta{Namespace: ns.Name, Name: scenario}
		if scenario != "first-insertion" {
			meta.Annotations = map[string]string{metricsPodsAnnotation: "[]"}
		}
		one := int32(1)
		labels := map[string]string{"app": scenario}
		sts := &appsv1.StatefulSet{ObjectMeta: meta, Spec: appsv1.StatefulSetSpec{Replicas: &one, ServiceName: scenario, Selector: &metav1.LabelSelector{MatchLabels: labels}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "registry.k8s.io/pause:3.10"}}}}}}
		if err := c.Create(ctx, sts); err != nil {
			t.Fatal(err)
		}
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: ns.Name, Name: scenario + "-0", Labels: labels, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(sts, appsv1.SchemeGroupVersion.WithKind("StatefulSet"))}}, Spec: *sts.Spec.Template.Spec.DeepCopy()}
		if err := c.Create(ctx, pod); err != nil && !apierrors.IsAlreadyExists(err) {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		interloper := &interveningClient{Client: c, beforePatch: func() error {
			annotations := map[string]string{"example.org/independent": "preserved"}
			if scenario != "other-metadata" {
				archived, _ := json.Marshal([]observedPod{{Name: "retired", UID: "retired-uid", Created: now.Add(-time.Minute), Ended: &now}})
				annotations[metricsPodsAnnotation] = string(archived)
			}
			data, _ := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": annotations}})
			return c.Patch(ctx, sts.DeepCopy(), client.RawPatch(types.MergePatchType, data))
		}}
		wp := WorkloadProcessor{client: interloper}
		w := discovery.Workload{Kind: "StatefulSet", Namespace: ns.Name, Name: sts.Name, Object: sts}
		pods, err := wp.metricPods(ctx, &w, &api.OptimizationPolicy{Spec: api.OptimizationPolicySpec{Mode: api.ModeRecommend}})
		if err != nil {
			t.Fatalf("%s: %v", scenario, err)
		}
		want := 2
		if scenario == "other-metadata" {
			want = 1
		}
		if len(pods) != want || interloper.writes != want {
			t.Fatalf("%s: %d pods, %d writes, want %d", scenario, len(pods), interloper.writes, want)
		}
		if err := c.Get(ctx, client.ObjectKeyFromObject(sts), sts); err != nil {
			t.Fatal(err)
		}
		saved, err := decodePodJournal(sts.Annotations[metricsPodsAnnotation])
		if err != nil || len(saved) != want || sts.Annotations["example.org/independent"] != "preserved" {
			t.Fatalf("%s: missing journal or independent annotation: %v", scenario, err)
		}
	}
}
