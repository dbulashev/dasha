//go:build integration

package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/testinfra"
)

func TestGetDatabaseSize(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getDatabaseSize(ctx, vNum, pool)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Greater(t, result.SizeBytes, int64(0), "database should have non-zero size")
	assert.NotEmpty(t, result.SizePretty, "size_pretty should be populated")
}

func TestGetStatsResetTime(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getStatsResetTime(ctx, vNum, pool)
	require.NoError(t, err)
	// May or may not have a reset time — just verify SQL executes
	assert.NotNil(t, result)
}

func TestGetPgssStatsResetTime(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	// pg_stat_statements_info available since PG14
	if vNum < 140000 {
		t.Skip("pg_stat_statements_info requires PG14+")
	}

	result, err := p.getPgssStatsResetTime(ctx, vNum, pool)
	require.NoError(t, err)
	// Result may be nil if stats were never reset — just verify SQL executes
	_ = result
}
