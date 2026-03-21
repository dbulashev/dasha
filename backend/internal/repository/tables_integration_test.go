//go:build integration

package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/testinfra"
)

func TestGetTablesTopKBySize(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	// Ensure pg_class.relpages is up to date for size calculations
	_, _ = pool.Exec(ctx, "VACUUM ANALYZE orders")

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getTablesTopKBySize(ctx, vNum, pool, 10)
	require.NoError(t, err)
	assert.NotEmpty(t, result, "should return tables")
	assert.LessOrEqual(t, len(result), 10)

	// Verify sorted by total_bytes DESC
	for i := 1; i < len(result); i++ {
		assert.GreaterOrEqual(t, result[i-1].TotalBytes, result[i].TotalBytes,
			"tables should be sorted by size descending")
	}

	// Verify field mapping
	for _, tbl := range result {
		assert.NotEmpty(t, tbl.Table)
		assert.NotEmpty(t, tbl.Total)
		assert.GreaterOrEqual(t, tbl.TotalBytes, int64(0))
	}
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
