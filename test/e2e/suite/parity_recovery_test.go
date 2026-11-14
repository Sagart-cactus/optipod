package suite

import (
	"context"
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/safety"
	"github.com/optipod/optipod/test/e2e/framework"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Parity: actual OOM recovery", Label("parity", "oom"), Serial, func() {
	It("recovers without metrics, preserves pod identity, freezes reductions and latches repeated failures", func(ctx context.Context) {
		ns := specNamespace(ctx, "oom")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		samples := smokeSamples()[:1]
		dep := framework.NewDeploymentFixture(ns.Name, "oom", 1, smokeWebhookContainers(samples)...)
		co := &dep.Spec.Template.Spec.Containers[0]
		co.Image = "docker.io/optipod-e2e/load:dev"
		co.ImagePullPolicy = corev1.PullNever
		co.Command = nil
		co.Args = nil
		co.Resources.Requests[corev1.ResourceMemory] = resource.MustParse("64Mi")
		co.Resources.Limits = corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("64Mi")}
		Expect(env.Clients.Ctrl.Create(ctx, dep)).To(Succeed())
		pods, err := framework.WaitForPodCountReady(ctx, env.Clients, ns.Name, "app=oom", 1, framework.WaitOptions{Timeout: rolloutWaitTimeout})
		Expect(err).NotTo(HaveOccurred())
		podName, uid := pods[0].Name, pods[0].UID
		series := registerContainerSeries(ctx, ns.Name, dep, samples)
		series[0].MemoryBytes = []float64{40 * 1024 * 1024, 40 * 1024 * 1024, 40 * 1024 * 1024}
		Expect(env.Fixture.SetSeries(ctx, series)).To(Succeed())
		p := framework.NewPolicyFixture(ns.Name, "oom", api.ModeAuto, framework.WithReconciliationInterval(time.Hour))
		p.Spec.ResourcePolicy = &api.ResourcePolicy{ControlledResources: []api.ControlledResource{"memory"}}
		one := int32(1)
		p.Spec.UpdateStrategy.OOMRecovery = &api.OOMRecoveryConfig{MaxRecoveries: &one}
		p.Spec.UpdateStrategy.Stability = &api.StabilityConfig{Cooldown: metav1.Duration{Duration: time.Hour}}
		p.Spec.ResourceBounds.Memory.Max = resource.MustParse("256Mi")
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		waitForDeploymentAnnotation(ctx, ns.Name, dep.Name, map[string]string{api.AnnotationMemoryRequestPrefix + ".app": "64Mi"})
		Expect(env.Fixture.SetSeries(ctx, nil)).To(Succeed())
		triggerOOM := func() {
			_, err := env.Clients.Kube.CoreV1().RESTClient().Post().Namespace(ns.Name).Resource("pods").Name(podName+":8080").SubResource("proxy").Suffix("configure").Param("memory_mib", "300").Do(ctx).Raw()
			Expect(err).NotTo(HaveOccurred())
		}
		waitMemory := func(value string) {
			expected := resource.MustParse(value)
			Expect(framework.WaitForCondition(ctx, framework.WaitOptions{Timeout: 2 * time.Minute, Description: "OOM recovered to " + value}, func(ctx context.Context) (bool, error) {
				pod, err := env.Clients.Kube.CoreV1().Pods(ns.Name).Get(ctx, podName, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				if pod.UID != uid {
					return false, nil
				}
				if pod.Spec.Containers[0].Resources.Requests.Memory().Cmp(expected) != 0 {
					return false, nil
				}
				for _, status := range pod.Status.ContainerStatuses {
					if status.Name == "app" && status.State.Running != nil && status.Resources != nil && status.Resources.Requests.Memory().Cmp(expected) == 0 {
						return true, nil
					}
				}
				return false, nil
			})).To(Succeed())
		}
		readState := func() *safety.RecoveryState {
			d, err := env.Clients.Kube.AppsV1().Deployments(ns.Name).Get(ctx, dep.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			var state safety.State
			Expect(json.Unmarshal([]byte(d.Annotations[safety.StateAnnotation]), &state)).To(Succeed())
			return state.Containers["app"].Recovery
		}
		By("observing a real kernel OOM and respecting the preserved limit")
		triggerOOM()
		waitForWorkloadReason(ctx, p, "preserved memory limit")
		Expect(readState().Count).To(Equal(int32(1)))
		By("enabling limit updates and recovering in place despite lost metrics and cooldown")
		updatePolicy(ctx, ns.Name, p.Name, func(p *api.OptimizationPolicy) {
			factor := 1.0
			p.Spec.UpdateStrategy.LimitConfig = &api.LimitConfig{MemoryMode: "Multiplier", MemoryLimitMultiplier: &factor}
		})
		waitMemory("128Mi")
		restartController(ctx)
		Expect(readState().Count).To(Equal(int32(1)))
		By("keeping the emergency floor when low metrics return, even with unsafe decreases enabled")
		Expect(env.Fixture.SetSeries(ctx, series)).To(Succeed())
		updatePolicy(ctx, ns.Name, p.Name, func(p *api.OptimizationPolicy) {
			yes := true
			p.Spec.UpdateStrategy.AllowUnsafeMemoryDecrease = &yes
			p.Spec.ReconciliationInterval.Duration = time.Second
		})
		waitForDeploymentAnnotation(ctx, ns.Name, dep.Name, map[string]string{api.AnnotationMemoryRequestPrefix + ".app": "128Mi"})
		By("opening the circuit on a second actual OOM and requiring a reset token")
		triggerOOM()
		waitForWorkloadReason(ctx, p, "OOM circuit breaker")
		Expect(readState().CircuitOpen).To(BeTrue())
		pod, err := env.Clients.Kube.CoreV1().Pods(ns.Name).Get(ctx, podName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(pod.Spec.Containers[0].Resources.Requests.Memory().String()).To(Equal("128Mi"))
		updatePolicy(ctx, ns.Name, p.Name, func(p *api.OptimizationPolicy) {
			p.Spec.UpdateStrategy.OOMRecovery.ResetToken = "reviewed-second-incident"
		})
		waitMemory("192Mi")
		Expect(readState().CircuitOpen).To(BeFalse())
		Expect(readState().Count).To(Equal(int32(1)))
	})
})
