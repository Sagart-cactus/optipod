package suite

import (
	"context"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/optipod/optipod/test/e2e/framework"
)

// The deliberate-failure spec exists to exercise the suite's failure path on
// demand: it fails a knowingly false assertion after seeding representative
// cluster state, which makes the suite collect the redacted diagnostic bundle
// BEFORE the owned cluster is torn down and exit nonzero with parseable JUnit.
// It is opt-in so an ordinary or label-filtered run never fails because of it.
var _ = Describe("Deliberate failure diagnostics validation", Label("deliberate-failure"), func() {
	BeforeEach(func() {
		if os.Getenv("OPTIPOD_E2E_DELIBERATE_FAILURE") != "1" {
			Skip("opt-in spec: set OPTIPOD_E2E_DELIBERATE_FAILURE=1 and select it with -ginkgo.label-filter=deliberate-failure")
		}
	})

	It("fails a deliberate assertion so the suite captures pre-teardown redacted diagnostics", func(ctx context.Context) {
		ns := specNamespace(ctx, "deliberate-failure")
		// No namespace cleanup is registered here on purpose: this spec fails
		// deliberately, and its namespace must still exist when AfterSuite
		// collects the failure diagnostics; the owned cluster teardown removes
		// it with everything else.
		workload := seedDiagnosticsCanary(ctx, ns.Name)

		Expect(framework.WaitForDeploymentAvailable(ctx, env.Clients, ns.Name, workload.Name,
			framework.WaitOptions{Timeout: 3 * time.Minute})).To(Succeed(),
			"seeding the deliberate-failure state failed before the deliberate assertion could run")

		By("failing a deliberate assertion (this is not a product defect)")
		Expect("deliberate-failure").To(Equal("deliberate failure value"),
			"deliberate diagnostics-validation failure: this assertion fails on purpose so the suite "+
				"exercises pre-teardown redacted diagnostic collection; inspect "+
				artifacts.DiagnosticsDir()+" before the owned cluster is removed")
	})
})
