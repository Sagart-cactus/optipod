package framework

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2/types"
)

func TestResolveArtifactsRootHonorsOverride(t *testing.T) {
	t.Setenv(ArtifactsEnvVar, "")
	if got := ResolveArtifactsRoot("/repo"); got != filepath.Join("/repo", "test", "e2e", "artifacts") {
		t.Errorf("default root = %q", got)
	}
	t.Setenv(ArtifactsEnvVar, "/custom/artifacts")
	if got := ResolveArtifactsRoot("/repo"); got != "/custom/artifacts" {
		t.Errorf("override root = %q", got)
	}
}

func TestNewRunArtifactsUniqueDirs(t *testing.T) {
	t.Setenv(ArtifactsEnvVar, t.TempDir())
	first, err := NewRunArtifacts("/irrelevant")
	if err != nil {
		t.Fatalf("NewRunArtifacts: %v", err)
	}
	second, err := NewRunArtifacts("/irrelevant")
	if err != nil {
		t.Fatalf("NewRunArtifacts: %v", err)
	}
	if first.Dir == second.Dir {
		t.Errorf("two runs share directory %q", first.Dir)
	}
	if !strings.HasPrefix(filepath.Base(first.Dir), "run-") {
		t.Errorf("run dir %q lacks run- prefix", first.Dir)
	}
	for _, path := range []string{first.JUnitPath(), first.TimingsPath()} {
		if !strings.HasPrefix(path, first.Dir+string(filepath.Separator)) {
			t.Errorf("artifact path %q outside run dir %q", path, first.Dir)
		}
	}
	if di := first.DiagnosticsDir(); !strings.HasPrefix(di, first.Dir) {
		t.Errorf("diagnostics dir %q outside run dir %q", di, first.Dir)
	}
}

func TestWriteTimingsReportMarksSkippedPhases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "timings.json")
	report := TimingsReport{
		SuiteDescription: "OptiPod installed-controller Kind suite",
		ClusterName:      "optipod-e2e-123-abcdef",
		StartedAt:        time.Now(),
		FinishedAt:       time.Now().Add(90 * time.Second),
		RunTimeSeconds:   90,
		SuiteSucceeded:   false,
		Phases: []PhaseTiming{
			{Name: "preflight", DurationSeconds: 0.2},
			{Name: "cluster-create", DurationSeconds: 40.1},
			{Name: "specs", DurationSeconds: 12.5},
			{Name: "diagnostics", Skipped: true},
			{Name: "teardown", DurationSeconds: 20.0},
		},
		Specs: []SpecTiming{
			{Name: "Harness diagnostics collects a redacted bundle", State: "Passed", DurationSeconds: 8.1},
			{Name: "Deliberate failure", State: "Failed", DurationSeconds: 4.4},
		},
	}
	if err := WriteTimingsReport(report, path); err != nil {
		t.Fatalf("WriteTimingsReport: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read timings: %v", err)
	}
	var back TimingsReport
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("timings are not parseable JSON: %v\n%s", err, raw)
	}
	if len(back.Phases) != 5 || !back.Phases[3].Skipped || back.Phases[3].DurationSeconds != 0 {
		t.Errorf("skipped phase not marked: %+v", back.Phases)
	}
	if len(back.Specs) != 2 || back.Specs[1].State != "Failed" {
		t.Errorf("spec timings wrong: %+v", back.Specs)
	}
	if back.ClusterName == "" || back.SuiteSucceeded {
		t.Errorf("suite provenance wrong: %+v", back)
	}
}

func TestWriteJUnitReportIsParseableAndRedacted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "junit.xml")
	start := time.Now()
	report := types.Report{
		SuiteDescription: "test suite",
		SuitePath:        "/repo/test/e2e/suite",
		SuiteSucceeded:   false,
		StartTime:        start,
		EndTime:          start.Add(3 * time.Second),
		RunTime:          3 * time.Second,
		SuiteConfig:      types.SuiteConfig{LabelFilter: "diagnostics"},
		SpecReports: []types.SpecReport{
			{
				ContainerHierarchyTexts: []string{"Harness diagnostics"},
				LeafNodeText:            "collects a redacted bundle",
				LeafNodeType:            types.NodeTypeIt,
				State:                   types.SpecStatePassed,
				RunTime:                 900 * time.Millisecond,
			},
			{
				ContainerHierarchyTexts: []string{"Harness diagnostics"},
				LeafNodeText:            "fails deliberately",
				LeafNodeType:            types.NodeTypeIt,
				State:                   types.SpecStateFailed,
				RunTime:                 200 * time.Millisecond,
				Failure: types.Failure{
					Message: "deliberate failure with Bearer " + canaryBearerToken + " inline",
				},
				CapturedGinkgoWriterOutput: "kubeconfig client-key-data: LS0tLS1CRUdJTiBFQyBQUklWQVRFIEtFWS0tLS0KTUlJQ0FOQVJZMDAyCg==",
			},
		},
	}
	if err := WriteJUnitReport(report, path); err != nil {
		t.Fatalf("WriteJUnitReport: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read junit: %v", err)
	}
	var suites struct {
		XMLName  xml.Name `xml:"testsuites"`
		Tests    int      `xml:"tests,attr"`
		Failures int      `xml:"failures,attr"`
		Suites   []struct {
			Name      string `xml:"name,attr"`
			Tests     int    `xml:"tests,attr"`
			Failures  int    `xml:"failures,attr"`
			TestCases []struct {
				Name    string `xml:"name,attr"`
				Status  string `xml:"status,attr"`
				Failure *struct {
					Message string `xml:"message,attr"`
					Body    string `xml:",chardata"`
				} `xml:"failure"`
			} `xml:"testcase"`
		} `xml:"testsuite"`
	}
	if err := xml.Unmarshal(raw, &suites); err != nil {
		t.Fatalf("junit is not parseable XML: %v\n%s", err, raw)
	}
	if suites.Tests != 2 || suites.Failures != 1 {
		t.Errorf("junit counts tests=%d failures=%d, want 2/1", suites.Tests, suites.Failures)
	}
	if len(suites.Suites) != 1 {
		t.Fatalf("suite count = %d, want 1", len(suites.Suites))
	}
	s := suites.Suites[0]
	if s.Tests != 2 || s.Failures != 1 || len(s.TestCases) != 2 {
		t.Errorf("suite counts tests=%d failures=%d cases=%d, want 2/1/2", s.Tests, s.Failures, len(s.TestCases))
	}
	failed := s.TestCases[1]
	if failed.Status != "failed" || failed.Failure == nil {
		t.Fatalf("failed testcase missing failure element: %+v", failed)
	}
	if !strings.Contains(failed.Name, "fails deliberately") {
		t.Errorf("failed spec name = %q", failed.Name)
	}
	content := string(raw)
	if strings.Contains(content, canaryBearerToken) {
		t.Errorf("canary bearer token survived into junit")
	}
	if strings.Contains(content, "LS0tLS1CRUdJTiBFQyBQUklWQVRFIEtFWS0tLS0") {
		t.Errorf("canary key material survived into junit")
	}
	if !strings.Contains(content, RedactPlaceholder) {
		t.Errorf("redaction placeholder missing from junit")
	}
	if !strings.Contains(failed.Failure.Message, "deliberate failure") {
		t.Errorf("original failure message unidentifiable: %q", failed.Failure.Message)
	}
}
