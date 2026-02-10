//go:build integration

package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/testinfra"
)

func TestGetReplicationStatus_Standalone(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	// On a standalone primary with no replicas, SQL should execute without error
	result, err := p.getReplicationStatus(ctx, vNum, pool)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestGetReplicationSlots_Standalone(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getReplicationSlots(ctx, vNum, pool)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestGetReplicationConfig(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	ctx := t.Context()

	var syncStandbyNames, syncCommit string
	err := pool.QueryRow(ctx,
		"SELECT current_setting('synchronous_standby_names'), current_setting('synchronous_commit')",
	).Scan(&syncStandbyNames, &syncCommit)
	require.NoError(t, err)

	validModes := map[string]bool{
		"on": true, "off": true, "local": true,
		"remote_write": true, "remote_apply": true,
	}
	assert.True(t, validModes[syncCommit],
		"synchronous_commit should be a valid mode, got: %s", syncCommit)
}

// TestReplication_WithReplica groups all tests that require a streaming replica.
// A single replica is created and shared across subtests to avoid
// "replication slot already exists" errors and reduce test time.
func TestReplication_WithReplica(t *testing.T) {
	// Not parallel — creates a replica container from the shared primary
	ctx := t.Context()

	replica, err := tc.CreateReplica(ctx)
	require.NoError(t, err, "failed to create replica container")
	t.Cleanup(func() { replica.TearDown(ctx) })

	t.Run("InRecovery_Primary", func(t *testing.T) {
		pool := testinfra.IsolatePool(t)
		p := NewTestPgxPool(pool, zap.NewNop())

		vNum, err := p.getServerVersionNum(ctx, pool)
		require.NoError(t, err)

		inRecovery, err := p.getInRecovery(ctx, vNum, pool)
		require.NoError(t, err)
		assert.False(t, inRecovery, "primary should not be in recovery")
	})

	t.Run("InRecovery_Replica", func(t *testing.T) {
		p := NewTestPgxPool(replica.Pool, zap.NewNop())

		vNum, err := p.getServerVersionNum(ctx, replica.Pool)
		require.NoError(t, err)

		inRecovery, err := p.getInRecovery(ctx, vNum, replica.Pool)
		require.NoError(t, err)
		assert.True(t, inRecovery, "replica should be in recovery")
	})

	t.Run("Status_Primary", func(t *testing.T) {
		pool := testinfra.IsolatePool(t)
		p := NewTestPgxPool(pool, zap.NewNop())

		vNum, err := p.getServerVersionNum(ctx, pool)
		require.NoError(t, err)

		result, err := p.getReplicationStatus(ctx, vNum, pool)
		require.NoError(t, err)
		assert.NotEmpty(t, result, "primary with a connected replica should have replication status")

		for _, rs := range result {
			assert.NotZero(t, rs.Pid)
			assert.NotEmpty(t, rs.State)
			assert.NotEmpty(t, rs.SyncState)
		}
	})

	t.Run("Status_Replica", func(t *testing.T) {
		p := NewTestPgxPool(replica.Pool, zap.NewNop())

		vNum, err := p.getServerVersionNum(ctx, replica.Pool)
		require.NoError(t, err)

		result, err := p.getReplicationStatus(ctx, vNum, replica.Pool)
		require.NoError(t, err)
		assert.Empty(t, result, "replica should have no replication status rows")
	})

	t.Run("Slots_Primary", func(t *testing.T) {
		pool := testinfra.IsolatePool(t)
		p := NewTestPgxPool(pool, zap.NewNop())

		vNum, err := p.getServerVersionNum(ctx, pool)
		require.NoError(t, err)

		result, err := p.getReplicationSlots(ctx, vNum, pool)
		require.NoError(t, err)
		assert.NotEmpty(t, result, "should have at least the replica_slot")

		found := false
		for _, slot := range result {
			assert.NotEmpty(t, slot.SlotName)
			assert.NotEmpty(t, slot.SlotType)
			if slot.SlotName == "replica_slot" {
				found = true
				assert.True(t, slot.Active, "replica_slot should be active while replica is connected")
				assert.Equal(t, "physical", slot.SlotType)
			}
		}
		assert.True(t, found, "replica_slot should exist")
	})

	t.Run("Slots_Replica", func(t *testing.T) {
		// pg_is_in_recovery() guard prevents calling pg_current_wal_lsn() on replica
		p := NewTestPgxPool(replica.Pool, zap.NewNop())

		vNum, err := p.getServerVersionNum(ctx, replica.Pool)
		require.NoError(t, err)

		result, err := p.getReplicationSlots(ctx, vNum, replica.Pool)
		require.NoError(t, err)
		assert.NotNil(t, result)
	})
}
