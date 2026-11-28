package http

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/repository"
	"github.com/dbulashev/dasha/internal/statlock"
)

type lockStatsRepo struct {
	repository.Repository

	vNum int
	err  error
}

func (r *lockStatsRepo) GetServerVersionNum(context.Context, string, string) (int, error) {
	return r.vNum, r.err
}

type lockStatsStore struct {
	snaps []statlock.Snapshot
}

func (f *lockStatsStore) GetLockSnapshotMetas(context.Context, string, string, time.Time, time.Time) ([]statlock.Meta, error) {
	out := make([]statlock.Meta, len(f.snaps))
	for i, s := range f.snaps {
		out[i] = s.Meta()
	}

	return out, nil
}

func (f *lockStatsStore) GetLockSnapshotsAt(context.Context, string, string, []time.Time) ([]statlock.Snapshot, error) {
	return f.snaps, nil
}

func (f *lockStatsStore) LockSnapshotRange(context.Context, string, string) (*time.Time, *time.Time, error) {
	if len(f.snaps) == 0 {
		return nil, nil, nil
	}

	return &f.snaps[0].CapturedAt, &f.snaps[len(f.snaps)-1].CapturedAt, nil
}

var lockStatsBase = time.Date(2026, 11, 28, 3, 0, 0, 0, time.UTC)

func lockStatsRequest() serverhttp.GetLockStatsHistoryRequestObject {
	return serverhttp.GetLockStatsHistoryRequestObject{
		Params: serverhttp.GetLockStatsHistoryParams{ //nolint:exhaustruct
			ClusterName: "c1",
			Instance:    "h1",
			From:        lockStatsBase,
			To:          lockStatsBase.Add(time.Hour),
		},
	}
}

func lockStatsHandlers(repo *lockStatsRepo, store *lockStatsStore) *Handlers {
	return &Handlers{repo: repo, lockHistory: statlock.NewHistoryReader(store)} //nolint:exhaustruct
}

func TestGetLockStatsHistoryWithoutStorage(t *testing.T) {
	t.Parallel()

	h := &Handlers{repo: &lockStatsRepo{vNum: 190001}} //nolint:exhaustruct

	resp, err := h.GetLockStatsHistory(t.Context(), lockStatsRequest())
	require.NoError(t, err)

	got, ok := resp.(serverhttp.GetLockStatsHistory501JSONResponse)
	require.True(t, ok, "got %T", resp)
	assert.Equal(t, serverhttp.FeatureUnavailableNoStorage, got.Reason)
}

func TestGetLockStatsHistoryUnsupportedVersion(t *testing.T) {
	t.Parallel()

	h := lockStatsHandlers(&lockStatsRepo{vNum: 180004}, &lockStatsStore{})

	resp, err := h.GetLockStatsHistory(t.Context(), lockStatsRequest())
	require.NoError(t, err)

	got, ok := resp.(serverhttp.GetLockStatsHistory501JSONResponse)
	require.True(t, ok, "got %T", resp)
	assert.Equal(t, serverhttp.FeatureUnavailableUnsupportedVersion, got.Reason)
}

func TestGetLockStatsHistoryUnknownHost(t *testing.T) {
	t.Parallel()

	h := lockStatsHandlers(&lockStatsRepo{err: repository.ErrNotFound}, &lockStatsStore{})

	resp, err := h.GetLockStatsHistory(t.Context(), lockStatsRequest())
	require.NoError(t, err)

	_, ok := resp.(serverhttp.GetLockStatsHistory404Response)
	assert.True(t, ok, "got %T", resp)
}

// Not captured yet is not the same as not supported.
func TestGetLockStatsHistoryEmptyWindow(t *testing.T) {
	t.Parallel()

	h := lockStatsHandlers(&lockStatsRepo{vNum: 190001}, &lockStatsStore{})

	resp, err := h.GetLockStatsHistory(t.Context(), lockStatsRequest())
	require.NoError(t, err)

	got, ok := resp.(serverhttp.GetLockStatsHistory200JSONResponse)
	require.True(t, ok, "got %T", resp)
	assert.Empty(t, got.Series)
	assert.Nil(t, got.Meta.DeadlockTimeoutMs)
	assert.Nil(t, got.Meta.EarliestAt)
}

func lockStatsSnap(at time.Time, deadlockMs int, rows ...statlock.Row) statlock.Snapshot {
	return statlock.Snapshot{
		CapturedAt:             at,
		VersionNum:             190001,
		DeadlockTimeoutMs:      deadlockMs,
		MaxLocksPerTransaction: 64,
		Rows:                   rows,
	}
}

func TestGetLockStatsHistoryServesStoredHistoryOfAnUnreachableHost(t *testing.T) {
	t.Parallel()

	store := &lockStatsStore{snaps: []statlock.Snapshot{
		lockStatsSnap(lockStatsBase, 1000, statlock.Row{LockType: "relation", Waits: 1}),
		lockStatsSnap(lockStatsBase.Add(5*time.Minute), 1000, statlock.Row{LockType: "relation", Waits: 4}),
	}}
	h := lockStatsHandlers(&lockStatsRepo{err: errors.New("connection refused")}, store)

	resp, err := h.GetLockStatsHistory(t.Context(), lockStatsRequest())
	require.NoError(t, err)

	got, ok := resp.(serverhttp.GetLockStatsHistory200JSONResponse)
	require.True(t, ok, "got %T", resp)
	require.Len(t, got.Series, 1)
	assert.EqualValues(t, 3, got.Series[0].Points[0].Waits)
}

func TestLockStatsSeriesKeepsOnePointPerBucket(t *testing.T) {
	t.Parallel()

	buckets := []statlock.Bucket{
		{
			From: lockStatsBase, To: lockStatsBase.Add(time.Minute), Duration: time.Minute,
			Rows: []statlock.Row{{LockType: "relation", Waits: 2, WaitTimeMs: 3000}},
		},
		{From: lockStatsBase.Add(time.Minute), To: lockStatsBase.Add(2 * time.Minute), Partial: true},
		{
			From: lockStatsBase.Add(2 * time.Minute), To: lockStatsBase.Add(3 * time.Minute), Duration: time.Minute,
			Rows: []statlock.Row{{LockType: "advisory", Waits: 1}},
		},
	}

	series := lockStatsSeries(buckets)
	require.Len(t, series, 2)

	for _, s := range series {
		require.Len(t, s.Points, len(buckets), s.Locktype)
		assert.False(t, s.Points[1].Complete, "the partial bucket is a gap in every series")
	}

	advisory, relation := series[0], series[1]
	assert.Equal(t, "advisory", advisory.Locktype, "series are ordered by lock type, not by first appearance")
	assert.Equal(t, "relation", relation.Locktype)

	assert.EqualValues(t, 3000, relation.Points[0].WaitTimeMs)
	assert.True(t, relation.Points[0].Complete)
	assert.InDelta(t, 60, advisory.Points[2].DurationSeconds, 0.001)

	assert.False(t, relation.Points[2].Complete, "a lock type absent from a complete bucket was not measured")
	assert.False(t, advisory.Points[0].Complete)
}

func TestLockStatsMetaReportsSettingChanges(t *testing.T) {
	t.Parallel()

	metas := []statlock.Meta{
		lockStatsSnap(lockStatsBase, 1000).Meta(),
		lockStatsSnap(lockStatsBase.Add(5*time.Minute), 200).Meta(),
	}

	meta := lockStatsMeta("h1", nil, nil, metas)

	require.NotNil(t, meta.DeadlockTimeoutMs)
	assert.Equal(t, 200, *meta.DeadlockTimeoutMs, "the newest value is the one to name")
	assert.True(t, meta.DeadlockTimeoutChanged)
	assert.False(t, meta.MaxLocksPerTransactionChanged)
	assert.False(t, meta.VersionChanged)
}
