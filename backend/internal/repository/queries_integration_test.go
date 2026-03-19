//go:build integration

package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/testinfra"
)

func TestGetQueryStatsAvailable(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	available, err := p.getQueryStatsAvailable(ctx, vNum, pool)
	require.NoError(t, err)
	assert.True(t, available, "pg_stat_statements should be available (shared_preload_libraries)")
}

func TestGetQueryStatsEnabled(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	enabled, err := p.getQueryStatsEnabled(ctx, vNum, pool)
	require.NoError(t, err)
	assert.True(t, enabled, "pg_stat_statements should be enabled")
}

func TestGetQueryStatsReadable(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	readable, err := p.getQueryStatsReadable(ctx, vNum, pool)
	require.NoError(t, err)
	assert.True(t, readable, "pg_stat_statements should be readable")
}

func TestGetQueriesBlocked(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getQueriesBlocked(ctx, vNum, pool)
	require.NoError(t, err)
	// No blocked queries expected in test environment — just verify SQL executes
	assert.NotNil(t, result)

	// Verify field mapping if any blocked queries exist
	for _, q := range result {
		assert.NotZero(t, q.BlockedPid)
		assert.NotEmpty(t, q.BlockedUser)
		assert.NotZero(t, q.BlockingPid)
		assert.NotEmpty(t, q.BlockingUser)
	}
}

func TestGetQueriesRunning(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getQueriesRunning(ctx, vNum, pool, 3)
	require.NoError(t, err)
	// At least our own query should be visible as running
	// (though it may have already completed by the time we check)

	// Verify field mapping
	for _, q := range result {
		assert.NotZero(t, q.Pid)
		assert.NotEmpty(t, q.State)
		assert.NotEmpty(t, q.User)
		assert.NotEmpty(t, q.Query)
		assert.False(t, q.StartedAt.IsZero(), "started_at should be set")
	}
}

func TestGetQueriesTop10ByTime(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getQueriesTop10ByTime(ctx, vNum, pool)
	require.NoError(t, err)
	assert.NotEmpty(t, result, "should return top queries by time (fixture generates pg_stat_statements data)")

	assert.LessOrEqual(t, len(result), 10, "should return at most 10 queries")

	// Verify sorted by exec_time_ms DESC
	for i := 1; i < len(result); i++ {
		assert.GreaterOrEqual(t, result[i-1].ExecTimeMs, result[i].ExecTimeMs,
			"queries should be sorted by exec time descending")
	}

	// Verify field mapping
	for _, q := range result {
		assert.NotZero(t, q.QueryID)
		assert.NotEmpty(t, q.ExecTime)
		assert.Greater(t, q.ExecTimeMs, 0.0)
		assert.NotEmpty(t, q.QueryTrunc)
	}
}

func TestGetQueriesTop10ByWal(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getQueriesTop10ByWal(ctx, vNum, pool)
	require.NoError(t, err)
	// WAL stats may be empty if no writes occurred — verify SQL executes

	assert.LessOrEqual(t, len(result), 10, "should return at most 10 queries")

	// Verify sorted by wal_bytes DESC
	for i := 1; i < len(result); i++ {
		assert.GreaterOrEqual(t, result[i-1].WalBytes, result[i].WalBytes,
			"queries should be sorted by WAL bytes descending")
	}

	// Verify field mapping
	for _, q := range result {
		assert.NotZero(t, q.QueryID)
		assert.NotEmpty(t, q.QueryTrunc)
	}
}

func TestGetQueriesReport(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getQueriesReport(ctx, vNum, pool)
	require.NoError(t, err)
	assert.NotEmpty(t, result, "should return query report (fixture generates pg_stat_statements data)")

	// Verify field mapping — all nullable fields handled correctly
	for _, q := range result {
		assert.NotZero(t, q.QueryID)

		// Percentages should be in valid range when present
		if q.TotalTimePct != nil {
			assert.GreaterOrEqual(t, *q.TotalTimePct, 0.0)
			assert.LessOrEqual(t, *q.TotalTimePct, 100.0)
		}
		if q.CacheHitRatio != nil {
			assert.GreaterOrEqual(t, *q.CacheHitRatio, 0.0)
			assert.LessOrEqual(t, *q.CacheHitRatio, 100.0)
		}
		if q.Calls != nil {
			assert.Greater(t, *q.Calls, int64(0))
		}
	}
}
