//go:build integration

package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/testinfra"
)

func singleConnPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	cfg := testinfra.IsolatePool(t).Config()
	cfg.MaxConns = 1

	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	return pool
}

func TestHealthScoreReads_SingleConnPool(t *testing.T) {
	t.Parallel()

	pool := singleConnPool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	var (
		wg      sync.WaitGroup
		snapErr error
		seqErr  error
	)

	wg.Go(func() { _, snapErr = p.healthScoreMetrics(ctx, pool) })
	wg.Go(func() { _, _, seqErr = p.readSequenceHeadroom(ctx, pool) })
	wg.Wait()

	require.NoError(t, snapErr)
	require.NoError(t, seqErr)
}

func TestAcquireConn_PoolBusy(t *testing.T) {
	t.Parallel()

	pool := singleConnPool(t)
	p := NewTestPgxPool(pool, zap.NewNop())

	_, err := p.getServerVersionNum(t.Context(), pool)
	require.NoError(t, err)

	held, err := pool.Acquire(t.Context())
	require.NoError(t, err)
	t.Cleanup(held.Release)

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	_, err = p.healthScoreMetrics(ctx, pool)
	assert.ErrorIs(t, err, ErrPoolBusy)
}

func TestServerVersion_ReadOncePerPool(t *testing.T) {
	t.Parallel()

	pool := singleConnPool(t)
	p := NewTestPgxPool(pool, zap.NewNop())

	first, err := p.getServerVersionNum(t.Context(), pool)
	require.NoError(t, err)

	held, err := pool.Acquire(t.Context())
	require.NoError(t, err)
	t.Cleanup(held.Release)

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	again, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err, "a cached version needs no connection")
	assert.Equal(t, first, again)
}

func TestForgetPool_DropsServerVersion(t *testing.T) {
	t.Parallel()

	pool := singleConnPool(t)
	p := NewTestPgxPool(pool, zap.NewNop())

	_, err := p.getServerVersionNum(t.Context(), pool)
	require.NoError(t, err)

	p.forgetPool(pool)

	_, cached := p.serverVersions.Load(pool)
	assert.False(t, cached)
}

func TestSequenceHeadroom_BusyPoolServesLastValue(t *testing.T) {
	t.Parallel()

	pool := singleConnPool(t)
	p := NewTestPgxPool(pool, zap.NewNop())

	key := sequenceHeadroomCacheKey("c", "i", "db")

	held, err := pool.Acquire(t.Context())
	require.NoError(t, err)
	t.Cleanup(held.Release)

	start := time.Now()

	worst, known, err := p.sequenceHeadroomForPool(t.Context(), "c", "i", "db", pool)
	require.NoError(t, err)
	assert.False(t, known, "no value yet")
	assert.Zero(t, worst)
	assert.Less(t, time.Since(start), time.Second, "a busy pool must not be waited on")

	_, stored := p.sequenceHeadroomCache.Load(key)
	assert.False(t, stored, "a skipped probe must not be cached")

	p.storeSequenceHeadroom(key, 0.9, true, -time.Minute)

	worst, known, err = p.sequenceHeadroomForPool(t.Context(), "c", "i", "db", pool)
	require.NoError(t, err)
	assert.True(t, known)
	assert.InDelta(t, 0.9, worst, 1e-9, "an expired value is served while the pool is busy")
}
