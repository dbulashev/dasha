package hotobjects

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ts(h int) time.Time {
	return time.Date(2026, 7, 17, h, 0, 0, 0, time.UTC)
}

func tableRow(schema, name string, size int64, c Counters) AnchorRow {
	return AnchorRow{Kind: KindTable, Schema: schema, Object: name, SizeBytes: size, Counters: c} //nolint:exhaustruct
}

func TestDelta(t *testing.T) {
	d, ok := Delta(Counters{"a": 10, "b": 5}, Counters{"a": 15, "b": 5, "c": 3})
	require.True(t, ok)
	assert.Equal(t, Counters{"a": 5, "b": 0, "c": 3}, d)

	// Counter regression → recreated object, not measurable.
	_, ok = Delta(Counters{"a": 10}, Counters{"a": 9})
	assert.False(t, ok)
}

func anchorsOf(capturedAt time.Time, reset *time.Time, rows ...AnchorRow) map[string]AnchorRow {
	m := make(map[string]AnchorRow, len(rows))

	for _, r := range rows {
		r.CapturedAt = capturedAt
		r.StatsReset = reset
		m[Key(r.Kind, r.Schema, r.Object)] = r
	}

	return m
}

func TestBuildSnapshot_SumsHostsAndRanks(t *testing.T) {
	reset := ts(0)

	anchors := anchorsOf(ts(1), &reset,
		tableRow("public", "orders", 100, Counters{"seq_tup_read": 100, "idx_tup_fetch": 0}),
		tableRow("public", "users", 100, Counters{"seq_tup_read": 10}),
	)

	sampleH1 := HostSample{Instance: "h1", CapturedAt: ts(25), StatsReset: &reset, Rows: []AnchorRow{
		tableRow("public", "orders", 150, Counters{"seq_tup_read": 400, "idx_tup_fetch": 100}),
		tableRow("public", "users", 90, Counters{"seq_tup_read": 30}),
	}}
	sampleH2 := HostSample{Instance: "h2", CapturedAt: ts(25), StatsReset: &reset, Rows: []AnchorRow{
		tableRow("public", "orders", 140, Counters{"seq_tup_read": 1100, "idx_tup_fetch": 0}),
		tableRow("public", "users", 80, Counters{"seq_tup_read": 15}),
	}}

	anchorsH2 := anchorsOf(ts(1), &reset,
		tableRow("public", "orders", 100, Counters{"seq_tup_read": 100}),
		tableRow("public", "users", 100, Counters{"seq_tup_read": 10}),
	)

	snap := BuildSnapshot("c1", "db", ts(25), []BuildInput{
		{Sample: sampleH1, Anchors: anchors},
		{Sample: sampleH2, Anchors: anchorsH2},
	}, nil, 1)

	// orders: (400-100)+(100-0) on h1 + (1100-100) on h2 = 1400; users: 20+5=25.
	var readsTop []TopEntry

	for _, e := range snap.Top {
		if e.Class == ClassReads {
			readsTop = append(readsTop, e)
		}
	}

	require.Len(t, readsTop, 1, "topN=1 keeps only the leader")
	assert.Equal(t, "orders", readsTop[0].Object)
	assert.EqualValues(t, 1400, RankKey(KindTable, ClassReads, readsTop[0].Delta))
	assert.EqualValues(t, 150, readsTop[0].SizeBytes, "largest copy across hosts")
	assert.Len(t, readsTop[0].PerHost, 2)

	// Tail histogram holds users with key 25; coverage = 1400/1425.
	hist := snap.Histograms[ClassReads].Tables
	require.NotNil(t, hist)
	assert.Equal(t, 1, hist.Count)
	assert.EqualValues(t, 25, hist.Sum)
	assert.InDelta(t, 1400.0/1425.0, snap.Coverage[ClassReads].Tables, 1e-9)

	// Windows recorded per host and complete.
	require.Len(t, snap.Windows, 2)
	assert.True(t, snap.Windows["h1"].Complete)
	assert.Equal(t, ts(1), snap.Windows["h1"].From)
}

func TestBuildSnapshot_EpochBreak(t *testing.T) {
	oldReset, newReset := ts(0), ts(12)

	anchors := anchorsOf(ts(1), &oldReset,
		tableRow("public", "orders", 100, Counters{"seq_tup_read": 100}),
	)

	sample := HostSample{Instance: "h1", CapturedAt: ts(25), StatsReset: &newReset, Rows: []AnchorRow{
		tableRow("public", "orders", 100, Counters{"seq_tup_read": 5}),
	}}

	snap := BuildSnapshot("c1", "db", ts(25), []BuildInput{{Sample: sample, Anchors: anchors}}, nil, 10)

	assert.False(t, snap.Windows["h1"].Complete, "stats reset breaks the epoch")
	assert.Empty(t, snap.Top, "no valid host delta → no top entries")
}

func TestBuildSnapshot_FirstRunAndRecreated(t *testing.T) {
	reset := ts(0)

	// First run: no anchors at all.
	sample := HostSample{Instance: "h1", CapturedAt: ts(25), StatsReset: &reset, Rows: []AnchorRow{
		tableRow("public", "orders", 100, Counters{"seq_tup_read": 100}),
	}}

	snap := BuildSnapshot("c1", "db", ts(25), []BuildInput{{Sample: sample, Anchors: nil}}, nil, 10)
	assert.False(t, snap.Windows["h1"].Complete)
	assert.Empty(t, snap.Top)

	// Recreated object: counter went down under an intact epoch.
	anchors := anchorsOf(ts(1), &reset,
		tableRow("public", "orders", 100, Counters{"seq_tup_read": 500}),
		tableRow("public", "users", 100, Counters{"seq_tup_read": 10}),
	)
	sample = HostSample{Instance: "h1", CapturedAt: ts(25), StatsReset: &reset, Rows: []AnchorRow{
		tableRow("public", "orders", 100, Counters{"seq_tup_read": 5}),
		tableRow("public", "users", 100, Counters{"seq_tup_read": 60}),
	}}

	snap = BuildSnapshot("c1", "db", ts(25), []BuildInput{{Sample: sample, Anchors: anchors}}, nil, 10)

	assert.True(t, snap.Windows["h1"].Complete)
	require.Len(t, topOf(snap, ClassReads), 1, "recreated orders is skipped, users stays")
	assert.Equal(t, "users", topOf(snap, ClassReads)[0].Object)
}

func topOf(s Snapshot, class Class) []TopEntry {
	var out []TopEntry

	for _, e := range s.Top {
		if e.Class == class && e.Kind == KindTable {
			out = append(out, e)
		}
	}

	return out
}

func TestBuildSnapshot_ZeroActivityNeverTops(t *testing.T) {
	reset := ts(0)

	anchors := anchorsOf(ts(1), &reset,
		tableRow("public", "busy", 100, Counters{"seq_tup_read": 10}),
		tableRow("public", "idle_a", 100, Counters{"seq_tup_read": 5}),
		tableRow("public", "idle_b", 100, Counters{"seq_tup_read": 5}),
	)

	sample := HostSample{Instance: "h1", CapturedAt: ts(25), StatsReset: &reset, Rows: []AnchorRow{
		tableRow("public", "busy", 100, Counters{"seq_tup_read": 60}),
		tableRow("public", "idle_a", 100, Counters{"seq_tup_read": 5}),
		tableRow("public", "idle_b", 100, Counters{"seq_tup_read": 5}),
	}}

	// topN is larger than the number of active objects: the idle ones must NOT
	// pad the top ("in the hot top" while doing nothing) — they belong to the
	// tail histogram.
	snap := BuildSnapshot("c1", "db", ts(25), []BuildInput{{Sample: sample, Anchors: anchors}}, nil, 10)

	top := topOf(snap, ClassReads)
	require.Len(t, top, 1)
	assert.Equal(t, "busy", top[0].Object)

	hist := snap.Histograms[ClassReads].Tables
	require.NotNil(t, hist)
	assert.Equal(t, 2, hist.Count, "idle objects end up in the tail")
}

func TestBuildSnapshot_HostsMissing(t *testing.T) {
	snap := BuildSnapshot("c1", "db", ts(25), nil, []string{"h2"}, 10)
	assert.Equal(t, []string{"h2"}, snap.HostsMissing)
}

func TestDeciles(t *testing.T) {
	assert.Empty(t, deciles(nil))

	vals := []float64{10, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	d := deciles(vals)
	require.Len(t, d, 9)
	assert.Equal(t, []float64{1, 2, 3, 4, 5, 6, 7, 8, 9}, d)
}

func TestHistogramPercentile(t *testing.T) {
	h := &Histogram{Deciles: []float64{1, 2, 3, 4, 5, 6, 7, 8, 9}, Count: 10, Sum: 55, Max: 10}

	assert.InDelta(t, 0.0, h.Percentile(0.5), 1e-9)
	assert.InDelta(t, 0.4, h.Percentile(5), 1e-9, "strict: a tie with the P50 decile does not count")
	assert.InDelta(t, 0.9, h.Percentile(100), 1e-9)

	var nilH *Histogram

	assert.Zero(t, nilH.Percentile(5))
}

func TestHistogramPercentile_IdleTail(t *testing.T) {
	// A quiet database: every tail object idle, all-zero deciles. An idle
	// object must NOT read as "hotter than 90%"; any real activity still does.
	h := &Histogram{Deciles: []float64{0, 0, 0, 0, 0, 0, 0, 0, 0}, Count: 10, Sum: 0, Max: 0}

	assert.Zero(t, h.Percentile(0))
	assert.InDelta(t, 0.9, h.Percentile(5), 1e-9)
}

func TestRankKey_IndexClasses(t *testing.T) {
	c := Counters{"idx_tup_read": 7, "idx_blks_read": 3}
	assert.EqualValues(t, 7, RankKey(KindIndex, ClassReads, c))
	assert.EqualValues(t, 3, RankKey(KindIndex, ClassIO, c))
	assert.Zero(t, RankKey(KindIndex, ClassWrites, c), "indexes have no writes class")
}
