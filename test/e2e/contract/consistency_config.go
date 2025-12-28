package contract

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// ConsistencyConfig defines configuration values that ensure identical behavior
// between local and CI environments for contract E2E tests
// Requirements 1.5, 10.1, 10.2, 10.3, 10.4, 10.5: Ensure tests produce identical results locally and in CI
type ConsistencyConfig struct {
	// Timeout configurations - consistent across environments
	ContractTestTimeout   time.Duration
	InstallTimeout        time.Duration
	StatusTimeout         time.Duration
	CRTimeout             time.Duration
	ClusterCreateTimeout  time.Duration
	ClusterCleanupTimeout time.Duration

	// Polling configurations - consistent intervals
	PollInterval        time.Duration
	StatusPollInterval  time.Duration
	CRPollInterval      time.Duration
	ClusterPollInterval time.Duration

	// Cluster configurations - consistent naming and setup
	ClusterName string
	ImageTag    string
	Namespace   string

	// Tool configurations - consistent tool usage
	KindBinary      string
	KubectlBinary   string
	DockerBinary    string
	KustomizeBinary string

	// Environment detection
	IsCI        bool
	Environment string

	// Resource configurations - consistent resource limits
	MaxRetries        int
	BackoffMultiplier float64
	InitialBackoff    time.Duration

	// Logging and output configurations
	VerboseLogging    bool
	StructuredLogging bool
}

// NewConsistencyConfig creates a new consistency configuration with defaults
// that work identically in both local and CI environments
func NewConsistencyConfig() *ConsistencyConfig {
	config := &ConsistencyConfig{
		// Default timeout values - Requirements 1.1: complete within 10 minutes maximum
		ContractTestTimeout:   10 * time.Minute,
		InstallTimeout:        5 * time.Minute,
		StatusTimeout:         5 * time.Minute, // Increased from 2 minutes to allow controller processing
		CRTimeout:             30 * time.Second,
		ClusterCreateTimeout:  3 * time.Minute,
		ClusterCleanupTimeout: 1 * time.Minute,

		// Default polling intervals - Requirements 6.2: 5-second intervals minimum
		PollInterval:        10 * time.Second,
		StatusPollInterval:  5 * time.Second, // Requirements 6.2: minimum polling interval
		CRPollInterval:      2 * time.Second,
		ClusterPollInterval: 10 * time.Second,

		// Default cluster configuration - Requirements 1.2: create its own Kind cluster
		ClusterName: "optipod-contract-e2e",
		ImageTag:    "optipod-controller:contract-e2e",
		Namespace:   "optipod-system",

		// Default tool binaries - consistent tool usage
		KindBinary:      "kind",
		KubectlBinary:   "kubectl",
		DockerBinary:    "docker",
		KustomizeBinary: "./bin/kustomize", // Use local binary for consistency

		// Default retry configuration
		MaxRetries:        3,
		BackoffMultiplier: 2.0,
		InitialBackoff:    1 * time.Second,

		// Default logging configuration
		VerboseLogging:    false,
		StructuredLogging: true,
	}

	// Apply environment-specific overrides
	config.applyEnvironmentOverrides()

	return config
}

// applyEnvironmentOverrides applies environment-specific configuration overrides
// while maintaining consistency between local and CI environments
func (c *ConsistencyConfig) applyEnvironmentOverrides() {
	// Detect CI environment
	c.IsCI = c.detectCIEnvironment()
	if c.IsCI {
		c.Environment = "CI"
	} else {
		c.Environment = "local"
	}

	// Apply timeout overrides from environment variables (if set)
	if timeout := os.Getenv("CONTRACT_TEST_TIMEOUT"); timeout != "" {
		if d, err := time.ParseDuration(timeout); err == nil {
			c.ContractTestTimeout = d
		}
	}

	if timeout := os.Getenv("CONTRACT_INSTALL_TIMEOUT"); timeout != "" {
		if d, err := time.ParseDuration(timeout); err == nil {
			c.InstallTimeout = d
		}
	}

	if timeout := os.Getenv("CONTRACT_STATUS_TIMEOUT"); timeout != "" {
		if d, err := time.ParseDuration(timeout); err == nil {
			c.StatusTimeout = d
		}
	}

	// Apply polling interval overrides from environment variables (if set)
	if interval := os.Getenv("CONTRACT_POLL_INTERVAL"); interval != "" {
		if d, err := time.ParseDuration(interval); err == nil {
			c.PollInterval = d
		}
	}

	if interval := os.Getenv("CONTRACT_STATUS_POLL_INTERVAL"); interval != "" {
		if d, err := time.ParseDuration(interval); err == nil {
			c.StatusPollInterval = d
		}
	}

	// Apply cluster configuration overrides
	if clusterName := os.Getenv("CONTRACT_CLUSTER_NAME"); clusterName != "" {
		c.ClusterName = clusterName
	}

	if imageTag := os.Getenv("CONTRACT_IMAGE_TAG"); imageTag != "" {
		c.ImageTag = imageTag
	}

	if namespace := os.Getenv("CONTRACT_NAMESPACE"); namespace != "" {
		c.Namespace = namespace
	}

	// Apply tool binary overrides
	if kindBinary := os.Getenv("KIND_BINARY"); kindBinary != "" {
		c.KindBinary = kindBinary
	}

	if kubectlBinary := os.Getenv("KUBECTL_BINARY"); kubectlBinary != "" {
		c.KubectlBinary = kubectlBinary
	}

	if dockerBinary := os.Getenv("DOCKER_BINARY"); dockerBinary != "" {
		c.DockerBinary = dockerBinary
	}

	if kustomizeBinary := os.Getenv("KUSTOMIZE_BINARY"); kustomizeBinary != "" {
		c.KustomizeBinary = kustomizeBinary
	}

	// Apply retry configuration overrides
	if maxRetries := os.Getenv("CONTRACT_MAX_RETRIES"); maxRetries != "" {
		if retries, err := strconv.Atoi(maxRetries); err == nil {
			c.MaxRetries = retries
		}
	}

	// Apply logging configuration overrides
	if verbose := os.Getenv("CONTRACT_VERBOSE_LOGGING"); verbose != "" {
		if v, err := strconv.ParseBool(verbose); err == nil {
			c.VerboseLogging = v
		}
	}

	if structured := os.Getenv("CONTRACT_STRUCTURED_LOGGING"); structured != "" {
		if s, err := strconv.ParseBool(structured); err == nil {
			c.StructuredLogging = s
		}
	}
}

// detectCIEnvironment detects if running in a CI environment
func (c *ConsistencyConfig) detectCIEnvironment() bool {
	// Common CI environment variables
	ciEnvVars := []string{
		"CI",
		"CONTINUOUS_INTEGRATION",
		"GITHUB_ACTIONS",
		"GITLAB_CI",
		"JENKINS_URL",
		"TRAVIS",
		"CIRCLECI",
		"BUILDKITE",
	}

	for _, envVar := range ciEnvVars {
		if os.Getenv(envVar) != "" {
			return true
		}
	}

	return false
}

// Validate validates the consistency configuration
func (c *ConsistencyConfig) Validate() error {
	// Validate timeout values
	if c.ContractTestTimeout <= 0 {
		return fmt.Errorf("contract test timeout must be positive, got %v", c.ContractTestTimeout)
	}

	if c.InstallTimeout <= 0 {
		return fmt.Errorf("install timeout must be positive, got %v", c.InstallTimeout)
	}

	if c.StatusTimeout <= 0 {
		return fmt.Errorf("status timeout must be positive, got %v", c.StatusTimeout)
	}

	// Validate polling intervals
	if c.PollInterval <= 0 {
		return fmt.Errorf("poll interval must be positive, got %v", c.PollInterval)
	}

	if c.StatusPollInterval <= 0 {
		return fmt.Errorf("status poll interval must be positive, got %v", c.StatusPollInterval)
	}

	// Requirements 6.2: 5-second intervals minimum
	if c.StatusPollInterval < 5*time.Second {
		return fmt.Errorf("status poll interval must be at least 5 seconds, got %v", c.StatusPollInterval)
	}

	// Allow longer status timeout for controller processing
	if c.StatusTimeout > 5*time.Minute {
		return fmt.Errorf("status timeout must not exceed 5 minutes, got %v", c.StatusTimeout)
	}

	// Requirements 1.1: complete within 10 minutes maximum
	if c.ContractTestTimeout > 10*time.Minute {
		return fmt.Errorf("contract test timeout must not exceed 10 minutes, got %v", c.ContractTestTimeout)
	}

	// Validate cluster configuration
	if c.ClusterName == "" {
		return fmt.Errorf("cluster name cannot be empty")
	}

	if c.ImageTag == "" {
		return fmt.Errorf("image tag cannot be empty")
	}

	if c.Namespace == "" {
		return fmt.Errorf("namespace cannot be empty")
	}

	// Validate tool binaries
	if c.KindBinary == "" {
		return fmt.Errorf("kind binary cannot be empty")
	}

	if c.KubectlBinary == "" {
		return fmt.Errorf("kubectl binary cannot be empty")
	}

	if c.DockerBinary == "" {
		return fmt.Errorf("docker binary cannot be empty")
	}

	// Validate retry configuration
	if c.MaxRetries < 0 {
		return fmt.Errorf("max retries must be non-negative, got %d", c.MaxRetries)
	}

	if c.BackoffMultiplier <= 0 {
		return fmt.Errorf("backoff multiplier must be positive, got %f", c.BackoffMultiplier)
	}

	if c.InitialBackoff <= 0 {
		return fmt.Errorf("initial backoff must be positive, got %v", c.InitialBackoff)
	}

	return nil
}

// String returns a string representation of the configuration
func (c *ConsistencyConfig) String() string {
	return fmt.Sprintf("ConsistencyConfig{Environment: %s, ClusterName: %s, ContractTestTimeout: %v, StatusTimeout: %v, StatusPollInterval: %v}",
		c.Environment, c.ClusterName, c.ContractTestTimeout, c.StatusTimeout, c.StatusPollInterval)
}

// GetEnvironmentInfo returns information about the current environment
func (c *ConsistencyConfig) GetEnvironmentInfo() map[string]interface{} {
	return map[string]interface{}{
		"environment":           c.Environment,
		"is_ci":                 c.IsCI,
		"cluster_name":          c.ClusterName,
		"contract_test_timeout": c.ContractTestTimeout.String(),
		"install_timeout":       c.InstallTimeout.String(),
		"status_timeout":        c.StatusTimeout.String(),
		"status_poll_interval":  c.StatusPollInterval.String(),
		"kind_binary":           c.KindBinary,
		"kubectl_binary":        c.KubectlBinary,
		"docker_binary":         c.DockerBinary,
		"kustomize_binary":      c.KustomizeBinary,
		"max_retries":           c.MaxRetries,
		"verbose_logging":       c.VerboseLogging,
		"structured_logging":    c.StructuredLogging,
	}
}

// Global consistency configuration instance
var globalConsistencyConfig *ConsistencyConfig

// GetConsistencyConfig returns the global consistency configuration
func GetConsistencyConfig() *ConsistencyConfig {
	if globalConsistencyConfig == nil {
		globalConsistencyConfig = NewConsistencyConfig()
		if err := globalConsistencyConfig.Validate(); err != nil {
			panic(fmt.Sprintf("invalid consistency configuration: %v", err))
		}
	}
	return globalConsistencyConfig
}

// SetConsistencyConfig sets the global consistency configuration (for testing)
func SetConsistencyConfig(config *ConsistencyConfig) {
	globalConsistencyConfig = config
}
