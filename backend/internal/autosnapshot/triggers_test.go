package autosnapshot

import (
	"testing"
	"time"
)

// spikeThresholdFixture is the threshold the fed counts are judged against.
const spikeThresholdFixture = 10.0

// feedSpike replays active counts spaced by interval and returns the resulting
// state together with the step the last probe produced.
func feedSpike(counts []int, interval, dipGrace time.Duration) (*hostState, spikeStep) {
	start := time.Date(2026, 7, 20, 3, 0, 0, 0, time.UTC)
	state := &hostState{}

	var step spikeStep

	for i, count := range counts {
		step = state.advanceSpike(start.Add(time.Duration(i)*interval), count, spikeThresholdFixture, dipGrace)
	}

	return state, step
}

func TestAdvanceSpikeToleratesBriefDips(t *testing.T) {
	t.Parallel()

	// 30s probes under spike_duration 2m (1m grace): saw-toothed load whose dips
	// last a single probe — the shape that never fired under the old rule.
	state, step := feedSpike([]int{14, 15, 8, 12, 17, 9, 13, 11}, 30*time.Second, time.Minute)

	if step != spikeForming {
		t.Fatalf("step = %d, want spikeForming", step)
	}

	if state.aboveThresholdSince == nil {
		t.Fatal("brief dips must not abort the forming spike")
	}

	if got, want := state.spikeCoverage(), 0.75; got != want {
		t.Fatalf("coverage = %v, want %v", got, want)
	}

	if got, want := state.spikePeak, 17; got != want {
		t.Fatalf("peak = %d, want %d", got, want)
	}
}

func TestAdvanceSpikeAbortsOnSustainedDip(t *testing.T) {
	t.Parallel()

	// The third consecutive dip sits 90s past the last high probe, beyond the 1m grace.
	state, step := feedSpike([]int{14, 15, 8, 7, 9}, 30*time.Second, time.Minute)

	if step != spikeAborted {
		t.Fatalf("step = %d, want spikeAborted", step)
	}

	if state.aboveThresholdSince != nil || state.lastAboveAt != nil || state.spikeTotal != 0 || state.spikePeak != 0 {
		t.Fatal("an aborted candidate must reset its counters")
	}
}

func TestSpikeCoverageRejectsFlapping(t *testing.T) {
	t.Parallel()

	// Every dip is short enough to be tolerated, but only half the probes are
	// above the threshold — coverage has to keep this from firing.
	state, _ := feedSpike([]int{14, 8, 12, 9, 11, 8}, 30*time.Second, time.Minute)

	if got := state.spikeCoverage(); got >= minSpikeCoverage {
		t.Fatalf("coverage = %v, want < %v", got, minSpikeCoverage)
	}
}

func TestAdvanceSpikeStartsOnlyOnHighProbe(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 20, 3, 0, 0, 0, time.UTC)
	state := &hostState{}

	if step := state.advanceSpike(now, 4, spikeThresholdFixture, time.Minute); step != spikeAborted {
		t.Fatalf("step = %d, want spikeAborted with no candidate open", step)
	}

	if step := state.advanceSpike(now.Add(30*time.Second), 12, spikeThresholdFixture, time.Minute); step != spikeStarted {
		t.Fatalf("step = %d, want spikeStarted", step)
	}

	if step := state.advanceSpike(now.Add(time.Minute), 16, spikeThresholdFixture, time.Minute); step != spikeForming {
		t.Fatalf("step = %d, want spikeForming", step)
	}

	if got := state.spikeCoverage(); got != 1 {
		t.Fatalf("coverage = %v, want 1", got)
	}

	if got, want := state.spikePeak, 16; got != want {
		t.Fatalf("peak = %d, want %d", got, want)
	}
}
