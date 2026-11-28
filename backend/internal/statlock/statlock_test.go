package statlock

import (
	"context"
	"testing"
	"time"
)

var base = time.Date(2026, 11, 28, 3, 0, 0, 0, time.UTC)

func snap(at time.Time, reset *time.Time, rows ...Row) Snapshot {
	return Snapshot{
		CapturedAt:             at,
		VersionNum:             190001,
		StatsReset:             reset,
		DeadlockTimeoutMs:      1000,
		MaxLocksPerTransaction: 64,
		Rows:                   rows,
	}
}

func relation(waits, waitMs, fastpath int64) Row {
	return Row{LockType: "relation", Waits: waits, WaitTimeMs: waitMs, FastpathExceeded: fastpath}
}

func TestDeltaSubtractsCounters(t *testing.T) {
	t.Parallel()

	reset := base.Add(-time.Hour)
	prev := snap(base, &reset, relation(10, 1000, 1), Row{LockType: "advisory", Waits: 2})
	cur := snap(base.Add(5*time.Minute), &reset, relation(15, 1600, 3), Row{LockType: "advisory", Waits: 2})

	iv, ok := Delta(prev, cur)
	if !ok || !iv.Complete {
		t.Fatalf("delta of one epoch must be complete: %+v", iv)
	}

	got := map[string]Row{}
	for _, r := range iv.Rows {
		got[r.LockType] = r
	}

	if r := got["relation"]; r != relation(5, 600, 2) {
		t.Errorf("relation delta = %+v", r)
	}

	if r := got["advisory"]; r.Waits != 0 {
		t.Errorf("advisory delta = %+v, want zero waits", r)
	}
}

func TestDeltaIncompleteOnStatsReset(t *testing.T) {
	t.Parallel()

	r1, r2 := base.Add(-time.Hour), base.Add(time.Minute)
	prev := snap(base, &r1, relation(10, 1000, 0))
	cur := snap(base.Add(5*time.Minute), &r2, relation(1, 10, 0))

	if iv, ok := Delta(prev, cur); ok || iv.Rows != nil {
		t.Fatalf("a reset between captures must break the interval: %+v", iv)
	}
}

func TestDeltaIncompleteWhenACounterFalls(t *testing.T) {
	t.Parallel()

	reset := base.Add(-time.Hour)
	prev := snap(base, &reset, relation(10, 1000, 4))
	cur := snap(base.Add(5*time.Minute), &reset, relation(12, 1200, 1))

	if iv, ok := Delta(prev, cur); ok || iv.Rows != nil {
		t.Fatalf("a falling counter without a visible reset must break the interval: %+v", iv)
	}
}

func TestDeltaIncompleteOnMajorUpgrade(t *testing.T) {
	t.Parallel()

	prev := snap(base, nil, relation(10, 1000, 0))
	prev.VersionNum = 180004
	cur := snap(base.Add(5*time.Minute), nil, relation(12, 1200, 0))

	if _, ok := Delta(prev, cur); ok {
		t.Fatal("an 18 → 19 upgrade must break the interval")
	}
}

func TestDeltaSkipsLockTypeWithoutBaseline(t *testing.T) {
	t.Parallel()

	prev := snap(base, nil, relation(10, 1000, 0))
	cur := snap(base.Add(5*time.Minute), nil, relation(11, 1100, 0), Row{LockType: "tuple", Waits: 3})

	iv, ok := Delta(prev, cur)
	if !ok {
		t.Fatal("a new lock type must not break the interval")
	}

	if len(iv.Rows) != 1 || iv.Rows[0].LockType != "relation" {
		t.Fatalf("rows = %+v, want only relation", iv.Rows)
	}
}

type fakeHistory struct {
	snaps  []Snapshot
	loaded []time.Time
}

func (f *fakeHistory) GetLockSnapshotMetas(context.Context, string, string, time.Time, time.Time) ([]Meta, error) {
	out := make([]Meta, len(f.snaps))
	for i, s := range f.snaps {
		out[i] = s.Meta()
	}

	return out, nil
}

func (f *fakeHistory) GetLockSnapshotsAt(_ context.Context, _, _ string, at []time.Time) ([]Snapshot, error) {
	f.loaded = at

	want := map[int64]struct{}{}
	for _, t := range at {
		want[t.UnixNano()] = struct{}{}
	}

	var out []Snapshot

	for _, s := range f.snaps {
		if _, ok := want[s.CapturedAt.UnixNano()]; ok {
			out = append(out, s)
		}
	}

	return out, nil
}

func (f *fakeHistory) LockSnapshotRange(context.Context, string, string) (*time.Time, *time.Time, error) {
	if len(f.snaps) == 0 {
		return nil, nil, nil
	}

	return &f.snaps[0].CapturedAt, &f.snaps[len(f.snaps)-1].CapturedAt, nil
}

func TestHistoryBreaksOnResetAndSumsTheRest(t *testing.T) {
	t.Parallel()

	r1, r2 := base.Add(-time.Hour), base.Add(7*time.Minute)
	st := &fakeHistory{snaps: []Snapshot{
		snap(base, &r1, relation(10, 1000, 0)),
		snap(base.Add(5*time.Minute), &r1, relation(14, 1400, 0)),
		snap(base.Add(10*time.Minute), &r2, relation(1, 50, 0)),
		snap(base.Add(15*time.Minute), &r2, relation(3, 250, 0)),
	}}

	h, err := NewHistoryReader(st).Read(t.Context(), "c", "h", base, base.Add(15*time.Minute), 3)
	if err != nil {
		t.Fatal(err)
	}

	if len(h.Buckets) != 3 {
		t.Fatalf("buckets = %d, want 3", len(h.Buckets))
	}

	if b := h.Buckets[0]; b.Partial || len(b.Rows) != 1 || b.Rows[0] != relation(4, 400, 0) {
		t.Errorf("first bucket = %+v", b)
	}

	if b := h.Buckets[1]; !b.Partial || b.Duration != 0 || len(b.Rows) != 0 {
		t.Errorf("reset bucket = %+v, want a gap", b)
	}

	if b := h.Buckets[2]; b.Partial || len(b.Rows) != 1 || b.Rows[0] != relation(2, 200, 0) {
		t.Errorf("bucket after the reset = %+v", b)
	}

	// One point over the whole window: the reset is reported, the measured
	// parts on both sides of it are summed.
	whole, err := NewHistoryReader(st).Read(t.Context(), "c", "h", base, base.Add(15*time.Minute), 1)
	if err != nil {
		t.Fatal(err)
	}

	if len(whole.Buckets) != 1 {
		t.Fatalf("buckets = %d, want 1", len(whole.Buckets))
	}

	b := whole.Buckets[0]
	if !b.Partial || b.Duration != 10*time.Minute || len(b.Rows) != 1 || b.Rows[0] != relation(6, 600, 0) {
		t.Errorf("window bucket = %+v", b)
	}

	if len(st.loaded) != 4 {
		t.Errorf("loaded %d captures, want the 4 span ends", len(st.loaded))
	}
}

func TestHistoryWithoutCaptures(t *testing.T) {
	t.Parallel()

	st := &fakeHistory{}

	h, err := NewHistoryReader(st).Read(t.Context(), "c", "h", base, base.Add(time.Hour), 5)
	if err != nil {
		t.Fatal(err)
	}

	if st.loaded != nil || len(h.Buckets) != 0 || h.Earliest != nil {
		t.Fatalf("history of a never captured host = %+v, loaded %v", h, st.loaded)
	}
}
