//go:build integration

package storage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/statio"
	"github.com/dbulashev/dasha/internal/testinfra"
)

// newIOTestStorage returns a Storage with io_snapshot and the current day's
// partition created.
func newIOTestStorage(t *testing.T) *Storage {
	t.Helper()

	pool := testinfra.IsolateEmptyPool(t)
	ctx := t.Context()

	for _, ddl := range []string{createIOSnapshotSQL, createIOSnapshotIdxSQL} {
		_, err := pool.Exec(ctx, ddl)
		require.NoError(t, err, "io DDL")
	}

	s := &Storage{pool: pool, ddlPool: pool, logger: zap.NewNop()}

	require.NoError(t, s.ensureIOPartitions(ctx, time.Now().UTC()))

	return s
}

func ioTestSnapshot(at time.Time, reset time.Time, reads int64) statio.Snapshot {
	opBytes := 8192

	return statio.Snapshot{
		CapturedAt:    at,
		VersionNum:    170004,
		OpBytes:       &opBytes,
		TrackIOTiming: true,
		StatsReset:    &reset,
		Rows: []statio.Row{
			{
				Key:    statio.Key{BackendType: "client backend", Object: "relation", Context: "normal"},
				Values: map[string]int64{"reads": reads, "hits": reads * 10},
			},
		},
	}
}

func TestIOSnapshotRoundTrip(t *testing.T) {
	s := newIOTestStorage(t)
	ctx := t.Context()

	now := time.Now().UTC().Truncate(time.Second)
	reset := now.Add(-24 * time.Hour)

	first := ioTestSnapshot(now.Add(-10*time.Minute), reset, 100)
	second := ioTestSnapshot(now.Add(-5*time.Minute), reset, 175)

	for _, snap := range []statio.Snapshot{first, second} {
		id, err := s.InsertIOSnapshot(ctx, "c1", "h1", snap)
		require.NoError(t, err)
		assert.NotEmpty(t, id)
	}

	got, err := s.GetIOSnapshots(ctx, "c1", "h1", now.Add(-time.Hour), now)
	require.NoError(t, err)
	require.Len(t, got, 2)

	assert.Equal(t, 170004, got[0].VersionNum)
	assert.True(t, got[0].TrackIOTiming)
	require.NotNil(t, got[0].OpBytes)
	assert.Equal(t, 8192, *got[0].OpBytes)
	require.NotNil(t, got[0].StatsReset)
	assert.WithinDuration(t, reset, *got[0].StatsReset, time.Second)
	require.Len(t, got[0].Rows, 1)
	assert.EqualValues(t, 100, got[0].Rows[0].Values["reads"])

	intervals := statio.Intervals(got)
	require.Len(t, intervals, 1)
	assert.True(t, intervals[0].Complete)
	assert.EqualValues(t, 75, intervals[0].Rows[0].Values["reads"])
	// PG 16/17 report op_bytes rather than byte counters; the domain derives them.
	assert.EqualValues(t, 75*8192, intervals[0].Rows[0].Values["read_bytes"])
}

// A window must start from the snapshot before it, otherwise its first interval
// would have no baseline and vanish.
func TestGetIOSnapshotsIncludesTheBaselineBeforeTheWindow(t *testing.T) {
	s := newIOTestStorage(t)
	ctx := t.Context()

	now := time.Now().UTC().Truncate(time.Second)
	reset := now.Add(-24 * time.Hour)

	_, err := s.InsertIOSnapshot(ctx, "c1", "h1", ioTestSnapshot(now.Add(-30*time.Minute), reset, 10))
	require.NoError(t, err)
	_, err = s.InsertIOSnapshot(ctx, "c1", "h1", ioTestSnapshot(now.Add(-5*time.Minute), reset, 20))
	require.NoError(t, err)

	got, err := s.GetIOSnapshots(ctx, "c1", "h1", now.Add(-10*time.Minute), now)
	require.NoError(t, err)
	require.Len(t, got, 2, "the snapshot before the window is the window's baseline")
	assert.True(t, got[0].CapturedAt.Before(now.Add(-10*time.Minute)))
}

func TestLastIOSnapshotAtAndRange(t *testing.T) {
	s := newIOTestStorage(t)
	ctx := t.Context()

	now := time.Now().UTC().Truncate(time.Second)
	reset := now.Add(-24 * time.Hour)

	oldest := now.Add(-20 * time.Minute)
	newest := now.Add(-time.Minute)

	_, err := s.InsertIOSnapshot(ctx, "c1", "h1", ioTestSnapshot(oldest, reset, 1))
	require.NoError(t, err)
	_, err = s.InsertIOSnapshot(ctx, "c1", "h1", ioTestSnapshot(newest, reset, 2))
	require.NoError(t, err)
	_, err = s.InsertIOSnapshot(ctx, "c1", "h2", ioTestSnapshot(newest, reset, 3))
	require.NoError(t, err)

	last, err := s.LastIOSnapshotAt(ctx)
	require.NoError(t, err)
	require.Contains(t, last, "c1/h1")
	require.Contains(t, last, "c1/h2")
	assert.WithinDuration(t, newest, last["c1/h1"], time.Second)

	earliest, latest, err := s.IOSnapshotRange(ctx, "c1", "h1")
	require.NoError(t, err)
	require.NotNil(t, earliest)
	require.NotNil(t, latest)
	assert.WithinDuration(t, oldest, *earliest, time.Second)
	assert.WithinDuration(t, newest, *latest, time.Second)

	// A host that was never captured has no range at all.
	earliest, latest, err = s.IOSnapshotRange(ctx, "c1", "absent")
	require.NoError(t, err)
	assert.Nil(t, earliest)
	assert.Nil(t, latest)
}

func TestDropIOPartitionsBefore(t *testing.T) {
	s := newIOTestStorage(t)
	ctx := t.Context()

	now := time.Now().UTC()
	old := now.AddDate(0, 0, -40)

	require.NoError(t, s.ensureIOPartitions(ctx, old))

	oldName := "io_snapshot_" + old.Format("20060102")
	curName := "io_snapshot_" + now.Format("20060102")

	require.NoError(t, s.DropIOPartitionsBefore(ctx, now.AddDate(0, 0, -30)))

	assert.False(t, ioPartitionExists(t, s, oldName), "partition beyond retention must be gone")
	assert.True(t, ioPartitionExists(t, s, curName), "today's partition must survive")
}

func ioPartitionExists(t *testing.T, s *Storage, name string) bool {
	t.Helper()

	var exists bool

	err := s.pool.QueryRow(t.Context(),
		`SELECT EXISTS (SELECT 1 FROM pg_class WHERE relname = $1)`, name).Scan(&exists)
	require.NoError(t, err)

	return exists
}
