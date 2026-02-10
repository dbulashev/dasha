//go:build integration

package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/testinfra"
)

func TestGetConnectionSources(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getConnectionSources(ctx, vNum, pool, 100, 0)
	require.NoError(t, err)
	assert.NotEmpty(t, result, "should return at least our own connection source")

	for _, src := range result {
		assert.Greater(t, src.TotalConnections, int64(0))
	}
}

func TestGetConnectionStates(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getConnectionStates(ctx, vNum, pool)
	require.NoError(t, err)
	assert.NotEmpty(t, result, "should return connection states")

	// Our pool connection should be visible
	for _, s := range result {
		// State may be empty string (NULL state from pgtype.Text)
		assert.Greater(t, s.Count, int64(0))
	}
}

func TestGetConnectionWaitEvents(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getConnectionWaitEvents(ctx, vNum, pool)
	require.NoError(t, err)
	// May be empty on an idle test instance — just verify SQL executes
	assert.NotNil(t, result)

	for _, we := range result {
		assert.NotEmpty(t, we.WaitEventType)
		assert.NotEmpty(t, we.WaitEvent)
		assert.Greater(t, we.Count, int64(0))
	}
}

func TestGetConnectionStatActivity(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	// No filters
	result, err := p.getConnectionStatActivity(ctx, vNum, pool, 50, 0, "", "")
	require.NoError(t, err)
	assert.NotEmpty(t, result, "should return at least our own connection")

	for _, a := range result {
		assert.NotZero(t, a.Pid)
		assert.NotEmpty(t, a.BackendType)
	}

	// Filter by username
	filtered, err := p.getConnectionStatActivity(ctx, vNum, pool, 50, 0, "test", "")
	require.NoError(t, err)
	for _, a := range filtered {
		assert.Equal(t, "test", a.UserName)
	}
}
