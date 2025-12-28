package contract

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ExecutionEnvironment manages the consistent execution environment
// for contract E2E tests across local and CI environments
// Requirements 10.1, 10.2, 10.3: Ensure consistent execution environment
type ExecutionEnvironment struct {
	config      *ConsistencyConfig
	projectRoot string
	binDir      string
	initialized bool
}

// NewExecutionEnvironment creates a new execution environment manager
func NewExecutionEnvironment(config *ConsistencyConfig) *ExecutionEnvironment {
	return &ExecutionEnvironment{
		config:      config,
		initialized: false,
	}
}

// Initialize sets up the execution environment for consistent behavior
// Requirements 10.4: Ensure no manual setup required for local execution
func (e *ExecutionEnvironment) Initialize(ctx context.Context) error {
	if e.initialized {
		return nil
	}

	// Determine project root directory
	if err := e.determineProjectRoot(); err != nil {
		return fmt.Errorf("failed to determine project root: %w", err)
	}

	// Set up binary directory
	e.binDir = filepath.Join(e.projectRoot, "bin")

	// Ensure required tools are available
	if err := e.ensureRequiredTools(); err != nil {
		return fmt.Errorf("failed to ensure required tools: %w", err)
	}

	// Validate environment consistency
	if err := e.validateEnvironmentConsistency(); err != nil {
		return fmt.Errorf("environment consistency validation failed: %w", err)
	}

	e.initialized = true

	if e.config.VerboseLogging {
		fmt.Printf("Execution environment initialized successfully\n")
		fmt.Printf("Project root: %s\n", e.projectRoot)
		fmt.Printf("Binary directory: %s\n", e.binDir)
		fmt.Printf("Environment: %s\n", e.config.Environment)
	}

	return nil
}

// determineProjectRoot determines the project root directory consistently
func (e *ExecutionEnvironment) determineProjectRoot() error {
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}

	// Navigate up from test/e2e/contract to project root
	projectRoot := strings.ReplaceAll(wd, "/test/e2e/contract", "")

	// Verify this is actually the project root by checking for key files
	keyFiles := []string{"go.mod", "Makefile", "Dockerfile"}
	for _, file := range keyFiles {
		if _, err := os.Stat(filepath.Join(projectRoot, file)); os.IsNotExist(err) {
			return fmt.Errorf("project root validation failed: %s not found in %s", file, projectRoot)
		}
	}

	e.projectRoot = projectRoot
	return nil
}

// ensureRequiredTools ensures all required tools are available and consistent
// Requirements 10.4: Ensure no manual setup required for local execution
func (e *ExecutionEnvironment) ensureRequiredTools() error {
	// Check Kind availability
	if err := e.checkToolAvailability(e.config.KindBinary, "version"); err != nil {
		return fmt.Errorf("kind is not available: %w", err)
	}

	// Check kubectl availability
	if err := e.checkToolAvailability(e.config.KubectlBinary, "version", "--client"); err != nil {
		return fmt.Errorf("kubectl is not available: %w", err)
	}

	// Check Docker availability
	if err := e.checkToolAvailability(e.config.DockerBinary, "version"); err != nil {
		return fmt.Errorf("docker is not available: %w", err)
	}

	// Ensure kustomize binary is available (download if needed)
	if err := e.ensureKustomizeBinary(); err != nil {
		return fmt.Errorf("failed to ensure kustomize binary: %w", err)
	}

	return nil
}

// checkToolAvailability checks if a tool is available and working
func (e *ExecutionEnvironment) checkToolAvailability(binary string, args ...string) error {
	cmd := exec.Command(binary, args...)
	cmd.Dir = e.projectRoot

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd = exec.CommandContext(ctx, binary, args...)
	cmd.Dir = e.projectRoot

	return cmd.Run()
}

// ensureKustomizeBinary ensures kustomize binary is available
func (e *ExecutionEnvironment) ensureKustomizeBinary() error {
	kustomizePath := filepath.Join(e.binDir, "kustomize")

	// Check if kustomize binary already exists
	if _, err := os.Stat(kustomizePath); err == nil {
		// Verify it works
		cmd := exec.Command(kustomizePath, "version")
		cmd.Dir = e.projectRoot
		if cmd.Run() == nil {
			// Update config to use the local binary
			e.config.KustomizeBinary = kustomizePath
			return nil
		}
	}

	// Download kustomize using the Makefile target
	if e.config.VerboseLogging {
		fmt.Printf("Downloading kustomize binary...\n")
	}

	cmd := exec.Command("make", "kustomize")
	cmd.Dir = e.projectRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to download kustomize: %w", err)
	}

	// Update config to use the local binary
	e.config.KustomizeBinary = kustomizePath
	return nil
}

// validateEnvironmentConsistency validates that the environment is set up consistently
func (e *ExecutionEnvironment) validateEnvironmentConsistency() error {
	// Validate Docker is running
	cmd := exec.Command(e.config.DockerBinary, "info")
	cmd.Dir = e.projectRoot
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker is not running or not accessible")
	}

	// Validate Kind can create clusters
	cmd = exec.Command(e.config.KindBinary, "version")
	cmd.Dir = e.projectRoot
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kind is not working properly")
	}

	// Validate kubectl can connect (if a cluster exists)
	cmd = exec.Command(e.config.KubectlBinary, "version", "--client")
	cmd.Dir = e.projectRoot
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kubectl is not working properly")
	}

	return nil
}

// CreateConsistentCluster creates a Kind cluster with consistent configuration
// Requirements 1.2: create its own Kind cluster for complete isolation
func (e *ExecutionEnvironment) CreateConsistentCluster(ctx context.Context) error {
	if !e.initialized {
		return fmt.Errorf("execution environment not initialized")
	}

	// Delete existing cluster if it exists (ensure clean state)
	if e.config.VerboseLogging {
		fmt.Printf("Deleting existing Kind cluster '%s' if it exists\n", e.config.ClusterName)
	}

	deleteCmd := exec.Command(e.config.KindBinary, "delete", "cluster", "--name", e.config.ClusterName)
	deleteCmd.Dir = e.projectRoot
	_ = deleteCmd.Run() // Ignore errors - cluster might not exist

	// Create new Kind cluster with consistent configuration
	if e.config.VerboseLogging {
		fmt.Printf("Creating new Kind cluster '%s'\n", e.config.ClusterName)
	}

	createCmd := exec.Command(e.config.KindBinary, "create", "cluster",
		"--name", e.config.ClusterName,
		"--wait", "60s")
	createCmd.Dir = e.projectRoot

	// Set timeout for cluster creation
	createCtx, cancel := context.WithTimeout(ctx, e.config.ClusterCreateTimeout)
	defer cancel()

	createCmd = exec.CommandContext(createCtx, e.config.KindBinary, "create", "cluster",
		"--name", e.config.ClusterName,
		"--wait", "60s")
	createCmd.Dir = e.projectRoot

	if output, err := createCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cluster creation failed: %w, output: %s", err, string(output))
	}

	// Verify cluster is ready
	if e.config.VerboseLogging {
		fmt.Printf("Verifying Kind cluster is ready\n")
	}

	verifyCmd := exec.Command(e.config.KubectlBinary, "cluster-info",
		"--context", fmt.Sprintf("kind-%s", e.config.ClusterName))
	verifyCmd.Dir = e.projectRoot

	verifyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	verifyCmd = exec.CommandContext(verifyCtx, e.config.KubectlBinary, "cluster-info",
		"--context", fmt.Sprintf("kind-%s", e.config.ClusterName))
	verifyCmd.Dir = e.projectRoot

	if output, err := verifyCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cluster verification failed: %w, output: %s", err, string(output))
	}

	return nil
}

// CleanupConsistentCluster cleans up the Kind cluster
// Requirements 1.3: clean up automatically even on test failures
func (e *ExecutionEnvironment) CleanupConsistentCluster(ctx context.Context) error {
	if !e.initialized {
		return fmt.Errorf("execution environment not initialized")
	}

	if e.config.VerboseLogging {
		fmt.Printf("Cleaning up Kind cluster '%s'\n", e.config.ClusterName)
	}

	// Set timeout for cleanup
	cleanupCtx, cancel := context.WithTimeout(ctx, e.config.ClusterCleanupTimeout)
	defer cancel()

	deleteCmd := exec.CommandContext(cleanupCtx, e.config.KindBinary, "delete", "cluster", "--name", e.config.ClusterName)
	deleteCmd.Dir = e.projectRoot

	if output, err := deleteCmd.CombinedOutput(); err != nil {
		// Return error for reporting but don't fail - cleanup should be best effort
		return fmt.Errorf("cluster cleanup encountered issues: %w, output: %s", err, string(output))
	}

	if e.config.VerboseLogging {
		fmt.Printf("Successfully cleaned up Kind cluster\n")
	}

	return nil
}

// BuildAndLoadConsistentImage builds and loads the controller image consistently
func (e *ExecutionEnvironment) BuildAndLoadConsistentImage(ctx context.Context) error {
	if !e.initialized {
		return fmt.Errorf("execution environment not initialized")
	}

	// Build the controller Docker image
	if e.config.VerboseLogging {
		fmt.Printf("Building OptiPod controller Docker image '%s'\n", e.config.ImageTag)
	}

	buildCmd := exec.Command(e.config.DockerBinary, "build", "-t", e.config.ImageTag, ".")
	buildCmd.Dir = e.projectRoot

	if e.config.VerboseLogging {
		buildCmd.Stdout = os.Stdout
		buildCmd.Stderr = os.Stderr
	}

	if err := buildCmd.Run(); err != nil {
		return fmt.Errorf("docker image build failed: %w", err)
	}

	// Load the image into Kind cluster
	if e.config.VerboseLogging {
		fmt.Printf("Loading controller image into Kind cluster '%s'\n", e.config.ClusterName)
	}

	loadCmd := exec.Command(e.config.KindBinary, "load", "docker-image", e.config.ImageTag, "--name", e.config.ClusterName)
	loadCmd.Dir = e.projectRoot

	if e.config.VerboseLogging {
		loadCmd.Stdout = os.Stdout
		loadCmd.Stderr = os.Stderr
	}

	if err := loadCmd.Run(); err != nil {
		return fmt.Errorf("image loading into cluster failed: %w", err)
	}

	return nil
}

// GetKubeconfig gets the kubeconfig for the Kind cluster consistently
func (e *ExecutionEnvironment) GetKubeconfig() (string, error) {
	if !e.initialized {
		return "", fmt.Errorf("execution environment not initialized")
	}

	cmd := exec.Command(e.config.KindBinary, "get", "kubeconfig", "--name", e.config.ClusterName)
	cmd.Dir = e.projectRoot

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get kubeconfig: %w, output: %s", err, string(output))
	}

	return string(output), nil
}

// GetProjectRoot returns the project root directory
func (e *ExecutionEnvironment) GetProjectRoot() string {
	return e.projectRoot
}

// GetBinDir returns the binary directory
func (e *ExecutionEnvironment) GetBinDir() string {
	return e.binDir
}

// IsInitialized returns whether the environment is initialized
func (e *ExecutionEnvironment) IsInitialized() bool {
	return e.initialized
}

// GetConfig returns the consistency configuration
func (e *ExecutionEnvironment) GetConfig() *ConsistencyConfig {
	return e.config
}

// Global execution environment instance
var globalExecutionEnvironment *ExecutionEnvironment

// GetExecutionEnvironment returns the global execution environment
func GetExecutionEnvironment() *ExecutionEnvironment {
	if globalExecutionEnvironment == nil {
		config := GetConsistencyConfig()
		globalExecutionEnvironment = NewExecutionEnvironment(config)
	}
	return globalExecutionEnvironment
}

// SetExecutionEnvironment sets the global execution environment (for testing)
func SetExecutionEnvironment(env *ExecutionEnvironment) {
	globalExecutionEnvironment = env
}
