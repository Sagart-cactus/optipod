package suite

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/history"
	"github.com/optipod/optipod/test/e2e/framework"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Parity: durable resource journal", Label("parity", "audit"), func() {
	It("keeps immutable before/after plans across restarts and workload deletion without recording renewals", func(ctx context.Context) {
		ns := specNamespace(ctx, "audit")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		samples := smokeSamples()[:1]
		dep := createResizeWorkload(ctx, ns.Name, "audited", samples)
		registerContainerSeries(ctx, ns.Name, dep, samples)
		p := framework.NewPolicyFixture(ns.Name, "audited", api.ModeRecommend, framework.WithStrategy("webhook", "immediate"), framework.WithReconciliationInterval(time.Second))
		p.Spec.UpdateStrategy.AllowInPlaceResize = true
		p.Spec.UpdateStrategy.AllowRecreate = false
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		var recommendationRecord *history.Record
		Eventually(func() string {
			Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(dep), dep)).To(Succeed())
			if dep.Annotations[history.Latest] == "" {
				return ""
			}
			var err error
			recommendationRecord, err = history.Read(ctx, env.Clients.Ctrl, ns.Name, dep.Annotations[history.Latest])
			if err != nil {
				return ""
			}
			return recommendationRecord.Phase
		}, time.Minute, time.Second).Should(Equal("Recommended"))
		updatePolicy(ctx, ns.Name, p.Name, func(p *api.OptimizationPolicy) { p.Spec.Mode = api.ModeAuto })
		waitForWorkloadReason(ctx, p, "InPlace")
		Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(dep), dep)).To(Succeed())
		record, err := history.Read(ctx, env.Clients.Ctrl, ns.Name, dep.Annotations[history.Latest])
		Expect(err).NotTo(HaveOccurred())
		Expect(record.Phase).To(Equal("Applied"))
		Expect(record.Method).To(Equal("InPlace"))
		Expect(record.PolicySpec).NotTo(BeNil())
		Expect(record.PolicySpec.Mode).To(Equal(api.ModeAuto))
		Expect(recommendationRecord.PolicySpec.Mode).To(Equal(api.ModeRecommend))
		Expect(record.Previous).To(Equal(recommendationRecord.Name))
		before := record.BeforePods[0].Resources["app"]
		Expect(before.Requests.Cpu().MilliValue()).To(Equal(int64(100)))
		Expect(record.Plan["app"]["requests"].(map[string]any)["cpu"]).To(Equal("300m"))
		cm := &corev1.ConfigMap{}
		Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: record.Name}, cm)).To(Succeed())
		Expect(cm.Immutable).NotTo(BeNil())
		Expect(*cm.Immutable).To(BeTrue())
		cm.Data[history.DataKey] = "{}"
		Expect(env.Clients.Ctrl.Update(ctx, cm)).NotTo(Succeed())
		restartController(ctx)
		Consistently(func() int {
			list := &corev1.ConfigMapList{}
			Expect(env.Clients.Ctrl.List(ctx, list, client.InNamespace(ns.Name), client.MatchingLabels{history.Label: "true", history.WorkloadLabel: string(dep.UID)})).To(Succeed())
			count := 0
			for _, cm := range list.Items {
				r, err := history.Read(ctx, env.Clients.Ctrl, ns.Name, cm.Name)
				Expect(err).NotTo(HaveOccurred())
				if r.Method != "Decision" {
					count++
				}
			}
			return count
		}, 5*time.Second, time.Second).Should(Equal(2))
		Expect(env.Clients.Ctrl.Delete(ctx, p)).To(Succeed())
		Expect(env.Clients.Ctrl.Delete(ctx, dep)).To(Succeed())
		retained, err := history.Read(ctx, env.Clients.Ctrl, ns.Name, record.Name)
		Expect(err).NotTo(HaveOccurred())
		Expect(retained.Workload.UID).To(Equal(dep.UID))
		Expect(retained.PlanHash).To(Equal(record.PlanHash))
		// Journals deliberately have no owner reference to the deleted workload.
		cm, err = env.Clients.Kube.CoreV1().ConfigMaps(ns.Name).Get(ctx, record.Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(cm.OwnerReferences).To(BeEmpty())
	})
})
