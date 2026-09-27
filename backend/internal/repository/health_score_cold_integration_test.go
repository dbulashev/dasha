//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/testinfra"
)

func coldWrites(ctx context.Context, pool *pgxpool.Pool, pattern string) (int64, error) {
	var n int64

	err := pool.QueryRow(ctx, `
		SELECT COALESCE(sum(n_tup_ins + n_tup_upd + n_tup_del), 0)::bigint
		FROM pg_stat_user_tables WHERE relname LIKE $1`, pattern).Scan(&n)

	return n, err
}

func dumpTableStats(ctx context.Context, t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()

	var out string

	_ = pool.QueryRow(ctx, `
		SELECT string_agg(format('%s ins=%s upd=%s del=%s dead=%s', relname, n_tup_ins, n_tup_upd, n_tup_del, n_dead_tup), '; ' ORDER BY relname)
		FROM pg_stat_user_tables`).Scan(&out)

	return out
}

// TestHealthScore_ColdTablesExcluded covers a plain table, a hash parent
// resolved to its leaves and a range partition anchored by itself.
func TestHealthScore_ColdTablesExcluded(t *testing.T) {
	t.Parallel()

	pool := testinfra.IsolateEmptyPool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	_, err := pool.Exec(ctx, `
		CREATE TABLE cold_plain (id int) WITH (autovacuum_enabled = false, autovacuum_freeze_max_age = 100000);
		INSERT INTO cold_plain SELECT generate_series(1, 30000);
		DELETE FROM cold_plain WHERE id <= 9000;

		CREATE TABLE cold_hash (id int) PARTITION BY HASH (id);
		CREATE TABLE cold_hash_p0 PARTITION OF cold_hash FOR VALUES WITH (MODULUS 2, REMAINDER 0) WITH (autovacuum_enabled = false);
		CREATE TABLE cold_hash_p1 PARTITION OF cold_hash FOR VALUES WITH (MODULUS 2, REMAINDER 1) WITH (autovacuum_enabled = false);
		INSERT INTO cold_hash SELECT generate_series(1, 60000);
		DELETE FROM cold_hash WHERE id <= 18000;

		CREATE TABLE cold_range (id int, d date) PARTITION BY RANGE (d);
		CREATE TABLE cold_range_2025 PARTITION OF cold_range
			FOR VALUES FROM ('2025-01-01') TO ('2026-01-01') WITH (autovacuum_enabled = false);
		INSERT INTO cold_range SELECT g, '2025-06-01' FROM generate_series(1, 30000) g;
		DELETE FROM cold_range WHERE id <= 9000;
	`)
	require.NoError(t, err)

	// Inserts plus deletes per rollup target.
	want := map[string]int64{"cold_plain": 39000, "cold_hash_p%": 78000, "cold_range_2025": 39000}

	flushed := assert.Eventually(t, func() bool {
		var dead int64
		if err := pool.QueryRow(ctx, `SELECT COALESCE(sum(n_dead_tup), 0) FROM pg_stat_user_tables`).Scan(&dead); err != nil || dead != 36000 {
			return false
		}

		for pattern, n := range want {
			if got, err := coldWrites(ctx, pool, pattern); err != nil || got != n {
				return false
			}
		}

		return true
	}, 15*time.Second, 100*time.Millisecond, "stats flushed")
	if !flushed {
		t.Fatalf("table stats: %s", dumpTableStats(ctx, t, pool))
	}

	active, err := p.healthScoreMetrics(ctx, pool, dto.ColdArgs{})
	require.NoError(t, err)
	assert.InDelta(t, 30.0, active.MaxDeadRatio, 2, "hash leaves split the rows unevenly")
	assert.Equal(t, 4, active.TablesHighBloat)
	assert.Equal(t, 4, active.TablesNeverVacuumed)
	assert.Zero(t, active.ColdMaxRelfrozenxidAge)
	assert.Zero(t, active.ColdFreezeRatio)

	cold := dto.ColdArgs{
		Schemas: []string{"public", "public", "public"},
		Tables:  []string{"cold_plain", "cold_hash", "cold_range_2025"},
		Writes:  []int64{want["cold_plain"], want["cold_hash_p%"], want["cold_range_2025"]},
	}

	var ageBefore int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT age(relfrozenxid) FROM pg_class WHERE relname = 'cold_plain'`).Scan(&ageBefore))

	quiet, err := p.healthScoreMetrics(ctx, pool, cold)
	require.NoError(t, err)

	if quiet.TablesHighBloat != 0 {
		t.Logf("table stats: %s", dumpTableStats(ctx, t, pool))
	}

	var ageAfter int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT age(relfrozenxid) FROM pg_class WHERE relname = 'cold_plain'`).Scan(&ageAfter))

	assert.Zero(t, quiet.MaxDeadRatio)
	assert.Zero(t, quiet.AvgDeadRatio)
	assert.Zero(t, quiet.TablesHighBloat)
	assert.Zero(t, quiet.TablesNeverVacuumed)
	assert.GreaterOrEqual(t, quiet.ColdMaxRelfrozenxidAge, ageBefore)
	assert.GreaterOrEqual(t, quiet.ColdFreezeRatio, float64(ageBefore)/100000, "reloption lowers the freeze threshold")
	assert.LessOrEqual(t, quiet.ColdFreezeRatio, float64(ageAfter)/100000)

	perDB, err := p.collectHealthScorePerDatabase(ctx, pool, "", cold)
	require.NoError(t, err)
	assert.Zero(t, perDB.MaxDeadRatio)
	assert.Zero(t, perDB.TablesNeverVacuumed)

	_, err = pool.Exec(ctx, `INSERT INTO cold_plain VALUES (0)`)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		m, err := p.healthScoreMetrics(ctx, pool, cold)

		return err == nil && m.TablesHighBloat == 1 && m.MaxDeadRatio > 29
	}, 15*time.Second, 100*time.Millisecond, "a write after the anchor makes the table active again")
}
