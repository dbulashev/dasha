//go:build integration

package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/testinfra"
)

func TestGetIndexesTopKBySize(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getIndexesTopKBySize(ctx, vNum, pool)
	require.NoError(t, err)
	assert.NotEmpty(t, result, "should return indexes")

	// Verify sorted by size_bytes DESC
	for i := 1; i < len(result); i++ {
		assert.GreaterOrEqual(t, result[i-1].SizeBytes, result[i].SizeBytes,
			"indexes should be sorted by size descending")
	}

	// Verify fields are populated
	for _, idx := range result {
		assert.NotEmpty(t, idx.Table)
		assert.NotEmpty(t, idx.Index)
		assert.NotEmpty(t, idx.Size)
	}
}

func TestGetIndexesBloat(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	// Query with large limit to get all results
	result, err := p.getIndexesBloat(ctx, vNum, pool, 100, 0)
	require.NoError(t, err)
	// Bloat may be empty on fresh data — just verify SQL executes and mapping works

	// Test pagination
	page1, err := p.getIndexesBloat(ctx, vNum, pool, 2, 0)
	require.NoError(t, err)

	page2, err := p.getIndexesBloat(ctx, vNum, pool, 2, 2)
	require.NoError(t, err)

	// Pages should not overlap (if enough data)
	if len(page1) == 2 && len(page2) > 0 {
		assert.NotEqual(t, page1[0].Index, page2[0].Index,
			"pagination should return different indexes")
	}

	// Verify field mapping
	for _, idx := range result {
		assert.NotEmpty(t, idx.Schema)
		assert.NotEmpty(t, idx.Table)
		assert.NotEmpty(t, idx.Index)
		assert.NotEmpty(t, idx.Definition)
	}
}

func TestGetIndexesBtreeOnArray(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getIndexesBtreeOnArray(ctx, vNum, pool)
	require.NoError(t, err)

	// Fixture creates idx_orders_tags (btree on integer[] column)
	found := false
	for _, idx := range result {
		if idx.Index == "idx_orders_tags" {
			found = true
			assert.Equal(t, "orders", idx.Table)
			break
		}
	}
	assert.True(t, found, "should detect idx_orders_tags as btree on array column")
}

func TestGetIndexesCaching(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getIndexesCaching(ctx, vNum, pool, 100, 0)
	require.NoError(t, err)
	assert.NotEmpty(t, result, "should return index caching stats")

	// Verify field mapping
	for _, idx := range result {
		assert.NotEmpty(t, idx.Schema)
		assert.NotEmpty(t, idx.Table)
		assert.NotEmpty(t, idx.Index)
		assert.GreaterOrEqual(t, idx.HitRate, 0.0, "hit rate should be >= 0")
		assert.LessOrEqual(t, idx.HitRate, 1.0, "hit rate should be <= 1")
	}

	// Test pagination
	page1, err := p.getIndexesCaching(ctx, vNum, pool, 2, 0)
	require.NoError(t, err)

	if len(page1) == 2 {
		page2, err := p.getIndexesCaching(ctx, vNum, pool, 2, 2)
		require.NoError(t, err)
		if len(page2) > 0 {
			assert.NotEqual(t, page1[0].Index, page2[0].Index,
				"pagination should return different indexes")
		}
	}
}

func TestGetIndexesHitRate(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getIndexesHitRate(ctx, vNum, pool)
	require.NoError(t, err)

	// Result is 0 or 1 element (aggregate query)
	assert.LessOrEqual(t, len(result), 1)

	if len(result) == 1 {
		assert.GreaterOrEqual(t, result[0].Rate, 0.0)
		assert.LessOrEqual(t, result[0].Rate, 1.0)
	}
}

func TestGetIndexesInvalidOrNotReady(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getIndexesInvalidOrNotReady(ctx, vNum, pool)
	require.NoError(t, err)

	// Fixture creates an invalid index (idx_orders_invalid)
	// It may or may not be present depending on whether the CONCURRENTLY trick worked
	if len(result) > 0 {
		for _, idx := range result {
			assert.NotEmpty(t, idx.Table)
			assert.NotEmpty(t, idx.IndexName)
			// At least one of these should be false for the index to appear
			assert.True(t, !idx.IsValid || !idx.IsReady,
				"index should be invalid or not ready")
		}
	}
}

func TestGetIndexesMissing(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getIndexesMissing(ctx, vNum, pool)
	require.NoError(t, err)
	// May be empty — the query filters for tables with <95% index usage and >=10K rows

	// Verify nullable PercentOfTimesIndexUsed mapping
	for _, idx := range result {
		assert.NotEmpty(t, idx.Table)
		assert.Greater(t, idx.EstimatedRows, int64(0))
		if idx.PercentOfTimesIndexUsed != nil {
			assert.GreaterOrEqual(t, *idx.PercentOfTimesIndexUsed, 0.0)
			assert.LessOrEqual(t, *idx.PercentOfTimesIndexUsed, 100.0)
		}
	}
}

func TestGetIndexesUnused(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	// Threshold is hardcoded as $1=100 in the repo method
	result, err := p.getIndexesUnused(ctx, vNum, pool, 100, 0)
	require.NoError(t, err)

	// Verify field mapping and threshold filter
	for _, idx := range result {
		assert.NotEmpty(t, idx.Schema)
		assert.NotEmpty(t, idx.Table)
		assert.NotEmpty(t, idx.Index)
		assert.LessOrEqual(t, idx.IndexScans, int64(100),
			"unused indexes should have idx_scan <= threshold")
	}

	// Test pagination
	if len(result) > 2 {
		page1, err := p.getIndexesUnused(ctx, vNum, pool, 2, 0)
		require.NoError(t, err)
		assert.Len(t, page1, 2)

		page2, err := p.getIndexesUnused(ctx, vNum, pool, 2, 2)
		require.NoError(t, err)
		if len(page2) > 0 {
			assert.NotEqual(t, page1[0].Index, page2[0].Index)
		}
	}
}

func TestGetIndexesUsage(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getIndexesUsage(ctx, vNum, pool, 100, 0)
	require.NoError(t, err)
	assert.NotEmpty(t, result, "should return index usage stats")

	// Verify nullable PercentOfTimesIndexUsed mapping
	for _, idx := range result {
		assert.NotEmpty(t, idx.Table)
		if idx.PercentOfTimesIndexUsed != nil {
			assert.GreaterOrEqual(t, *idx.PercentOfTimesIndexUsed, 0.0)
			assert.LessOrEqual(t, *idx.PercentOfTimesIndexUsed, 100.0)
		}
	}

	// Test pagination
	page1, err := p.getIndexesUsage(ctx, vNum, pool, 2, 0)
	require.NoError(t, err)

	if len(page1) == 2 {
		page2, err := p.getIndexesUsage(ctx, vNum, pool, 2, 2)
		require.NoError(t, err)
		if len(page2) > 0 {
			assert.NotEqual(t, page1[0].Table, page2[0].Table)
		}
	}
}

func TestGetIndexesSimilar1(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getIndexesSimilar1(ctx, vNum, pool)
	require.NoError(t, err)
	// May be empty — depends on index structure matching similar_1 criteria

	for _, idx := range result {
		assert.NotEmpty(t, idx.Table)
		assert.NotEmpty(t, idx.I1UniqueIndexName)
		assert.NotEmpty(t, idx.I2IndexName)
	}
}

func TestGetIndexesSimilar2(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getIndexesSimilar2(ctx, vNum, pool)
	require.NoError(t, err)

	for _, idx := range result {
		assert.NotEmpty(t, idx.Table)
		assert.NotEmpty(t, idx.FkName)
	}
}

func TestGetIndexesSimilar3(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getIndexesSimilar3(ctx, vNum, pool)
	require.NoError(t, err)

	for _, idx := range result {
		assert.NotEmpty(t, idx.Table)
		assert.NotEmpty(t, idx.I1IndexName)
		assert.NotEmpty(t, idx.I2IndexName)
	}
}

func TestGetIndexesAllScans(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getIndexesAllScans(ctx, vNum, pool)
	require.NoError(t, err)
	assert.NotEmpty(t, result, "should return all non-unique indexes with scan counts")

	for _, idx := range result {
		assert.NotEmpty(t, idx.Schema)
		assert.NotEmpty(t, idx.Table)
		assert.NotEmpty(t, idx.Index)
		assert.GreaterOrEqual(t, idx.IndexScans, int64(0))
		assert.GreaterOrEqual(t, idx.SizeBytes, int64(0))
	}
}

func TestSortIndexesUnusedBySize(t *testing.T) {
	t.Parallel()

	items := []dto.IndexUnused{
		{Index: "a", SizeBytes: 100},
		{Index: "c", SizeBytes: 300},
		{Index: "b", SizeBytes: 200},
		{Index: "d", SizeBytes: 200},
	}

	sortIndexesUnusedBySize(items)

	// Sorted by size DESC, then by name ASC for equal sizes
	assert.Equal(t, int64(300), items[0].SizeBytes)
	assert.Equal(t, "b", items[1].Index)
	assert.Equal(t, "d", items[2].Index)
	assert.Equal(t, int64(100), items[3].SizeBytes)
}
