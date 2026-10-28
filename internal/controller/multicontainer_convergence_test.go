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
	"fmt"
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

// perContainerMetricsProvider serves deterministic metrics per container name so
// a two-container workload gets distinct, reproducible recommendations through
// the production processing path.
type perContainerMetricsProvider struct {
	metricsByContainer map[string]*metrics.ContainerMetrics
}

func (p *perContainerMetricsProvider) GetContainerMetrics(
	ctx context.Context,
	namespace, podName, containerName string,
	window time.Duration,
) (*metrics.ContainerMetrics, error) {
	if m, ok := p.metricsByContainer[containerName]; ok {
		return m, nil
	}
	return nil, &metrics.MetricsError{Message: fmt.Sprintf("no metrics available for container %s", containerName)}
}

func (p *perContainerMetricsProvider) HealthCheck(ctx context.Context) error {
	return nil
}

// buildConvergenceEngine wires the production application engine and workload
// processor against the suite's envtest API server.
func buildConvergenceProcessor(provider *perContainerMetricsProvider) *WorkloadProcessor {
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

// createTwoContainerAutoFixture creates a two-container Deployment plus a pod
// matching its selector, and an Auto-mode policy using the given strategy.
// It returns the fetched policy and a cleanup function.
func createTwoContainerAutoFixture(ctx context.Context, namespace, deploymentName, strategy string) (*optipodv1alpha1.OptimizationPolicy, func()) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}
	Expect(k8sClient.Create(ctx, ns)).To(Succeed())

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": deploymentName},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": deploymentName},
				},
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

	// The production processor resolves the workload's pod by selector before
	// collecting metrics, so a matching pod must exist in the API.
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
	policy := &optipodv1alpha1.OptimizationPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName + "-policy",
			Namespace: "default",
		},
		Spec: optipodv1alpha1.OptimizationPolicySpec{
			Mode: optipodv1alpha1.ModeAuto,
			Selector: optipodv1alpha1.WorkloadSelector{
				Namespaces: &optipodv1alpha1.NamespaceFilter{
					Allow: []string{namespace},
				},
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
				AllowInPlaceResize: true,
				UpdateRequestsOnly: true,
			},
		},
	}
	if strategy != "" {
		policy.Spec.UpdateStrategy.Strategy = &strategy
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

// convergenceProvider returns deterministic per-container P90 metrics:
// app -> 300m/512Mi and worker -> 150m/256Mi with a 1.0 safety factor.
func convergenceProvider() *perContainerMetricsProvider {
	return &perContainerMetricsProvider{
		metricsByContainer: map[string]*metrics.ContainerMetrics{
			"app": {
				CPU: metrics.ResourceMetrics{
					P50:     resource.MustParse("200m"),
					P90:     resource.MustParse("300m"),
					P99:     resource.MustParse("400m"),
					Samples: 50,
				},
				Memory: metrics.ResourceMetrics{
					P50:     resource.MustParse("384Mi"),
					P90:     resource.MustParse("512Mi"),
					P99:     resource.MustParse("640Mi"),
					Samples: 50,
				},
			},
			"worker": {
				CPU: metrics.ResourceMetrics{
					P50:     resource.MustParse("100m"),
					P90:     resource.MustParse("150m"),
					P99:     resource.MustParse("200m"),
					Samples: 50,
				},
				Memory: metrics.ResourceMetrics{
					P50:     resource.MustParse("192Mi"),
					P90:     resource.MustParse("256Mi"),
					P99:     resource.MustParse("320Mi"),
					Samples: 50,
				},
			},
		},
	}
}

// fetchDeployment reads the deployment back through the API.
func fetchDeployment(ctx context.Context, namespace, name string) *appsv1.Deployment {
	fetched := &appsv1.Deployment{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, fetched)).To(Succeed())
	return fetched
}

// containerRequest returns the container's CPU/memory request strings, failing
// with a clear message when requests were pruned away.
func containerRequest(deployment *appsv1.Deployment, containerName string) (cpu, memory string) {
	for _, c := range deployment.Spec.Template.Spec.Containers {
		if c.Name != containerName {
			continue
		}
		if c.Resources.Requests == nil {
			return "", ""
		}
		return c.Resources.Requests.Cpu().String(), c.Resources.Requests.Memory().String()
	}
	Fail(fmt.Sprintf("container %s not found in deployment %s", containerName, deployment.Name))
	return "", ""
}

// optipodApplyManagedFields returns the managedFields entry owned by the
// optipod field manager with an Apply operation, or nil when absent.
func optipodApplyManagedFields(obj metav1.Object) *metav1.ManagedFieldsEntry {
	for i := range obj.GetManagedFields() {
		mf := &obj.GetManagedFields()[i]
		if mf.Manager == application.FieldManagerName && mf.Operation == metav1.ManagedFieldsOperationApply {
			return mf
		}
	}
	return nil
}

var _ = Describe("Multi-container Auto convergence", func() {
	ctx := context.Background()

	Context("Under an explicit strategy: ssa policy", func() {
		const (
			namespace      = "multicontainer-ssa-test"
			deploymentName = "mc-ssa-deployment"
		)

		It("applies every container's recommendation in one convergent apply", func() {
			policy, cleanup := createTwoContainerAutoFixture(ctx, namespace, deploymentName, string(optipodv1alpha1.StrategySSA))
			defer cleanup()

			processor := buildConvergenceProcessor(convergenceProvider())

			// First reconcile through the production processor.
			workload := &optipoddiscovery.Workload{
				Kind:      "Deployment",
				Namespace: namespace,
				Name:      deploymentName,
				Object:    fetchDeployment(ctx, namespace, deploymentName),
			}
			status, err := processor.ProcessWorkload(ctx, workload, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Status).To(Equal(StatusApplied), "expected the two-container workload to be applied, reason: %s", status.Reason)

			afterFirst := fetchDeployment(ctx, namespace, deploymentName)

			// Both containers must retain their recommended requests after one
			// workload-wide apply; a per-container apply would have pruned the
			// sibling container's fields.
			appCPU, appMemory := containerRequest(afterFirst, "app")
			Expect(appCPU).To(Equal("300m"), "container app lost its applied request")
			Expect(appMemory).To(Equal("512Mi"), "container app lost its applied request")
			workerCPU, workerMemory := containerRequest(afterFirst, "worker")
			Expect(workerCPU).To(Equal("150m"), "container worker lost its applied request")
			Expect(workerMemory).To(Equal("256Mi"), "container worker lost its applied request")

			// Unrelated template fields are untouched.
			Expect(afterFirst.Spec.Template.Spec.Containers[0].Image).To(Equal("nginx:1.21"))
			Expect(afterFirst.Spec.Template.Spec.Containers[1].Image).To(Equal("busybox:1.36"))
			Expect(afterFirst.Spec.Template.Labels).To(HaveKeyWithValue("app", deploymentName))
			Expect(*afterFirst.Spec.Replicas).To(Equal(int32(1)))

			// The optipod field manager owns both containers' applied leaves.
			owner := optipodApplyManagedFields(afterFirst)
			Expect(owner).NotTo(BeNil(), "expected optipod to own the applied fields")
			Expect(owner.FieldsV1.String()).To(ContainSubstring(`k:{\"name\":\"app\"}`), "field owner must cover container app")
			Expect(owner.FieldsV1.String()).To(ContainSubstring(`k:{\"name\":\"worker\"}`), "field owner must cover container worker")

			generationAfterFirst := afterFirst.Generation

			// Second reconcile with identical metrics must reach a steady state:
			// no further template mutation, no rollout churn, same field owner.
			workload = &optipoddiscovery.Workload{
				Kind:      "Deployment",
				Namespace: namespace,
				Name:      deploymentName,
				Object:    fetchDeployment(ctx, namespace, deploymentName),
			}
			status, err = processor.ProcessWorkload(ctx, workload, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Status).To(Equal(StatusApplied), "second reconcile should still succeed, reason: %s", status.Reason)

			afterSecond := fetchDeployment(ctx, namespace, deploymentName)
			appCPU, appMemory = containerRequest(afterSecond, "app")
			Expect(appCPU).To(Equal("300m"), "container app request churned on repeat reconcile")
			Expect(appMemory).To(Equal("512Mi"), "container app request churned on repeat reconcile")
			workerCPU, workerMemory = containerRequest(afterSecond, "worker")
			Expect(workerCPU).To(Equal("150m"), "container worker request churned on repeat reconcile")
			Expect(workerMemory).To(Equal("256Mi"), "container worker request churned on repeat reconcile")

			Expect(afterSecond.Generation).To(Equal(generationAfterFirst), "repeat reconcile must not bump the template generation")
			Expect(afterSecond.Spec.Template.Spec.Containers[0].Image).To(Equal("nginx:1.21"))
			Expect(afterSecond.Spec.Template.Spec.Containers[1].Image).To(Equal("busybox:1.36"))

			owner = optipodApplyManagedFields(afterSecond)
			Expect(owner).NotTo(BeNil(), "optipod must remain the applied-field owner after the repeat reconcile")
			Expect(owner.FieldsV1.String()).To(ContainSubstring(`k:{\"name\":\"app\"}`))
			Expect(owner.FieldsV1.String()).To(ContainSubstring(`k:{\"name\":\"worker\"}`))

			Expect(status.Recommendations).To(HaveLen(2))
		})
	})

	Context("Under the default webhook strategy", func() {
		const (
			namespace      = "multicontainer-webhook-test"
			deploymentName = "mc-webhook-deployment"
		)

		It("persists every container's recommendation annotations across reconciles", func() {
			policy, cleanup := createTwoContainerAutoFixture(ctx, namespace, deploymentName, "")
			defer cleanup()

			processor := buildConvergenceProcessor(convergenceProvider())

			workload := &optipoddiscovery.Workload{
				Kind:      "Deployment",
				Namespace: namespace,
				Name:      deploymentName,
				Object:    fetchDeployment(ctx, namespace, deploymentName),
			}
			status, err := processor.ProcessWorkload(ctx, workload, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Status).To(Equal(StatusApplied), "expected the two-container workload to be applied, reason: %s", status.Reason)

			afterFirst := fetchDeployment(ctx, namespace, deploymentName)

			// Every container's recommendation annotations must persist; a
			// later container's store must not clear an earlier container's keys.
			expectedAnnotations := map[string]string{
				optipodv1alpha1.AnnotationCPURequestPrefix + ".app":       "300m",
				optipodv1alpha1.AnnotationMemoryRequestPrefix + ".app":    "512Mi",
				optipodv1alpha1.AnnotationCPURequestPrefix + ".worker":    "150m",
				optipodv1alpha1.AnnotationMemoryRequestPrefix + ".worker": "256Mi",
				optipodv1alpha1.AnnotationWebhookEnabled:                  "true",
				optipodv1alpha1.AnnotationStrategy:                        string(optipodv1alpha1.StrategyWebhook),
			}
			for key, want := range expectedAnnotations {
				Expect(afterFirst.Annotations).To(HaveKeyWithValue(key, want), "workload must retain every container's recommendation annotation")
			}

			// The default webhook rollout (onNextRestart) never edits the pod
			// template resources directly.
			appCPU, appMemory := containerRequest(afterFirst, "app")
			Expect(appCPU).To(Equal("100m"), "webhook strategy must not edit template requests")
			Expect(appMemory).To(Equal("128Mi"), "webhook strategy must not edit template requests")
			workerCPU, workerMemory := containerRequest(afterFirst, "worker")
			Expect(workerCPU).To(Equal("50m"), "webhook strategy must not edit template requests")
			Expect(workerMemory).To(Equal("64Mi"), "webhook strategy must not edit template requests")

			// Second reconcile with identical metrics keeps every container's keys.
			workload = &optipoddiscovery.Workload{
				Kind:      "Deployment",
				Namespace: namespace,
				Name:      deploymentName,
				Object:    fetchDeployment(ctx, namespace, deploymentName),
			}
			status, err = processor.ProcessWorkload(ctx, workload, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Status).To(Equal(StatusApplied), "second reconcile should still succeed, reason: %s", status.Reason)

			afterSecond := fetchDeployment(ctx, namespace, deploymentName)
			for key, want := range expectedAnnotations {
				Expect(afterSecond.Annotations).To(HaveKeyWithValue(key, want), "repeat reconcile must keep every container's recommendation annotation")
			}
		})
	})
})
