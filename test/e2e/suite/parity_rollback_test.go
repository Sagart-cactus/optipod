package suite

import (
	"context"
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/history"
	"github.com/optipod/optipod/internal/webhook"
	"github.com/optipod/optipod/test/e2e/framework"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Parity: guarded rollback", Label("parity", "rollback"), Serial, func() {
	for _, strategy := range []string{"webhook", "ssa"} {

		It("restores original resources and stays paused using "+strategy, func(ctx context.Context) {
			ns := specNamespace(ctx, "rollback")
			DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
			samples := smokeSamples()[:1]
			dep := createResizeWorkload(ctx, ns.Name, "rollback", samples)
			originalUIDs := podUIDs(ctx, ns.Name, "app="+dep.Name)
			registerContainerSeries(ctx, ns.Name, dep, samples)
			p := framework.NewPolicyFixture(ns.Name, "rollback", api.ModeAuto, framework.WithStrategy(strategy, "immediate"), framework.WithReconciliationInterval(time.Second))
			if strategy == "webhook" {
				p.Spec.UpdateStrategy.AllowInPlaceResize = true
				p.Spec.UpdateStrategy.AllowRecreate = false
			}
			Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
			var record *history.Record
			Eventually(func() string {
				Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(dep), dep)).To(Succeed())
				var err error
				record, err = history.Read(ctx, env.Clients.Ctrl, ns.Name, dep.Annotations[history.Latest])
				if err != nil {
					return ""
				}
				return record.Phase
			}, time.Minute, time.Second).Should(Equal("Applied"))
			_, err := framework.WaitForPodCountReady(ctx, env.Clients, ns.Name, "app="+dep.Name, 1, framework.WaitOptions{Timeout: rolloutWaitTimeout})
			Expect(err).NotTo(HaveOccurred())
			patch, _ := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{history.RollbackTo: record.Name}}})
			Expect(env.Clients.Ctrl.Patch(ctx, dep, client.RawPatch(types.MergePatchType, patch))).To(Succeed())
			Eventually(func() string {
				current, err := history.Read(ctx, env.Clients.Ctrl, ns.Name, record.Name)
				if err != nil {
					return err.Error()
				}
				return current.Phase + ":" + current.RollbackError
			}, 2*time.Minute, time.Second).Should(Equal("RolledBack:"))
			Eventually(func() int64 {
				pods, err := env.Clients.Kube.CoreV1().Pods(ns.Name).List(ctx, metav1.ListOptions{LabelSelector: "app=" + dep.Name})
				if err != nil || len(pods.Items) != 1 {
					return -1
				}
				return pods.Items[0].Spec.Containers[0].Resources.Requests.Cpu().MilliValue()
			}, time.Minute, time.Second).Should(Equal(int64(100)))
			if strategy == "webhook" {
				Expect(podUIDs(ctx, ns.Name, "app="+dep.Name)).To(Equal(originalUIDs))
			}
			Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(dep), dep)).To(Succeed())
			Expect(dep.Annotations[history.Paused]).To(Equal("true"))
			if strategy == "ssa" {
				Expect(dep.Spec.Template.Spec.Containers[0].Resources.Requests.Cpu().MilliValue()).To(Equal(int64(100)))
			}
			restartController(ctx)
			Consistently(func() string {
				Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(dep), dep)).To(Succeed())
				return dep.Annotations[history.Latest]
			}, 4*time.Second, time.Second).Should(Equal(record.Name))
			// Paused restoration remains bound to its snapshot after ordinary plan expiry.
			patch, _ = json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{webhook.RecommendationExpiry: time.Now().Add(-time.Hour).Format(time.RFC3339)}}})
			Expect(env.Clients.Ctrl.Patch(ctx, dep, client.RawPatch(types.MergePatchType, patch))).To(Succeed())
			pods, err := env.Clients.Kube.CoreV1().Pods(ns.Name).List(ctx, metav1.ListOptions{LabelSelector: "app=" + dep.Name})
			Expect(err).NotTo(HaveOccurred())
			Expect(pods.Items).To(HaveLen(1))
			probe := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "rollback-probe", Namespace: ns.Name, Labels: dep.Spec.Template.Labels, OwnerReferences: pods.Items[0].OwnerReferences}, Spec: *dep.Spec.Template.Spec.DeepCopy()}
			created, err := env.Clients.Kube.CoreV1().Pods(ns.Name).Create(ctx, probe, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
			Expect(err).NotTo(HaveOccurred())
			Expect(created.Spec.Containers[0].Resources.Requests.Cpu().MilliValue()).To(Equal(int64(100)))
		})
	}
	It("rejects rollback after an independent template change", func(ctx context.Context) {
		ns := specNamespace(ctx, "rollback-drift")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		samples := smokeSamples()[:1]
		dep := createResizeWorkload(ctx, ns.Name, "drift", samples)
		registerContainerSeries(ctx, ns.Name, dep, samples)
		p := framework.NewPolicyFixture(ns.Name, "drift", api.ModeAuto, framework.WithStrategy("webhook", "onNextRestart"), framework.WithReconciliationInterval(time.Second))
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		Eventually(func() string {
			Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(dep), dep)).To(Succeed())
			return dep.Annotations[history.Latest]
		}, time.Minute, time.Second).ShouldNot(BeEmpty())
		patch, _ := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{history.RollbackTo: dep.Annotations[history.Latest]}}, "spec": map[string]any{"template": map[string]any{"metadata": map[string]any{"annotations": map[string]string{"external-change": "true"}}}}})
		Expect(env.Clients.Ctrl.Patch(ctx, dep, client.RawPatch(types.MergePatchType, patch))).To(Succeed())
		waitForWorkloadReason(ctx, p, "non-resource template changed")
		Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(dep), dep)).To(Succeed())
		Expect(dep.Annotations[history.Paused]).NotTo(Equal("true"))
	})
})
