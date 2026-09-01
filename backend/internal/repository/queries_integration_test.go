//go:build integration

package repository

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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

	src := p.statsSource(ctx, pool)
	require.True(t, src.Present(), "pg_stat_statements should resolve as the source")

	available, name, err := p.getQueryStatsAvailable(ctx, vNum, pool, src)
	require.NoError(t, err)
	assert.True(t, available, "pg_stat_statements should be available (shared_preload_libraries)")
	assert.Equal(t, "pg_stat_statements", name)
}

func TestGetQueryStatsEnabled(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	enabled, err := p.getQueryStatsEnabled(ctx, vNum, pool, p.statsSource(ctx, pool).Name())
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

	var ownDB string

	require.NoError(t, pool.QueryRow(ctx, "SELECT current_database()").Scan(&ownDB))

	// Scoped to this test's own database: tests share the instance, and an
	// instance-wide read would pick up contention another test is holding.
	result, err := p.getQueriesBlocked(ctx, vNum, pool, &ownDB)
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

// TestGetQueriesBlockedInstanceWide creates contention in another database of
// the same instance and checks it is visible without a filter and invisible
// with one — the reason an auto-snapshot captures locks instance-wide.
func TestGetQueriesBlockedInstanceWide(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	var ownDB string

	require.NoError(t, pool.QueryRow(ctx, "SELECT current_database()").Scan(&ownDB))

	// A pool of its own: holding two long-lived connections in the shared admin
	// pool would starve the parallel tests that create databases through it.
	otherDB, err := pgxpool.New(ctx, tc.AdminDSN)
	require.NoError(t, err)

	defer otherDB.Close()

	// A table of its own too: the "postgres" database is shared.
	table := "dasha_lock_probe_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	_, err = otherDB.Exec(ctx, "CREATE TABLE "+table+" (id int PRIMARY KEY)")
	require.NoError(t, err)

	t.Cleanup(func() {
		// Through the shared pool: the dedicated one is closed by then.
		_, _ = tc.Admin.Exec(context.Background(), "DROP TABLE IF EXISTS "+table)
	})

	_, err = otherDB.Exec(ctx, "INSERT INTO "+table+" VALUES (1)")
	require.NoError(t, err)

	holder, err := otherDB.Acquire(ctx)
	require.NoError(t, err)

	defer holder.Release()

	_, err = holder.Exec(ctx, "BEGIN")
	require.NoError(t, err)
	_, err = holder.Exec(ctx, "SELECT id FROM "+table+" FOR UPDATE")
	require.NoError(t, err)

	bgCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	blockerDone := make(chan struct{})

	go func() {
		defer close(blockerDone)

		c, e := otherDB.Acquire(bgCtx)
		if e != nil {
			return
		}
		defer c.Release()

		_, _ = c.Exec(bgCtx, "BEGIN")
		_, _ = c.Exec(bgCtx, "SELECT id FROM "+table+" FOR UPDATE")
		_, _ = c.Exec(bgCtx, "ROLLBACK")
	}()

	// Read through our own database: pg_locks is instance-wide, so the wait in
	// "postgres" must show up, tagged with the database it happened in.
	require.Eventually(t, func() bool {
		rows, e := p.getQueriesBlocked(ctx, vNum, pool, nil)
		if e != nil {
			// Logged, not swallowed: a read that keeps erroring looks exactly
			// like contention that never appeared.
			t.Logf("instance-wide blocked read failed: %v", e)

			return false
		}

		for _, r := range rows {
			if r.BlockedDatabase == "postgres" {
				return true
			}
		}

		return false
	}, 5*time.Second, 100*time.Millisecond, "contention in another database should be visible instance-wide")

	scoped, err := p.getQueriesBlocked(ctx, vNum, pool, &ownDB)
	require.NoError(t, err)

	for _, r := range scoped {
		assert.Equal(t, ownDB, r.BlockedDatabase, "a database-scoped read must not leak other databases")
	}

	_, err = holder.Exec(ctx, "ROLLBACK")
	require.NoError(t, err)

	select {
	case <-blockerDone:
	case <-time.After(10 * time.Second):
		t.Fatal("blocker goroutine did not finish")
	}
}

// TestGetBlockedSessionCount creates real row-lock contention and verifies the
// background sampler detects it. This count is what arms an activity-spike
// snapshot: if it stops seeing contention, lock captures silently never happen.
func TestGetBlockedSessionCount(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	// No baseline assertion: the count is instance-wide now, and a parallel test
	// may legitimately be holding contention of its own. Only the rise this test
	// causes is its own to claim.
	holder, err := pool.Acquire(ctx)
	require.NoError(t, err)

	defer holder.Release()

	_, err = holder.Exec(ctx, "BEGIN")
	require.NoError(t, err)
	_, err = holder.Exec(ctx, "SELECT id FROM users ORDER BY id LIMIT 1 FOR UPDATE")
	require.NoError(t, err)

	// A bounded context so a pathological pool state fails fast instead of
	// hanging CI forever.
	bgCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	blockerDone := make(chan struct{})

	go func() {
		defer close(blockerDone)

		c, e := pool.Acquire(bgCtx)
		if e != nil {
			return
		}
		defer c.Release()

		_, _ = c.Exec(bgCtx, "BEGIN")
		_, _ = c.Exec(bgCtx, "SELECT id FROM users ORDER BY id LIMIT 1 FOR UPDATE")
		_, _ = c.Exec(bgCtx, "ROLLBACK")
	}()

	require.Eventually(t, func() bool {
		got, e := p.getBlockedSessionCount(ctx, pool)
		return e == nil && got >= 1
	}, 5*time.Second, 100*time.Millisecond, "blocked session should be detected")

	// Release the lock so the blocker proceeds and the goroutine exits.
	_, err = holder.Exec(ctx, "ROLLBACK")
	require.NoError(t, err)

	select {
	case <-blockerDone:
	case <-time.After(10 * time.Second):
		t.Fatal("blocker goroutine did not finish")
	}
}

func TestGetQueriesRunning(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getQueriesRunning(ctx, vNum, pool, 3, nil, "like", nil)
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

	result, err := p.getQueriesTop10ByTime(ctx, vNum, pool, nil)
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

		assert.GreaterOrEqual(t, q.IoPct, 0.0)
		assert.LessOrEqual(t, q.IoPct, 100.0)
		assert.GreaterOrEqual(t, q.CpuPct, 0.0)
		assert.LessOrEqual(t, q.CpuPct, 100.0)
		assert.InDelta(t, 100.0, q.IoPct+q.CpuPct, 0.01)
		assert.NotEmpty(t, q.IoCpuPct)
	}
}

func TestGetQueriesTop10ByWal(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getQueriesTop10ByWal(ctx, vNum, pool, nil)
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

	var ownDB string

	require.NoError(t, pool.QueryRow(ctx, "SELECT current_database()").Scan(&ownDB))

	// Statements are attributed to the database they ran in, and the fixture
	// workload ran in the template this database was cloned from — so give the
	// clone one statement of its own before reading the report.
	var marker int64

	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM generate_series(1, 1000)").Scan(&marker))

	result, err := p.getQueriesReport(ctx, vNum, pool, []string{}, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, result, "should return query report (fixture generates pg_stat_statements data)")

	seenOwn := false

	for _, q := range result {
		if q.Datname == ownDB {
			seenOwn = true
		}

		// A row's share of its own database can never be smaller than its share
		// of the instance the database is part of.
		if q.TotalTimePct != nil && q.TotalTimePctInstance != nil {
			assert.GreaterOrEqual(t, *q.TotalTimePct, *q.TotalTimePctInstance-1e-9)
		}
	}

	assert.True(t, seenOwn, "the connected database must be attributed in the report")

	// Verify field mapping — all nullable fields handled correctly
	for _, q := range result {
		assert.NotZero(t, q.QueryID)

		// Percentages should be in valid range when present. Time shares are
		// float8 division, so a lone row in its database can land an ULP over 100.
		if q.TotalTimePct != nil {
			assert.GreaterOrEqual(t, *q.TotalTimePct, 0.0)
			assert.LessOrEqual(t, *q.TotalTimePct, 100.0+1e-9)
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
