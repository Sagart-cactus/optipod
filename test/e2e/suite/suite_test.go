// Package suite is the new hermetic installed-controller Kind suite. It is the
// ONLY package the new Make entry points run; the legacy suites under
// test/e2e and test/e2e/contract are never invoked through it.
//
// The suite owns one collision-resistant Kind cluster with a private
// kubeconfig, builds and installs the checkout controller once, runs
// label-selected specs, and tears only its own cluster down. On failure it
// collects a redacted diagnostic bundle BEFORE teardown. Every invocation
// writes parseable per-spec JUnit and measured phase/spec timings into a
// git-ignored artifacts directory; phases that did not run are explicitly
// marked skipped.
package suite

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"

	"github.com/optipod/optipod/test/e2e/framework"
)

var (
	// env is nil until BeforeSuite succeeds; SetupEnvironment removes the owned
	// cluster itself when any setup phase fails.
	env       *framework.Environment
	artifacts *framework.RunArtifacts
	run       = newRunRecord()
)

// runRecord accumulates what the suite needs after its specs: per-spec
// timings, the original failure texts, and measured diagnostics/teardown
// phases.
type runRecord struct {
	failed       bool
	failureTexts []string
	// snapshotErrors records per-spec forensic snapshot failures. They are
	// diagnostics about diagnostics: they are reported alongside the bundle
	// and never mask the original spec failure.
	snapshotErrors []string
	specs          []framework.SpecTiming
	specSeconds    float64
	specsRan       bool
	setupSeconds   float64
	diagnostics    framework.PhaseTiming
	teardown       framework.PhaseTiming
}

func newRunRecord() runRecord {
	return runRecord{
		diagnostics: framework.SkippedPhase("diagnostics"),
		teardown:    framework.SkippedPhase("teardown"),
	}
}

func TestKindSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("the installed-controller Kind suite provisions a real cluster; skipped under -short")
	}
	RegisterFailHandler(Fail)
	RunSpecs(t, "OptiPod installed-controller Kind suite")
}

var _ = BeforeSuite(func(ctx context.Context) {
	var err error
	artifacts, err = framework.NewRunArtifacts(framework.DefaultRepoRoot())
	Expect(err).NotTo(HaveOccurred(), "create per-run artifacts directory")
	fmt.Printf("=== optipod e2e artifacts: %s\n", artifacts.Dir)

	// Focused reuse is explicit only: unless the operator names a retained
	// harness cluster and its private kubeconfig, the suite creates a new
	// owned cluster. A set-but-unusable reuse request fails the run rather
	// than falling back to anything implicit.
	reuse, err := framework.ReuseFromEnv(ctx, framework.ClusterOptions{})
	if err != nil {
		Fail(fmt.Sprintf("explicit focused cluster reuse was refused: %v", err))
	}
	if reuse != nil {
		fmt.Printf("=== focused reuse of retained harness cluster %s (installed controller kept; not rebuilt)\n", reuse.Name)
	}

	setupStart := time.Now()
	env, err = framework.SetupEnvironment(ctx, framework.EnvironmentOptions{
		Cluster:         framework.OptionsFromEnv(),
		ExistingCluster: reuse,
	})
	run.setupSeconds = time.Since(setupStart).Seconds()
	if err != nil {
		// SetupEnvironment already removed the owned cluster (unless
		// KEEP_CLUSTER retention was requested); the setup failure stays
		// identifiable in the console output and the JUnit report.
		Fail(fmt.Sprintf("suite setup failed (owned cluster cleaned up unless KEEP_CLUSTER is set): %v", err))
	}
	for _, phase := range env.Phases {
		fmt.Printf("=== phase %-14s %s\n", phase.Name, phase.Duration)
	}
	fmt.Printf("=== owned cluster: %s (private kubeconfig: %s)\n", env.Cluster.Name, env.Cluster.Kubeconfig)
})

var _ = ReportAfterEach(func(sr types.SpecReport) {
	run.specs = append(run.specs, framework.SpecTiming{
		Name:            sr.FullText(),
		State:           sr.State.String(),
		DurationSeconds: sr.RunTime.Seconds(),
		Labels:          sr.Labels(),
	})
	if sr.State.Is(types.SpecStateFailureStates) {
		run.failed = true
		run.failureTexts = append(run.failureTexts, specFailureText(sr))
		snapshotFixtureTraceOnFailure(sr)
	}
	if sr.State != types.SpecStateInvalid &&
		!sr.State.Is(types.SpecStateSkipped|types.SpecStatePending) {
		run.specsRan = true
		run.specSeconds += sr.RunTime.Seconds()
	}
})

func specFailureText(sr types.SpecReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "spec %q finished %s", sr.FullText(), sr.State)
	if sr.Failure.Message != "" {
		fmt.Fprintf(&b, "\n  message: %s", sr.Failure.Message)
	}
	if loc := sr.Failure.Location; loc.FileName != "" {
		fmt.Fprintf(&b, "\n  location: %s", loc.String())
	}
	if fwd := sr.Failure.ForwardedPanic; fwd != "" {
		fmt.Fprintf(&b, "\n  panic: %s", fwd)
	}
	return b.String()
}

// snapshotFixtureTraceOnFailure captures the fixture's recorded queries
// immediately after a spec fails, BEFORE any later spec's ClearTrace can wipe
// the failing window (the M3 VAL-CONF-005 forensics gap). The snapshot lands
// in the run's diagnostics directory through the framework's redaction
// pipeline. It is error-isolated: a snapshot failure is recorded (and joins
// the bundle's collection errors) but never fails a spec or masks the
// original failure.
func snapshotFixtureTraceOnFailure(sr types.SpecReport) {
	if env == nil || artifacts == nil {
		return
	}
	// A short independent bound: the report path must not hang, and the run
	// context may already be cancelled when a spec fails on interruption.
	sctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// The failure-count prefix keeps snapshot file names unique when several
	// specs fail in one run.
	name := fmt.Sprintf("%02d-%s", len(run.failureTexts), sr.FullText())
	path, redactions, err := framework.SnapshotFixtureTrace(sctx, env.Clients, artifacts.DiagnosticsDir(), name)
	if err != nil {
		note := fmt.Sprintf("fixture-trace failure snapshot for spec %q failed (the original failure remains primary): %s",
			sr.FullText(), framework.Redact(err.Error()))
		run.snapshotErrors = append(run.snapshotErrors, note)
		fmt.Printf("=== %s\n", note)
		return
	}
	fmt.Printf("=== fixture-trace failure snapshot: %s (%d redactions)\n", path, redactions)
}

// AfterSuite collects failure diagnostics BEFORE tearing the owned cluster
// down, then tears down with a bounded context independent of any cancelled
// run context. Ginkgo runs this even when BeforeSuite failed or the suite was
// interrupted, so every exit path reaches ownership-aware cleanup.
var _ = AfterSuite(func() {
	if env == nil {
		// Setup failed; SetupEnvironment already removed the owned cluster.
		return
	}

	if run.failed {
		start := time.Now()
		// List the sweep scope first so a listing error is itself recorded in
		// the failure summary the bundle keeps.
		names := diagnosticsNamespaces()
		scope := framework.DiagnosticsScope{
			ClusterName:    env.Cluster.Name,
			FailureSummary: strings.Join(run.failureTexts, "\n\n"),
			Pins:           framework.PinManifest(),
			Namespaces:     names,
			// Per-spec snapshot failures (if any) stay visible in the bundle
			// without ever masking the original failure.
			PreRecordedErrors: run.snapshotErrors,
		}
		dctx, cancel := framework.DiagnosticsContext()
		defer cancel()
		bundle, err := framework.CollectDiagnostics(dctx, env.Clients, scope, artifacts.DiagnosticsDir())
		Expect(err).NotTo(HaveOccurred(),
			"collecting failure diagnostics before teardown failed; the original failure is:\n%s",
			strings.Join(run.failureTexts, "\n\n"))
		run.diagnostics = framework.PhaseTiming{Name: "diagnostics", DurationSeconds: time.Since(start).Seconds()}
		fmt.Printf("=== failure diagnostics: %s (%d files, %d redactions)\n",
			bundle.Dir, len(bundle.Files), bundle.Redactions)
		if len(bundle.CollectionErrors) > 0 {
			fmt.Printf("=== diagnostics collection errors (original failure remains primary):\n  %s\n",
				strings.Join(bundle.CollectionErrors, "\n  "))
		}
	}

	start := time.Now()
	err := env.Teardown()
	run.teardown = framework.PhaseTiming{Name: "teardown", DurationSeconds: time.Since(start).Seconds()}
	// A cleanup failure stays visible alongside the original failure.
	Expect(err).NotTo(HaveOccurred(), "teardown of the owned cluster failed")

	if env.Cluster.Keep() {
		env.Cluster.PrintRetentionNotice(os.Stdout)
	}
})

// diagnosticsNamespaces scopes the failure sweep to this run's own resources:
// every harness-created namespace plus the installed system and fixture
// namespaces.
func diagnosticsNamespaces() []string {
	dctx, cancel := framework.DiagnosticsContext()
	defer cancel()
	names, err := framework.ListHarnessNamespaces(dctx, env.Clients)
	if err != nil {
		run.failureTexts = append(run.failureTexts, "diagnostics scope could not list harness namespaces: "+err.Error())
		names = nil
	}
	return appendUnique(names, framework.SystemNamespace, framework.FixtureNamespace)
}

func appendUnique(names []string, extras ...string) []string {
	seen := make(map[string]bool, len(names)+len(extras))
	for _, n := range names {
		seen[n] = true
	}
	for _, extra := range extras {
		if !seen[extra] {
			names = append(names, extra)
			seen[extra] = true
		}
	}
	return names
}

// ReportAfterSuite writes the always-emitted artifacts: parseable per-spec
// JUnit (success and failure alike) and the measured phase/spec timings with
// unexecuted phases explicitly marked skipped.
var _ = ReportAfterSuite("kind suite JUnit and timing artifacts", func(report types.Report) {
	if artifacts == nil {
		return
	}
	phases := make([]framework.PhaseTiming, 0, 12)
	if env != nil {
		for _, p := range env.Phases {
			phases = append(phases, framework.PhaseTimingFrom(p))
		}
	} else {
		// Setup failed; the granular phases were lost with the failed
		// environment, so record one measured setup phase instead of guessing.
		phases = append(phases, framework.PhaseTiming{Name: "setup", DurationSeconds: run.setupSeconds})
	}
	if run.specsRan {
		phases = append(phases, framework.PhaseTiming{Name: "specs", DurationSeconds: run.specSeconds})
	} else {
		phases = append(phases, framework.SkippedPhase("specs"))
	}
	phases = append(phases, run.diagnostics, run.teardown)

	timings := framework.TimingsReport{
		SuiteDescription: report.SuiteDescription,
		SuitePath:        report.SuitePath,
		ClusterName:      clusterName(),
		ArtifactsDir:     artifacts.Dir,
		StartedAt:        report.StartTime,
		FinishedAt:       report.EndTime,
		RunTimeSeconds:   report.RunTime.Seconds(),
		SuiteSucceeded:   report.SuiteSucceeded,
		Phases:           phases,
		Specs:            run.specs,
	}
	Expect(framework.WriteTimingsReport(timings, artifacts.TimingsPath())).To(Succeed(),
		"write timings report")
	Expect(framework.WriteJUnitReport(report, artifacts.JUnitPath())).To(Succeed(),
		"write JUnit report")

	result := "PASSED"
	if !report.SuiteSucceeded {
		result = "FAILED"
	}
	fmt.Printf("=== suite %s in %s\n", result, report.RunTime.Round(time.Second))
	fmt.Printf("=== JUnit: %s\n=== timings: %s\n", artifacts.JUnitPath(), artifacts.TimingsPath())
})

func clusterName() string {
	if env == nil {
		return ""
	}
	return env.Cluster.Name
}
