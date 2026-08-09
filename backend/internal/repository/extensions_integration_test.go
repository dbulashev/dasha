//go:build integration

package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/testinfra"
)

// TestExtensionSchemaResolution moves both extensions off the search_path, the
// layout of "CREATE EXTENSION … SCHEMA ext" installations, and checks that every
// query built on them keeps working — the failure mode was
// `relation "pg_stat_statements" does not exist`.
func TestExtensionSchemaResolution(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	require.NotEmpty(t, p.extensionSchema(ctx, pool, extPgss), "extension schema should resolve as installed")

	_, err = pool.Exec(ctx, "CREATE SCHEMA ext")
	require.NoError(t, err)

	for _, ext := range []string{extPgss, extPgstattuple} {
		_, err = pool.Exec(ctx, "ALTER EXTENSION "+ext+" SET SCHEMA ext")
		require.NoError(t, err, "relocate %s", ext)
	}

	// The schema was resolved before the move, so drop what is cached for this
	// pool — exactly what happens when a pool is replaced at runtime.
	p.forgetPool(pool)

	assert.Equal(t, `"ext"`, p.extensionSchema(ctx, pool, extPgss))
	assert.Equal(t, `"ext"`, p.extensionSchema(ctx, pool, extPgstattuple))

	readable, err := p.getQueryStatsReadable(ctx, vNum, pool)
	require.NoError(t, err)
	assert.True(t, readable, "pg_stat_statements must stay readable outside search_path")

	_, err = p.getQueriesTop10ByTime(ctx, vNum, pool, nil)
	require.NoError(t, err, "top by time")

	_, err = p.getQueriesTop10ByWal(ctx, vNum, pool, nil)
	require.NoError(t, err, "top by wal")

	_, err = p.getQueriesTop10Chart(ctx, vNum, pool, nil)
	require.NoError(t, err, "top chart")

	_, err = p.getQueriesReport(ctx, vNum, pool, []string{})
	require.NoError(t, err, "report")

	if vNum >= 140000 {
		_, err = p.getPgssStatsResetTime(ctx, vNum, pool)
		require.NoError(t, err, "pgss stats reset time")
	}

	bloat, err := p.getTablesDescribeBloat(ctx, vNum, pool, "public", "orders")
	require.NoError(t, err, "pgstattuple_approx")
	require.NotNil(t, bloat)
}

// TestExtensionSchemaMissing covers the other branch: no extension, no schema,
// and the query is left bare so it fails on its own terms.
func TestExtensionSchemaMissing(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	assert.Empty(t, p.extensionSchema(ctx, pool, "pg_trgm"), "extension is not installed in the fixture")
	assert.Equal(t, "pg_stat_statements", qualify("", "pg_stat_statements"))
}
