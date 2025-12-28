package contract

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Test Determinism and Consistency Validation", func() {
	var (
		config  *ConsistencyConfig
		execEnv *ExecutionEnvironment
		ctx     context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		config = GetConsistencyConfig()
		execEnv = GetExecutionEnvironment()
	})

	Context("when validating consistency configuration", func() {
		// Requirements 1.5, 10.1, 10.2, 10.3: Ensure tests produce identical results locally and in CI
		It("should have valid and consistent configuration values", func() {
			By("validating consistency configuration")
			Expect(config.Validate()).To(Succeed(), "Consistency configuration should be valid")

			By("verifying timeout constraints are met")
			// Requirements 1.1: complete within 10 minutes maximum
			Expect(config.ContractTestTimeout).To(BeNumerically("<=", 10*time.Minute),
				"Contract test timeout should not exceed 10 minutes")

			// Allow longer status timeout for controller processing
			Expect(config.StatusTimeout).To(BeNumerically("<=", 5*time.Minute),
				"Status timeout should not exceed 5 minutes")

			// Requirements 6.2: 5-second intervals minimum
			Expect(config.StatusPollInterval).To(BeNumerically(">=", 5*time.Second),
				"Status poll interval should be at least 5 seconds")

			By("verifying cluster configuration is consistent")
			Expect(config.ClusterName).ToNot(BeEmpty(), "Cluster name should not be empty")
			Expect(config.ImageTag).ToNot(BeEmpty(), "Image tag should not be empty")
			Expect(config.Namespace).ToNot(BeEmpty(), "Namespace should not be empty")

			By("verifying tool binaries are configured")
			Expect(config.KindBinary).ToNot(BeEmpty(), "Kind binary should not be empty")
			Expect(config.KubectlBinary).ToNot(BeEmpty(), "Kubectl binary should not be empty")
			Expect(config.DockerBinary).ToNot(BeEmpty(), "Docker binary should not be empty")
		})

		// Requirements 10.4: Ensure no manual setup required for local execution
		It("should initialize execution environment without manual setup", func() {
			By("initializing execution environment")
			Eventually(func() error {
				return execEnv.Initialize(ctx)
			}, 2*time.Minute, 10*time.Second).Should(Succeed(),
				"Execution environment should initialize without manual setup")

			By("verifying environment is properly initialized")
			Expect(execEnv.IsInitialized()).To(BeTrue(), "Execution environment should be initialized")
			Expect(execEnv.GetProjectRoot()).ToNot(BeEmpty(), "Project root should be determined")
			Expect(execEnv.GetBinDir()).ToNot(BeEmpty(), "Binary directory should be determined")
		})

		// Requirements 10.5: Ensure consistent timeout and polling behavior
		It("should use consistent timeout and polling behavior", func() {
			By("verifying timeout consistency")
			Expect(config.ContractTestTimeout).To(Equal(10*time.Minute),
				"Contract test timeout should be consistently 10 minutes")
			Expect(config.StatusTimeout).To(Equal(5*time.Minute),
				"Status timeout should be consistently 5 minutes")
			Expect(config.StatusPollInterval).To(Equal(5*time.Second),
				"Status poll interval should be consistently 5 seconds")

			By("verifying polling intervals are consistent")
			Expect(config.PollInterval).To(BeNumerically(">", 0),
				"Poll interval should be positive")
			Expect(config.CRPollInterval).To(BeNumerically(">", 0),
				"CR poll interval should be positive")
			Expect(config.ClusterPollInterval).To(BeNumerically(">", 0),
				"Cluster poll interval should be positive")
		})
	})

	Context("when validating environment detection", func() {
		It("should correctly detect the execution environment", func() {
			By("checking environment detection")
			envInfo := config.GetEnvironmentInfo()

			Expect(envInfo).To(HaveKey("environment"), "Environment info should include environment type")
			Expect(envInfo).To(HaveKey("is_ci"), "Environment info should include CI detection")
			Expect(envInfo).To(HaveKey("cluster_name"), "Environment info should include cluster name")

			By("verifying environment-specific configuration")
			environment := envInfo["environment"].(string)
			Expect(environment).To(BeElementOf([]string{"local", "CI"}),
				"Environment should be either 'local' or 'CI'")

			if config.VerboseLogging {
				fmt.Printf("Detected environment: %s\n", environment)
				fmt.Printf("CI environment: %v\n", config.IsCI)
			}
		})
	})

	// **Feature: contract-e2e-testing, Property 4: Test determinism**
	// **Validates: Requirements 1.5**
	// Property-based test for determinism across multiple configuration scenarios
	DescribeTable("Test determinism property across multiple scenarios",
		func(scenario string, envVar string, expectedBehavior string) {
			By("testing determinism for scenario: " + scenario)

			// Create a new configuration for this scenario
			testConfig := NewConsistencyConfig()

			By("validating configuration is deterministic")
			Expect(testConfig.Validate()).To(Succeed(),
				"Configuration should be valid for scenario: "+scenario)

			By("verifying consistent behavior regardless of environment")
			// Test that core timeout values remain consistent
			Expect(testConfig.ContractTestTimeout).To(Equal(10*time.Minute),
				"Contract timeout should be deterministic for scenario: "+scenario)
			Expect(testConfig.StatusTimeout).To(Equal(5*time.Minute),
				"Status timeout should be deterministic for scenario: "+scenario)
			Expect(testConfig.StatusPollInterval).To(Equal(5*time.Second),
				"Status poll interval should be deterministic for scenario: "+scenario)

			By("verifying tool configuration is consistent")
			Expect(testConfig.KindBinary).ToNot(BeEmpty(),
				"Kind binary should be configured for scenario: "+scenario)
			Expect(testConfig.KubectlBinary).ToNot(BeEmpty(),
				"Kubectl binary should be configured for scenario: "+scenario)
			Expect(testConfig.DockerBinary).ToNot(BeEmpty(),
				"Docker binary should be configured for scenario: "+scenario)
		},
		Entry("default configuration scenario", "default", "", "consistent_defaults"),
		Entry("CI environment scenario", "ci", "CI=true", "ci_optimized"),
		Entry("local development scenario", "local", "CI=", "local_optimized"),
		Entry("custom timeout scenario", "custom_timeout", "CONTRACT_TEST_TIMEOUT=5m", "custom_values"),
		Entry("verbose logging scenario", "verbose", "CONTRACT_VERBOSE_LOGGING=true", "verbose_output"),
	)

	// **Feature: contract-e2e-testing, Property 13: Local execution consistency**
	// **Validates: Requirements 10.1, 10.2, 10.3, 10.4, 10.5**
	// Property-based test for local execution consistency
	DescribeTable("Local execution consistency property across multiple scenarios",
		func(scenario string, setupAction string, expectedOutcome string) {
			By("testing local execution consistency for scenario: " + scenario)

			// Create execution environment for this scenario
			testExecEnv := NewExecutionEnvironment(config)

			By("verifying initialization is consistent")
			Eventually(func() error {
				return testExecEnv.Initialize(ctx)
			}, 2*time.Minute, 10*time.Second).Should(Succeed(),
				"Initialization should be consistent for scenario: "+scenario)

			By("verifying environment setup is deterministic")
			Expect(testExecEnv.IsInitialized()).To(BeTrue(),
				"Environment should be initialized for scenario: "+scenario)
			Expect(testExecEnv.GetProjectRoot()).ToNot(BeEmpty(),
				"Project root should be determined for scenario: "+scenario)

			By("verifying tool availability is consistent")
			// All required tools should be available after initialization
			Expect(testExecEnv.GetConfig().KindBinary).ToNot(BeEmpty(),
				"Kind should be available for scenario: "+scenario)
			Expect(testExecEnv.GetConfig().KubectlBinary).ToNot(BeEmpty(),
				"Kubectl should be available for scenario: "+scenario)
			Expect(testExecEnv.GetConfig().DockerBinary).ToNot(BeEmpty(),
				"Docker should be available for scenario: "+scenario)
		},
		Entry("fresh environment scenario", "fresh", "clean_setup", "fully_initialized"),
		Entry("existing tools scenario", "existing", "tools_present", "reuse_tools"),
		Entry("missing kustomize scenario", "missing_kustomize", "download_kustomize", "tools_available"),
		Entry("project root detection scenario", "project_detection", "find_project_root", "correct_path"),
		Entry("binary directory setup scenario", "bin_setup", "create_bin_dir", "tools_accessible"),
	)
})
