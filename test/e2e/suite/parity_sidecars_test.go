package suite

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Parity: sidecar controls", Label("parity", "sidecars"), func() {
	DescribeTable("optimizes opted-in native sidecars while preserving one-shot init containers", func(ctx context.Context, strategy string) {
		ns := specNamespace(ctx, "native-sidecars")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		samples := smokeSamples()
		dep := framework.NewDeploymentFixture(ns.Name, "native", 1, smokeWebhookContainers(samples)...)
		sidecar := dep.Spec.Template.Spec.Containers[1]
		always := corev1.ContainerRestartPolicyAlways
		sidecar.RestartPolicy = &always
		init := batchTemplate(ns.Name).Spec.Containers[0]
		init.Name = "setup"
		dep.Spec.Template.Spec.InitContainers = []corev1.Container{init, sidecar}
		dep.Spec.Template.Spec.Containers = dep.Spec.Template.Spec.Containers[:1]
		Expect(env.Clients.Ctrl.Create(ctx, dep)).To(Succeed())
		_, err := framework.WaitForPodCountReady(ctx, env.Clients, ns.Name, "app=native", 1, framework.WaitOptions{Timeout: time.Minute})
		Expect(err).NotTo(HaveOccurred())
		registerContainerSeries(ctx, ns.Name, dep, samples)
		before := podUIDs(ctx, ns.Name, "app=native")
		p := framework.NewPolicyFixture(ns.Name, "native", api.ModeAuto, framework.WithStrategy(strategy, "immediate"), framework.WithReconciliationInterval(time.Second))
		p.Spec.ResourcePolicy = &api.ResourcePolicy{IncludeNativeSidecars: true}
		if strategy == "webhook" {
			p.Spec.UpdateStrategy.AllowInPlaceResize = true
			p.Spec.UpdateStrategy.AllowRecreate = false
		}
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		Eventually(func() int64 {
			pods, err := env.Clients.Kube.CoreV1().Pods(ns.Name).List(ctx, metav1.ListOptions{LabelSelector: "app=native"})
			if err != nil || len(pods.Items) != 1 {
				return 0
			}
			return pods.Items[0].Spec.InitContainers[1].Resources.Requests.Cpu().MilliValue()
		}, time.Minute, time.Second).Should(Equal(int64(150)))
		pods, err := framework.WaitForPodCountReady(ctx, env.Clients, ns.Name, "app=native", 1, framework.WaitOptions{Timeout: time.Minute})
		Expect(err).NotTo(HaveOccurred())
		Expect(pods[0].Spec.InitContainers[0].Resources.Requests.Cpu().MilliValue()).To(Equal(int64(100)))
		Expect(pods[0].Spec.InitContainers[1].Resources.Requests.Memory().String()).To(Equal(smokeSidecar.expectedMemory))
		if strategy == "webhook" {
			waitForWorkloadReason(ctx, p, "InPlace")
			Expect(podUIDs(ctx, ns.Name, "app=native")).To(Equal(before))
			Expect(pods[0].Status.InitContainerStatuses[1].RestartCount).To(Equal(int32(0)))
		}
		// Relinquishing a previously SSA-owned native sidecar must preserve its values.
		registerContainerSeries(ctx, ns.Name, dep, samples)
		updatePolicy(ctx, ns.Name, p.Name, func(p *api.OptimizationPolicy) { p.Spec.ResourcePolicy.IncludeNativeSidecars = false })
		Consistently(func() int64 {
			pods, err := env.Clients.Kube.CoreV1().Pods(ns.Name).List(ctx, metav1.ListOptions{LabelSelector: "app=native"})
			if err != nil || len(pods.Items) != 1 {
				return 0
			}
			return pods.Items[0].Spec.InitContainers[1].Resources.Requests.Cpu().MilliValue()
		}, 4*time.Second, time.Second).Should(Equal(int64(150)))
	}, Entry("in-place native sidecar", "webhook"), Entry("SSA native sidecar", "ssa"))

	It("learns a real injected sidecar and applies it on admission without adding it to the workload template", func(ctx context.Context) {
		ns := specNamespace(ctx, "injected-sidecar")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		installSidecarInjector(ctx, ns.Name)
		samples := smokeSamples()[:1]
		dep := framework.NewDeploymentFixture(ns.Name, "injected", 1, smokeWebhookContainers(samples)...)
		dep.Spec.Template.Labels["optipod-e2e/inject"] = "true"
		Expect(env.Clients.Ctrl.Create(ctx, dep)).To(Succeed())
		pods, err := framework.WaitForPodCountReady(ctx, env.Clients, ns.Name, "app=injected", 1, framework.WaitOptions{Timeout: time.Minute})
		Expect(err).NotTo(HaveOccurred())
		Expect(pods[0].Spec.Containers).To(HaveLen(2))
		series := registerContainerSeries(ctx, ns.Name, dep, samples)
		series = append(series, framework.FixtureSeries{Namespace: ns.Name, Pod: pods[0].Name, Container: "injected-proxy", CPUCores: []float64{0.125, 0.125}, MemoryBytes: []float64{50 * 1024 * 1024, 50 * 1024 * 1024}})
		Expect(env.Fixture.SetSeries(ctx, series)).To(Succeed())
		p := framework.NewPolicyFixture(ns.Name, "injected", api.ModeAuto, framework.WithStrategy("webhook", "onNextRestart"), framework.WithReconciliationInterval(time.Second))
		p.Spec.ResourcePolicy = &api.ResourcePolicy{IncludeInjectedSidecars: true}
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		Eventually(func() string {
			Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(dep), dep)).To(Succeed())
			return dep.Annotations[api.AnnotationCPURequestPrefix+".injected-proxy"]
		}, time.Minute, time.Second).Should(Equal("150m"))
		Expect(dep.Spec.Template.Spec.Containers).To(HaveLen(1))
		probe := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "admission-injected", Namespace: ns.Name, Labels: dep.Spec.Template.Labels, OwnerReferences: pods[0].OwnerReferences}, Spec: *dep.Spec.Template.Spec.DeepCopy()}
		created, err := env.Clients.Kube.CoreV1().Pods(ns.Name).Create(ctx, probe, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
		Expect(err).NotTo(HaveOccurred())
		Expect(created.Spec.Containers).To(HaveLen(2))
		Expect(created.Spec.Containers[1].Name).To(Equal("injected-proxy"))
		Expect(created.Spec.Containers[1].Resources.Requests.Cpu().MilliValue()).To(Equal(int64(150)))
		updatePolicy(ctx, ns.Name, p.Name, func(p *api.OptimizationPolicy) { p.Spec.ResourcePolicy.ExcludedContainers = []string{"injected-proxy"} })
		Eventually(func() string {
			Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(dep), dep)).To(Succeed())
			return dep.Annotations[api.AnnotationCPURequestPrefix+".injected-proxy"]
		}, time.Minute, time.Second).Should(BeEmpty())
		created, err = env.Clients.Kube.CoreV1().Pods(ns.Name).Create(ctx, probe, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
		Expect(err).NotTo(HaveOccurred())
		Expect(created.Spec.Containers[1].Resources.Requests.Cpu().MilliValue()).To(Equal(int64(50)))
	})
})

func installSidecarInjector(ctx context.Context, ns string) {
	cert, err := framework.GenerateWebhookServingCert("injector", ns)
	Expect(err).NotTo(HaveOccurred())
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "injector-tls", Namespace: ns}, Type: corev1.SecretTypeTLS, Data: map[string][]byte{"tls.crt": cert.CertPEM, "tls.key": cert.KeyPEM}}
	Expect(env.Clients.Ctrl.Create(ctx, secret)).To(Succeed())
	fixture, err := env.Clients.Kube.AppsV1().Deployments(framework.FixtureNamespace).Get(ctx, framework.FixtureServiceName, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())
	dep := framework.NewDeploymentFixture(ns, "injector", 1, framework.ContainerFixture{Name: "injector", CPURequest: "20m", MemoryRequest: "32Mi"})
	dep.Spec.Template.Spec.Containers[0].Image = fixture.Spec.Template.Spec.Containers[0].Image
	dep.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{{Name: "INJECTOR_TLS_CERT", Value: "/certs/tls.crt"}, {Name: "INJECTOR_TLS_KEY", Value: "/certs/tls.key"}, {Name: "INJECTED_IMAGE", Value: framework.WorkloadImage()}}
	dep.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "certs", MountPath: "/certs", ReadOnly: true}}
	dep.Spec.Template.Spec.Containers[0].ReadinessProbe = &corev1.Probe{ProbeHandler: corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(9443)}}, PeriodSeconds: 1}
	dep.Spec.Template.Spec.Volumes = []corev1.Volume{{Name: "certs", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: secret.Name}}}}
	Expect(env.Clients.Ctrl.Create(ctx, dep)).To(Succeed())
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "injector", Namespace: ns}, Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "injector"}, Ports: []corev1.ServicePort{{Port: 443, TargetPort: intstr.FromInt32(9443)}}}}
	Expect(env.Clients.Ctrl.Create(ctx, svc)).To(Succeed())
	_, err = framework.WaitForPodCountReady(ctx, env.Clients, ns, "app=injector", 1, framework.WaitOptions{Timeout: time.Minute})
	Expect(err).NotTo(HaveOccurred())
	path := "/inject"
	none := admissionv1.SideEffectClassNone
	fail := admissionv1.Fail
	reinvoke := admissionv1.IfNeededReinvocationPolicy
	wh := &admissionv1.MutatingWebhookConfiguration{ObjectMeta: metav1.ObjectMeta{Name: "zzz-optipod-e2e-injector-" + ns}, Webhooks: []admissionv1.MutatingWebhook{{Name: "injector.optipod-e2e.local", ClientConfig: admissionv1.WebhookClientConfig{CABundle: cert.CAPEM, Service: &admissionv1.ServiceReference{Name: "injector", Namespace: ns, Path: &path}}, Rules: []admissionv1.RuleWithOperations{{Operations: []admissionv1.OperationType{admissionv1.Create}, Rule: admissionv1.Rule{APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"pods"}}}}, NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": ns}}, ObjectSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"optipod-e2e/inject": "true"}}, SideEffects: &none, FailurePolicy: &fail, ReinvocationPolicy: &reinvoke, AdmissionReviewVersions: []string{"v1"}}}}
	Expect(env.Clients.Ctrl.Create(ctx, wh)).To(Succeed())
	DeferCleanup(func(ctx context.Context) { Expect(env.Clients.Ctrl.Delete(ctx, wh)).To(Succeed()) })
}
