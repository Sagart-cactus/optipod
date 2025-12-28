package contract

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Build System Behavior Validation", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
	})

	AfterEach(func() {
		if cancel != nil {
			cancel()
		}
	})

	Context("Contract E2E Build Target Validation", func() {
		It("should validate that test-e2e-contract target runs only contract tests", func() {
			By("Checking that test-e2e-contract target exists in Makefile")
			makefilePath := filepath.Join(getProjectRoot(), "Makefile")
			makefileContent, err := os.ReadFile(makefilePath)
			Expect(err).NotTo(HaveOccurred(), "Should be able to read Makefile")

			makefileStr := string(makefileContent)
			Expect(makefileStr).To(ContainSubstring("test-e2e-contract:"), "Makefile should contain test-e2e-contract target")

			By("Verifying test-e2e-contract target runs only contract tests")
			// Look for the complete target definition including the command
			Expect(makefileStr).To(ContainSubstring("go test ./test/e2e/contract"), "test-e2e-contract should run contract tests specifically")

			By("Verifying test-e2e-contract has proper timeout")
			Expect(makefileStr).To(ContainSubstring("-timeout=10m"), "test-e2e-contract should have 10-minute timeout")
		})

		It("should validate that e2e-cluster target creates fresh clusters", func() {
			By("Checking that e2e-cluster target exists in Makefile")
			makefilePath := filepath.Join(getProjectRoot(), "Makefile")
			makefileContent, err := os.ReadFile(makefilePath)
			Expect(err).NotTo(HaveOccurred(), "Should be able to read Makefile")

			makefileStr := string(makefileContent)
			Expect(makefileStr).To(ContainSubstring("e2e-cluster:"), "Makefile should contain e2e-cluster target")

			By("Verifying e2e-cluster target deletes existing cluster")
			// Look for the pattern that deletes existing cluster
			Expect(makefileStr).To(ContainSubstring("kind delete cluster"), "e2e-cluster should delete existing cluster")

			By("Verifying e2e-cluster target creates new cluster")
			Expect(makefileStr).To(ContainSubstring("kind create cluster"), "e2e-cluster should create new cluster")
		})

		It("should validate that contract target has no code generation dependencies", func() {
			By("Checking test-e2e-contract target dependencies")
			makefilePath := filepath.Join(getProjectRoot(), "Makefile")
			makefileContent, err := os.ReadFile(makefilePath)
			Expect(err).NotTo(HaveOccurred(), "Should be able to read Makefile")

			makefileStr := string(makefileContent)

			// Find the test-e2e-contract target line
			lines := strings.Split(makefileStr, "\n")
			var targetLine string
			for _, line := range lines {
				if strings.Contains(line, "test-e2e-contract:") {
					targetLine = line
					break
				}
			}
			Expect(targetLine).NotTo(BeEmpty(), "test-e2e-contract target should be found")

			By("Verifying no manifests dependency")
			Expect(targetLine).NotTo(ContainSubstring("manifests"), "test-e2e-contract should not depend on manifests")

			By("Verifying no generate dependency")
			Expect(targetLine).NotTo(ContainSubstring("generate"), "test-e2e-contract should not depend on generate")

			By("Verifying no fmt dependency")
			Expect(targetLine).NotTo(ContainSubstring("fmt"), "test-e2e-contract should not depend on fmt")

			By("Verifying no vet dependency")
			Expect(targetLine).NotTo(ContainSubstring("vet"), "test-e2e-contract should not depend on vet")

			By("Verifying no e2e-cluster dependency (cluster creation handled by BeforeSuite)")
			Expect(targetLine).NotTo(ContainSubstring("e2e-cluster"), "test-e2e-contract should not depend on e2e-cluster as cluster creation is handled by BeforeSuite")
		})

		It("should validate that existing comprehensive test targets are preserved", func() {
			By("Checking that test-e2e target still exists")
			makefilePath := filepath.Join(getProjectRoot(), "Makefile")
			makefileContent, err := os.ReadFile(makefilePath)
			Expect(err).NotTo(HaveOccurred(), "Should be able to read Makefile")

			makefileStr := string(makefileContent)
			Expect(makefileStr).To(ContainSubstring("test-e2e:"), "Makefile should preserve existing test-e2e target")

			By("Verifying test-e2e target still has comprehensive dependencies")
			// Find the test-e2e target line
			lines := strings.Split(makefileStr, "\n")
			var targetLine string
			for _, line := range lines {
				if strings.Contains(line, "test-e2e:") && !strings.Contains(line, "test-e2e-contract") {
					targetLine = line
					break
				}
			}
			Expect(targetLine).NotTo(BeEmpty(), "test-e2e target should be found")

			// Verify comprehensive test target still has its dependencies
			Expect(targetLine).To(ContainSubstring("manifests"), "test-e2e should still depend on manifests")
			Expect(targetLine).To(ContainSubstring("generate"), "test-e2e should still depend on generate")
			Expect(targetLine).To(ContainSubstring("fmt"), "test-e2e should still depend on fmt")
			Expect(targetLine).To(ContainSubstring("vet"), "test-e2e should still depend on vet")
		})
	})

	Context("Build Target Execution Validation", func() {
		It("should validate that e2e-cluster target can be executed", func() {
			By("Attempting to run make e2e-cluster --dry-run")
			cmd := exec.CommandContext(ctx, "make", "e2e-cluster", "--dry-run")
			cmd.Dir = getProjectRoot()

			output, err := cmd.CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("make e2e-cluster --dry-run should succeed, output: %s", string(output)))

			By("Verifying dry-run output contains expected commands")
			outputStr := string(output)
			Expect(outputStr).To(ContainSubstring("kind delete cluster"), "Dry run should show cluster deletion")
			Expect(outputStr).To(ContainSubstring("kind create cluster"), "Dry run should show cluster creation")
		})

		It("should validate that test-e2e-contract target can be executed in dry-run mode", func() {
			By("Attempting to run make test-e2e-contract --dry-run")
			cmd := exec.CommandContext(ctx, "make", "test-e2e-contract", "--dry-run")
			cmd.Dir = getProjectRoot()

			output, err := cmd.CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("make test-e2e-contract --dry-run should succeed, output: %s", string(output)))

			By("Verifying dry-run output contains expected test command")
			outputStr := string(output)
			Expect(outputStr).To(ContainSubstring("go test ./test/e2e/contract"), "Dry run should show contract test execution")
			Expect(outputStr).To(ContainSubstring("-timeout=10m"), "Dry run should show 10-minute timeout")
		})
	})

	Context("Makefile Target Consistency Validation", func() {
		It("should validate that contract targets have proper help documentation", func() {
			By("Checking that test-e2e-contract has help documentation")
			makefilePath := filepath.Join(getProjectRoot(), "Makefile")
			makefileContent, err := os.ReadFile(makefilePath)
			Expect(err).NotTo(HaveOccurred(), "Should be able to read Makefile")

			makefileStr := string(makefileContent)

			// Look for help documentation pattern
			Expect(makefileStr).To(ContainSubstring("test-e2e-contract:"), "test-e2e-contract target should exist")

			By("Checking that e2e-cluster has help documentation")
			Expect(makefileStr).To(ContainSubstring("e2e-cluster:"), "e2e-cluster target should exist")
		})

		It("should validate that contract targets are in the E2E Tests section", func() {
			By("Checking Makefile organization")
			makefilePath := filepath.Join(getProjectRoot(), "Makefile")
			makefileContent, err := os.ReadFile(makefilePath)
			Expect(err).NotTo(HaveOccurred(), "Should be able to read Makefile")

			makefileStr := string(makefileContent)

			// Find the E2E Tests section
			Expect(makefileStr).To(ContainSubstring("##@ E2E Tests"), "Makefile should have E2E Tests section")

			// Verify contract targets are in the right section
			lines := strings.Split(makefileStr, "\n")
			var inE2ESection bool
			var foundContractTarget bool
			var foundClusterTarget bool

			for _, line := range lines {
				if strings.Contains(line, "##@ E2E Tests") {
					inE2ESection = true
					continue
				}
				if strings.HasPrefix(line, "##@") && !strings.Contains(line, "E2E Tests") {
					inE2ESection = false
					continue
				}
				if inE2ESection {
					if strings.Contains(line, "test-e2e-contract:") {
						foundContractTarget = true
					}
					if strings.Contains(line, "e2e-cluster:") {
						foundClusterTarget = true
					}
				}
			}

			Expect(foundContractTarget).To(BeTrue(), "test-e2e-contract should be in E2E Tests section")
			Expect(foundClusterTarget).To(BeTrue(), "e2e-cluster should be in E2E Tests section")
		})
	})
})

// getProjectRoot returns the project root directory
func getProjectRoot() string {
	// Start from current directory and walk up to find go.mod
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			break
		}
		dir = parent
	}

	return ""
}
