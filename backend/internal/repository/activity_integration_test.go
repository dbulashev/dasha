//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/activity"
	"github.com/dbulashev/dasha/internal/testinfra"
)

func TestGetActivitySample(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	if vNum < activity.MinVersionNum {
		t.Skip("pg_stat_activity.query_id requires PG14+")
	}

	_, err = pool.Exec(ctx, "CREATE TABLE activity_lock_probe (id int)")
	require.NoError(t, err)

	holder, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = holder.Rollback(ctx) }()

	_, err = holder.Exec(ctx, "LOCK TABLE activity_lock_probe IN ACCESS EXCLUSIVE MODE")
	require.NoError(t, err)

	waiter, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer waiter.Release()

	var waiterPID int32
	require.NoError(t, waiter.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&waiterPID))

	waitDone := make(chan struct{})

	go func() {
		defer close(waitDone)
		_, _ = waiter.Exec(ctx, "SELECT count(*) FROM activity_lock_probe WHERE id = 42")
	}()

	var (
		found *activity.Session
		smp   activity.Sample
	)

	require.Eventually(t, func() bool {
		smp, err = p.getActivitySample(ctx, vNum, pool, activity.DefaultMaxSessions)
		if err != nil {
			return false
		}

		for i := range smp.Sessions {
			if smp.Sessions[i].PID == waiterPID && smp.Sessions[i].WaitType == "Lock" {
				found = &smp.Sessions[i]

				return true
			}
		}

		return false
	}, 5*time.Second, 50*time.Millisecond)

	assert.False(t, smp.At.IsZero())
	assert.Equal(t, activity.ClassLock, activity.Class(*found))
	assert.True(t, activity.Counted(*found, false))
	assert.Contains(t, found.Text, "activity_lock_probe")
	assert.False(t, found.QueryStart.IsZero())
	assert.NotEqual(t, activity.KeyKindNone, found.KeyKind)

	require.NoError(t, holder.Rollback(ctx))
	<-waitDone
}

func TestGetActivitySampleExcludesOwnSession(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	if vNum < activity.MinVersionNum {
		t.Skip("pg_stat_activity.query_id requires PG14+")
	}

	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()

	ownPID := int32(conn.Conn().PgConn().PID())

	smp, err := p.getActivitySample(ctx, vNum, conn, activity.DefaultMaxSessions)
	require.NoError(t, err)
	assert.False(t, smp.At.IsZero(), "now() comes back even with no sessions")

	for _, s := range smp.Sessions {
		assert.NotEqual(t, ownPID, s.PID, "the sampling session is excluded")
		assert.NotEqual(t, "idle", s.State)
	}
}

func TestGetActivitySampleTruncates(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	if vNum < activity.MinVersionNum {
		t.Skip("pg_stat_activity.query_id requires PG14+")
	}

	for range 2 {
		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)

		_, err = conn.Exec(ctx, "BEGIN")
		require.NoError(t, err)

		t.Cleanup(func() {
			_, _ = conn.Exec(context.Background(), "ROLLBACK")
			conn.Release()
		})
	}

	smp, err := p.getActivitySample(ctx, vNum, pool, 1)
	require.NoError(t, err)
	assert.Len(t, smp.Sessions, 1)
	assert.True(t, smp.Truncated)
}

func TestGetActivityAccess(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	if vNum < activity.MinVersionNum {
		t.Skip("pg_stat_activity.query_id requires PG14+")
	}

	acc, err := getActivityAccess(ctx, vNum, pool)
	require.NoError(t, err)
	assert.True(t, acc.CanReadAllStats, "the test role is a superuser")
	assert.True(t, acc.QueryIDAvailable, "pg_stat_statements is preloaded and compute_query_id is auto")
}

func TestGetActivityAccessUnderUnprivilegedRole(t *testing.T) {
	t.Parallel()
	adminPool := testinfra.IsolatePool(t)
	admin := NewTestPgxPool(adminPool, zap.NewNop())
	ctx := t.Context()

	vNum, err := admin.getServerVersionNum(ctx, adminPool)
	require.NoError(t, err)

	if vNum < activity.MinVersionNum {
		t.Skip("pg_stat_activity.query_id requires PG14+")
	}

	const role = "activity_watcher"

	_, err = adminPool.Exec(ctx, `CREATE ROLE `+role+` LOGIN PASSWORD 'secret'`)
	require.NoError(t, err)

	t.Cleanup(func() {
		cleanCtx := context.Background()
		_, _ = adminPool.Exec(cleanCtx, `DROP OWNED BY `+role)
		_, _ = adminPool.Exec(cleanCtx, `DROP ROLE IF EXISTS `+role)
	})

	var dbName string

	require.NoError(t, adminPool.QueryRow(ctx, `SELECT current_database()`).Scan(&dbName))

	conn := adminPool.Config().ConnConfig
	dsn := fmt.Sprintf("postgres://%s:secret@%s:%d/%s?sslmode=disable", role, conn.Host, conn.Port, dbName)

	rolePool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err, "connect as the unprivileged role")

	defer rolePool.Close()

	acc, err := getActivityAccess(ctx, vNum, rolePool)
	require.NoError(t, err, "shared_preload_libraries is hidden from the role, not an error")
	assert.False(t, acc.CanReadAllStats)
	assert.True(t, acc.QueryIDAvailable, "compute_query_id is auto and the hidden preload list reads as preloaded")
}
