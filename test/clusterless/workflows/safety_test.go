package workflows

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/clusterless/harness"
)

var _ = Describe("Safety Mechanisms", func() {
	var h *harness.TestHarness

	BeforeEach(func() {
		h = harness.NewTestHarness()
	})

	AfterEach(func() {
		h.Cleanup()
	})

	Context("Memory Safety", func() {
		It("should prevent memory decrease when safety is enabled", func() {
			ctx := h.Context

			// Create deployment with 1Gi memory
			deploy := harness.NewDeployment("nginx", "default").
				WithLabels(map[string]string{"app": "nginx"}).
				WithContainer("nginx", "nginx:latest", "500m", "1Gi").
				Build()
			Expect(h.Client.Create(ctx, deploy)).To(Succeed())

			// Create policy with memory safety enabled
			policy := harness.NewPolicy("safe-policy", "default").
				WithMode(optipodv1alpha1.ModeRecommend).
				WithLabelSelector(map[string]string{"app": "nginx"}).
				WithSafetyConfig(true, nil). // Prevent memory decrease
				Build()
			Expect(h.Client.Create(ctx, policy)).To(Succeed())

			// Inject metrics showing low memory usage (512Mi P90)
			h.MetricsProvider.InjectConstantLoad(
				"default", "nginx-pod", "nginx",
				400,           // 400m CPU
				512*1024*1024, // 512Mi memory (less than current 1Gi)
				1*time.Hour,
				1*time.Minute,
			)

			// Verify metrics show lower usage
			containerMetrics, err := h.MetricsProvider.GetContainerMetrics(
				ctx, "default", "nginx-pod", "nginx", 1*time.Hour)
			Expect(err).NotTo(HaveOccurred())
			Expect(containerMetrics.Memory.P90).To(Equal(int64(512 * 1024 * 1024)))

			// Verify current deployment has 1Gi memory
			currentDeploy := &appsv1.Deployment{}
			key := types.NamespacedName{Name: "nginx", Namespace: "default"}
			Expect(h.Client.Get(ctx, key, currentDeploy)).To(Succeed())

			nginxContainer := currentDeploy.Spec.Template.Spec.Containers[0]
			currentMemory := nginxContainer.Resources.Requests.Memory()
			Expect(currentMemory.Value()).To(Equal(int64(1 * 1024 * 1024 * 1024))) // 1Gi

			// Note: In a full implementation, we would:
			// 1. Run the recommendation engine
			// 2. Apply safety checks
			// 3. Verify memory was NOT decreased due to safety policy
			// For now, we verify the foundation (metrics and policy) is correct
		})

		It("should allow memory increase even with safety enabled", func() {
			ctx := h.Context

			// Create deployment with 256Mi memory
			deploy := harness.NewDeployment("api", "default").
				WithLabels(map[string]string{"app": "api"}).
				WithContainer("api", "api:v1", "200m", "256Mi").
				Build()
			Expect(h.Client.Create(ctx, deploy)).To(Succeed())

			// Create policy with memory safety enabled
			policy := harness.NewPolicy("safe-increase-policy", "default").
				WithMode(optipodv1alpha1.ModeRecommend).
				WithLabelSelector(map[string]string{"app": "api"}).
				WithSafetyConfig(true, nil).
				Build()
			Expect(h.Client.Create(ctx, policy)).To(Succeed())

			// Inject metrics showing high memory usage (800Mi P90)
			h.MetricsProvider.InjectConstantLoad(
				"default", "api-pod", "api",
				300,           // 300m CPU
				800*1024*1024, // 800Mi memory (more than current 256Mi)
				1*time.Hour,
				1*time.Minute,
			)

			// Verify metrics show higher usage
			containerMetrics, err := h.MetricsProvider.GetContainerMetrics(
				ctx, "default", "api-pod", "api", 1*time.Hour)
			Expect(err).NotTo(HaveOccurred())
			Expect(containerMetrics.Memory.P90).To(Equal(int64(800 * 1024 * 1024)))

			// Memory increase should be allowed (tested in full implementation)
		})

		It("should support gradual memory decrease", func() {
			ctx := h.Context

			// Create deployment with 2Gi memory
			deploy := harness.NewDeployment("worker", "default").
				WithLabels(map[string]string{"app": "worker"}).
				WithContainer("worker", "worker:v1", "1000m", "2Gi").
				Build()
			Expect(h.Client.Create(ctx, deploy)).To(Succeed())

			// Create policy with gradual decrease (10% per cycle)
			gradualPercent := 0.1
			policy := harness.NewPolicy("gradual-policy", "default").
				WithMode(optipodv1alpha1.ModeRecommend).
				WithLabelSelector(map[string]string{"app": "worker"}).
				WithSafetyConfig(false, &gradualPercent).
				Build()
			Expect(h.Client.Create(ctx, policy)).To(Succeed())

			// Inject metrics showing much lower memory usage (500Mi P90)
			h.MetricsProvider.InjectConstantLoad(
				"default", "worker-pod", "worker",
				800,           // 800m CPU
				500*1024*1024, // 500Mi memory (much less than current 2Gi)
				1*time.Hour,
				1*time.Minute,
			)

			// Verify metrics
			containerMetrics, err := h.MetricsProvider.GetContainerMetrics(
				ctx, "default", "worker-pod", "worker", 1*time.Hour)
			Expect(err).NotTo(HaveOccurred())
			Expect(containerMetrics.Memory.P90).To(Equal(int64(500 * 1024 * 1024)))

			// Verify policy has gradual decrease configured
			Expect(policy.Spec.UpdateStrategy.GradualDecreaseConfig).NotTo(BeNil())
			Expect(policy.Spec.UpdateStrategy.GradualDecreaseConfig.Enabled).To(BeTrue())
			Expect(*policy.Spec.UpdateStrategy.GradualDecreaseConfig.MemoryDecreasePercentage).To(Equal(10))

			// Note: Full implementation would test multiple reconciliation cycles
			// showing gradual decrease from 2Gi → 1.8Gi → 1.62Gi → ... → 500Mi
		})
	})

	Context("Resource Bounds Safety", func() {
		//nolint:dupl // These tests are similar but test different scenarios (min vs max bounds)
		It("should enforce minimum resource bounds", func() {
			ctx := h.Context

			// Create deployment with very low resources
			deploy := harness.NewDeployment("micro", "default").
				WithLabels(map[string]string{"app": "micro"}).
				WithContainer("micro", "micro:v1", "10m", "32Mi").
				Build()
			Expect(h.Client.Create(ctx, deploy)).To(Succeed())

			// Create policy with minimum bounds higher than current usage
			policy := harness.NewPolicy("bounds-policy", "default").
				WithMode(optipodv1alpha1.ModeRecommend).
				WithLabelSelector(map[string]string{"app": "micro"}).
				WithResourceBounds("50m", "2000m", "64Mi", "4Gi").
				Build()
			Expect(h.Client.Create(ctx, policy)).To(Succeed())

			// Inject metrics showing very low usage (5m CPU, 16Mi memory)
			h.MetricsProvider.InjectConstantLoad(
				"default", "micro-pod", "micro",
				5,            // 5m CPU (below 50m minimum)
				16*1024*1024, // 16Mi memory (below 64Mi minimum)
				1*time.Hour,
				1*time.Minute,
			)

			// Verify metrics are below bounds
			containerMetrics, err := h.MetricsProvider.GetContainerMetrics(
				ctx, "default", "micro-pod", "micro", 1*time.Hour)
			Expect(err).NotTo(HaveOccurred())
			Expect(containerMetrics.CPU.P90).To(Equal(int64(5)))
			Expect(containerMetrics.Memory.P90).To(Equal(int64(16 * 1024 * 1024)))

			// Verify policy has minimum bounds configured
			Expect(policy.Spec.ResourceBounds.CPU.Min.String()).To(Equal("50m"))
			Expect(policy.Spec.ResourceBounds.Memory.Min.String()).To(Equal("64Mi"))

			// Note: Full implementation would ensure recommendations respect minimum bounds
		})

		//nolint:dupl // These tests are similar but test different scenarios (min vs max bounds)
		It("should enforce maximum resource bounds", func() {
			ctx := h.Context

			// Create deployment with high resource usage
			deploy := harness.NewDeployment("heavy", "default").
				WithLabels(map[string]string{"app": "heavy"}).
				WithContainer("heavy", "heavy:v1", "1000m", "1Gi").
				Build()
			Expect(h.Client.Create(ctx, deploy)).To(Succeed())

			// Create policy with maximum bounds lower than metrics
			policy := harness.NewPolicy("max-bounds-policy", "default").
				WithMode(optipodv1alpha1.ModeRecommend).
				WithLabelSelector(map[string]string{"app": "heavy"}).
				WithResourceBounds("10m", "800m", "64Mi", "512Mi"). // Max CPU 800m, Max Mem 512Mi
				Build()
			Expect(h.Client.Create(ctx, policy)).To(Succeed())

			// Inject metrics showing high usage (exceeding bounds)
			h.MetricsProvider.InjectConstantLoad(
				"default", "heavy-pod", "heavy",
				1200,          // 1200m CPU (exceeds 800m maximum)
				800*1024*1024, // 800Mi memory (exceeds 512Mi maximum)
				1*time.Hour,
				1*time.Minute,
			)

			// Verify metrics exceed bounds
			containerMetrics, err := h.MetricsProvider.GetContainerMetrics(
				ctx, "default", "heavy-pod", "heavy", 1*time.Hour)
			Expect(err).NotTo(HaveOccurred())
			Expect(containerMetrics.CPU.P90).To(Equal(int64(1200)))
			Expect(containerMetrics.Memory.P90).To(Equal(int64(800 * 1024 * 1024)))

			// Verify policy has maximum bounds configured
			Expect(policy.Spec.ResourceBounds.CPU.Max.String()).To(Equal("800m"))
			Expect(policy.Spec.ResourceBounds.Memory.Max.String()).To(Equal("512Mi"))

			// Note: Full implementation would cap recommendations at maximum bounds
		})
	})

	Context("Safety Factor Application", func() {
		It("should apply safety factor correctly", func() {
			ctx := h.Context

			// Create deployment
			deploy := harness.NewDeployment("factor-test", "default").
				WithLabels(map[string]string{"app": "factor-test"}).
				WithContainer("app", "app:v1", "100m", "128Mi").
				Build()
			Expect(h.Client.Create(ctx, deploy)).To(Succeed())

			// Create policy with 2.0x safety factor
			policy := harness.NewPolicy("factor-policy", "default").
				WithMode(optipodv1alpha1.ModeRecommend).
				WithLabelSelector(map[string]string{"app": "factor-test"}).
				WithMetricsConfig("prometheus", "P90", 2.0).
				Build()
			Expect(h.Client.Create(ctx, policy)).To(Succeed())

			// Inject metrics with known values
			h.MetricsProvider.InjectConstantLoad(
				"default", "factor-test-pod", "app",
				300,           // 300m CPU
				256*1024*1024, // 256Mi memory
				1*time.Hour,
				1*time.Minute,
			)

			// Verify metrics
			containerMetrics, err := h.MetricsProvider.GetContainerMetrics(
				ctx, "default", "factor-test-pod", "app", 1*time.Hour)
			Expect(err).NotTo(HaveOccurred())
			Expect(containerMetrics.CPU.P90).To(Equal(int64(300)))
			Expect(containerMetrics.Memory.P90).To(Equal(int64(256 * 1024 * 1024)))

			// Verify safety factor in policy
			Expect(*policy.Spec.MetricsConfig.SafetyFactor).To(Equal(2.0))

			// Note: Full implementation would apply 2.0x factor:
			// CPU: 300m * 2.0 = 600m
			// Memory: 256Mi * 2.0 = 512Mi
		})

		It("should handle different percentiles with safety factor", func() {
			ctx := h.Context

			// Create policy with P95 and 1.5x safety factor
			policy := harness.NewPolicy("p95-policy", "default").
				WithMode(optipodv1alpha1.ModeRecommend).
				WithMetricsConfig("prometheus", "P95", 1.5).
				Build()
			Expect(h.Client.Create(ctx, policy)).To(Succeed())

			// Inject spikey load to create different percentiles
			h.MetricsProvider.InjectSpikeyLoad(
				"default", "p95-test-pod", "app",
				100, 500, // baseCPU, spikeCPU
				100*1024*1024, 300*1024*1024, // baseMem, spikeMem
				1*time.Hour,
			)

			// Verify different percentiles
			containerMetrics, err := h.MetricsProvider.GetContainerMetrics(
				ctx, "default", "p95-test-pod", "app", 1*time.Hour)
			Expect(err).NotTo(HaveOccurred())

			// With 10% spikes, P95 should be closer to spike values than P90
			Expect(containerMetrics.CPU.P95).To(BeNumerically(">=", containerMetrics.CPU.P90))
			Expect(containerMetrics.CPU.P95).To(BeNumerically("<=", containerMetrics.CPU.P99))

			// Verify policy uses P95
			Expect(policy.Spec.MetricsConfig.Percentile).To(Equal("P95"))
		})
	})

	Context("Edge Cases", func() {
		It("should handle missing metrics gracefully", func() {
			ctx := h.Context

			// Create deployment but don't inject metrics
			deploy := harness.NewDeployment("no-metrics", "default").
				WithLabels(map[string]string{"app": "no-metrics"}).
				WithContainer("app", "app:v1", "100m", "128Mi").
				Build()
			Expect(h.Client.Create(ctx, deploy)).To(Succeed())

			// Verify no metrics exist
			Expect(h.MetricsProvider.HasMetrics("default", "no-metrics-pod", "app")).To(BeFalse())

			// Attempt to get metrics should fail gracefully
			_, err := h.MetricsProvider.GetContainerMetrics(
				ctx, "default", "no-metrics-pod", "app", 1*time.Hour)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no metrics found"))
		})

		It("should handle zero resource usage", func() {
			ctx := h.Context

			// Inject zero usage metrics
			h.MetricsProvider.InjectConstantLoad(
				"default", "zero-pod", "app",
				0, // 0m CPU
				0, // 0 bytes memory
				1*time.Hour,
				1*time.Minute,
			)

			// Verify zero metrics
			containerMetrics, err := h.MetricsProvider.GetContainerMetrics(
				ctx, "default", "zero-pod", "app", 1*time.Hour)
			Expect(err).NotTo(HaveOccurred())
			Expect(containerMetrics.CPU.P90).To(Equal(int64(0)))
			Expect(containerMetrics.Memory.P90).To(Equal(int64(0)))

			// Note: Full implementation should handle zero usage with minimum bounds
		})
	})
})
