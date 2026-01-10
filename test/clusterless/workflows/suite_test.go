package workflows

import (
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestClusterlessWorkflows(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Cluster-Free E2E Workflows Suite")
}

var _ = BeforeSuite(func() {
	// Global setup for the test suite
	// This runs once before all tests

	// Set default timeouts for Eventually/Consistently
	SetDefaultEventuallyTimeout(5 * time.Second)
	SetDefaultEventuallyPollingInterval(100 * time.Millisecond)
	SetDefaultConsistentlyDuration(1 * time.Second)
	SetDefaultConsistentlyPollingInterval(50 * time.Millisecond)
})

var _ = AfterSuite(func() {
	// Global cleanup for the test suite
	// This runs once after all tests
})
