//go:build integration

package storage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dbulashev/dasha/internal/statlock"
)

func lockTestSnapshot(at, reset time.Time, waits int64) statlock.Snapshot {
	return statlock.Snapshot{
		CapturedAt:             at,
		VersionNum:             190001,
		StatsReset:             &reset,
		DeadlockTimeoutMs:      1000,
		MaxLocksPerTransaction: 64,
		Rows: []statlock.Row{
			{LockType: "relation", Waits: waits, WaitTimeMs: waits * 100, FastpathExceeded: 1},
			{LockType: "advisory"},
		},
	}
}

func TestLockSnapshotRoundTrip(t *testing.T) {
	s := newIOTestStorage(t)
	ctx := t.Context()

	now := time.Now().UTC().Truncate(time.Second)
	reset := now.Add(-24 * time.Hour)

	require.NoError(t, s.InsertLockSnapshot(ctx, "c1", "h1", lockTestSnapshot(now.Add(-10*time.Minute), reset, 10)))
	require.NoError(t, s.InsertLockSnapshot(ctx, "c1", "h1", lockTestSnapshot(now.Add(-5*time.Minute), reset, 14)))

	metas, err := s.GetLockSnapshotMetas(ctx, "c1", "h1", now.Add(-time.Hour), now)
	require.NoError(t, err)
	require.Len(t, metas, 2)
	assert.Equal(t, 190001, metas[0].VersionNum)
	assert.Equal(t, 1000, metas[0].DeadlockTimeoutMs)
	assert.Equal(t, 64, metas[0].MaxLocksPerTransaction)
	require.NotNil(t, metas[0].StatsReset)
	assert.WithinDuration(t, reset, *metas[0].StatsReset, time.Second)

	plan := statlock.PlanBuckets(metas, 200)

	got, err := s.GetLockSnapshotsAt(ctx, "c1", "h1", plan.At())
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Len(t, got[0].Rows, 2)

	buckets := statlock.Assemble(plan, got)
	require.Len(t, buckets, 1)
	assert.False(t, buckets[0].Partial)
	assert.Equal(t, []statlock.Row{
		{LockType: "advisory"},
		{LockType: "relation", Waits: 4, WaitTimeMs: 400},
	}, buckets[0].Rows)
}

func TestGetLockSnapshotMetasIncludeTheBaselineBeforeTheWindow(t *testing.T) {
	s := newIOTestStorage(t)
	ctx := t.Context()

	now := time.Now().UTC().Truncate(time.Second)
	reset := now.Add(-24 * time.Hour)

	require.NoError(t, s.InsertLockSnapshot(ctx, "c1", "h1", lockTestSnapshot(now.Add(-30*time.Minute), reset, 1)))
	require.NoError(t, s.InsertLockSnapshot(ctx, "c1", "h1", lockTestSnapshot(now.Add(-5*time.Minute), reset, 2)))

	metas, err := s.GetLockSnapshotMetas(ctx, "c1", "h1", now.Add(-10*time.Minute), now)
	require.NoError(t, err)
	require.Len(t, metas, 2, "the capture before the window is the window's baseline")
	assert.True(t, metas[0].CapturedAt.Before(now.Add(-10*time.Minute)))
}

func TestLockSnapshotRange(t *testing.T) {
	s := newIOTestStorage(t)
	ctx := t.Context()

	now := time.Now().UTC().Truncate(time.Second)
	reset := now.Add(-24 * time.Hour)
	oldest, newest := now.Add(-20*time.Minute), now.Add(-time.Minute)

	require.NoError(t, s.InsertLockSnapshot(ctx, "c1", "h1", lockTestSnapshot(oldest, reset, 1)))
	require.NoError(t, s.InsertLockSnapshot(ctx, "c1", "h1", lockTestSnapshot(newest, reset, 2)))

	earliest, latest, err := s.LockSnapshotRange(ctx, "c1", "h1")
	require.NoError(t, err)
	require.NotNil(t, earliest)
	require.NotNil(t, latest)
	assert.WithinDuration(t, oldest, *earliest, time.Second)
	assert.WithinDuration(t, newest, *latest, time.Second)

	earliest, latest, err = s.LockSnapshotRange(ctx, "c1", "absent")
	require.NoError(t, err)
	assert.Nil(t, earliest)
	assert.Nil(t, latest)
}
