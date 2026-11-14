package suite

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/safety"
	"github.com/optipod/optipod/internal/webhook"
	"github.com/optipod/optipod/test/e2e/framework"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

var _ = Describe("Parity: persistent stability guards", Label("parity", "stability"), Serial, func() {
	DescribeTable("enforces memory safety and percentage thresholds", func(ctx context.Context, mode string) {
		ns := specNamespace(ctx, "stability-"+mode)
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		samples := smokeSamples()[:1]
		dep := framework.NewDeploymentFixture(ns.Name, "stability", 1, smokeWebhookContainers(samples)...)
		dep.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceMemory] = resource.MustParse("512Mi")
		dep.Spec.Template.Spec.Containers[0].Resources.Limits = corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")}
		Expect(env.Clients.Ctrl.Create(ctx, dep)).To(Succeed())
		_, err := framework.WaitForPodCountReady(ctx, env.Clients, ns.Name, "app=stability", 1, framework.WaitOptions{Timeout: rolloutWaitTimeout})
		Expect(err).NotTo(HaveOccurred())
		series := registerContainerSeries(ctx, ns.Name, dep, samples)
		series[0].CPUCores = []float64{0.105, 0.105, 0.105}
		Expect(env.Fixture.SetSeries(ctx, series)).To(Succeed())
		p := framework.NewPolicyFixture(ns.Name, "stability", api.ModeAuto, framework.WithReconciliationInterval(time.Second))
		one := 1.0
		p.Spec.MetricsConfig.SafetyFactor = &one
		p.Spec.UpdateStrategy.Stability = &api.StabilityConfig{MinimumChangePercent: 10, Cooldown: metav1.Duration{Duration: time.Hour}}
		p.Spec.UpdateStrategy.LimitConfig = &api.LimitConfig{MemoryMode: "Multiplier", MemoryLimitMultiplier: &one}
		expectedMemory := int64(512 * 1024 * 1024)
		expectedLimit := int64(1024 * 1024 * 1024)
		switch mode {
		case "gradual":
			p.Spec.UpdateStrategy.GradualDecreaseConfig = &api.GradualDecreaseConfig{Enabled: true}
			expectedMemory = int64(math.Ceil(float64(expectedMemory) * 0.9))
			expectedLimit = int64(math.Ceil(float64(expectedLimit) * 0.9))
		case "unsafe":
			yes := true
			p.Spec.UpdateStrategy.AllowUnsafeMemoryDecrease = &yes
			expectedMemory = 100 * 1024 * 1024
			expectedLimit = expectedMemory
		}
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		got := waitForDeploymentAnnotation(ctx, ns.Name, dep.Name, map[string]string{api.AnnotationCPURequestPrefix + ".app": "100m", api.AnnotationMemoryRequestPrefix + ".app": resource.NewQuantity(expectedMemory, resource.BinarySI).String(), api.AnnotationMemoryLimitPrefix + ".app": resource.NewQuantity(expectedLimit, resource.BinarySI).String()})
		if mode != "gradual" {
			return
		}
		var before safety.State
		Expect(json.Unmarshal([]byte(got.Annotations[safety.StateAnnotation]), &before)).To(Succeed())
		Expect(before.LastChange.IsZero()).To(BeFalse())
		series[0].CPUCores = []float64{0.9, 0.9, 0.9}
		Expect(env.Fixture.SetSeries(ctx, series)).To(Succeed())
		// Restart the controller and wait for a renewed plan. The unchanged
		// cooldown timestamp proves it was recovered from durable state.
		restartController(ctx)
		got, err = env.Clients.Kube.AppsV1().Deployments(ns.Name).Get(ctx, dep.Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		oldExpiry := got.Annotations[webhook.RecommendationExpiry]
		Expect(framework.WaitForCondition(ctx, framework.WaitOptions{Timeout: 3 * time.Minute, Description: "cooldown after controller restart"}, func(ctx context.Context) (bool, error) {
			got, err = env.Clients.Kube.AppsV1().Deployments(ns.Name).Get(ctx, dep.Name, metav1.GetOptions{})
			if err != nil {
				return false, err
			}
			return got.Annotations[webhook.RecommendationExpiry] > oldExpiry, nil
		})).To(Succeed())
		Expect(got.Annotations[api.AnnotationCPURequestPrefix+".app"]).To(Equal("100m"))
		Expect(got.Annotations[api.AnnotationMemoryRequestPrefix+".app"]).To(Equal(resource.NewQuantity(expectedMemory, resource.BinarySI).String()))
		var after safety.State
		Expect(json.Unmarshal([]byte(got.Annotations[safety.StateAnnotation]), &after)).To(Succeed())
		Expect(after.LastChange).To(Equal(before.LastChange))
	}, Entry("default blocks reductions", "blocked"), Entry("bounded gradual decrease with durable cooldown", "gradual"), Entry("explicit unsafe decrease opt-in", "unsafe"))
})

func restartController(ctx context.Context) {
	d, err := env.Clients.Kube.AppsV1().Deployments(framework.SystemNamespace).Patch(ctx, framework.ControllerDeploymentName, "application/strategic-merge-patch+json", []byte(`{"spec":{"template":{"metadata":{"annotations":{"optipod.io/e2e-restart":"`+time.Now().Format(time.RFC3339Nano)+`"}}}}}`), metav1.PatchOptions{})
	Expect(err).NotTo(HaveOccurred())
	Expect(framework.WaitForCondition(ctx, framework.WaitOptions{Timeout: 3 * time.Minute, Description: "replacement controller ready and holding reconciliation lease"}, func(ctx context.Context) (bool, error) {
		cur, err := env.Clients.Kube.AppsV1().Deployments(d.Namespace).Get(ctx, d.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		if cur.Status.ObservedGeneration < d.Generation || cur.Status.UpdatedReplicas != 1 || cur.Status.ReadyReplicas != 1 || cur.Status.Replicas != 1 {
			return false, nil
		}
		pods, err := env.Clients.Kube.CoreV1().Pods(d.Namespace).List(ctx, metav1.ListOptions{LabelSelector: labels.Set(cur.Spec.Selector.MatchLabels).String()})
		if err != nil {
			return false, err
		}
		lease, err := env.Clients.Kube.CoordinationV1().Leases(d.Namespace).Get(ctx, "2e85d309.optipod.io", metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, pod := range pods.Items {
			if pod.DeletionTimestamp == nil && isReady(&pod) && lease.Spec.HolderIdentity != nil && strings.HasPrefix(*lease.Spec.HolderIdentity, pod.Name+"_") {
				return true, nil
			}
		}
		return false, nil
	})).To(Succeed())
}
