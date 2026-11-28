//go:build integration

package repository

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/statlock"
	"github.com/dbulashev/dasha/internal/testinfra"
)

func TestGetLockSample_UnsupportedVersion(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	if vNum >= statlock.MinVersionNum {
		t.Skip("pg_stat_lock exists on PG19+")
	}

	_, err = p.getLockSample(ctx, vNum, pool)
	require.ErrorIs(t, err, statlock.ErrUnsupportedVersion)
}

// The FR-1 acceptance scenario: a lock wait between two captures shows up as a
// delta, and pg_stat_reset_shared('lock') breaks the series. Not parallel: the
// reset is cluster-wide.
func TestGetLockSample_DeltaAndReset(t *testing.T) {
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	if vNum < statlock.MinVersionNum {
		t.Skip("pg_stat_lock requires PG19+")
	}

	first, err := p.getLockSample(ctx, vNum, pool)
	require.NoError(t, err)
	require.NotEmpty(t, first.Rows, "pg_stat_lock reports every lock type")
	assert.Positive(t, first.DeadlockTimeoutMs)
	assert.Positive(t, first.MaxLocksPerTransaction)
	require.NotNil(t, first.StatsReset)

	types := map[string]struct{}{}
	for _, r := range first.Rows {
		types[r.LockType] = struct{}{}
	}

	require.Contains(t, types, "relation")
	assert.Len(t, types, len(first.Rows), "one row per lock type")

	_, err = pool.Exec(ctx, "CREATE TABLE lock_stat_probe (id int)")
	require.NoError(t, err)

	holder, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = holder.Rollback(ctx) }()

	_, err = holder.Exec(ctx, "LOCK TABLE lock_stat_probe IN ACCESS EXCLUSIVE MODE")
	require.NoError(t, err)

	waiter, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer waiter.Release()

	// Only waits longer than deadlock_timeout are counted.
	_, err = waiter.Exec(ctx, "SET deadlock_timeout = '10ms'")
	require.NoError(t, err)

	waitDone := make(chan error, 1)

	go func() {
		_, err := waiter.Exec(ctx, "SELECT count(*) FROM lock_stat_probe")
		waitDone <- err
	}()

	require.Eventually(t, func() bool {
		var waiting bool

		err := pool.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM pg_locks
			               WHERE relation = 'lock_stat_probe'::regclass AND NOT granted)`).Scan(&waiting)

		return err == nil && waiting
	}, 5*time.Second, 20*time.Millisecond)

	time.Sleep(200 * time.Millisecond)
	require.NoError(t, holder.Rollback(ctx))
	require.NoError(t, <-waitDone)

	// Without it the waiter defers its flush by up to PGSTAT_IDLE_INTERVAL
	// (10 s) when it flushed less than a second ago.
	_, err = waiter.Exec(ctx, "SELECT pg_stat_force_next_flush()")
	require.NoError(t, err)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		second, err := p.getLockSample(ctx, vNum, pool)
		if !assert.NoError(c, err) {
			return
		}

		iv, ok := statlock.Delta(*first, *second)
		if !assert.True(c, ok, "interval broken: first %+v, second %+v", first.Rows, second.Rows) {
			return
		}

		assert.Positive(c, relationWaits(iv.Rows), "relation waits delta, rows %+v", iv.Rows)
	}, 5*time.Second, 100*time.Millisecond)

	second, err := p.getLockSample(ctx, vNum, pool)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, "SELECT pg_stat_reset_shared('lock')")
	require.NoError(t, err)

	third, err := p.getLockSample(ctx, vNum, pool)
	require.NoError(t, err)
	require.NotNil(t, third.StatsReset)
	assert.True(t, third.StatsReset.After(*second.StatsReset), "the reset moves stats_reset forward")

	_, ok := statlock.Delta(*second, *third)
	assert.False(t, ok, "the interval spanning the reset is a gap")
}

func relationWaits(rows []statlock.Row) int64 {
	for _, r := range rows {
		if r.LockType == "relation" {
			return r.Waits
		}
	}

	return 0
}
