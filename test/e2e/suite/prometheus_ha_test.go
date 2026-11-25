package suite

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Prometheus HA", Label("prometheus-ha"), Serial, func() {
	It("preserves a completed execution peak observed by just one scraper and never joins counters from different scrapers", func(ctx context.Context) {
		ns := specNamespace(ctx, "ha-usage")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		second := env.Fixture.InNamespace(ns.Name)
		fn := framework.FixtureServiceName
		meta := metav1.ObjectMeta{Name: fn, Namespace: ns.Name}
		original, err := env.Clients.Kube.AppsV1().Deployments(framework.FixtureNamespace).Get(ctx, fn, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(env.Clients.Ctrl.Create(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: fn, Namespace: ns.Name, Labels: map[string]string{"fixture": "replica"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "fixture", Image: original.Spec.Template.Spec.Containers[0].Image, ImagePullPolicy: corev1.PullIfNotPresent}}}})).To(Succeed())
		svc := &corev1.Service{ObjectMeta: meta, Spec: corev1.ServiceSpec{Selector: map[string]string{"fixture": "replica"}, Ports: []corev1.ServicePort{{Name: "http", Port: framework.FixturePort}}}}
		Expect(env.Clients.Ctrl.Create(ctx, svc)).To(Succeed())
		Eventually(func() error { return second.SetSeries(ctx, nil) }, time.Minute, time.Second).Should(Succeed())
		args := setControllerMetricArgs(ctx, []string{"--prometheus-replica-urls=" + env.Fixture.URL() + "," + second.URL()})
		DeferCleanup(restoreControllerArgs, args)
		job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "short", Namespace: ns.Name}, Spec: batchv1.JobSpec{Template: batchTemplate(ns.Name)}}
		Expect(env.Clients.Ctrl.Create(ctx, job)).To(Succeed())
		pod := waitCompletedJob(ctx, job)
		low := completedSeries(pod, .1)
		high := completedSeries(pod, .25)
		low.CPUCores = low.CPUCores[:1]
		high.CPUCores = high.CPUCores[:1]
		low.MemoryBytes = []float64{16 << 20, 16 << 20, 16 << 20}
		high.MemoryBytes = []float64{16 << 20, 160 << 20, 16 << 20}
		Expect(env.Fixture.SetSeries(ctx, []framework.FixtureSeries{low})).To(Succeed())
		Expect(second.SetSeries(ctx, []framework.FixtureSeries{high})).To(Succeed())
		policy := batchPolicy(ns.Name, "ha", api.WorkloadTypeJob, 1)
		policy.Spec.Mode = api.ModeRecommend
		Expect(env.Clients.Ctrl.Create(ctx, policy)).To(Succeed())
		waitForBatchDecision(ctx, policy, job, "insufficient samples for CPU")
		high.CPUCores = []float64{.25, .25, .25}
		Expect(second.SetSeries(ctx, []framework.FixtureSeries{high})).To(Succeed())
		Eventually(func() []string {
			Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(job), job)).To(Succeed())
			return []string{job.Annotations["optipod.io/recommendation.app.cpu-request"], job.Annotations["optipod.io/recommendation.app.memory-request"]}
		}, time.Minute, time.Second).Should(Equal([]string{"300m", "192Mi"}))
		low.Mode = framework.FixtureModeUnavailable
		Expect(env.Fixture.SetSeries(ctx, []framework.FixtureSeries{low})).To(Succeed())
		waitForBatchDecision(ctx, policy, job, "prometheus replica 1")
	})
})
