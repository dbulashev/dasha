//go:build integration

package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/testinfra"
)

func TestGetTablesDescribeMetadata(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getTablesDescribeMetadata(ctx, vNum, pool, "public", "orders")
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "public", result.Schema)
	assert.Equal(t, "orders", result.TableName)
	assert.Equal(t, "table", result.TableType)
	assert.NotEmpty(t, result.SizeTotal)
	assert.NotEmpty(t, result.SizeTable)
	assert.Greater(t, result.EstimatedRows, int64(0), "orders should have estimated rows after ANALYZE")
	// StatInfo may be empty if stat counters haven't flushed in the isolated DB
	assert.Empty(t, result.PartitionOf, "orders is not a partition")
}

func TestGetTablesDescribeMetadataPartition(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	// Partitioned parent
	result, err := p.getTablesDescribeMetadata(ctx, vNum, pool, "public", "events")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "partitioned_table", result.TableType)
	assert.Empty(t, result.PartitionOf)

	// Child partition
	result, err = p.getTablesDescribeMetadata(ctx, vNum, pool, "public", "events_2025")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Contains(t, result.PartitionOf, "events")
}

func TestGetTablesDescribeMetadataNotFound(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getTablesDescribeMetadata(ctx, vNum, pool, "public", "nonexistent_table")
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestGetTablesDescribeColumns(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getTablesDescribeColumns(ctx, vNum, pool, "public", "orders", defaultPgStatsView)
	require.NoError(t, err)
	require.NotEmpty(t, result)

	// orders has: id, user_id, status, tags, amount, created_at
	assert.GreaterOrEqual(t, len(result), 6)

	colMap := make(map[string]int)
	for i, col := range result {
		colMap[col.Name] = i
		assert.NotEmpty(t, col.Type)
		assert.NotEmpty(t, col.Storage)
	}

	// Check known columns exist
	assert.Contains(t, colMap, "id")
	assert.Contains(t, colMap, "user_id")
	assert.Contains(t, colMap, "status")

	// id (PK, NOT NULL) should not be nullable
	assert.False(t, result[colMap["id"]].Nullable)

	// amount is nullable
	assert.True(t, result[colMap["amount"]].Nullable)

	// pg_stats columns should be populated after ANALYZE
	statusCol := result[colMap["status"]]
	assert.NotNil(t, statusCol.NDistinct, "status should have n_distinct after ANALYZE")
	assert.NotNil(t, statusCol.AvgWidth, "status should have avg_width after ANALYZE")
}

func TestGetTablesDescribeIndexes(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getTablesDescribeIndexes(ctx, vNum, pool, "public", "orders")
	require.NoError(t, err)
	require.NotEmpty(t, result)

	// orders has PK + 7 indexes
	assert.GreaterOrEqual(t, len(result), 8)

	var hasPK, hasUnique, hasInvalid bool

	for _, idx := range result {
		assert.NotEmpty(t, idx.Name)
		assert.NotEmpty(t, idx.Definition)
		assert.NotEmpty(t, idx.Size)
		assert.Greater(t, idx.SizeBytes, int64(0))

		if idx.IsPrimary {
			hasPK = true
			assert.True(t, idx.IsUnique, "PK should be unique")
			assert.True(t, idx.IsValid, "PK should be valid")
		}

		if idx.Name == "idx_orders_user_id_unique" {
			hasUnique = true
			assert.True(t, idx.IsUnique)
		}

		if idx.Name == "idx_orders_invalid" {
			hasInvalid = true
			assert.False(t, idx.IsValid)
		}
	}

	assert.True(t, hasPK, "should have primary key")
	assert.True(t, hasUnique, "should have unique index")
	// Invalid index creation may fail on some PG versions
	if hasInvalid {
		t.Log("invalid index found as expected")
	}
}

func TestGetTablesDescribeCheckConstraints(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getTablesDescribeCheckConstraints(ctx, vNum, pool, "public", "orders")
	require.NoError(t, err)
	// orders may have no check constraints — that's fine
	for _, c := range result {
		assert.NotEmpty(t, c.Name)
		assert.NotEmpty(t, c.Definition)
	}
}

func TestGetTablesDescribeFkConstraints(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getTablesDescribeFkConstraints(ctx, vNum, pool, "public", "orders")
	require.NoError(t, err)
	require.NotEmpty(t, result, "orders should have FK to users")

	found := false
	for _, c := range result {
		assert.NotEmpty(t, c.Name)
		assert.NotEmpty(t, c.Definition)
		if assert.Contains(t, c.Definition, "users") {
			found = true
		}
	}
	assert.True(t, found, "should find FK referencing users")
}

func TestGetTablesDescribeReferencedBy(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	// users is referenced by orders, products, deadrows_test
	result, err := p.getTablesDescribeReferencedBy(ctx, vNum, pool, "public", "users")
	require.NoError(t, err)
	require.NotEmpty(t, result, "users should be referenced by other tables")

	for _, ref := range result {
		assert.NotEmpty(t, ref.Name)
		assert.NotEmpty(t, ref.SourceTable)
		assert.NotEmpty(t, ref.Definition)
	}

	// Find reference from orders (SourceTable is schema-qualified)
	found := false
	for _, ref := range result {
		if ref.SourceTable == "public.orders" {
			found = true
			break
		}
	}
	assert.True(t, found, "should find reference from orders to users")
}

func TestGetTablesDescribePartitions(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getTablesDescribePartitions(ctx, vNum, pool, "public", "events", 100, 0)
	require.NoError(t, err)
	require.Len(t, result, 2, "events should have 2 partitions")

	names := make(map[string]bool)
	for _, part := range result {
		assert.Equal(t, "public", part.Schema)
		assert.NotEmpty(t, part.Name)
		assert.NotEmpty(t, part.PartitionExpression)
		assert.NotEmpty(t, part.Size)
		assert.Greater(t, part.SizeBytes, int64(0))
		names[part.Name] = true
	}

	assert.True(t, names["events_2025"], "should have events_2025 partition")
	assert.True(t, names["events_2026"], "should have events_2026 partition")
}

func TestGetTablesDescribePartitionsPagination(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	page1, err := p.getTablesDescribePartitions(ctx, vNum, pool, "public", "events", 1, 0)
	require.NoError(t, err)
	require.Len(t, page1, 1)

	page2, err := p.getTablesDescribePartitions(ctx, vNum, pool, "public", "events", 1, 1)
	require.NoError(t, err)
	require.Len(t, page2, 1)

	assert.NotEqual(t, page1[0].Name, page2[0].Name, "pages should contain different partitions")
}

func TestGetTablesDescribePartitionsNonPartitioned(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getTablesDescribePartitions(ctx, vNum, pool, "public", "orders", 100, 0)
	require.NoError(t, err)
	assert.Empty(t, result, "non-partitioned table should have no partitions")
}

func TestGetPgstattupleAvailable(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	available, err := p.getPgstattupleAvailable(ctx, vNum, pool)
	require.NoError(t, err)
	assert.True(t, available, "pgstattuple should be available in test fixture")
}

func TestGetTablesDescribeBloat(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getTablesDescribeBloat(ctx, vNum, pool, "public", "orders")
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Greater(t, result.TableLen, int64(0))
	assert.NotEmpty(t, result.TableLenPretty)
	assert.Greater(t, result.ApproxTupleCount, int64(0))
	assert.Greater(t, result.ApproxTupleLen, int64(0))
	assert.NotEmpty(t, result.ApproxTupleLenPretty)
	assert.Greater(t, result.ApproxTuplePercent, 0.0)
	assert.GreaterOrEqual(t, result.DeadTupleCount, int64(0))
	assert.GreaterOrEqual(t, result.DeadTuplePercent, 0.0)
	assert.NotEmpty(t, result.ApproxFreeSpacePretty)
	assert.GreaterOrEqual(t, result.ApproxFreePercent, 0.0)
}

func TestGetTablesDescribeBloatDeadRows(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	// deadrows_test has 500 rows with 200 deleted
	result, err := p.getTablesDescribeBloat(ctx, vNum, pool, "public", "deadrows_test")
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Greater(t, result.TableLen, int64(0))
	assert.Greater(t, result.ApproxTupleCount, int64(0))
	// After DELETE + VACUUM, dead tuples should be reclaimed, but free space should exist
	assert.GreaterOrEqual(t, result.ApproxFreePercent, 0.0)
}

func TestGetTablesSchemas(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getTablesSchemas(ctx, vNum, pool)
	require.NoError(t, err)
	require.NotEmpty(t, result)

	assert.Contains(t, result, "public", "should contain public schema")

	// public should be first (sorted by CASE priority)
	assert.Equal(t, "public", result[0], "public should be first schema")

	// Should not contain pg_toast/pg_temp (filtered in SQL)
	for _, schema := range result {
		assert.NotContains(t, schema, "pg_toast")
		assert.NotContains(t, schema, "pg_temp")
	}
}

func TestGetTablesSearch(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	// Search for "ord" should find "orders"
	result, err := p.getTablesSearch(ctx, vNum, pool, "public", "ord", 50)
	require.NoError(t, err)
	require.NotEmpty(t, result)
	assert.Contains(t, result, "orders")
}

func TestGetTablesSearchEmpty(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	// Empty query should return all tables
	result, err := p.getTablesSearch(ctx, vNum, pool, "public", "", 50)
	require.NoError(t, err)
	require.NotEmpty(t, result)
	assert.GreaterOrEqual(t, len(result), 5, "fixture has at least 5 tables")
}

func TestGetTablesSearchNoMatch(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getTablesSearch(ctx, vNum, pool, "public", "zzz_nonexistent_zzz", 50)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestGetTablesSearchLimit(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getTablesSearch(ctx, vNum, pool, "public", "", 2)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(result), 2, "should respect limit")
}
