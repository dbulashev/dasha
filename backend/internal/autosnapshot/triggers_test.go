package autosnapshot

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/dto"
)

// spikeThresholdFixture is the threshold the fed counts are judged against.
const spikeThresholdFixture = 10.0

// feedSpike replays active counts spaced by interval and returns the resulting
// state together with the step the last probe produced.
func feedSpike(counts []int, interval, spikeDuration time.Duration) (*hostState, spikeStep) {
	start := time.Date(2026, 7, 20, 3, 0, 0, 0, time.UTC)
	state := &hostState{}

	var step spikeStep

	for i, count := range counts {
		step = state.advanceSpike(start.Add(time.Duration(i)*interval), count, spikeThresholdFixture, spikeDuration)
	}

	return state, step
}

func TestAdvanceSpikeToleratesBriefDips(t *testing.T) {
	t.Parallel()

	// 30s probes under spike_duration 2m (1m grace): saw-toothed load whose dips
	// last a single probe — the shape that never fired under the old rule.
	state, step := feedSpike([]int{14, 15, 8, 12, 17, 9, 13, 11}, 30*time.Second, 2*time.Minute)

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
	state, step := feedSpike([]int{14, 15, 8, 7, 9}, 30*time.Second, 2*time.Minute)

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
	state, _ := feedSpike([]int{14, 8, 12, 9, 11, 8}, 30*time.Second, 2*time.Minute)

	if got := state.spikeCoverage(); got >= minSpikeCoverage {
		t.Fatalf("coverage = %v, want < %v", got, minSpikeCoverage)
	}
}

func TestAdvanceSpikeAgesOutStuckCandidate(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 7, 20, 3, 0, 0, 0, time.UTC)
	state := &hostState{}

	// Alternating probes are each within the dip grace, so nothing else would ever
	// close this candidate; with spike_duration 2m it must restart past 8m.
	for i := range 21 {
		count := 14
		if i%2 == 1 {
			count = 8
		}

		state.advanceSpike(start.Add(time.Duration(i)*30*time.Second), count, spikeThresholdFixture, 2*time.Minute)
	}

	if state.aboveThresholdSince == nil {
		t.Fatal("a restarted candidate should be open")
	}

	if got := state.aboveThresholdSince.Sub(start); got < staleCandidateFactor*2*time.Minute {
		t.Fatalf("candidate opened at +%v, want it restarted past the stale age", got)
	}
}

func TestAdvanceSpikeStartsOnlyOnHighProbe(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 20, 3, 0, 0, 0, time.UTC)
	state := &hostState{}

	if step := state.advanceSpike(now, 4, spikeThresholdFixture, 2*time.Minute); step != spikeAborted {
		t.Fatalf("step = %d, want spikeAborted with no candidate open", step)
	}

	if step := state.advanceSpike(now.Add(30*time.Second), 12, spikeThresholdFixture, 2*time.Minute); step != spikeStarted {
		t.Fatalf("step = %d, want spikeStarted", step)
	}

	if step := state.advanceSpike(now.Add(time.Minute), 16, spikeThresholdFixture, 2*time.Minute); step != spikeForming {
		t.Fatalf("step = %d, want spikeForming", step)
	}

	if got := state.spikeCoverage(); got != 1 {
		t.Fatalf("coverage = %v, want 1", got)
	}

	if got, want := state.spikePeak, 16; got != want {
		t.Fatalf("peak = %d, want %d", got, want)
	}
}

// fakeStore implements only what the spike path touches; every other Store call
// hits the embedded nil interface and panics, keeping the fake honest.
type fakeStore struct {
	Store
	now       func() time.Time
	snapshots int
	events    []TriggerEvent
}

func (f *fakeStore) CreateSnapshot(
	context.Context, string, string, string, []dto.QueryReport, SnapshotOpts,
) (uuid.UUID, time.Time, error) {
	f.snapshots++

	return uuid.New(), f.now(), nil
}

func (f *fakeStore) InsertTriggerEvent(_ context.Context, e TriggerEvent) error {
	f.events = append(f.events, e)

	return nil
}

// spikeHarness replays tickActivitySpike over synthetic time: one tick is one
// poll_interval, so a multi-minute spike costs no wall-clock time.
type spikeHarness struct {
	daemon *Daemon
	store  *fakeStore
	state  *hostState
	cfg    Config
	now    time.Time
	count  int
}

func newSpikeHarness() *spikeHarness {
	h := &spikeHarness{ //nolint:exhaustruct
		state: &hostState{},
		now:   time.Date(2026, 7, 20, 2, 0, 0, 0, time.UTC),
	}

	// The shipped defaults: a 30m baseline window, +50%, held for 2m.
	h.cfg = Config{ //nolint:exhaustruct
		PollInterval:         30 * time.Second,
		MaxSnapshotFrequency: time.Hour,
		MinBaselineActive:    10,
		Defaults: TriggerDefaults{ //nolint:exhaustruct
			ActivitySpike: ActivitySpikeTrigger{ //nolint:exhaustruct
				Enabled:            true,
				WindowSize:         30 * time.Minute,
				ActiveThresholdPct: 50,
				SpikeDuration:      2 * time.Minute,
			},
		},
	}

	h.store = &fakeStore{Store: nil, now: func() time.Time { return h.now }, snapshots: 0, events: nil}
	h.daemon = &Daemon{ //nolint:exhaustruct
		repo:   &fakeRepo{activeCount: func(context.Context) (int, error) { return h.count, nil }}, //nolint:exhaustruct
		store:  h.store,
		logger: zap.NewNop(),
		hosts:  map[hostKey]*hostState{},
		clock:  func() time.Time { return h.now },
	}

	return h
}

func (h *spikeHarness) tick(count int) {
	h.now = h.now.Add(h.cfg.PollInterval)
	h.count = count

	h.daemon.tickActivitySpike(
		context.Background(), h.cfg, h.cfg.Defaults.ActivitySpike,
		oneHostCluster(), "h1", "db1", h.state,
	)
}

func (h *spikeHarness) ticks(n, count int) {
	for range n {
		h.tick(count)
	}
}

func (h *spikeHarness) spikeContext(t *testing.T) map[string]any {
	t.Helper()

	for _, e := range h.store.events {
		if e.TriggerType == TriggerActivitySpike && e.Outcome == OutcomeSnapshotCreated {
			return e.TriggerContext
		}
	}

	t.Fatal("no activity_spike snapshot event was recorded")

	return nil
}

func TestTickActivitySpikeFiresOnceForSustainedSpike(t *testing.T) {
	t.Parallel()

	h := newSpikeHarness()

	h.ticks(60, 10) // 30m of steady load: baseline 10, threshold 15
	h.ticks(12, 20) // 6m above it — the 2m duration is met on the 4th probe

	if h.store.snapshots != 1 {
		t.Fatalf("snapshots = %d, want exactly 1 (max_snapshot_frequency debounces the rest)", h.store.snapshots)
	}

	ctx := h.spikeContext(t)

	if got, want := ctx["peak_value"], 20; got != want {
		t.Fatalf("peak_value = %v, want %v", got, want)
	}

	if got, want := ctx["coverage"], 1.0; got != want {
		t.Fatalf("coverage = %v, want %v", got, want)
	}
}

func TestTickActivitySpikeSkipsFlappingLoad(t *testing.T) {
	t.Parallel()

	h := newSpikeHarness()

	h.ticks(60, 10) // baseline 10, threshold 15

	// Each dip is inside the grace window, so the candidate survives — but only
	// half of its probes are above the threshold.
	for range 8 {
		h.tick(30)
		h.tick(12)
	}

	if h.store.snapshots != 0 {
		t.Fatalf("snapshots = %d, want 0: coverage never reaches %v", h.store.snapshots, minSpikeCoverage)
	}

	if h.state.aboveThresholdSince == nil {
		t.Fatal("dips within the grace window must keep the candidate alive")
	}

	if got := h.state.spikeCoverage(); got >= minSpikeCoverage {
		t.Fatalf("coverage = %v, want < %v", got, minSpikeCoverage)
	}
}
