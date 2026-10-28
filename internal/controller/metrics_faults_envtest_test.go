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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/metrics"
)

// These specs prove VAL-FAST-007's processor dimension: while the metrics
// backend is unavailable the production workload processor skips the workload
// with an explicit signal and never mutates the requests, the processor
// forwards the policy's configured rolling window to the provider (the same
// range-query window the Prometheus provider embeds in its queries), and
// processing resumes with an exact application once valid samples return.

var _ = Describe("Metrics faults through the production processor", func() {
	const (
		namespace      = "metrics-faults-ns"
		deploymentName = "metrics-faults-deploy"
		policyName     = "metrics-faults-policy"
	)

	It("leaves the workload unchanged while the provider is unavailable, then resumes after valid samples", func() {
		ctx := context.Background()
		cleanup := createModesWorkloadFixture(ctx, namespace, deploymentName)
		defer cleanup()

		// Auto policy with an explicit 90m rolling window so the processor's
		// window propagation to the provider is observable.
		createModesPolicy(ctx, policyName, namespace, func(p *optipodv1alpha1.OptimizationPolicy) {
			p.Spec.MetricsConfig.RollingWindow = metav1.Duration{Duration: 90 * time.Minute}
		})
		policy := fetchModesPolicy(ctx, policyName)

		provider := &scriptedMetricsProvider{
			metricsByContainer: modesProvider().metricsByContainer,
			missingContainers:  map[string]error{},
			unavailable:        true,
		}
		processor := buildScriptedProcessor(provider)

		By("signaling a skip with the provider outage while leaving the workload unchanged")
		status, err := processor.ProcessWorkload(ctx, partialWorkload(ctx, namespace, deploymentName), policy)
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Status).To(Equal(StatusSkipped))
		Expect(status.Reason).To(ContainSubstring("Missing metrics:"))
		Expect(status.Reason).To(ContainSubstring("Failed to collect metrics for container app"))
		Expect(status.Reason).To(ContainSubstring("metrics backend unavailable"))
		Expect(status.LastApplied).To(BeNil())

		afterOutage := fetchDeployment(ctx, namespace, deploymentName)
		appCPU, appMemory := containerRequest(afterOutage, "app")
		Expect(appCPU).To(Equal("100m"), "requests must not change while metrics are unavailable")
		Expect(appMemory).To(Equal("128Mi"), "requests must not change while metrics are unavailable")
		assertNoRecommendationAnnotations(afterOutage.GetAnnotations())

		By("forwarding the policy's rolling window to the provider")
		Expect(provider.windowCalls).NotTo(BeEmpty())
		for _, window := range provider.windowCalls {
			Expect(window).To(Equal(90*time.Minute),
				"the processor must request the policy's configured rolling window, not a default")
		}

		By("applying the exact recommendation once valid samples return")
		provider.unavailable = false
		status, err = processor.ProcessWorkload(ctx, partialWorkload(ctx, namespace, deploymentName), policy)
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Status).To(Equal(StatusApplied), "reason: %s", status.Reason)
		Expect(status.LastApplied).NotTo(BeNil())
		Expect(status.LastApplyMethod).To(Equal("ServerSideApply"))

		afterRecovery := fetchDeployment(ctx, namespace, deploymentName)
		recoveryCPU, recoveryMemory := containerRequest(afterRecovery, "app")
		Expect(recoveryCPU).To(Equal("300m"), "the recovered apply must use the valid sample values")
		Expect(recoveryMemory).To(Equal("512Mi"), "the recovered apply must use the valid sample values")
		Expect(afterRecovery.GetAnnotations()).To(HaveKeyWithValue(
			optipodv1alpha1.AnnotationRecommendationPrefix+".app.cpu-request", "300m"))
	})

	It("skips with an explicit missing-series signal when the container has no metrics at all", func() {
		ctx := context.Background()
		cleanup := createModesWorkloadFixture(ctx, namespace+"-missing", deploymentName+"-missing")
		defer cleanup()

		createModesPolicy(ctx, policyName+"-missing", namespace+"-missing", func(p *optipodv1alpha1.OptimizationPolicy) {
			p.Spec.MetricsConfig.RollingWindow = metav1.Duration{Duration: 90 * time.Minute}
		})
		policy := fetchModesPolicy(ctx, policyName+"-missing")

		// The provider knows no series for the container (e.g. a brand-new
		// pod with no samples yet).
		provider := &scriptedMetricsProvider{
			metricsByContainer: map[string]*metrics.ContainerMetrics{},
			missingContainers: map[string]error{
				"app": &metrics.MetricsError{Message: "no data returned from Prometheus"},
			},
		}
		processor := buildScriptedProcessor(provider)

		status, err := processor.ProcessWorkload(ctx, partialWorkload(ctx, namespace+"-missing", deploymentName+"-missing"), policy)
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Status).To(Equal(StatusSkipped))
		Expect(status.Reason).To(ContainSubstring("Missing metrics:"))
		Expect(status.Reason).To(ContainSubstring("no data returned from Prometheus"))
		Expect(status.LastApplied).To(BeNil())

		afterSkip := fetchDeployment(ctx, namespace+"-missing", deploymentName+"-missing")
		skipCPU, skipMemory := containerRequest(afterSkip, "app")
		Expect(skipCPU).To(Equal("100m"))
		Expect(skipMemory).To(Equal("128Mi"))
		assertNoRecommendationAnnotations(afterSkip.GetAnnotations())
	})
})
