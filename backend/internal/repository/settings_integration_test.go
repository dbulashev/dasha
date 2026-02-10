//go:build integration

package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/enums"
	"github.com/dbulashev/dasha/internal/testinfra"
)

func TestGetPgSettings(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getPgSettings(ctx, vNum, pool, enums.QuerySettingsPgSettings, 50, 0)
	require.NoError(t, err)
	assert.NotEmpty(t, result, "should return PG settings")
	assert.LessOrEqual(t, len(result), 50)

	for _, s := range result {
		assert.NotEmpty(t, s.Name)
		assert.NotEmpty(t, s.Setting)
		assert.NotEmpty(t, s.Source)
	}

	// Test pagination
	page1, err := p.getPgSettings(ctx, vNum, pool, enums.QuerySettingsPgSettings, 5, 0)
	require.NoError(t, err)
	assert.Len(t, page1, 5)

	page2, err := p.getPgSettings(ctx, vNum, pool, enums.QuerySettingsPgSettings, 5, 5)
	require.NoError(t, err)
	if len(page2) > 0 {
		assert.NotEqual(t, page1[0].Name, page2[0].Name, "pagination should return different settings")
	}
}

func TestGetAutovacuumSettings(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getAutovacuumSettings(ctx, vNum, pool)
	require.NoError(t, err)
	assert.NotEmpty(t, result, "should return autovacuum settings (always present)")

	// Should contain known autovacuum settings
	names := make(map[string]bool)
	for _, s := range result {
		names[s.Name] = true
	}
	assert.True(t, names["autovacuum_vacuum_threshold"] || names["autovacuum"],
		"should contain autovacuum-related settings")
}

func TestGetSettingsAnalyze(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getSettingsAnalyze(ctx, vNum, pool)
	require.NoError(t, err)
	// Settings analysis returns notifications for non-optimal settings
	for _, n := range result {
		assert.NotEmpty(t, n.Key)
		assert.NotNil(t, n.Params)
	}
}
