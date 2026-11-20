package suite

import (
	"context"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Parity: replica metrics and confidence", Label("parity", "metrics-history"), func() {
	It("queries every owned replica and selects the combined P95", func(ctx context.Context) {
		ns := specNamespace(ctx, "parity-replicas")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		dep := framework.NewDeploymentFixture(ns.Name, "replicas", 2, smokeContainers(smokeSamples()[:1])...)
		parent := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "fixture-operator", Namespace: ns.Name}}
		Expect(env.Clients.Ctrl.Create(ctx, parent)).To(Succeed())
		dep.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(parent, corev1.SchemeGroupVersion.WithKind("ConfigMap"))}
		Expect(env.Clients.Ctrl.Create(ctx, dep)).To(Succeed())
		pods, err := framework.WaitForPodCountReady(ctx, env.Clients, ns.Name, "app="+dep.Name, 2, framework.WaitOptions{Timeout: rolloutWaitTimeout})
		Expect(err).NotTo(HaveOccurred())
		series := []framework.FixtureSeries{{Namespace: ns.Name, Pod: pods[0].Name, Container: "app", CPUCores: []float64{0.1, 0.1, 0.1}, MemoryBytes: []float64{64 * 1024 * 1024, 64 * 1024 * 1024, 64 * 1024 * 1024}}, {Namespace: ns.Name, Pod: pods[1].Name, Container: "app", CPUCores: []float64{0.5, 0.5, 0.5}, MemoryBytes: []float64{256 * 1024 * 1024, 256 * 1024 * 1024, 256 * 1024 * 1024}}}
		Expect(env.Fixture.SetSeries(ctx, series)).To(Succeed())
		Expect(env.Fixture.ClearTrace(ctx)).To(Succeed())
		p := framework.NewPolicyFixture(ns.Name, "replicas", api.ModeRecommend, framework.WithReconciliationInterval(time.Second))
		one := 1.0
		p.Spec.MetricsConfig.SafetyFactor = &one
		p.Spec.MetricsConfig.Percentile = "P95"
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		annotated := waitForDeploymentAnnotation(ctx, ns.Name, dep.Name, map[string]string{"optipod.io/recommendation.app.cpu-request": "500m", "optipod.io/recommendation.app.memory-request": "256Mi"})
		Expect(annotated.Annotations["optipod.io/metrics-pods"]).To(ContainSubstring(pods[0].Name))
		Expect(annotated.Annotations["optipod.io/metrics-pods"]).To(ContainSubstring(pods[1].Name))
		trace, err := env.Fixture.Trace(ctx)
		Expect(err).NotTo(HaveOccurred())
		for _, pod := range pods {
			found := false
			for _, q := range trace {
				if strings.Contains(q.Query, `pod="`+pod.Name+`"`) && strings.Contains(q.Query, "[1m]") {
					found = true
				}
			}
			Expect(found).To(BeTrue(), pod.Name)
		}
		Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(p), p)).To(Succeed())
		p.Spec.MetricsConfig.CPU = &api.ResourceMetricsConfig{ReplicaAggregation: api.ReplicaAggregationMeanPercentile}
		Expect(env.Clients.Ctrl.Update(ctx, p)).To(Succeed())
		waitForDeploymentAnnotation(ctx, ns.Name, dep.Name, map[string]string{"optipod.io/recommendation.app.cpu-request": "300m", "optipod.io/recommendation.app.memory-request": "256Mi"})
		Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(p), p)).To(Succeed())
		p.Spec.MetricsConfig.CPU.Percentile = "P100"
		Expect(env.Clients.Ctrl.Update(ctx, p)).To(Succeed())
		waitForDeploymentAnnotation(ctx, ns.Name, dep.Name, map[string]string{"optipod.io/recommendation.app.cpu-request": "500m"})
		Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(p), p)).To(Succeed())
		p.Spec.Mode = api.ModeAuto
		Expect(env.Clients.Ctrl.Update(ctx, p)).To(Succeed())
		waitForBatchDecision(ctx, p, dep, "automatic pod metrics require a top-level workload")

	})
	It("distinguishes zero replicas and zero eligible nodes from missing telemetry", Label("idle-workload"), func(ctx context.Context) {
		ns := specNamespace(ctx, "idle-workload")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		dep := framework.NewDeploymentFixture(ns.Name, "idle", 0, smokeContainers(smokeSamples()[:1])...)
		ds := framework.NewDaemonSetFixture(ns.Name, "unscheduled", smokeContainers(smokeSamples()[:1])...)
		ds.Spec.Template.Spec.NodeSelector = map[string]string{"fixture.optipod.io/eligible": "never"}
		Expect(env.Clients.Ctrl.Create(ctx, dep)).To(Succeed())
		Expect(env.Clients.Ctrl.Create(ctx, ds)).To(Succeed())
		p := framework.NewPolicyFixture(ns.Name, "idle-targets", api.ModeRecommend, framework.WithReconciliationInterval(time.Second))
		p.Spec.Selector.WorkloadTypes = &api.WorkloadTypeFilter{Include: []api.WorkloadType{api.WorkloadTypeDeployment, api.WorkloadTypeDaemonSet}}
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		waitForBatchDecision(ctx, p, dep, "Idle workload: scaled to zero replicas")
		waitForBatchDecision(ctx, p, ds, "Idle workload: no eligible nodes")
	})
	It("blocks stale startup and insufficient-history samples before recovering", func(ctx context.Context) {
		ns := specNamespace(ctx, "parity-quality")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		dep := createSmokeWorkload(ctx, ns.Name, "quality", smokeSamples()[:1])
		series := registerContainerSeries(ctx, ns.Name, dep, smokeSamples()[:1])
		p := framework.NewPolicyFixture(ns.Name, "quality", api.ModeAuto, framework.WithReconciliationInterval(time.Second))
		p.Spec.MetricsConfig.StartupIgnore = metav1.Duration{Duration: time.Hour}
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		waitForWorkloadReason(ctx, p, "warming up")
		Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(p), p)).To(Succeed())
		p.Spec.MetricsConfig.StartupIgnore.Duration = 0
		p.Spec.MetricsConfig.MinimumHistory.Duration = time.Hour
		Expect(env.Clients.Ctrl.Update(ctx, p)).To(Succeed())
		waitForWorkloadReason(ctx, p, "insufficient history")
		Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(p), p)).To(Succeed())
		p.Spec.MetricsConfig.MinimumHistory.Duration = 0
		p.Spec.MetricsConfig.MaxSampleAge.Duration = 100 * time.Millisecond
		for i := range series {
			series[i].AgeSeconds = 0.5
		}
		Expect(env.Fixture.SetSeries(ctx, series)).To(Succeed())
		Expect(env.Clients.Ctrl.Update(ctx, p)).To(Succeed())
		waitForWorkloadReason(ctx, p, "stale metrics")
		Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(p), p)).To(Succeed())
		p.Spec.MetricsConfig.MaxSampleAge.Duration = 5 * time.Minute
		Expect(env.Clients.Ctrl.Update(ctx, p)).To(Succeed())
		waitForDeploymentAnnotation(ctx, ns.Name, dep.Name, webhookApplyAnnotations(smokeSamples()[:1]))
	})
})

func waitForWorkloadReason(ctx context.Context, p *api.OptimizationPolicy, fragment string) {
	Expect(framework.WaitForCondition(ctx, framework.WaitOptions{Timeout: time.Minute, Description: fragment}, func(ctx context.Context) (bool, error) {
		current, err := env.Clients.Kube.CoreV1().Events(p.Namespace).List(ctx, metav1.ListOptions{FieldSelector: "involvedObject.uid=" + string(p.UID)})
		if err != nil {
			return false, err
		}
		for _, w := range current.Items {
			if strings.Contains(w.Message, fragment) {
				return true, nil
			}
		}
		return false, nil
	})).To(Succeed())
}
