//go:build integration

package repository

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/testinfra"
)

func TestGetTablesTopKBySize(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getTablesTopKBySize(ctx, vNum, pool, 10, defaultPgStatsView)
	require.NoError(t, err)
	require.NotEmpty(t, result, "fixture tables exceed the 500 KiB cutoff")
	assert.LessOrEqual(t, len(result), 10)

	for i := 1; i < len(result); i++ {
		assert.GreaterOrEqual(t, result[i-1].TotalBytes, result[i].TotalBytes,
			"tables should be sorted by total size descending")
	}

	var orders *dto.TableTopKBySize

	for i := range result {
		assert.NotEmpty(t, result[i].Table)
		assert.NotEmpty(t, result[i].Total)
		assert.Positive(t, result[i].TotalBytes)

		if result[i].Table == "public.orders" {
			orders = &result[i]
		}
	}

	require.NotNil(t, orders, "fixture 'orders' table should be among the largest")
	assert.Positive(t, orders.NIdx, "orders has indexes")
	assert.NotEmpty(t, orders.Bloat, "analyzed table should get a bloat estimate")

	// pg_locks spans the whole cluster and every database cloned from the fixture template
	// shares its relation OIDs, so an AccessExclusiveLock taken elsewhere must not hide
	// the table here — which is what made this test fail against a parallel locking test.
	t.Run("lock in another database", func(t *testing.T) {
		other := testinfra.IsolatePool(t)

		conn, err := other.Acquire(ctx)
		require.NoError(t, err)

		defer conn.Release()

		tx, err := conn.Begin(ctx)
		require.NoError(t, err)

		defer func() { _ = tx.Rollback(ctx) }()

		_, err = tx.Exec(ctx, "LOCK TABLE orders IN ACCESS EXCLUSIVE MODE")
		require.NoError(t, err)

		withLock, err := p.getTablesTopKBySize(ctx, vNum, pool, 10, defaultPgStatsView)
		require.NoError(t, err)

		found := slices.ContainsFunc(withLock, func(tbl dto.TableTopKBySize) bool {
			return tbl.Table == "public.orders"
		})
		assert.True(t, found, "a lock in another database must not hide public.orders")
	})

	// The fixture has fewer relations than the candidate floor, so the catalog prefilter
	// never cuts anything — pad past it to check the exact-size step still ranks correctly.
	t.Run("catalog prefilter binds", func(t *testing.T) {
		padded := testinfra.IsolatePool(t)
		pp := NewTestPgxPool(padded, zap.NewNop())

		for i := range 60 {
			_, err := padded.Exec(ctx, fmt.Sprintf("CREATE TABLE pad_%02d (id int)", i))
			require.NoError(t, err)
		}

		top, err := pp.getTablesTopKBySize(ctx, vNum, padded, 10, defaultPgStatsView)
		require.NoError(t, err)
		require.NotEmpty(t, top)
		assert.Equal(t, result[0].Table, top[0].Table)

		single, err := pp.getTablesTopKBySize(ctx, vNum, padded, 1, defaultPgStatsView)
		require.NoError(t, err)
		require.Len(t, single, 1)
		assert.Equal(t, result[0].Table, single[0].Table)
	})
}

func TestGetTablesCaching(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getTablesCaching(ctx, vNum, pool, 100, 0)
	require.NoError(t, err)
	// May be empty on fresh databases with no buffer cache activity

	// Verify field mapping and nullable hit rates
	for _, tbl := range result {
		assert.NotEmpty(t, tbl.Schema)
		assert.NotEmpty(t, tbl.Table)
		if tbl.HitRate != nil {
			assert.GreaterOrEqual(t, *tbl.HitRate, 0.0)
			assert.LessOrEqual(t, *tbl.HitRate, 1.0)
		}
		if tbl.IdxHitRate != nil {
			assert.GreaterOrEqual(t, *tbl.IdxHitRate, 0.0)
			assert.LessOrEqual(t, *tbl.IdxHitRate, 1.0)
		}
	}
}

func TestGetTablesHitRate(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getTablesHitRate(ctx, vNum, pool)
	require.NoError(t, err)

	// Aggregate query — 0 or 1 element
	assert.LessOrEqual(t, len(result), 1)

	if len(result) == 1 {
		assert.GreaterOrEqual(t, result[0].Rate, 0.0)
		assert.LessOrEqual(t, result[0].Rate, 1.0)
	}
}

func TestGetTablesPartitions(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getTablesPartitions(ctx, vNum, pool)
	require.NoError(t, err)

	// Fixture creates partitioned 'events' table with 2 partitions
	found := false
	for _, p := range result {
		if p.Parent == "events" {
			found = true
			assert.Equal(t, "public", p.ParentSchema)
			assert.Equal(t, int64(2), p.ChildsCount, "events should have 2 partitions")
			assert.Greater(t, p.ChildsSizeBytes, int64(0))
			assert.Greater(t, p.ChildsAvgSizeBytes, int64(0))
			break
		}
	}
	assert.True(t, found, "should find partitioned 'events' table from fixture")
}
