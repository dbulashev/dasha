package statio

import (
	"testing"
	"time"
)

func ptrInt(v int) *int { return &v }

func ptrTime(t time.Time) *time.Time { return &t }

var base = time.Date(2026, 8, 22, 3, 0, 0, 0, time.UTC)

func snap(at time.Time, reset *time.Time, rows ...Row) Snapshot {
	return Snapshot{CapturedAt: at, VersionNum: 170004, StatsReset: reset, Rows: rows}
}

func row(bt, obj, ctx string, values map[string]int64) Row {
	return Row{Key: Key{BackendType: bt, Object: obj, Context: ctx}, Values: values}
}

func TestDeltaSubtractsCounters(t *testing.T) {
	reset := ptrTime(base.Add(-time.Hour))

	prev := snap(base, reset, row("client backend", "relation", "normal", map[string]int64{"reads": 10, "hits": 100}))
	cur := snap(base.Add(5*time.Minute), reset, row("client backend", "relation", "normal", map[string]int64{"reads": 25, "hits": 400}))

	iv, ok := Delta(prev, cur)
	if !ok || !iv.Complete {
		t.Fatalf("expected a complete interval, got %+v", iv)
	}

	if iv.Duration != 5*time.Minute {
		t.Fatalf("duration = %v, want 5m", iv.Duration)
	}

	if len(iv.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(iv.Rows))
	}

	if got := iv.Rows[0].Values["reads"]; got != 15 {
		t.Errorf("reads delta = %d, want 15", got)
	}

	if got := iv.Rows[0].Values["hits"]; got != 300 {
		t.Errorf("hits delta = %d, want 300", got)
	}
}

func TestDeltaIncompleteOnEpochChange(t *testing.T) {
	r := row("client backend", "relation", "normal", map[string]int64{"reads": 10})

	tests := map[string]struct {
		prev, cur Snapshot
	}{
		"stats_reset moved": {
			prev: snap(base, ptrTime(base.Add(-time.Hour)), r),
			cur:  snap(base.Add(5*time.Minute), ptrTime(base), r),
		},
		"stats_reset appeared": {
			prev: snap(base, nil, r),
			cur:  snap(base.Add(5*time.Minute), ptrTime(base), r),
		},
		"counter went backwards": {
			prev: snap(base, nil, row("client backend", "relation", "normal", map[string]int64{"reads": 10})),
			cur:  snap(base.Add(5*time.Minute), nil, row("client backend", "relation", "normal", map[string]int64{"reads": 4})),
		},
		"captures out of order": {
			prev: snap(base.Add(5*time.Minute), nil, r),
			cur:  snap(base, nil, r),
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			iv, ok := Delta(tc.prev, tc.cur)
			if ok || iv.Complete {
				t.Fatalf("expected an incomplete interval, got %+v", iv)
			}

			if len(iv.Rows) != 0 {
				t.Errorf("incomplete interval carries %d rows, want none", len(iv.Rows))
			}
		})
	}
}

func TestDeltaIncompleteOnMajorUpgrade(t *testing.T) {
	reset := ptrTime(base.Add(-time.Hour))
	r := row("client backend", "relation", "normal", map[string]int64{"reads": 10})

	prev := Snapshot{CapturedAt: base, VersionNum: 170004, StatsReset: reset, Rows: []Row{r}}
	cur := Snapshot{CapturedAt: base.Add(5 * time.Minute), VersionNum: 180001, StatsReset: reset, Rows: []Row{r}}

	if iv, ok := Delta(prev, cur); ok || iv.Complete {
		t.Fatalf("major upgrade must break the interval, got %+v", iv)
	}
}

func TestDeltaSkipsRowWithoutBaseline(t *testing.T) {
	reset := ptrTime(base)

	prev := snap(base, reset, row("client backend", "relation", "normal", map[string]int64{"reads": 10}))
	cur := snap(base.Add(5*time.Minute), reset,
		row("client backend", "relation", "normal", map[string]int64{"reads": 12}),
		row("autovacuum worker", "relation", "vacuum", map[string]int64{"reads": 7}),
	)

	iv, ok := Delta(prev, cur)
	if !ok {
		t.Fatalf("a new row must not break the interval, got %+v", iv)
	}

	if len(iv.Rows) != 1 || iv.Rows[0].BackendType != "client backend" {
		t.Fatalf("rows = %+v, want only the row that had a baseline", iv.Rows)
	}
}

func TestNormalizedDerivesBytesFromOpBytes(t *testing.T) {
	s := Snapshot{
		CapturedAt: base,
		VersionNum: 170004,
		OpBytes:    ptrInt(8192),
		Rows:       []Row{row("client backend", "relation", "normal", map[string]int64{"reads": 3, "writes": 2, "extends": 1})},
	}

	got := s.Normalized().Rows[0].Values

	for name, want := range map[string]int64{"read_bytes": 3 * 8192, "write_bytes": 2 * 8192, "extend_bytes": 8192} {
		if got[name] != want {
			t.Errorf("%s = %d, want %d", name, got[name], want)
		}
	}

	if s.Rows[0].Values["read_bytes"] != 0 {
		t.Error("Normalized must not mutate the receiver's rows")
	}
}

func TestNormalizedKeepsNativeByteCounters(t *testing.T) {
	s := Snapshot{
		CapturedAt: base,
		VersionNum: 180001,
		Rows:       []Row{row("client backend", "relation", "normal", map[string]int64{"reads": 3, "read_bytes": 999})},
	}

	if got := s.Normalized().Rows[0].Values["read_bytes"]; got != 999 {
		t.Errorf("read_bytes = %d, want the reported 999", got)
	}
}

// ioRun is a chronological run of captures one minute apart whose single
// counter grows by step between them.
func ioRun(n int, step int64, reset *time.Time) []Snapshot {
	out := make([]Snapshot, 0, n)

	for i := range n {
		out = append(out, snap(base.Add(time.Duration(i)*time.Minute), reset,
			row("client backend", "relation", "normal", map[string]int64{"reads": int64(i) * step})))
	}

	return out
}

func metasOf(snaps []Snapshot) []Meta {
	out := make([]Meta, 0, len(snaps))
	for _, s := range snaps {
		out = append(out, s.Meta())
	}

	return out
}

// loaded keeps only the captures the plan asked for — what the handler reads.
func loaded(snaps []Snapshot, at []time.Time) []Snapshot {
	want := make(map[int64]struct{}, len(at))
	for _, t := range at {
		want[t.UnixNano()] = struct{}{}
	}

	out := make([]Snapshot, 0, len(at))

	for _, s := range snaps {
		if _, ok := want[s.CapturedAt.UnixNano()]; ok {
			out = append(out, s)
		}
	}

	return out
}

func TestPlanBucketsReadsOnlyTheBucketEnds(t *testing.T) {
	snaps := ioRun(11, 1, ptrTime(base.Add(-time.Hour)))
	plan := PlanBuckets(metasOf(snaps), 5)

	if len(plan.Buckets) != 5 {
		t.Fatalf("buckets = %d, want 5", len(plan.Buckets))
	}

	at := plan.At()
	if len(at) != 6 {
		t.Fatalf("captures to load = %d of %d, want 6: neighbours share a boundary", len(at), len(snaps))
	}

	for _, b := range plan.Assemble(loaded(snaps, at)) {
		if b.Partial {
			t.Errorf("bucket %v..%v marked partial", b.From, b.To)
		}

		if b.Duration != 2*time.Minute {
			t.Errorf("bucket duration = %v, want 2m", b.Duration)
		}

		if got := b.Rows[0].Values["reads"]; got != 2 {
			t.Errorf("bucket reads = %d, want the two intervals it spans", got)
		}
	}
}

func TestPlanBucketsBreaksTheSpanOnAnEpochChange(t *testing.T) {
	snaps := ioRun(5, 10, ptrTime(base.Add(-time.Hour)))

	after := ptrTime(base.Add(90 * time.Second))
	for i := 2; i < len(snaps); i++ {
		snaps[i].StatsReset = after
	}

	plan := PlanBuckets(metasOf(snaps), 1)

	if len(plan.Buckets) != 1 {
		t.Fatalf("buckets = %d, want 1", len(plan.Buckets))
	}

	b := plan.Buckets[0]
	if len(b.Spans) != 2 {
		t.Fatalf("spans = %+v, want the reset to split the run in two", b.Spans)
	}

	if !b.Partial || b.Duration != 3*time.Minute {
		t.Errorf("partial = %v, duration = %v; want true and only the three measurable minutes", b.Partial, b.Duration)
	}

	at := plan.At()
	if len(at) != 4 {
		t.Fatalf("captures to load = %d, want 4: the capture inside the second span stays unread", len(at))
	}

	got := plan.Assemble(loaded(snaps, at))
	if v := got[0].Rows[0].Values["reads"]; v != 30 {
		t.Errorf("reads = %d, want 30: both spans, none of the gap", v)
	}
}

func TestPlanAssembleDropsASpanWhoseCountersRegressed(t *testing.T) {
	reset := ptrTime(base)
	r := func(v int64) Row { return row("client backend", "relation", "normal", map[string]int64{"reads": v}) }

	// The headers show one continuous epoch; only the counters give the reset away.
	snaps := []Snapshot{
		snap(base, reset, r(10)),
		snap(base.Add(time.Minute), reset, r(20)),
		snap(base.Add(2*time.Minute), reset, r(5)),
	}

	plan := PlanBuckets(metasOf(snaps), 1)
	b := plan.Assemble(loaded(snaps, plan.At()))[0]

	if !b.Partial {
		t.Error("a span that cannot be measured must mark its bucket partial")
	}

	if b.Duration != 0 {
		t.Errorf("duration = %v, want the unmeasured span taken out of it", b.Duration)
	}

	if len(b.Rows) != 0 {
		t.Errorf("rows = %+v, want none", b.Rows)
	}
}

func TestPlanAssembleMeasuresEveryIntervalWhenTheyFitInThePoints(t *testing.T) {
	reset := ptrTime(base)
	r := func(v int64) Row { return row("client backend", "relation", "normal", map[string]int64{"reads": v}) }

	snaps := []Snapshot{
		snap(base, reset, r(1)),
		snap(base.Add(time.Minute), reset, r(3)),
		snap(base.Add(2*time.Minute), reset, r(6)),
	}

	plan := PlanBuckets(metasOf(snaps), 200)

	got := plan.Assemble(loaded(snaps, plan.At()))
	if len(got) != 2 {
		t.Fatalf("buckets = %d, want one per interval", len(got))
	}

	if got[0].Rows[0].Values["reads"] != 2 || got[1].Rows[0].Values["reads"] != 3 {
		t.Errorf("deltas = %d, %d; want 2, 3", got[0].Rows[0].Values["reads"], got[1].Rows[0].Values["reads"])
	}
}

func TestReduceGroupsAndFilters(t *testing.T) {
	rows := []Row{
		row("client backend", "relation", "normal", map[string]int64{"reads": 1}),
		row("autovacuum worker", "relation", "normal", map[string]int64{"reads": 2}),
		row("autovacuum worker", "relation", "vacuum", map[string]int64{"reads": 4}),
		row("client backend", "temp relation", "normal", map[string]int64{"reads": 8}),
	}

	byContext := Reduce(rows, Filter{}, GroupByContext)
	if len(byContext) != 2 {
		t.Fatalf("context groups = %d, want 2", len(byContext))
	}

	got := map[string]int64{}
	for _, r := range byContext {
		got[r.Context] = r.Values["reads"]
	}

	if got["normal"] != 11 || got["vacuum"] != 4 {
		t.Errorf("grouped reads = %v, want normal 11 / vacuum 4", got)
	}

	filtered := Reduce(rows, Filter{Object: "relation"}, GroupByBackendType)
	if len(filtered) != 2 {
		t.Fatalf("backend_type groups = %d, want 2", len(filtered))
	}

	for _, r := range filtered {
		if r.Context != "" || r.Object != "" {
			t.Errorf("grouped key keeps summed dimensions: %+v", r.Key)
		}
	}

	full := Reduce(rows, Filter{Context: "normal"}, GroupByFull)
	if len(full) != 3 {
		t.Errorf("full groups = %d, want 3", len(full))
	}
}
