package framework

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/onsi/ginkgo/v2/reporters"
	"github.com/onsi/ginkgo/v2/types"
)

// ArtifactsEnvVar overrides the directory a suite run writes its JUnit,
// timing and diagnostic artifacts into (absolute, or relative to the process
// working directory). CI uses it to point at an upload-stable path.
const ArtifactsEnvVar = "OPTIPOD_E2E_ARTIFACTS_DIR"

// DefaultArtifactsDir is the git-ignored default artifacts root for the new
// Kind suite.
func DefaultArtifactsDir(repoRoot string) string {
	return filepath.Join(repoRoot, "test", "e2e", "artifacts")
}

// ResolveArtifactsRoot returns the artifacts root: the OPTIPOD_E2E_ARTIFACTS_DIR
// override when set, otherwise <repoRoot>/test/e2e/artifacts.
func ResolveArtifactsRoot(repoRoot string) string {
	if dir := os.Getenv(ArtifactsEnvVar); dir != "" {
		return dir
	}
	return DefaultArtifactsDir(repoRoot)
}

// RunArtifacts is one suite invocation's artifact directory. JUnit, timings and
// the failure diagnostics bundle live side by side so a run's evidence stays
// together. The directory is git-ignored and never committed.
type RunArtifacts struct {
	// Dir is the unique per-run directory.
	Dir string
	// Root is the artifacts root the run directory was created under.
	Root string
}

// NewRunArtifacts creates a unique run directory under the resolved artifacts
// root. Two concurrent suites never share a run directory.
func NewRunArtifacts(repoRoot string) (*RunArtifacts, error) {
	root := ResolveArtifactsRoot(repoRoot)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create artifacts root %s: %w", root, err)
	}
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("generate run id: %w", err)
	}
	runDir := filepath.Join(root, fmt.Sprintf("run-%d-%s", time.Now().Unix(), hex.EncodeToString(buf)))
	if err := os.Mkdir(runDir, 0o755); err != nil {
		return nil, fmt.Errorf("create run artifacts dir %s: %w", runDir, err)
	}
	return &RunArtifacts{Dir: runDir, Root: root}, nil
}

// JUnitPath is where the suite writes its parseable per-spec JUnit XML.
func (a *RunArtifacts) JUnitPath() string { return filepath.Join(a.Dir, "junit.xml") }

// TimingsPath is where the suite writes measured phase and spec timings.
func (a *RunArtifacts) TimingsPath() string { return filepath.Join(a.Dir, "timings.json") }

// DiagnosticsDir is where a failed run's redacted diagnostic bundle is
// collected before the owned cluster is torn down.
func (a *RunArtifacts) DiagnosticsDir() string { return filepath.Join(a.Dir, "diagnostics") }

// SpecTiming is one Ginkgo spec's measured outcome.
type SpecTiming struct {
	Name            string   `json:"name"`
	State           string   `json:"state"`
	DurationSeconds float64  `json:"durationSeconds"`
	Labels          []string `json:"labels,omitempty"`
}

// PhaseTiming is one measured suite phase. A phase that did not run in this
// invocation (e.g. diagnostics on a successful run) is explicitly marked
// skipped rather than silently omitted or zero-filled.
type PhaseTiming struct {
	Name            string  `json:"name"`
	DurationSeconds float64 `json:"durationSeconds,omitempty"`
	Skipped         bool    `json:"skipped"`
}

// PhaseTimingFrom converts a measured setup Phase into a PhaseTiming.
func PhaseTimingFrom(p Phase) PhaseTiming {
	return PhaseTiming{Name: p.Name, DurationSeconds: p.Duration.Seconds()}
}

// SkippedPhase marks a named phase as not executed in this run.
func SkippedPhase(name string) PhaseTiming {
	return PhaseTiming{Name: name, Skipped: true}
}

// TimingsReport is the per-run timing file: the suite result, measured setup
// phases, the spec-execution phase, diagnostics and teardown phases, and
// per-spec durations.
type TimingsReport struct {
	SuiteDescription string        `json:"suiteDescription"`
	SuitePath        string        `json:"suitePath"`
	ClusterName      string        `json:"clusterName,omitempty"`
	ArtifactsDir     string        `json:"artifactsDir"`
	StartedAt        time.Time     `json:"startedAt"`
	FinishedAt       time.Time     `json:"finishedAt"`
	RunTimeSeconds   float64       `json:"runTimeSeconds"`
	SuiteSucceeded   bool          `json:"suiteSucceeded"`
	Phases           []PhaseTiming `json:"phases"`
	Specs            []SpecTiming  `json:"specs"`
}

// WriteTimingsReport writes the timing file as JSON.
func WriteTimingsReport(report TimingsReport, path string) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode timings report: %w", err)
	}
	// Timings carry names, states and durations only, but everything the
	// suite writes to disk passes the scrubber uniformly.
	scrubbed, _ := RedactCount(string(data))
	data = append([]byte(scrubbed), '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write timings report %s: %w", path, err)
	}
	return nil
}

// WriteJUnitReport writes Ginkgo's per-spec JUnit XML for the run (success and
// failure alike), then scrubs the file so captured spec output can never
// carry credentials into an artifact.
func WriteJUnitReport(report types.Report, path string) error {
	if err := reporters.GenerateJUnitReport(report, path); err != nil {
		return fmt.Errorf("generate junit report: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read junit report for redaction: %w", err)
	}
	scrubbed, _ := RedactCount(string(data))
	if scrubbed != string(data) {
		if err := os.WriteFile(path, []byte(scrubbed), 0o644); err != nil {
			return fmt.Errorf("write redacted junit report: %w", err)
		}
	}
	return nil
}
