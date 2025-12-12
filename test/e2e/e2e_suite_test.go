//go:build e2e

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

package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/optipod/optipod/test/utils"
)

var (
	// Optional Environment Variables:
	// - CERT_MANAGER_INSTALL_SKIP=true: Skips CertManager installation during test setup.
	// - METRICS_SERVER_INSTALL_SKIP=true: Skips MetricsServer installation during test setup.
	// - E2E_PARALLEL_NODES: Number of parallel nodes for test execution (default: 4)
	// - E2E_TIMEOUT_MULTIPLIER: Multiplier for test timeouts in parallel execution (default: 1.0)
	// These variables are useful if CertManager/MetricsServer is already installed, avoiding
	// re-installation and conflicts.
	skipCertManagerInstall   = os.Getenv("CERT_MANAGER_INSTALL_SKIP") == "true"
	skipMetricsServerInstall = os.Getenv("METRICS_SERVER_INSTALL_SKIP") == "true"
	// isCertManagerAlreadyInstalled will be set true when CertManager CRDs be found on the cluster
	isCertManagerAlreadyInstalled = false
	// isMetricsServerAlreadyInstalled will be set true when MetricsServer is found on the cluster
	isMetricsServerAlreadyInstalled = false

	// projectImage is the name of the image which will be build and loaded
	// with the code source changes to be tested.
	projectImage = "example.com/optipod:v0.0.1"

	// k8sClient is the Kubernetes client for test operations
	k8sClient client.Client
)

// TestE2E runs the end-to-end (e2e) test suite for the project. These tests execute in an isolated,
// temporary environment to validate project changes with the purpose of being used in CI jobs.
// The default setup requires Kind, builds/loads the Manager Docker image locally, and installs
// CertManager.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting optipod integration test suite\n")
	RunSpecs(t, "e2e suite")
}

var _ = BeforeSuite(func() {
	By("initializing Kubernetes client")
	var err error
	k8sClient, err = utils.GetK8sClient()
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to initialize Kubernetes client")

	By("initializing parallel test execution")
	InitializeParallelExecution(k8sClient)

	By("initializing performance configuration")
	InitializePerformanceConfig()

	// Only build and load image once for the entire suite (not per parallel node)
	if GinkgoParallelProcess() == 1 {
		By("building the manager(Operator) image")
		cmd := exec.Command("make", "docker-build", fmt.Sprintf("IMG=%s", projectImage))
		_, err := utils.Run(cmd)
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to build the manager(Operator) image")

		// TODO(user): If you want to change the e2e test vendor from Kind, ensure the image is
		// built and available before running the tests. Also, remove the following block.
		By("loading the manager(Operator) image on Kind")
		err = utils.LoadImageToKindClusterWithName(projectImage)
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to load the manager(Operator) image into Kind")

		// The tests-e2e are intended to run on a temporary cluster that is created and destroyed for testing.
		// To prevent errors when tests run in environments with CertManager already installed,
		// we check for its presence before execution.
		// Setup CertManager before the suite if not skipped and if not already installed
		if !skipCertManagerInstall {
			By("checking if cert manager is installed already")
			isCertManagerAlreadyInstalled = utils.IsCertManagerCRDsInstalled()
			if !isCertManagerAlreadyInstalled {
				_, _ = fmt.Fprintf(GinkgoWriter, "Installing CertManager...\n")
				Expect(utils.InstallCertManager()).To(Succeed(), "Failed to install CertManager")
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "WARNING: CertManager is already installed. Skipping installation...\n")
			}
		}

		// Setup MetricsServer before the suite if not skipped and if not already installed
		if !skipMetricsServerInstall {
			By("checking if metrics-server is installed already")
			isMetricsServerAlreadyInstalled = utils.IsMetricsServerInstalled()
			if !isMetricsServerAlreadyInstalled {
				_, _ = fmt.Fprintf(GinkgoWriter, "Installing MetricsServer...\n")
				Expect(utils.InstallMetricsServer()).To(Succeed(), "Failed to install MetricsServer")
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "WARNING: MetricsServer is already installed. Skipping installation...\n")
			}
		}
	} else {
		// For parallel nodes, wait for the first node to complete setup
		By("waiting for suite setup to complete")
		time.Sleep(30 * time.Second) // Give first node time to complete setup
	}

	By("waiting for parallel test stability")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	err = WaitForTestStability(ctx)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to achieve test stability")
})

var _ = AfterSuite(func() {
	By("cleaning up parallel test resources")
	if parallelTestManager != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		err := parallelTestManager.CleanupIsolatedNamespace(ctx)
		if err != nil {
			_, _ = fmt.Fprintf(GinkgoWriter, "WARNING: Failed to cleanup isolated namespace: %v\n", err)
		}
	}

	// Only cleanup shared resources from the first parallel node
	if GinkgoParallelProcess() == 1 {
		// Teardown MetricsServer after the suite if not skipped and if it was not already installed
		if !skipMetricsServerInstall && !isMetricsServerAlreadyInstalled {
			_, _ = fmt.Fprintf(GinkgoWriter, "Uninstalling MetricsServer...\n")
			utils.UninstallMetricsServer()
		}

		// Teardown CertManager after the suite if not skipped and if it was not already installed
		if !skipCertManagerInstall && !isCertManagerAlreadyInstalled {
			_, _ = fmt.Fprintf(GinkgoWriter, "Uninstalling CertManager...\n")
			utils.UninstallCertManager()
		}
	}
})
