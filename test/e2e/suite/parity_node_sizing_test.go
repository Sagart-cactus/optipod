package suite

import (
	"context"
	"math"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/history"
	"github.com/optipod/optipod/internal/nodesizing"
	"github.com/optipod/optipod/test/e2e/framework"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Parity: node-aware DaemonSets", Label("parity", "node-sizing"), func() {
	It("sizes new pods from two real nodes, retains existing pods, and rejects ambiguous destinations", func(ctx context.Context) {
		ns := specNamespace(ctx, "node-sizing")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		nodes, err := env.Clients.Kube.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(nodes.Items)).To(BeNumerically(">=", 2), "node-sizing requires the multi-node dev cluster")
		Expect(nodes.Items[0].Status.Allocatable.Cpu().MilliValue()).NotTo(Equal(nodes.Items[1].Status.Allocatable.Cpu().MilliValue()))
		d := framework.NewDaemonSetFixture(ns.Name, "node-sized", smokeWebhookContainers(smokeSamples())...)
		d.Spec.Template.Spec.Tolerations = []corev1.Toleration{{Operator: corev1.TolerationOpExists}}
		Expect(env.Clients.Ctrl.Create(ctx, d)).To(Succeed())
		pods, err := framework.WaitForPodCountReady(ctx, env.Clients, ns.Name, "app=node-sized", len(nodes.Items), framework.WaitOptions{Timeout: rolloutWaitTimeout})
		Expect(err).NotTo(HaveOccurred())
		cpu, memory := 1.0, 0.3
		p := framework.NewPolicyFixture(ns.Name, "node-sized", api.ModeRecommend, framework.WithStrategy("webhook", "onNextRestart"), framework.WithReconciliationInterval(time.Second), framework.WithResourceBounds("1m", "4", "16Mi", "1Gi"))
		p.Spec.Selector.WorkloadTypes = &api.WorkloadTypeFilter{Include: []api.WorkloadType{api.WorkloadTypeDaemonSet}}
		p.Spec.UpdateStrategy.LimitConfig = &api.LimitConfig{CPUMode: "None", MemoryMode: "Multiplier"}
		p.Spec.ResourcePolicy = &api.ResourcePolicy{ExcludedContainers: []string{"sidecar"}}
		p.Spec.DaemonSetSizing = &api.DaemonSetSizing{Default: &api.NodePercentages{CPUPercent: &cpu, MemoryPercent: &memory}}
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		Eventually(func() string {
			Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(d), d)).To(Succeed())
			return d.Annotations[nodesizing.SummaryAnnotation]
		}, time.Minute, time.Second).ShouldNot(BeEmpty())
		before := podUIDs(ctx, ns.Name, "app=node-sized")
		updatePolicy(ctx, ns.Name, p.Name, func(p *api.OptimizationPolicy) { p.Spec.Mode = api.ModeAuto })
		Eventually(func() string {
			Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(d), d)).To(Succeed())
			return d.Annotations[api.AnnotationWebhookEnabled]
		}, time.Minute, time.Second).Should(Equal("true"))
		Consistently(func() map[types.UID]string { return podUIDs(ctx, ns.Name, "app=node-sized") }, 5*time.Second, time.Second).Should(Equal(before))
		probe := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "unknown-destination", Namespace: ns.Name, Labels: d.Spec.Template.Labels, OwnerReferences: pods[0].OwnerReferences}, Spec: *d.Spec.Template.Spec.DeepCopy()}
		unchanged, err := env.Clients.Kube.CoreV1().Pods(ns.Name).Create(ctx, probe, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
		Expect(err).NotTo(HaveOccurred())
		Expect(unchanged.Spec.Containers[0].Resources.Requests.Cpu().MilliValue()).To(Equal(int64(100)))
		for _, pod := range pods {
			Expect(env.Clients.Kube.CoreV1().Pods(ns.Name).Delete(ctx, pod.Name, metav1.DeleteOptions{})).To(Succeed())
		}
		Eventually(func() bool {
			items, err := env.Clients.Kube.CoreV1().Pods(ns.Name).List(ctx, metav1.ListOptions{LabelSelector: "app=node-sized"})
			if err != nil || len(items.Items) != len(nodes.Items) {
				return false
			}
			for _, pod := range items.Items {
				if _, old := before[pod.UID]; old || pod.Spec.NodeName == "" {
					return false
				}
				node, err := env.Clients.Kube.CoreV1().Nodes().Get(ctx, pod.Spec.NodeName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				want := int64(math.Ceil(float64(node.Status.Allocatable.Cpu().MilliValue()) / 100))
				if pod.Spec.Containers[0].Resources.Requests.Cpu().MilliValue() != want {
					return false
				}
				wantMem := int64(math.Ceil(float64(node.Status.Allocatable.Memory().Value()) * .003))
				if pod.Spec.Containers[0].Resources.Requests.Memory().Value() != wantMem {
					return false
				}
				if pod.Spec.Containers[1].Resources.Requests.Cpu().MilliValue() != 50 {
					return false
				}
			}
			return true
		}, time.Minute, time.Second).Should(BeTrue())
		Eventually(func() int {
			cms := &corev1.ConfigMapList{}
			Expect(env.Clients.Ctrl.List(ctx, cms, client.InNamespace(ns.Name), client.MatchingLabels{history.Label: "true"})).To(Succeed())
			count := 0
			for _, cm := range cms.Items {
				record, err := history.Read(ctx, env.Clients.Ctrl, ns.Name, cm.Name)
				Expect(err).NotTo(HaveOccurred())
				if record.Method == "NodeAdmission" && record.Phase == "Observed" && record.Node != "" {
					original := record.BeforeTemplate["app"]
					Expect(original.Requests.Cpu().MilliValue()).To(Equal(int64(100)))
					count++
				}
			}
			return count
		}, time.Minute, time.Second).Should(Equal(len(nodes.Items)))

	})
})
