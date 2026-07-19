//go:build integration

package storage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/hotobjects"
	"github.com/dbulashev/dasha/internal/testinfra"
)

// newHotTestStorage returns a Storage with the hot-objects tables and the
// current day's partitions created.
func newHotTestStorage(t *testing.T) *Storage {
	t.Helper()

	pool := testinfra.IsolatePool(t)
	ctx := t.Context()

	for _, ddl := range []string{createHotAnchorSQL, createHotSnapshotSQL, createHotSnapshotIdxSQL, createHotTopSQL, createHotTopObjectIdxSQL} {
		_, err := pool.Exec(ctx, ddl)
		require.NoError(t, err, "hot DDL")
	}

	s := &Storage{pool: pool, ddlPool: pool, logger: zap.NewNop()}

	require.NoError(t, s.ensureHotPartitions(ctx, time.Now().UTC()))

	return s
}

func TestHotAnchors_UpsertReadPrune(t *testing.T) {
	s := newHotTestStorage(t)
	ctx := t.Context()

	now := time.Now().UTC()
	reset := now.Add(-24 * time.Hour)

	rows := []hotobjects.AnchorRow{
		{Instance: "h1", Kind: hotobjects.KindTable, Schema: "public", Object: "orders",
			CapturedAt: now, StatsReset: &reset, SizeBytes: 1000,
			Counters: hotobjects.Counters{"seq_tup_read": 10, "n_tup_ins": 5}},
		{Instance: "h1", Kind: hotobjects.KindIndex, Schema: "public", Object: "orders_pkey", TableName: "orders",
			CapturedAt: now, StatsReset: &reset, SizeBytes: 500,
			Counters: hotobjects.Counters{"idx_tup_read": 7}},
	}

	require.NoError(t, s.UpsertHotAnchors(ctx, "c1", "h1", "db", now, rows))

	got, err := s.GetHotAnchors(ctx, "c1", "h1", "db")
	require.NoError(t, err)
	require.Len(t, got, 2)

	a := got[hotobjects.Key(hotobjects.KindTable, "public", "orders")]
	assert.EqualValues(t, 10, a.Counters["seq_tup_read"])
	assert.EqualValues(t, 1000, a.SizeBytes)

	// Second upsert without the index: the index anchor must be pruned
	// (object disappeared from the target DB).
	later := now.Add(time.Hour)
	rows[0].Counters["seq_tup_read"] = 25

	require.NoError(t, s.UpsertHotAnchors(ctx, "c1", "h1", "db", later, rows[:1]))

	got, err = s.GetHotAnchors(ctx, "c1", "h1", "db")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.EqualValues(t, 25, got[hotobjects.Key(hotobjects.KindTable, "public", "orders")].Counters["seq_tup_read"])

	// Per-object lookup across hosts.
	require.NoError(t, s.UpsertHotAnchors(ctx, "c1", "h2", "db", later, rows[:1]))

	byObj, err := s.GetHotAnchorsForObject(ctx, "c1", "db", hotobjects.KindTable, "public", "orders")
	require.NoError(t, err)
	assert.Len(t, byObj, 2)
}

func testSnapshot(capturedAt time.Time) hotobjects.Snapshot {
	return hotobjects.Snapshot{
		ClusterName: "c1",
		Database:    "db",
		CapturedAt:  capturedAt,
		Windows: map[string]hotobjects.HostWindow{
			"h1": {From: capturedAt.Add(-24 * time.Hour), To: capturedAt, Complete: true},
		},
		HostsMissing: nil,
		Coverage: map[hotobjects.Class]hotobjects.KindRatio{
			hotobjects.ClassReads: {Tables: 0.97, Indexes: 0.9},
		},
		Histograms: map[hotobjects.Class]hotobjects.KindHistogram{
			hotobjects.ClassReads: {Tables: &hotobjects.Histogram{
				Deciles: []float64{1, 2, 3, 4, 5, 6, 7, 8, 9}, Count: 40, Sum: 100, Max: 9,
			}},
		},
		Top: []hotobjects.TopEntry{
			{Rank: 1, Kind: hotobjects.KindTable, Class: hotobjects.ClassReads,
				Schema: "public", Object: "orders", SizeBytes: 1000,
				Delta:   hotobjects.Counters{"seq_tup_read": 500},
				PerHost: map[string]hotobjects.HostDelta{"h1": {Complete: true, Delta: hotobjects.Counters{"seq_tup_read": 500}}}},
			{Rank: 2, Kind: hotobjects.KindTable, Class: hotobjects.ClassReads,
				Schema: "public", Object: "users", SizeBytes: 800,
				Delta:   hotobjects.Counters{"seq_tup_read": 200},
				PerHost: map[string]hotobjects.HostDelta{"h1": {Complete: true, Delta: hotobjects.Counters{"seq_tup_read": 200}}}},
		},
	}
}

func TestHotSnapshot_InsertReadTopHistory(t *testing.T) {
	s := newHotTestStorage(t)
	ctx := t.Context()

	now := time.Now().UTC()

	id, err := s.InsertHotSnapshot(ctx, testSnapshot(now))
	require.NoError(t, err)

	// Latest snapshot.
	snap, err := s.GetHotSnapshot(ctx, "c1", "db", nil)
	require.NoError(t, err)
	require.NotNil(t, snap)
	assert.Equal(t, id, snap.ID)
	assert.InDelta(t, 0.97, snap.Coverage[hotobjects.ClassReads].Tables, 1e-9)
	require.NotNil(t, snap.Histograms[hotobjects.ClassReads].Tables)
	assert.Equal(t, 40, snap.Histograms[hotobjects.ClassReads].Tables.Count)

	// Unknown database → nil, not an error.
	none, err := s.GetHotSnapshot(ctx, "c1", "nope", nil)
	require.NoError(t, err)
	assert.Nil(t, none)

	// Top page.
	top, err := s.GetHotTop(ctx, snap.ID, snap.CapturedAt, hotobjects.KindTable, hotobjects.ClassReads, 10, 0)
	require.NoError(t, err)
	require.Len(t, top, 2)
	assert.Equal(t, "orders", top[0].Object)
	assert.EqualValues(t, 500, top[0].Delta["seq_tup_read"])
	assert.True(t, top[0].PerHost["h1"].Complete)

	// Ranks map.
	ranks, err := s.GetHotRanks(ctx, snap.ID, snap.CapturedAt, hotobjects.KindTable, hotobjects.ClassReads)
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"public.orders": 1, "public.users": 2}, ranks)

	// Object history.
	hist, err := s.GetHotObjectHistory(ctx, "c1", "db", hotobjects.KindTable, "public", "orders",
		now.Add(-time.Hour), now.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, hist, 1)
	assert.Equal(t, hotobjects.ClassReads, hist[0].Class)
	assert.Equal(t, 1, hist[0].Rank)

	// Dates list + debounce map.
	dates, err := s.ListHotSnapshotDates(ctx, "c1", "db", 10)
	require.NoError(t, err)
	assert.Len(t, dates, 1)

	last, err := s.LastHotSnapshotAt(ctx)
	require.NoError(t, err)
	assert.Contains(t, last, "c1/db")
}

func TestHotSnapshot_ByExactCapture(t *testing.T) {
	s := newHotTestStorage(t)
	ctx := t.Context()

	now := time.Now().UTC()
	_, err := s.InsertHotSnapshot(ctx, testSnapshot(now))
	require.NoError(t, err)

	// The exact capture time (as returned by ListHotSnapshotDates) hits...
	dates, err := s.ListHotSnapshotDates(ctx, "c1", "db", 1)
	require.NoError(t, err)
	require.Len(t, dates, 1)

	snap, err := s.GetHotSnapshot(ctx, "c1", "db", &dates[0])
	require.NoError(t, err)
	require.NotNil(t, snap)

	// ...and any other time misses.
	other := now.Add(-time.Minute)
	snap, err = s.GetHotSnapshot(ctx, "c1", "db", &other)
	require.NoError(t, err)
	assert.Nil(t, snap)
}

func TestDropHotPartitionsBefore(t *testing.T) {
	s := newHotTestStorage(t)
	ctx := t.Context()

	old := time.Now().UTC().AddDate(0, 0, -400)
	require.NoError(t, s.ensureHotPartitions(ctx, old))

	// Old partition exists before, gone after.
	var exists bool
	name := "hot_snapshot_" + old.Format("20060102")
	require.NoError(t, s.pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, name).Scan(&exists))
	require.True(t, exists)

	require.NoError(t, s.DropHotPartitionsBefore(ctx, time.Now().UTC().AddDate(0, 0, -180)))

	require.NoError(t, s.pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, name).Scan(&exists))
	assert.False(t, exists)

	// Today's partition survives.
	name = "hot_snapshot_" + time.Now().UTC().Format("20060102")
	require.NoError(t, s.pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, name).Scan(&exists))
	assert.True(t, exists)
}
