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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Parity: completed execution history", Label("parity", "batch"), func() {
	It("uses completed Job observations without mutating its immutable template or restarting it", func(ctx context.Context) {
		ns := specNamespace(ctx, "job-history")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		suspended := true
		job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "standalone", Namespace: ns.Name}, Spec: batchv1.JobSpec{Suspend: &suspended, Template: batchTemplate(ns.Name)}}
		Expect(env.Clients.Ctrl.Create(ctx, job)).To(Succeed())
		p := batchPolicy(ns.Name, "jobs", api.WorkloadTypeJob, 1)
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		// Keep the execution suspended until the controller has observed the
		// no-history state; an eight-second Job may otherwise finish while a
		// replacement controller is still acquiring its leadership lease.
		waitForWorkloadReason(ctx, p, "no owned pods observed")
		Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(job), job)).To(Succeed())
		Expect(job.Annotations[api.AnnotationCPURequestPrefix+".app"]).To(BeEmpty())
		Expect(env.Clients.Ctrl.Patch(ctx, job, client.RawPatch(types.MergePatchType, []byte(`{"spec":{"suspend":false}}`)))).To(Succeed())
		pod := waitCompletedJob(ctx, job)
		Expect(env.Fixture.SetSeries(ctx, []framework.FixtureSeries{completedSeries(pod, 0.25)})).To(Succeed())
		Eventually(func() string {
			Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(job), job)).To(Succeed())
			return job.Annotations[api.AnnotationCPURequestPrefix+".app"]
		}, time.Minute, time.Second).Should(Equal("300m"))
		Expect(job.Spec.Template.Spec.Containers[0].Resources.Requests.Cpu().MilliValue()).To(Equal(int64(100)))
		Expect(job.Status.Succeeded).To(Equal(int32(1)))
		probe := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "next-pod", Namespace: ns.Name, Labels: job.Spec.Template.Labels, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(job, batchv1.SchemeGroupVersion.WithKind("Job"))}}, Spec: *job.Spec.Template.Spec.DeepCopy()}
		created, err := env.Clients.Kube.CoreV1().Pods(ns.Name).Create(ctx, probe, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
		Expect(err).NotTo(HaveOccurred())
		Expect(created.Spec.Containers[0].Resources.Requests.Cpu().MilliValue()).To(Equal(int64(300)))
	})
	It("warms up across distinct CronJob executions and retains their peak across controller restart", func(ctx context.Context) {
		ns := specNamespace(ctx, "cron-history")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		suspend := true
		cron := &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "scheduled", Namespace: ns.Name}, Spec: batchv1.CronJobSpec{Schedule: "* * * * *", Suspend: &suspend, JobTemplate: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Template: batchTemplate(ns.Name)}}}}
		Expect(env.Clients.Ctrl.Create(ctx, cron)).To(Succeed())
		p := batchPolicy(ns.Name, "cron", api.WorkloadTypeCronJob, 2)
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		first := cronExecution(cron, "first")
		Expect(env.Clients.Ctrl.Create(ctx, first)).To(Succeed())
		pod := waitCompletedJob(ctx, first)
		series := []framework.FixtureSeries{completedSeries(pod, 0.5)}
		Expect(env.Fixture.SetSeries(ctx, series)).To(Succeed())
		waitForWorkloadReason(ctx, p, "have 1 need 2")
		second := cronExecution(cron, "second")
		Expect(env.Clients.Ctrl.Create(ctx, second)).To(Succeed())
		secondPod := waitCompletedJob(ctx, second)
		series = append(series, completedSeries(secondPod, 0.1))
		Expect(env.Fixture.SetSeries(ctx, series)).To(Succeed())
		Eventually(func() string {
			Expect(env.Clients.Ctrl.Get(ctx, client.ObjectKeyFromObject(cron), cron)).To(Succeed())
			return cron.Annotations[api.AnnotationCPURequestPrefix+".app"]
		}, time.Minute, time.Second).Should(Equal("600m"))
		Expect(env.Clients.Ctrl.Delete(ctx, pod)).To(Succeed())
		restartController(ctx)
		// failurePolicy=Ignore intentionally permits unmodified pods during a
		// webhook outage. Prove the API server can reach the replacement and
		// read the retained plan before launching the next real execution.
		probe := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "restart-readiness", Namespace: ns.Name,
			Labels: second.Spec.Template.Labels, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(second, batchv1.SchemeGroupVersion.WithKind("Job"))}},
			Spec: *second.Spec.Template.Spec.DeepCopy()}
		Eventually(func() int64 {
			admitted, err := env.Clients.Kube.CoreV1().Pods(ns.Name).Create(ctx, probe, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
			Expect(err).NotTo(HaveOccurred())
			return admitted.Spec.Containers[0].Resources.Requests.Cpu().MilliValue()
		}, time.Minute, time.Second).Should(Equal(int64(600)), "retained plan must be applied through the replacement webhook")
		third := cronExecution(cron, "third")
		Expect(env.Clients.Ctrl.Create(ctx, third)).To(Succeed())
		Eventually(func() int64 {
			pods, err := env.Clients.Kube.CoreV1().Pods(ns.Name).List(ctx, metav1.ListOptions{LabelSelector: "batch.kubernetes.io/job-name=third"})
			if err != nil || len(pods.Items) == 0 {
				return 0
			}
			return pods.Items[0].Spec.Containers[0].Resources.Requests.Cpu().MilliValue()
		}, time.Minute, time.Second).Should(Equal(int64(600)))
		Expect(cron.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Resources.Requests.Cpu().MilliValue()).To(Equal(int64(100)))
	})
})

func batchTemplate(ns string) corev1.PodTemplateSpec {
	t := framework.NewDeploymentFixture(ns, "batch", 1, smokeWebhookContainers(smokeSamples()[:1])...).Spec.Template
	t.Spec.RestartPolicy = corev1.RestartPolicyNever
	t.Spec.Containers[0].Image = "docker.io/optipod-e2e/load:dev"
	t.Spec.Containers[0].ImagePullPolicy = corev1.PullNever
	t.Spec.Containers[0].Env = []corev1.EnvVar{{Name: "EXIT_AFTER_SECONDS", Value: "8"}, {Name: "CPU_MILLICORES", Value: "100"}, {Name: "MEMORY_MIB", Value: "20"}}
	return t
}
func batchPolicy(ns, name string, kind api.WorkloadType, min int32) *api.OptimizationPolicy {
	p := framework.NewPolicyFixture(ns, name, api.ModeAuto, framework.WithStrategy("webhook", "immediate"), framework.WithReconciliationInterval(time.Second))
	p.Spec.Selector.WorkloadTypes = &api.WorkloadTypeFilter{Include: []api.WorkloadType{kind}}
	p.Spec.Batch = &api.BatchConfig{MinCompletedExecutions: &min, MaxExecutionAge: metav1.Duration{Duration: time.Hour}}
	p.Spec.MetricsConfig.Percentile = "P100"
	p.Spec.MetricsConfig.MaxSampleAge = metav1.Duration{Duration: time.Second}
	return p
}
func cronExecution(c *batchv1.CronJob, name string) *batchv1.Job {
	return &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: c.Namespace, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(c, batchv1.SchemeGroupVersion.WithKind("CronJob"))}}, Spec: *c.Spec.JobTemplate.Spec.DeepCopy()}
}
func waitCompletedJob(ctx context.Context, j *batchv1.Job) *corev1.Pod {
	var result *corev1.Pod
	Eventually(func() bool {
		pods, err := env.Clients.Kube.CoreV1().Pods(j.Namespace).List(ctx, metav1.ListOptions{LabelSelector: "batch.kubernetes.io/job-name=" + j.Name})
		if err != nil || len(pods.Items) == 0 {
			return false
		}
		result = pods.Items[0].DeepCopy()
		return result.Status.Phase == corev1.PodSucceeded
	}, time.Minute, time.Second).Should(BeTrue())
	return result
}
func completedSeries(p *corev1.Pod, cpu float64) framework.FixtureSeries {
	end := p.Status.ContainerStatuses[0].State.Terminated.FinishedAt.Add(-time.Second)
	return framework.FixtureSeries{Namespace: p.Namespace, Pod: p.Name, Container: "app", FixedEnd: float64(end.UnixNano()) / 1e9, SpacingSeconds: 1, CPUCores: []float64{cpu, cpu, cpu}, MemoryBytes: []float64{100 * 1024 * 1024, 100 * 1024 * 1024, 100 * 1024 * 1024}}
}
