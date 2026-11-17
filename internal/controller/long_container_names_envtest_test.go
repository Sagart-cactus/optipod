package controller

import (
	"context"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/report"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Recommend with long container names", func() {
	It("persists and reports distinct recommendations without changing the workload spec", func() {
		ctx := context.Background()
		const namespace = "recommend-long-containers"
		const name = "long-container-workload"
		const policyName = "recommend-long-container-policy"
		cleanup := createModesWorkloadFixture(ctx, namespace, name)
		defer cleanup()
		deployment := fetchDeployment(ctx, namespace, name)
		first := "application-background-processing-worker"
		second := strings.Repeat("b", 63)
		container := deployment.Spec.Template.Spec.Containers[0]
		container.Name = first
		other := *container.DeepCopy()
		other.Name = second
		deployment.Spec.Template.Spec.Containers = []corev1.Container{container, other}
		Expect(k8sClient.Update(ctx, deployment)).To(Succeed())
		before := deployment.Spec.DeepCopy()
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "long-container-metrics", Namespace: namespace, Labels: map[string]string{"app": name}}, Spec: *deployment.Spec.Template.Spec.DeepCopy()}
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, pod) }()
		provider := modesProvider()
		provider.metricsByContainer[first] = provider.metricsByContainer["app"]
		metrics := *provider.metricsByContainer["app"]
		metrics.CPU.P90 = resource.MustParse("600m")
		metrics.Memory.P90 = resource.MustParse("1Gi")
		provider.metricsByContainer[second] = &metrics
		createModesPolicy(ctx, policyName, namespace, func(p *api.OptimizationPolicy) { p.Spec.Mode = api.ModeRecommend })
		defer func() { _ = k8sClient.Delete(ctx, fetchModesPolicy(ctx, policyName)) }()
		reconcilePolicy(ctx, newModesReconciler(provider), policyName)
		policy := fetchModesPolicy(ctx, policyName)
		Expect(policy.Status.WorkloadsProcessed).To(Equal(1))
		deployment = fetchDeployment(ctx, namespace, name)
		Expect(deployment.Spec).To(Equal(*before))
		Expect(deployment.Annotations[api.RecommendationAnnotationKey(first, "cpu-request")]).To(Equal("300m"))
		Expect(deployment.Annotations[api.RecommendationAnnotationKey(second, "memory-request")]).To(Equal("1Gi"))
		fleet, err := (&report.Collector{Client: k8sClient, Namespace: namespace}).Fleet(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(fleet.Workloads).To(HaveLen(1))
		Expect(fleet.Workloads[0].Containers).To(HaveLen(2))
		Expect(fleet.Workloads[0].Containers[0].Recommended).To(HaveKeyWithValue("cpu-request", "300m"))
		Expect(fleet.Workloads[0].Containers[1].Recommended).To(HaveKeyWithValue("memory-request", "1Gi"))
	})
})
