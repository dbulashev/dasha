//go:build integration

package repository

import (
	"cmp"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/enums"
	"github.com/dbulashev/dasha/internal/query"
	"github.com/dbulashev/dasha/internal/testinfra"
)

func TestGetMaintenanceInfo(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getMaintenanceInfo(ctx, vNum, pool, nil, "all", dto.ColdArgs{}, 30, 0)
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

func TestGetMaintenanceAutovacuumSummary(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	got, err := p.getMaintenanceAutovacuumSummary(ctx, vNum, pool)
	require.NoError(t, err)

	split := []*int32{
		got.RunningVacuumsManual, got.RunningVacuumsAutovacuum, got.RunningVacuumsWraparound,
		got.RunningAnalyzesManual, got.RunningAnalyzesAutovacuum,
	}

	if vNum < pg19VersionNum {
		assert.Equal(t, dto.AutovacuumSummarySourceFormula, got.Source)

		for _, v := range split {
			assert.Nil(t, v)
		}

		return
	}

	assert.Equal(t, dto.AutovacuumSummarySourceScores, got.Source)

	for _, v := range split {
		assert.NotNil(t, v)
	}
}

// The acceptance criterion of FR-3, word for word. Both counts are read in one
// transaction with a stats snapshot, so autovacuum cannot move them apart.
func TestGetMaintenanceAutovacuumSummary_MatchesScores(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	if vNum < pg19VersionNum {
		t.Skip("pg_stat_autovacuum_scores requires PG19+")
	}

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, "SET LOCAL stats_fetch_consistency = snapshot")
	require.NoError(t, err)

	qStr, err := query.Get(vNum, enums.QueryMaintenanceAutovacuumSummary, nil)
	require.NoError(t, err)

	var vacuumOnly, analyzeOnly, both, total, skip int32

	require.NoError(t, tx.QueryRow(ctx, qStr).Scan(&vacuumOnly, &analyzeOnly, &both, &total,
		&skip, &skip, &skip, &skip, &skip, &skip, &skip))

	var wantVacuum, wantTotal int32

	require.NoError(t, tx.QueryRow(ctx,
		"SELECT count(*) FILTER (WHERE do_vacuum)::int, count(*)::int FROM pg_stat_autovacuum_scores",
	).Scan(&wantVacuum, &wantTotal))

	assert.Equal(t, wantVacuum, vacuumOnly+both)
	assert.Equal(t, wantTotal, total)
}

func TestGetMaintenanceAutovacuumQueue(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	if vNum < pg19VersionNum {
		_, err := p.getMaintenanceAutovacuumQueue(ctx, vNum, pool, 50, 0)
		require.ErrorIs(t, err, ErrAutovacuumScoresUnsupported)

		return
	}

	const limit = 5

	got, err := p.getMaintenanceAutovacuumQueue(ctx, vNum, pool, limit, 0)
	require.NoError(t, err)
	assert.Positive(t, got.Total, "the view covers at least the system catalogs")
	assert.LessOrEqual(t, len(got.Rows), limit)
	assert.True(t, slices.IsSortedFunc(got.Rows, func(a, b dto.MaintenanceAutovacuumQueueRow) int {
		return cmp.Compare(b.Score, a.Score)
	}), "rows must be ordered by score, highest first")

	for _, r := range got.Rows {
		assert.NotEmpty(t, r.Schema)
		assert.NotEmpty(t, r.Table)
		assert.Equal(t, r.DominantComponent(), r.Dominant)
	}

	past, err := p.getMaintenanceAutovacuumQueue(ctx, vNum, pool, limit, int(got.Total)+10)
	require.NoError(t, err)
	assert.Empty(t, past.Rows)
	assert.Positive(t, past.Total, "the total survives an offset past the end")
}
