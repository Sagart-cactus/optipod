/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8sdiscovery "k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/application"
	optipoddiscovery "github.com/optipod/optipod/internal/discovery"
	"github.com/optipod/optipod/internal/metrics"
	"github.com/optipod/optipod/internal/recommendation"
)

// These specs drive the production workload processor with deterministic
// per-container metrics to prove the multi-container and partial-data behavior
// of VAL-FAST-006: independently named per-container recommendations on the
// returned status, a missing container's metrics skipping the WHOLE workload
// (no partial Auto application, explicit signal, unchanged requests, no
// annotations), and recovery applying every container once the missing
// metrics return.

// scriptedMetricsProvider serves deterministic metrics per container with
// fault scripting (a container can be forced to fail with a specific error,
// or the whole provider can be marked unavailable) and records the windows
// the processor requests.
type scriptedMetricsProvider struct {
	metricsByContainer map[string]*metrics.ContainerMetrics
	missingContainers  map[string]error
	unavailable        bool
	windowCalls        []time.Duration
}

func (p *scriptedMetricsProvider) GetContainerMetrics(
	ctx context.Context,
	namespace, podName, containerName string,
	window time.Duration,
) (*metrics.ContainerMetrics, error) {
	p.windowCalls = append(p.windowCalls, window)
	if p.unavailable {
		return nil, &metrics.MetricsError{Message: "metrics backend unavailable: connection refused"}
	}
	if err, missing := p.missingContainers[containerName]; missing {
		return nil, err
	}
	if m, ok := p.metricsByContainer[containerName]; ok {
		return m, nil
	}
	return nil, &metrics.MetricsError{Message: "no metrics available for container " + containerName}
}

func (p *scriptedMetricsProvider) HealthCheck(ctx context.Context) error {
	return nil
}

// partialProvider returns the scripted provider with the complete two-container
// metrics of the convergence fixture (app -> 300m/512Mi, worker -> 150m/256Mi).
func partialProvider() *scriptedMetricsProvider {
	base := convergenceProvider()
	return &scriptedMetricsProvider{
		metricsByContainer: base.metricsByContainer,
		missingContainers:  map[string]error{},
	}
}

// buildScriptedProcessor wires the production engines against the suite's
// envtest API server with the scripted provider.
func buildScriptedProcessor(provider *scriptedMetricsProvider) *WorkloadProcessor {
	dynamicClient, err := dynamic.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())

	discoveryClient, err := k8sdiscovery.NewDiscoveryClientForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())

	return NewWorkloadProcessor(
		provider,
		recommendation.NewEngine(),
		application.NewEngine(k8sClient, dynamicClient, discoveryClient, false),
		k8sClient,
	)
}

// createPartialTwoContainerFixture creates a two-container Deployment, a
// matching pod and an explicit strategy: ssa Auto policy (so application is
// observable as template request changes). It returns the fetched policy and a
// cleanup function.
func createPartialTwoContainerFixture(ctx context.Context, namespace, deploymentName string) (*optipodv1alpha1.OptimizationPolicy, func()) {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
	Expect(k8sClient.Create(ctx, ns)).To(Succeed())

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": deploymentName}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": deploymentName}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "nginx:1.21",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
							},
						},
						{
							Name:  "worker",
							Image: "busybox:1.36",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("50m"),
									corev1.ResourceMemory: resource.MustParse("64Mi"),
								},
							},
						},
					},
				},
			},
		},
	}
	Expect(k8sClient.Create(ctx, deployment)).To(Succeed())

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName + "-pod",
			Namespace: namespace,
			Labels:    map[string]string{"app": deploymentName},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "app", Image: "nginx:1.21"},
				{Name: "worker", Image: "busybox:1.36"},
			},
		},
	}
	Expect(k8sClient.Create(ctx, pod)).To(Succeed())

	safetyFactor := 1.0
	ssaStrategy := string(optipodv1alpha1.StrategySSA)
	policy := &optipodv1alpha1.OptimizationPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName + "-policy",
			Namespace: "default",
		},
		Spec: optipodv1alpha1.OptimizationPolicySpec{
			Mode: optipodv1alpha1.ModeAuto,
			Selector: optipodv1alpha1.WorkloadSelector{
				Namespaces: &optipodv1alpha1.NamespaceFilter{Allow: []string{namespace}},
			},
			MetricsConfig: optipodv1alpha1.MetricsConfig{
				Provider:     "prometheus",
				Percentile:   "P90",
				SafetyFactor: &safetyFactor,
			},
			ResourceBounds: optipodv1alpha1.ResourceBounds{
				CPU: optipodv1alpha1.ResourceBound{
					Min: resource.MustParse("50m"),
					Max: resource.MustParse("2000m"),
				},
				Memory: optipodv1alpha1.ResourceBound{
					Min: resource.MustParse("64Mi"),
					Max: resource.MustParse("4Gi"),
				},
			},
			UpdateStrategy: optipodv1alpha1.UpdateStrategy{
				Strategy:           &ssaStrategy,
				UpdateRequestsOnly: true,
			},
		},
	}
	Expect(k8sClient.Create(ctx, policy)).To(Succeed())

	fetchedPolicy := &optipodv1alpha1.OptimizationPolicy{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: policy.Name, Namespace: policy.Namespace}, fetchedPolicy)).To(Succeed())

	return fetchedPolicy, func() {
		_ = k8sClient.Delete(ctx, policy)
		_ = k8sClient.Delete(ctx, deployment)
		_ = k8sClient.Delete(ctx, pod)
		_ = k8sClient.Delete(ctx, ns)
	}
}

// partialWorkload builds the discovery.Workload input for the processor from
// the live API object.
func partialWorkload(ctx context.Context, namespace, deploymentName string) *optipoddiscovery.Workload {
	return &optipoddiscovery.Workload{
		Kind:      "Deployment",
		Namespace: namespace,
		Name:      deploymentName,
		Object:    fetchDeployment(ctx, namespace, deploymentName),
	}
}

// assertNoRecommendationAnnotations verifies no recommendation metadata of
// either family (processor display annotations, webhook store keys) exists.
func assertNoRecommendationAnnotations(annotations map[string]string) {
	for key := range annotations {
		Expect(key).NotTo(HavePrefix(optipodv1alpha1.AnnotationRecommendationPrefix+"."),
			"no processor recommendation annotation may exist")
		Expect(key).NotTo(HavePrefix(optipodv1alpha1.AnnotationCPURequestPrefix+"."),
			"no webhook recommendation annotation may exist")
		Expect(key).NotTo(HavePrefix(optipodv1alpha1.AnnotationMemoryRequestPrefix+"."),
			"no webhook recommendation annotation may exist")
		Expect(key).NotTo(Equal(optipodv1alpha1.AnnotationManaged),
			"the managed marker annotation may not exist without a recommendation")
	}
}

var _ = Describe("Multi-container and partial-metrics processing through the production processor", func() {
	Context("Complete metrics for a two-container workload", func() {
		const (
			namespace      = "partial-complete-ns"
			deploymentName = "partial-complete-deploy"
		)

		It("computes independently named per-container recommendations and applies both", func() {
			ctx := context.Background()
			policy, cleanup := createPartialTwoContainerFixture(ctx, namespace, deploymentName)
			defer cleanup()

			provider := partialProvider()
			processor := buildScriptedProcessor(provider)

			status, err := processor.ProcessWorkload(ctx, partialWorkload(ctx, namespace, deploymentName), policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Status).To(Equal(StatusApplied), "reason: %s", status.Reason)

			By("returning exactly one independently named recommendation per container with exact values")
			Expect(status.Recommendations).To(HaveLen(2))
			byName := map[string]optipodv1alpha1.ContainerRecommendation{}
			for _, rec := range status.Recommendations {
				byName[rec.Container] = rec
			}
			Expect(byName).To(HaveKey("app"))
			Expect(byName).To(HaveKey("worker"))
			Expect(byName["app"].CPU.MilliValue()).To(Equal(int64(300)), "container app CPU recommendation")
			Expect(byName["app"].Memory.Value()).To(Equal(int64(512*1024*1024)), "container app memory recommendation")
			Expect(byName["worker"].CPU.MilliValue()).To(Equal(int64(150)), "container worker CPU recommendation")
			Expect(byName["worker"].Memory.Value()).To(Equal(int64(256*1024*1024)), "container worker memory recommendation")

			By("applying every container's distinct values")
			after := fetchDeployment(ctx, namespace, deploymentName)
			appCPU, appMemory := containerRequest(after, "app")
			Expect(appCPU).To(Equal("300m"))
			Expect(appMemory).To(Equal("512Mi"))
			workerCPU, workerMemory := containerRequest(after, "worker")
			Expect(workerCPU).To(Equal("150m"))
			Expect(workerMemory).To(Equal("256Mi"))
		})
	})

	Context("One container's metrics are missing", func() {
		const (
			namespace      = "partial-missing-ns"
			deploymentName = "partial-missing-deploy"
		)

		It("skips the whole workload with an explicit signal and applies nothing", func() {
			ctx := context.Background()
			policy, cleanup := createPartialTwoContainerFixture(ctx, namespace, deploymentName)
			defer cleanup()

			provider := partialProvider()
			// The worker container reports insufficient samples; app's
			// metrics remain complete.
			provider.missingContainers["worker"] = &metrics.ErrInsufficientSamples{Have: 2, Need: 10}
			processor := buildScriptedProcessor(provider)

			status, err := processor.ProcessWorkload(ctx, partialWorkload(ctx, namespace, deploymentName), policy)
			Expect(err).NotTo(HaveOccurred())

			By("skipping the whole workload with an explicit missing-metrics signal")
			Expect(status.Status).To(Equal(StatusSkipped))
			Expect(status.Reason).To(ContainSubstring("Missing metrics:"))
			Expect(status.Reason).To(ContainSubstring("Insufficient metrics samples for container worker"))
			Expect(status.LastApplied).To(BeNil(), "nothing may be recorded as applied")

			By("recording only the measurable container's recommendation without applying anything")
			Expect(status.Recommendations).To(HaveLen(1))
			Expect(status.Recommendations[0].Container).To(Equal("app"))
			Expect(status.Recommendations[0].CPU.MilliValue()).To(Equal(int64(300)))

			By("leaving both containers' requests unchanged")
			after := fetchDeployment(ctx, namespace, deploymentName)
			appCPU, appMemory := containerRequest(after, "app")
			Expect(appCPU).To(Equal("100m"), "container app request must not change on a partial-metrics skip")
			Expect(appMemory).To(Equal("128Mi"), "container app request must not change on a partial-metrics skip")
			workerCPU, workerMemory := containerRequest(after, "worker")
			Expect(workerCPU).To(Equal("50m"), "container worker request must not change on a partial-metrics skip")
			Expect(workerMemory).To(Equal("64Mi"), "container worker request must not change on a partial-metrics skip")

			By("writing no recommendation annotations at all")
			assertNoRecommendationAnnotations(after.GetAnnotations())
		})
	})

	Context("Recovery after missing metrics", func() {
		const (
			namespace      = "partial-recovery-ns"
			deploymentName = "partial-recovery-deploy"
		)

		It("applies every container once the missing metrics return", func() {
			ctx := context.Background()
			policy, cleanup := createPartialTwoContainerFixture(ctx, namespace, deploymentName)
			defer cleanup()

			provider := partialProvider()
			provider.missingContainers["worker"] = &metrics.ErrInsufficientSamples{Have: 2, Need: 10}
			processor := buildScriptedProcessor(provider)

			// First pass: the partial data must be skipped without any
			// application.
			status, err := processor.ProcessWorkload(ctx, partialWorkload(ctx, namespace, deploymentName), policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Status).To(Equal(StatusSkipped))

			// Recovery: the missing container's metrics return.
			delete(provider.missingContainers, "worker")

			status, err = processor.ProcessWorkload(ctx, partialWorkload(ctx, namespace, deploymentName), policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Status).To(Equal(StatusApplied), "reason: %s", status.Reason)
			Expect(status.LastApplied).NotTo(BeNil())
			Expect(status.LastApplyMethod).To(Equal("ServerSideApply"))

			By("applying every container after recovery")
			after := fetchDeployment(ctx, namespace, deploymentName)
			appCPU, appMemory := containerRequest(after, "app")
			Expect(appCPU).To(Equal("300m"))
			Expect(appMemory).To(Equal("512Mi"))
			workerCPU, workerMemory := containerRequest(after, "worker")
			Expect(workerCPU).To(Equal("150m"))
			Expect(workerMemory).To(Equal("256Mi"))

			By("persisting recommendation annotations for both containers")
			Expect(after.GetAnnotations()).To(HaveKeyWithValue(
				optipodv1alpha1.AnnotationRecommendationPrefix+".app.cpu-request", "300m"))
			Expect(after.GetAnnotations()).To(HaveKeyWithValue(
				optipodv1alpha1.AnnotationRecommendationPrefix+".worker.cpu-request", "150m"))
		})
	})
})
