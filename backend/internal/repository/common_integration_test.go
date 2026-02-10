//go:build integration

package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/testinfra"
)

func TestGetCommonSummary(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getCommonSummary(ctx, vNum, pool)
	require.NoError(t, err)
	assert.NotEmpty(t, result, "should return object summary (tables, indexes, sequences)")

	// Should have at least tables and indexes
	kinds := make(map[string]bool)
	for _, s := range result {
		kinds[s.Kind] = true
		assert.NotEmpty(t, s.Namespace)
		assert.NotEmpty(t, s.Kind)
		assert.Greater(t, s.Amount, int64(0))
	}
	assert.True(t, kinds["Tables"],
		"should have Tables in summary")
}
