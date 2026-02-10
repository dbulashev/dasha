//go:build integration

package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/testinfra"
)

func TestGetMaintenanceInfo(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getMaintenanceInfo(ctx, vNum, pool)
	require.NoError(t, err)
	assert.NotEmpty(t, result, "should return maintenance info for fixture tables")

	// Verify field mapping — stats may be zeroed in isolated DB (template stats not copied)
	for _, info := range result {
		assert.NotEmpty(t, info.Schema)
		assert.NotEmpty(t, info.Table)
		// LiveRows and DeadRows may be 0 on freshly isolated DB
		assert.GreaterOrEqual(t, info.LiveRows, int64(0))
		assert.GreaterOrEqual(t, info.DeadRows, int64(0))
	}
}

func TestGetMaintenanceAutovacuumFreezeMaxAge(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getMaintenanceAutovacuumFreezeMaxAge(ctx, vNum, pool)
	require.NoError(t, err)
	assert.NotEmpty(t, result, "should return autovacuum_freeze_max_age setting")
	assert.Greater(t, result[0].AutovacuumFreezeMaxAge, int64(0))
}

func TestGetMaintenanceTransactionIdDanger(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getMaintenanceTransactionIdDanger(ctx, vNum, pool)
	require.NoError(t, err)
	// Fresh DB should have no tables in danger — just verify SQL executes
	assert.NotNil(t, result)
}

func TestGetMaintenanceVacuumProgress(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getMaintenanceVacuumProgress(ctx, vNum, pool)
	require.NoError(t, err)
	// No vacuum in progress expected — just verify SQL executes
	assert.NotNil(t, result)
}
