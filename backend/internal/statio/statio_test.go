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

func TestBucketizeSumsAdjacentIntervals(t *testing.T) {
	var intervals []Interval

	for i := range 10 {
		from := base.Add(time.Duration(i) * time.Minute)
		intervals = append(intervals, Interval{
			From:     from,
			To:       from.Add(time.Minute),
			Duration: time.Minute,
			Complete: true,
			Rows:     []Row{row("client backend", "relation", "normal", map[string]int64{"reads": 1})},
		})
	}

	buckets := Bucketize(intervals, 5)
	if len(buckets) != 5 {
		t.Fatalf("buckets = %d, want 5", len(buckets))
	}

	for _, b := range buckets {
		if b.Partial {
			t.Errorf("bucket %v..%v marked partial", b.From, b.To)
		}

		if b.Duration != 2*time.Minute {
			t.Errorf("bucket duration = %v, want 2m", b.Duration)
		}

		if got := b.Rows[0].Values["reads"]; got != 2 {
			t.Errorf("bucket reads = %d, want 2", got)
		}
	}
}

func TestBucketizeMarksPartialAndSumsOnlyCompleteIntervals(t *testing.T) {
	intervals := []Interval{
		{
			From: base, To: base.Add(time.Minute), Duration: time.Minute, Complete: true,
			Rows: []Row{row("client backend", "relation", "normal", map[string]int64{"reads": 5})},
		},
		{From: base.Add(time.Minute), To: base.Add(2 * time.Minute), Duration: time.Minute},
	}

	buckets := Bucketize(intervals, 1)
	if len(buckets) != 1 {
		t.Fatalf("buckets = %d, want 1", len(buckets))
	}

	b := buckets[0]
	if !b.Partial {
		t.Error("a bucket holding an incomplete interval must be partial")
	}

	if b.Duration != time.Minute {
		t.Errorf("duration = %v, want only the complete interval's minute", b.Duration)
	}

	if got := b.Rows[0].Values["reads"]; got != 5 {
		t.Errorf("reads = %d, want 5", got)
	}
}

func TestBucketizeKeepsIntervalsWhenFewerThanPoints(t *testing.T) {
	intervals := []Interval{
		{From: base, To: base.Add(time.Minute), Duration: time.Minute, Complete: true},
		{From: base.Add(time.Minute), To: base.Add(2 * time.Minute), Duration: time.Minute, Complete: true},
	}

	if got := len(Bucketize(intervals, 200)); got != 2 {
		t.Fatalf("buckets = %d, want one per interval", got)
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

func TestIntervalsPairsAdjacentSnapshots(t *testing.T) {
	reset := ptrTime(base)
	r := func(v int64) Row { return row("client backend", "relation", "normal", map[string]int64{"reads": v}) }

	got := Intervals([]Snapshot{
		snap(base, reset, r(1)),
		snap(base.Add(time.Minute), reset, r(3)),
		snap(base.Add(2*time.Minute), reset, r(6)),
	})

	if len(got) != 2 {
		t.Fatalf("intervals = %d, want 2", len(got))
	}

	if got[0].Rows[0].Values["reads"] != 2 || got[1].Rows[0].Values["reads"] != 3 {
		t.Errorf("deltas = %d, %d; want 2, 3", got[0].Rows[0].Values["reads"], got[1].Rows[0].Values["reads"])
	}
}
