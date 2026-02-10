//go:build integration

package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/testinfra"
)

func TestGetInRecovery_Primary(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	inRecovery, err := p.getInRecovery(ctx, vNum, pool)
	require.NoError(t, err)
	assert.False(t, inRecovery, "primary should not be in recovery")
}

func TestGetInRecovery_Replica(t *testing.T) {
	// Not parallel — creates a replica container from the shared primary
	ctx := t.Context()

	replica, err := tc.CreateReplica(ctx)
	require.NoError(t, err, "failed to create replica container")
	t.Cleanup(func() { replica.TearDown(ctx) })

	p := NewTestPgxPool(replica.Pool, zap.NewNop())

	vNum, err := p.getServerVersionNum(ctx, replica.Pool)
	require.NoError(t, err)

	inRecovery, err := p.getInRecovery(ctx, vNum, replica.Pool)
	require.NoError(t, err)
	assert.True(t, inRecovery, "replica should be in recovery")
}
