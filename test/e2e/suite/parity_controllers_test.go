package suite

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/history"
	"github.com/optipod/optipod/test/e2e/framework"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Parity: additional workload controllers", Label("parity", "controllers"), func() {
	for _, kind := range []string{"ReplicaSet", "Rollout"} {
		for _, resize := range []bool{true, false} {
			method := "restart"
			if resize {
				method = "resize"
			}
			It("optimizes "+kind+" through "+method+" and resolves owning UIDs", func(ctx context.Context) {
				ns := specNamespace(ctx, "controllers")
				DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
				samples := smokeSamples()[:1]
				template := framework.NewDeploymentFixture(ns.Name, "extra", 1, smokeWebhookContainers(samples)...)
				var object client.Object
				if kind == "ReplicaSet" {
					object = &appsv1.ReplicaSet{ObjectMeta: template.ObjectMeta, Spec: appsv1.ReplicaSetSpec{Replicas: template.Spec.Replicas, Selector: template.Spec.Selector, Template: template.Spec.Template}}
				} else {
					raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(template)
					Expect(err).NotTo(HaveOccurred())
					object = &unstructured.Unstructured{Object: map[string]any{"apiVersion": "argoproj.io/v1alpha1", "kind": "Rollout", "metadata": raw["metadata"], "spec": map[string]any{"replicas": int64(1), "selector": map[string]any{"matchLabels": map[string]any{"app": "extra"}}, "template": raw["spec"].(map[string]any)["template"], "strategy": map[string]any{"canary": map[string]any{}}}}}
				}
				Expect(env.Clients.Ctrl.Create(ctx, object)).To(Succeed())
				pods, err := framework.WaitForPodCountReady(ctx, env.Clients, ns.Name, "app=extra", 1, framework.WaitOptions{Timeout: rolloutWaitTimeout})
				Expect(err).NotTo(HaveOccurred())
				if kind == "Rollout" {
					Eventually(func() string {
						Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(object), object)).To(Succeed())
						phase, _, _ := unstructured.NestedString(object.(*unstructured.Unstructured).Object, "status", "phase")
						return phase
					}, time.Minute, time.Second).Should(Equal("Healthy"))
				}
				Expect(env.Fixture.SetSeries(ctx, []framework.FixtureSeries{{Namespace: ns.Name, Pod: pods[0].Name, Container: "app", CPUCores: []float64{.25, .25, .25}, MemoryBytes: []float64{100 * 1024 * 1024, 100 * 1024 * 1024, 100 * 1024 * 1024}}})).To(Succeed())
				before := podUIDs(ctx, ns.Name, "app=extra")
				p := framework.NewPolicyFixture(ns.Name, "extra", api.ModeAuto, framework.WithStrategy("webhook", "immediate"), framework.WithReconciliationInterval(time.Second))
				p.Spec.Selector.WorkloadTypes = &api.WorkloadTypeFilter{Include: []api.WorkloadType{api.WorkloadTypeReplicaSet, api.WorkloadTypeRollout}}
				p.Spec.UpdateStrategy.AllowInPlaceResize = resize
				p.Spec.UpdateStrategy.AllowRecreate = !resize
				if kind == "Rollout" && resize {
					u := object.(*unstructured.Unstructured)
					Expect(unstructured.SetNestedField(u.Object, true, "spec", "paused")).To(Succeed())
					Expect(env.Clients.Ctrl.Update(ctx, u)).To(Succeed())
				}
				Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
				if kind == "Rollout" && resize {
					// Both the early capability guard and final uncached guard
					// reject paused Rollouts. Their messages differ only in the
					// leading name; assert the common reason and actual resources.
					waitForWorkloadReason(ctx, p, "Rollout is paused")
					Expect(podUIDs(ctx, ns.Name, "app=extra")).To(Equal(before))
					pausedPods, err := env.Clients.Kube.CoreV1().Pods(ns.Name).List(ctx, metav1.ListOptions{LabelSelector: "app=extra"})
					Expect(err).NotTo(HaveOccurred())
					Expect(pausedPods.Items).To(HaveLen(1))
					expectUnappliedContainerRequests(pausedPods.Items[0].Spec.Containers, samples, "paused Argo Rollout")
					u := object.(*unstructured.Unstructured)
					Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(u), u)).To(Succeed())
					Expect(unstructured.SetNestedField(u.Object, false, "spec", "paused")).To(Succeed())
					Expect(env.Clients.Ctrl.Update(ctx, u)).To(Succeed())
				}

				if resize {
					waitForWorkloadReason(ctx, p, "InPlace")
					Expect(podUIDs(ctx, ns.Name, "app=extra")).To(Equal(before))
				} else {
					waitForReplacementPod(ctx, ns.Name, "app=extra", before)
				}
				Eventually(func() int64 {
					list, err := env.Clients.Kube.CoreV1().Pods(ns.Name).List(ctx, metav1.ListOptions{LabelSelector: "app=extra"})
					if err != nil || len(list.Items) != 1 {
						return -1
					}
					return list.Items[0].Spec.Containers[0].Resources.Requests.Cpu().MilliValue()
				}, time.Minute, time.Second).Should(Equal(int64(300)))
				Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(object), object)).To(Succeed())
				Eventually(func() string {
					Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(object), object)).To(Succeed())
					return object.GetAnnotations()[history.Latest]
				}, time.Minute, time.Second).ShouldNot(BeEmpty())
				record, err := history.Read(ctx, env.Clients.Ctrl, ns.Name, object.GetAnnotations()[history.Latest])
				Expect(err).NotTo(HaveOccurred())
				Expect(record.Workload.UID).To(Equal(object.GetUID()))
				Expect(record.Workload.Kind).To(Equal(kind))
				Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(p), p)).To(Succeed())
				Expect(p.Status.WorkloadsDiscovered).To(Equal(1))
			})
		}
	}
})
