package framework

import (
	"testing"
	"time"
)

// A focused reuse rerun must report every setup phase: the install-cycle
// phases it deliberately does not perform are marked skipped in the same
// position an ordinary run measures them, so the timing file stays uniform
// across smoke, failure and reuse runs.
func TestMarkSkippedInstallPhasesUniformWithOrdinaryRun(t *testing.T) {
	env := &Environment{Phases: []Phase{
		{Name: phasePreflight, Duration: 200 * time.Millisecond},
		{Name: phaseClusterReuse, Duration: 50 * time.Millisecond},
		{Name: phaseClients, Duration: 300 * time.Millisecond},
	}}
	env.markSkippedInstallPhases()
	env.Phases = append(env.Phases, Phase{Name: phaseAwaitReady, Duration: 2 * time.Second})

	want := []struct {
		name    string
		skipped bool
	}{
		{phasePreflight, false},
		{phaseClusterReuse, false},
		{phaseClients, false},
		{phaseImageBuild, true},
		{phaseImageLoad, true},
		{phaseRender, true},
		{phaseInstallApply, true},
		{phaseAwaitReady, false},
	}
	if len(env.Phases) != len(want) {
		t.Fatalf("phases = %v, want %d entries", phaseNamesForTest(env.Phases), len(want))
	}
	for i, w := range want {
		got := env.Phases[i]
		if got.Name != w.name {
			t.Errorf("phase %d = %q, want %q", i, got.Name, w.name)
		}
		if got.Skipped != w.skipped {
			t.Errorf("phase %q skipped = %v, want %v", got.Name, got.Skipped, w.skipped)
		}
		if got.Skipped && got.Duration != 0 {
			t.Errorf("skipped phase %q carries duration %s", got.Name, got.Duration)
		}
		if !got.Skipped && got.Duration <= 0 {
			t.Errorf("measured phase %q has no duration", got.Name)
		}
	}

	// The converted report phases must carry the skipped markers through, so a
	// reuse run's timings.json names every phase an ordinary run reports.
	timings := make([]PhaseTiming, 0, len(env.Phases))
	for _, p := range env.Phases {
		timings = append(timings, PhaseTimingFrom(p))
	}
	for i, w := range want {
		if timings[i].Name != w.name || timings[i].Skipped != w.skipped {
			t.Errorf("converted phase %d = %+v, want name %q skipped %v", i, timings[i], w.name, w.skipped)
		}
	}
}

func phaseNamesForTest(phases []Phase) []string {
	names := make([]string, 0, len(phases))
	for _, p := range phases {
		names = append(names, p.Name)
	}
	return names
}
