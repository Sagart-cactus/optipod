package suite

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Real metrics backends", Label("live-metrics"), Serial, func() {
	DescribeTable("observes live CPU and memory load and applies its recommendations", func(ctx context.Context, provider string) {
		By("selecting the real backend, with restoration after the test")
		args := []string{"--metrics-provider=" + provider, "--metrics-server-sampling-interval=5s", "--metrics-server-min-samples-required=2"}
		if provider == "prometheus" {
			args = append(args, "--prometheus-url=http://prometheus.optipod-e2e-backends.svc:9090")
		}
		original := setControllerMetricArgs(ctx, args)
		DeferCleanup(func(ctx context.Context) { restoreControllerArgs(ctx, original) })
		ns := specNamespace(ctx, "live-"+provider)
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		two := int32(2)
		dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "load", Namespace: ns.Name, Labels: map[string]string{"app": "load"}}, Spec: appsv1.DeploymentSpec{Replicas: &two, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "load"}}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "load", "optipod.io/webhook-enabled": "true"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "docker.io/optipod-e2e/load:dev", ImagePullPolicy: corev1.PullNever, Ports: []corev1.ContainerPort{{ContainerPort: 8080}}, Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("50m"), corev1.ResourceMemory: resource.MustParse("32Mi")}, Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("256Mi")}}}}}}}}
		Expect(env.Clients.Ctrl.Create(ctx, dep)).To(Succeed())
		_, err := framework.WaitForPodCountReady(ctx, env.Clients, ns.Name, "app=load", 2, framework.WaitOptions{Timeout: rolloutWaitTimeout})
		Expect(err).NotTo(HaveOccurred())
		p := framework.NewPolicyFixture(ns.Name, "live", api.ModeRecommend, framework.WithReconciliationInterval(5*time.Second))
		p.Spec.MetricsConfig.Provider = provider
		p.Spec.MetricsConfig.RollingWindow.Duration = 2 * time.Minute
		p.Spec.MetricsConfig.MetricsServer = &api.MetricsServerConfig{MinSamplesRequired: &two}
		p.Spec.MetricsConfig.MinSamples = &two
		p.Spec.MetricsConfig.MinimumHistory.Duration = time.Minute
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		By("waiting for actual usage to produce a nonzero CPU recommendation")
		Expect(framework.WaitForCondition(ctx, framework.WaitOptions{Timeout: 4 * time.Minute, Description: "live source recommendation"}, func(ctx context.Context) (bool, error) {
			if err := env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(dep), dep); err != nil {
				return false, err
			}
			cpu, err := resource.ParseQuantity(dep.Annotations["optipod.io/recommendation.app.cpu-request"])
			if err != nil {
				return false, nil
			}
			mem, err := resource.ParseQuantity(dep.Annotations["optipod.io/recommendation.app.memory-request"])
			if err != nil {
				return false, nil
			}
			return cpu.MilliValue() >= 180 && cpu.MilliValue() <= 400 && mem.Value() >= 64*1024*1024 && mem.Value() <= 192*1024*1024, nil
		})).To(Succeed())
		Expect(dep.Spec.Template.Spec.Containers[0].Resources.Requests.Cpu().MilliValue()).To(Equal(int64(50)))
		By("enabling Auto and checking admission of a naturally replaced pod")
		Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(p), p)).To(Succeed())
		p.Spec.Mode = api.ModeAuto
		Expect(env.Clients.Ctrl.Update(ctx, p)).To(Succeed())
		waitForDeploymentAnnotation(ctx, ns.Name, dep.Name, map[string]string{"optipod.io/applied-policy-generation": fmt.Sprint(p.Generation)})
		before := podUIDs(ctx, ns.Name, "app=load")
		for _, name := range before {
			Expect(env.Clients.Kube.CoreV1().Pods(ns.Name).Delete(ctx, name, metav1.DeleteOptions{})).To(Succeed())
			break
		}
		pods := waitForReplacementPod(ctx, ns.Name, "app=load", before)
		Expect(pods[0].Spec.Containers[0].Resources.Requests.Cpu().MilliValue()).To(BeNumerically(">=", 100))
		Expect(pods[0].Spec.Containers[0].Resources.Requests.Memory().Value()).To(BeNumerically(">=", 64*1024*1024))
	}, Entry("Prometheus with kubelet cAdvisor scraping", "prometheus"), Entry("metrics-server with asynchronous sample history", "metrics-server"))
})

func setControllerMetricArgs(ctx context.Context, overrides []string) []string {
	d, err := env.Clients.Kube.AppsV1().Deployments(framework.SystemNamespace).Get(ctx, framework.ControllerDeploymentName, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())
	original := append([]string(nil), d.Spec.Template.Spec.Containers[0].Args...)
	args := []string{}
	for _, arg := range original {
		keep := true
		for _, override := range overrides {
			if strings.HasPrefix(arg, strings.SplitN(override, "=", 2)[0]+"=") {
				keep = false
			}
		}
		if keep {
			args = append(args, arg)
		}
	}
	restoreControllerArgs(ctx, append(args, overrides...))
	return original
}

func restoreControllerArgs(ctx context.Context, args []string) {
	d, err := env.Clients.Kube.AppsV1().Deployments(framework.SystemNamespace).Get(ctx, framework.ControllerDeploymentName, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())
	d.Spec.Template.Spec.Containers[0].Args = args
	d, err = env.Clients.Kube.AppsV1().Deployments(d.Namespace).Update(ctx, d, metav1.UpdateOptions{})
	Expect(err).NotTo(HaveOccurred())
	gen := d.Generation
	Expect(framework.WaitForCondition(ctx, framework.WaitOptions{Timeout: 3 * time.Minute, Description: "metrics backend controller rollout"}, func(ctx context.Context) (bool, error) {
		current, err := env.Clients.Kube.AppsV1().Deployments(d.Namespace).Get(ctx, d.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return current.Status.ObservedGeneration >= gen && current.Status.UpdatedReplicas == 1 && current.Status.ReadyReplicas == 1 && current.Status.Replicas == 1, nil
	})).To(Succeed())
}
