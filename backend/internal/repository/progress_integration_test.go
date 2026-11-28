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
	"github.com/dbulashev/dasha/internal/enums"
	"github.com/dbulashev/dasha/internal/query"
	"github.com/dbulashev/dasha/internal/testinfra"
)

const pg19VersionNum = 190000

// Every version variant of a query must return the same columns in the same
// order: the repository has a single Scan for all of them.
func TestVersionedQueries_ColumnShape(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	tests := []struct {
		q    enums.Query
		cols []string
	}{
		{enums.QueryProgressVacuum, []string{
			"pid", "datname", "table_name", "phase",
			"heap_blks_total", "heap_blks_scanned", "heap_blks_vacuumed", "index_vacuum_count",
			"max_dead_tuples", "num_dead_tuples", "dead_tuple_bytes", "max_dead_tuple_bytes",
			"started_by", "mode",
		}},
		{enums.QueryProgressAnalyze, []string{
			"pid", "datname", "table_name", "phase",
			"sample_blks_total", "sample_blks_scanned", "ext_stats_total", "ext_stats_computed",
			"current_child_table", "started_by",
		}},
		{enums.QueryProgressCluster, []string{
			"pid", "datname", "table_name", "command", "phase", "cluster_index",
			"heap_tuples_scanned", "heap_tuples_written",
			"heap_tuples_inserted", "heap_tuples_updated", "heap_tuples_deleted",
			"heap_blks_total", "heap_blks_scanned", "index_rebuild_count",
		}},
		{enums.QueryProgressBaseBackup, []string{
			"pid", "phase", "backup_total", "backup_streamed", "progress_percentage",
			"tablespaces_total", "tablespaces_streamed", "backup_type",
		}},
		{enums.QueryMaintenanceVacuumProgress, []string{"pid", "phase", "started_by", "mode"}},
		{enums.QueryReplicationSlots, []string{
			"slot_name", "slot_type", "database", "active", "wal_status", "safe_wal_size", "backlog_bytes",
			"failover", "synced", "slotsync_skip_reason", "on_standby",
		}},
	}

	for _, tt := range tests {
		t.Run(tt.q.String(), func(t *testing.T) {
			qStr, err := query.Get(vNum, tt.q, nil)
			require.NoError(t, err)

			rows, err := pool.Query(ctx, qStr)
			require.NoError(t, err)

			got := make([]string, 0, len(tt.cols))
			for _, fd := range rows.FieldDescriptions() {
				got = append(got, fd.Name)
			}

			rows.Close()
			require.NoError(t, rows.Err())
			assert.Equal(t, tt.cols, got)
		})
	}
}

func TestGetProgressClusterBaseBackup_Idle(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	_, err = p.getProgressCluster(ctx, vNum, pool)
	require.NoError(t, err)

	_, err = p.getProgressBaseBackup(ctx, vNum, pool)
	require.NoError(t, err)
}

func TestProgressVacuum_StartedByMode(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	createSlowTable(t, pool, "pg19_vac")
	pid := runThrottled(t, pool, "VACUUM pg19_vac")

	vac := waitForRow(t, func() ([]dto.ProgressVacuum, error) {
		return p.getProgressVacuum(ctx, vNum, pool)
	}, func(r dto.ProgressVacuum) bool { return r.Pid == pid })

	mvac := waitForRow(t, func() ([]dto.MaintenanceVacuumProgress, error) {
		return p.getMaintenanceVacuumProgress(ctx, vNum, pool)
	}, func(r dto.MaintenanceVacuumProgress) bool { return r.Pid == pid })

	if vNum >= pg19VersionNum {
		for _, got := range [][2]*string{{vac.StartedBy, vac.Mode}, {mvac.StartedBy, mvac.Mode}} {
			require.NotNil(t, got[0])
			require.NotNil(t, got[1])
			assert.Equal(t, "manual", *got[0])
			assert.Equal(t, "normal", *got[1])
		}

		return
	}

	assert.Nil(t, vac.StartedBy)
	assert.Nil(t, vac.Mode)
	assert.Nil(t, mvac.StartedBy)
	assert.Nil(t, mvac.Mode)
}

func TestProgressAnalyze_StartedBy(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	createSlowTable(t, pool, "pg19_an")
	pid := runThrottled(t, pool, "ANALYZE pg19_an")

	an := waitForRow(t, func() ([]dto.ProgressAnalyze, error) {
		return p.getProgressAnalyze(ctx, vNum, pool)
	}, func(r dto.ProgressAnalyze) bool { return r.Pid == pid })

	if vNum >= pg19VersionNum {
		require.NotNil(t, an.StartedBy)
		assert.Equal(t, "manual", *an.StartedBy)

		return
	}

	assert.Nil(t, an.StartedBy)
}

func createSlowTable(t *testing.T, pool *pgxpool.Pool, name string) {
	t.Helper()

	_, err := pool.Exec(t.Context(),
		"CREATE TABLE "+name+" AS SELECT g AS id, md5(g::text) AS v FROM generate_series(1, 200000) g; "+
			"DELETE FROM "+name+" WHERE id % 2 = 0")
	require.NoError(t, err)
}

// runThrottled starts stmt on its own connection with the cost-based delay
// maxed out, so the command sleeps on every page and stays visible in the
// progress views; it is cancelled when the test ends.
func runThrottled(t *testing.T, pool *pgxpool.Pool, stmt string) int32 {
	t.Helper()
	ctx := t.Context()

	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)

	var pid int32
	require.NoError(t, conn.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid))

	_, err = conn.Exec(ctx, "SET vacuum_cost_delay = 100; SET vacuum_cost_limit = 1")
	require.NoError(t, err)

	done := make(chan struct{})

	go func() {
		defer close(done)

		_, _ = conn.Exec(context.Background(), stmt)
	}()

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "SELECT pg_cancel_backend($1)", pid)
		<-done
		conn.Release()
	})

	return pid
}

func waitForRow[T any](t *testing.T, list func() ([]T, error), match func(T) bool) T {
	t.Helper()

	deadline := time.Now().Add(15 * time.Second)

	for time.Now().Before(deadline) {
		rows, err := list()
		require.NoError(t, err)

		for _, r := range rows {
			if match(r) {
				return r
			}
		}

		time.Sleep(100 * time.Millisecond)
	}

	t.Fatal("progress row did not appear")

	var zero T

	return zero
}
