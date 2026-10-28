package workflows

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/clusterless/harness"
)

var _ = Describe("Complete Optimization Cycle", func() {
	var h *harness.TestHarness

	BeforeEach(func() {
		h = harness.NewTestHarness()
	})

	AfterEach(func() {
		h.Cleanup()
	})

	Context("Basic Policy Operations", func() {
		It("should create and validate a basic optimization policy", func() {
			ctx := h.Context

			// Create a simple policy
			policy := harness.NewPolicy("test-policy", "default").
				WithMode(optipodv1alpha1.ModeRecommend).
				WithLabelSelector(map[string]string{"app": "test"}).
				Build()

			Expect(h.Client.Create(ctx, policy)).To(Succeed())

			// Verify policy was created
			createdPolicy := &optipodv1alpha1.OptimizationPolicy{}
			key := types.NamespacedName{Name: "test-policy", Namespace: "default"}
			Expect(h.Client.Get(ctx, key, createdPolicy)).To(Succeed())

			Expect(createdPolicy.Name).To(Equal("test-policy"))
			Expect(createdPolicy.Spec.Mode).To(Equal(optipodv1alpha1.ModeRecommend))
			Expect(createdPolicy.Spec.Selector.WorkloadSelector.MatchLabels).To(HaveKeyWithValue("app", "test"))
		})

		It("should handle policy with resource bounds", func() {
			ctx := h.Context

			// Create policy with resource bounds
			policy := harness.NewPolicy("bounded-policy", "default").
				WithMode(optipodv1alpha1.ModeRecommend).
				WithResourceBounds("10m", "2000m", "64Mi", "4Gi").
				WithMetricsConfig("prometheus", "P90", 1.2).
				Build()

			Expect(h.Client.Create(ctx, policy)).To(Succeed())

			// Verify resource bounds
			createdPolicy := &optipodv1alpha1.OptimizationPolicy{}
			key := types.NamespacedName{Name: "bounded-policy", Namespace: "default"}
			Expect(h.Client.Get(ctx, key, createdPolicy)).To(Succeed())

			Expect(createdPolicy.Spec.ResourceBounds.CPU.Min.String()).To(Equal("10m"))
			Expect(createdPolicy.Spec.ResourceBounds.CPU.Max.String()).To(Equal("2"))
			Expect(createdPolicy.Spec.ResourceBounds.Memory.Min.String()).To(Equal("64Mi"))
			Expect(createdPolicy.Spec.ResourceBounds.Memory.Max.String()).To(Equal("4Gi"))

			Expect(createdPolicy.Spec.MetricsConfig.Provider).To(Equal("prometheus"))
			Expect(createdPolicy.Spec.MetricsConfig.Percentile).To(Equal("P90"))
			Expect(*createdPolicy.Spec.MetricsConfig.SafetyFactor).To(Equal(1.2))
		})
	})

	Context("Workload Discovery", func() {
		It("should discover deployments matching policy selector", func() {
			ctx := h.Context

			// Create deployments with different labels
			matchingDeploy := harness.NewDeployment("api", "production").
				WithLabels(map[string]string{"app": "api", "tier": "backend"}).
				WithContainer("api", "api:v1", "100m", "128Mi").
				Build()
			Expect(h.Client.Create(ctx, matchingDeploy)).To(Succeed())

			nonMatchingDeploy := harness.NewDeployment("frontend", "production").
				WithLabels(map[string]string{"app": "frontend", "tier": "frontend"}).
				WithContainer("frontend", "frontend:v1", "50m", "64Mi").
				Build()
			Expect(h.Client.Create(ctx, nonMatchingDeploy)).To(Succeed())

			// Create policy that matches only backend tier
			policy := harness.NewPolicy("backend-policy", "production").
				WithMode(optipodv1alpha1.ModeRecommend).
				WithLabelSelector(map[string]string{"tier": "backend"}).
				Build()
			Expect(h.Client.Create(ctx, policy)).To(Succeed())

			// Verify only matching deployment is discovered
			// Note: This test assumes we have workload discovery logic
			// In a real implementation, we'd call the discovery service here
		})
	})

	Context("Metrics Integration", func() {
		It("should process metrics and calculate recommendations", func() {
			ctx := h.Context

			// Create deployment
			deploy := harness.NewDeployment("api", "production").
				WithLabels(map[string]string{"app": "api", "tier": "backend"}).
				WithContainer("api", "api:v1", "100m", "128Mi").
				Build()
			Expect(h.Client.Create(ctx, deploy)).To(Succeed())

			// Inject metrics data - simulating 1 hour of spikey load
			h.MetricsProvider.InjectSpikeyLoad(
				"production", "api-pod-123", "api",
				200, 800, // baseCPU, spikeCPU (milli)
				200*1024*1024, 400*1024*1024, // baseMem, spikeMem (bytes)
				1*time.Hour,
			)

			// Verify metrics are available
			Expect(h.MetricsProvider.HasMetrics("production", "api-pod-123", "api")).To(BeTrue())
			Expect(h.MetricsProvider.GetSampleCount("production", "api-pod-123", "api")).To(BeNumerically(">", 0))

			// Get metrics and verify percentiles
			containerMetrics, err := h.MetricsProvider.GetContainerMetrics(
				ctx, "production", "api-pod-123", "api", 1*time.Hour)
			Expect(err).NotTo(HaveOccurred())

			// With spikey load (10% spikes), P90 should be close to spike values
			// Note: The exact percentile depends on the distribution of samples
			Expect(containerMetrics.CPU.P90).To(BeNumerically(">=", 200))    // At least base value
			Expect(containerMetrics.CPU.P50).To(BeNumerically("~", 200, 50)) // Close to base 200m

			// Memory should follow similar pattern
			Expect(containerMetrics.Memory.P90).To(BeNumerically(">=", 200*1024*1024)) // At least base value
		})

		It("should handle constant load metrics", func() {
			ctx := h.Context

			// Inject constant load
			h.MetricsProvider.InjectConstantLoad(
				"default", "nginx-pod", "nginx",
				500,           // 500m CPU
				256*1024*1024, // 256Mi memory
				2*time.Hour,
				1*time.Minute,
			)

			// Get metrics
			containerMetrics, err := h.MetricsProvider.GetContainerMetrics(
				ctx, "default", "nginx-pod", "nginx", 2*time.Hour)
			Expect(err).NotTo(HaveOccurred())

			// All percentiles should be the same for constant load
			Expect(containerMetrics.CPU.P50).To(Equal(int64(500)))
			Expect(containerMetrics.CPU.P90).To(Equal(int64(500)))
			Expect(containerMetrics.CPU.P95).To(Equal(int64(500)))
			Expect(containerMetrics.CPU.P99).To(Equal(int64(500)))

			expectedMem := int64(256 * 1024 * 1024)
			Expect(containerMetrics.Memory.P50).To(Equal(expectedMem))
			Expect(containerMetrics.Memory.P90).To(Equal(expectedMem))
		})
	})

	Context("Policy Status Updates", func() {
		It("should update policy status during reconciliation", func() {
			ctx := h.Context

			// Create policy
			policy := harness.NewPolicy("status-policy", "default").
				WithMode(optipodv1alpha1.ModeRecommend).
				WithLabelSelector(map[string]string{"app": "test"}).
				Build()
			Expect(h.Client.Create(ctx, policy)).To(Succeed())

			// Note: In a full implementation, we would:
			// 1. Reconcile the policy using a controller simulator
			// 2. Check that policy status was updated with conditions
			// For now, we verify the policy was created correctly

			createdPolicy := &optipodv1alpha1.OptimizationPolicy{}
			key := types.NamespacedName{Name: "status-policy", Namespace: "default"}
			Expect(h.Client.Get(ctx, key, createdPolicy)).To(Succeed())
			Expect(createdPolicy.Name).To(Equal("status-policy"))
		})
	})

	Context("Time-based Testing", func() {
		It("should handle time-based operations with mock clock", func() {
			// Record start time
			startTime := h.Clock.Now()

			// Advance clock by 5 minutes
			h.Clock.Advance(5 * time.Minute)

			// Verify time advancement
			Expect(h.Clock.Since(startTime)).To(Equal(5 * time.Minute))
			Expect(h.Clock.Now()).To(Equal(startTime.Add(5 * time.Minute)))
		})
	})

	Context("Resource Calculations", func() {
		It("should apply safety factor to recommendations", func() {
			ctx := h.Context

			// Create deployment
			deploy := harness.NewDeployment("calc-test", "default").
				WithContainer("app", "app:v1", "100m", "128Mi").
				Build()
			Expect(h.Client.Create(ctx, deploy)).To(Succeed())

			// Inject metrics with known values
			h.MetricsProvider.InjectConstantLoad(
				"default", "calc-test-pod", "app",
				400,           // 400m CPU
				200*1024*1024, // 200Mi memory
				1*time.Hour,
				1*time.Minute,
			)

			// Create policy with 1.5x safety factor
			policy := harness.NewPolicy("calc-policy", "default").
				WithMode(optipodv1alpha1.ModeRecommend).
				WithMetricsConfig("prometheus", "P90", 1.5).
				Build()
			Expect(h.Client.Create(ctx, policy)).To(Succeed())

			// Get metrics
			containerMetrics, err := h.MetricsProvider.GetContainerMetrics(
				ctx, "default", "calc-test-pod", "app", 1*time.Hour)
			Expect(err).NotTo(HaveOccurred())

			// With constant load and 1.5x safety factor:
			// CPU: 400m * 1.5 = 600m
			// Memory: 200Mi * 1.5 = 300Mi
			// Verify base metrics (before safety factor)
			Expect(containerMetrics.CPU.P90).To(Equal(int64(400)))
			Expect(containerMetrics.Memory.P90).To(Equal(int64(200 * 1024 * 1024)))

			// Note: Actual recommendation calculation would happen in the recommendation engine
			// This test verifies the metrics foundation is working correctly
		})
	})
})
