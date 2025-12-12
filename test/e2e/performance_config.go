//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// PerformanceConfig holds configuration for test performance optimization
type PerformanceConfig struct {
	// DefaultTimeout is the default timeout for test operations
	DefaultTimeout time.Duration
	// ShortTimeout is used for quick operations
	ShortTimeout time.Duration
	// LongTimeout is used for complex operations
	LongTimeout time.Duration
	// RetryInterval is the interval between retries
	RetryInterval time.Duration
	// MaxRetries is the maximum number of retries for flaky operations
	MaxRetries int
	// EnableRetryOnFailure enables automatic retry for flaky tests
	EnableRetryOnFailure bool
	// PerformanceMonitoring enables performance monitoring during tests
	PerformanceMonitoring bool
}

// DefaultPerformanceConfig returns the default performance configuration
func DefaultPerformanceConfig() *PerformanceConfig {
	config := &PerformanceConfig{
		DefaultTimeout:        2 * time.Minute,
		ShortTimeout:          30 * time.Second,
		LongTimeout:           5 * time.Minute,
		RetryInterval:         5 * time.Second,
		MaxRetries:            3,
		EnableRetryOnFailure:  true,
		PerformanceMonitoring: false,
	}

	// Apply environment variable overrides
	if envTimeout := os.Getenv("E2E_DEFAULT_TIMEOUT"); envTimeout != "" {
		if duration, err := time.ParseDuration(envTimeout); err == nil {
			config.DefaultTimeout = duration
		}
	}

	if envRetries := os.Getenv("E2E_MAX_RETRIES"); envRetries != "" {
		if retries, err := strconv.Atoi(envRetries); err == nil && retries >= 0 {
			config.MaxRetries = retries
		}
	}

	if os.Getenv("E2E_DISABLE_RETRY") == "true" {
		config.EnableRetryOnFailure = false
		config.MaxRetries = 0
	}

	if os.Getenv("E2E_PERFORMANCE_MONITORING") == "true" {
		config.PerformanceMonitoring = true
	}

	// Adjust timeouts for parallel execution
	if parallelTestManager != nil && parallelTestManager.IsParallelExecution() {
		multiplier := parallelTestManager.config.TimeoutMultiplier
		config.DefaultTimeout = time.Duration(float64(config.DefaultTimeout) * multiplier)
		config.ShortTimeout = time.Duration(float64(config.ShortTimeout) * multiplier)
		config.LongTimeout = time.Duration(float64(config.LongTimeout) * multiplier)
	}

	return config
}

// TestTimeoutManager manages test timeouts and performance optimization
type TestTimeoutManager struct {
	config *PerformanceConfig
}

// NewTestTimeoutManager creates a new test timeout manager
func NewTestTimeoutManager(config *PerformanceConfig) *TestTimeoutManager {
	if config == nil {
		config = DefaultPerformanceConfig()
	}

	return &TestTimeoutManager{
		config: config,
	}
}

// GetTimeout returns the appropriate timeout for the given operation type
func (ttm *TestTimeoutManager) GetTimeout(operationType string) time.Duration {
	switch operationType {
	case "short", "quick", "immediate":
		return ttm.config.ShortTimeout
	case "long", "complex", "extended":
		return ttm.config.LongTimeout
	default:
		return ttm.config.DefaultTimeout
	}
}

// WithTimeout creates a context with the appropriate timeout
func (ttm *TestTimeoutManager) WithTimeout(ctx context.Context, operationType string) (context.Context, context.CancelFunc) {
	timeout := ttm.GetTimeout(operationType)
	return context.WithTimeout(ctx, timeout)
}

// RetryWithBackoff executes a function with retry logic and exponential backoff
func (ttm *TestTimeoutManager) RetryWithBackoff(ctx context.Context, operation func() error, operationType string) error {
	if !ttm.config.EnableRetryOnFailure || ttm.config.MaxRetries == 0 {
		return operation()
	}

	var lastErr error
	backoffInterval := ttm.config.RetryInterval

	for attempt := 0; attempt <= ttm.config.MaxRetries; attempt++ {
		if attempt > 0 {
			_, _ = fmt.Fprintf(GinkgoWriter, "Retrying operation (attempt %d/%d) after %v\n",
				attempt, ttm.config.MaxRetries, backoffInterval)

			select {
			case <-time.After(backoffInterval):
				// Continue with retry
			case <-ctx.Done():
				return fmt.Errorf("context cancelled during retry: %w", ctx.Err())
			}

			// Exponential backoff with jitter
			backoffInterval = time.Duration(float64(backoffInterval) * 1.5)
			if backoffInterval > 30*time.Second {
				backoffInterval = 30 * time.Second
			}
		}

		err := operation()
		if err == nil {
			if attempt > 0 {
				_, _ = fmt.Fprintf(GinkgoWriter, "Operation succeeded after %d retries\n", attempt)
			}
			return nil
		}

		lastErr = err

		// Don't retry on context cancellation
		if ctx.Err() != nil {
			return fmt.Errorf("context cancelled: %w", err)
		}
	}

	return fmt.Errorf("operation failed after %d retries: %w", ttm.config.MaxRetries, lastErr)
}

// EventuallyWithTimeout is a wrapper around Gomega's Eventually with proper timeout handling
func (ttm *TestTimeoutManager) EventuallyWithTimeout(actual interface{}, operationType string) AsyncAssertion {
	timeout := ttm.GetTimeout(operationType)
	return Eventually(actual, timeout, ttm.config.RetryInterval)
}

// ConsistentlyWithTimeout is a wrapper around Gomega's Consistently with proper timeout handling
func (ttm *TestTimeoutManager) ConsistentlyWithTimeout(actual interface{}, operationType string) AsyncAssertion {
	timeout := ttm.GetTimeout(operationType)
	// For consistency checks, use a shorter duration
	consistencyDuration := timeout / 3
	if consistencyDuration < 10*time.Second {
		consistencyDuration = 10 * time.Second
	}
	return Consistently(actual, consistencyDuration, ttm.config.RetryInterval)
}

// MonitorPerformance monitors the performance of a test operation
func (ttm *TestTimeoutManager) MonitorPerformance(operationName string, operation func() error) error {
	if !ttm.config.PerformanceMonitoring {
		return operation()
	}

	startTime := time.Now()
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting performance monitoring for: %s\n", operationName)

	err := operation()

	duration := time.Since(startTime)
	_, _ = fmt.Fprintf(GinkgoWriter, "Performance monitoring - %s completed in %v\n", operationName, duration)

	// Log performance warnings
	expectedTimeout := ttm.GetTimeout("default")
	if duration > expectedTimeout/2 {
		_, _ = fmt.Fprintf(GinkgoWriter, "WARNING: Operation %s took %v (>50%% of timeout %v)\n",
			operationName, duration, expectedTimeout)
	}

	return err
}

// Global timeout manager instance
var testTimeoutManager *TestTimeoutManager

// InitializePerformanceConfig initializes the global performance configuration
func InitializePerformanceConfig() {
	config := DefaultPerformanceConfig()
	testTimeoutManager = NewTestTimeoutManager(config)

	_, _ = fmt.Fprintf(GinkgoWriter, "Performance config initialized - Default timeout: %v, Max retries: %d\n",
		config.DefaultTimeout, config.MaxRetries)
}

// GetTestTimeoutManager returns the global timeout manager
func GetTestTimeoutManager() *TestTimeoutManager {
	if testTimeoutManager == nil {
		InitializePerformanceConfig()
	}
	return testTimeoutManager
}

// Helper functions for common timeout operations

// WithTestTimeout creates a context with test-appropriate timeout
func WithTestTimeout(ctx context.Context, operationType string) (context.Context, context.CancelFunc) {
	return GetTestTimeoutManager().WithTimeout(ctx, operationType)
}

// RetryOperation executes an operation with retry logic
func RetryOperation(ctx context.Context, operation func() error, operationType string) error {
	return GetTestTimeoutManager().RetryWithBackoff(ctx, operation, operationType)
}

// EventuallyWithTestTimeout is a convenience wrapper for Eventually with test timeouts
func EventuallyWithTestTimeout(actual interface{}, operationType string) AsyncAssertion {
	return GetTestTimeoutManager().EventuallyWithTimeout(actual, operationType)
}

// ConsistentlyWithTestTimeout is a convenience wrapper for Consistently with test timeouts
func ConsistentlyWithTestTimeout(actual interface{}, operationType string) AsyncAssertion {
	return GetTestTimeoutManager().ConsistentlyWithTimeout(actual, operationType)
}

// MonitorTestPerformance monitors the performance of a test operation
func MonitorTestPerformance(operationName string, operation func() error) error {
	return GetTestTimeoutManager().MonitorPerformance(operationName, operation)
}

// FlakeRetry is a decorator for flaky test operations
func FlakeRetry(description string, operation func()) {
	ttm := GetTestTimeoutManager()
	if !ttm.config.EnableRetryOnFailure {
		operation()
		return
	}

	var lastPanic interface{}
	for attempt := 0; attempt <= ttm.config.MaxRetries; attempt++ {
		func() {
			defer func() {
				if r := recover(); r != nil {
					lastPanic = r
					if attempt < ttm.config.MaxRetries {
						_, _ = fmt.Fprintf(GinkgoWriter, "Test failed with panic (attempt %d/%d): %v\n",
							attempt+1, ttm.config.MaxRetries+1, r)
						time.Sleep(ttm.config.RetryInterval)
					}
				}
			}()

			operation()
			lastPanic = nil // Success, clear any previous panic
		}()

		if lastPanic == nil {
			if attempt > 0 {
				_, _ = fmt.Fprintf(GinkgoWriter, "Test '%s' succeeded after %d retries\n", description, attempt)
			}
			return
		}
	}

	// If we get here, all attempts failed
	panic(fmt.Sprintf("Test '%s' failed after %d retries. Last error: %v",
		description, ttm.config.MaxRetries, lastPanic))
}
