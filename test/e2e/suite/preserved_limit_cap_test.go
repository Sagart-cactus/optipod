package suite

import (
	"context"
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/capturedlimits"
	"github.com/optipod/optipod/internal/report"
	"github.com/optipod/optipod/test/e2e/framework"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Caps requests at preserved container limits", Label("parity", "preserved-limit-cap"), func() {
	It("records two capped cycles and uses the current plan on a later admission", func(ctx context.Context) {
		ns := specNamespace(ctx, "preserved-cap")
		DeferCleanup(func(ctx context.Context) {
			Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed())
		})
		defaults := &corev1.LimitRange{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns.Name, Name: "initial-defaults"},
			Spec: corev1.LimitRangeSpec{Limits: []corev1.LimitRangeItem{{
				Type:    corev1.LimitTypeContainer,
				Default: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("256Mi")},
			}}},
		}
		Expect(env.Clients.Ctrl.Create(ctx, defaults)).To(Succeed())
		const appName = "application-container-with-a-long-name"
		samples := smokeSamples()
		samples[0].name = appName
		samples[0].memoryBytes = 500 << 20
		dep := framework.NewDeploymentFixture(ns.Name, "bounded", 1, smokeWebhookContainers(samples)...)
		Expect(env.Clients.Ctrl.Create(ctx, dep)).To(Succeed())
		pods, err := framework.WaitForPodCountReady(ctx, env.Clients, ns.Name, "app=bounded", 1, framework.WaitOptions{Timeout: time.Minute})
		Expect(err).NotTo(HaveOccurred())
		original := pods[0].DeepCopy()
		Expect(original.Spec.Containers[0].Resources.Limits.Memory().String()).To(Equal("256Mi"))
		Expect(dep.Spec.Template.Spec.Containers[0].Resources.Limits).To(BeEmpty())

		By("capturing the admitted limits before removing the source of the defaults")
		snapshot, err := capturedlimits.Read(ctx, env.Clients.Ctrl, "Deployment", client.ObjectKeyFromObject(dep), dep.UID, false, 0)
		Expect(err).NotTo(HaveOccurred())
		data, err := json.Marshal(snapshot)
		Expect(err).NotTo(HaveOccurred())
		Expect(env.Clients.Ctrl.Delete(ctx, defaults)).To(Succeed())
		policy := framework.NewPolicyFixture(ns.Name, "bounded", api.ModeAuto, framework.WithStrategy("webhook", "onNextRestart"), framework.WithReconciliationInterval(31*time.Second))
		policy.Annotations = map[string]string{capturedlimits.Annotation: string(data)}
		series := registerContainerSeries(ctx, ns.Name, dep, samples)
		Expect(env.Clients.Ctrl.Create(ctx, policy)).To(Succeed())

		appMemory := api.RecommendationAnnotationKey(appName, "memory-request")
		appRawMemory := api.RecommendationAnnotationKey(appName, "raw-memory-request")
		appExplanation := api.RecommendationAnnotationKey(appName, "explanation")
		Expect(appRawMemory).To(HavePrefix(api.AnnotationRecommendationPrefix + "._"))
		cycle := func(raw string) *appsv1.Deployment {
			result := waitForDeploymentAnnotation(ctx, ns.Name, dep.Name, map[string]string{
				appMemory: "256Mi", appRawMemory: raw,
				api.RecommendationAnnotationKey(appName, "cpu-request"):      "300m",
				api.RecommendationAnnotationKey("sidecar", "memory-request"): "72Mi",
				api.RecommendationAnnotationKey("sidecar", "cpu-request"):    "150m",
				api.AnnotationMemoryRequestPrefix + "." + appName:            "256Mi",
			})
			Expect(result.Annotations[appExplanation]).To(ContainSubstring("memory request capped from " + raw + " to 256Mi by preserved limit 256Mi"))
			Expect(result.Annotations).NotTo(HaveKey(api.RecommendationAnnotationKey(appName, "raw-cpu-request")))
			Expect(result.Annotations).NotTo(HaveKey(api.RecommendationAnnotationKey("sidecar", "raw-memory-request")))
			return result
		}
		By("recording the first capped decision from measured data")
		first := cycle("600Mi")
		firstTime := first.Annotations[api.AnnotationLastRecommendation]
		expectFixtureQueried(ctx, series)

		By("recording a second reconciled raw value against the same effective ceiling")
		samples[0].memoryBytes = 600 << 20
		series = registerContainerSeries(ctx, ns.Name, dep, samples)
		second := cycle("720Mi")
		Expect(second.Annotations[api.AnnotationLastRecommendation]).NotTo(Equal(firstTime))
		expectFixtureQueried(ctx, series)

		By("reading both exact raw and effective recommendations from decision history")
		collector := &report.Collector{Client: env.Clients.Ctrl, Namespace: ns.Name}
		Eventually(func(g Gomega) []string {
			journal, err := collector.History(ctx, ns.Name, string(dep.UID))
			g.Expect(err).NotTo(HaveOccurred())
			var values []string
			for _, record := range journal.Records {
				if record.Decision == nil {
					continue
				}
				for _, rec := range record.Decision.Recommendations {
					if rec.Container == appName && rec.RawMemory != nil && rec.Memory != nil {
						g.Expect(rec.Explanation).To(ContainSubstring("by preserved limit 256Mi"))
						values = append(values, rec.RawMemory.String()+"="+rec.Memory.String())
					}
				}
			}
			return values
		}, time.Minute, time.Second).Should(ContainElements("600Mi=256Mi", "720Mi=256Mi"))

		By("checking existing Pod resources and the Deployment template before preview")
		Expect(second.Spec.Template).To(Equal(dep.Spec.Template))
		current, err := env.Clients.Kube.CoreV1().Pods(ns.Name).Get(ctx, original.Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(current.UID).To(Equal(original.UID))
		for i, container := range current.Spec.Containers {
			Expect(container.Resources).To(Equal(original.Spec.Containers[i].Resources))
		}
		Expect(podUIDs(ctx, ns.Name, "app=bounded")).To(HaveLen(1))

		By("previewing a later Pod admission without persisting or replacing any Pod")
		probe := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "bounded-preview", Namespace: ns.Name, Labels: second.Spec.Template.Labels, OwnerReferences: original.OwnerReferences},
			Spec:       *second.Spec.Template.Spec.DeepCopy(),
		}
		admitted, err := env.Clients.Kube.CoreV1().Pods(ns.Name).Create(ctx, probe, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
		Expect(err).NotTo(HaveOccurred())
		app, sidecar := admitted.Spec.Containers[0], admitted.Spec.Containers[1]
		Expect(app.Resources.Requests.Memory().String()).To(Equal("256Mi"))
		Expect(app.Resources.Requests.Cpu().String()).To(Equal("300m"))
		Expect(sidecar.Resources.Requests.Memory().String()).To(Equal("72Mi"))
		Expect(sidecar.Resources.Requests.Cpu().String()).To(Equal("150m"))
		Expect(app.Resources.Limits).To(Equal(original.Spec.Containers[0].Resources.Limits))
		Expect(sidecar.Resources.Limits).To(Equal(original.Spec.Containers[1].Resources.Limits))
		Expect(podUIDs(ctx, ns.Name, "app=bounded")).To(HaveKey(original.UID))
		Expect(podUIDs(ctx, ns.Name, "app=bounded")).To(HaveLen(1))
	})
})
