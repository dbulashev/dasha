package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

const defaultHealthDBConcurrency = 4

// acquireConn waits for a pool slot under ctx, whose deadline also bounds the
// query that follows. A deadline hit with every slot taken is ErrPoolBusy.
func (p *PgxPool) acquireConn(ctx context.Context, pool *pgxpool.Pool) (*pgxpool.Conn, error) {
	conn, err := pool.Acquire(ctx)
	if err == nil {
		return conn, nil
	}

	if !errors.Is(ctx.Err(), context.DeadlineExceeded) || hasFreeConn(pool) {
		return nil, fmt.Errorf("acquire | %w", err)
	}

	st := pool.Stat()

	p.logger.Warn("connection pool busy",
		zap.String("database", poolDatabase(pool, "")),
		zap.Int32("acquired", st.AcquiredConns()),
		zap.Int32("max", st.MaxConns()),
		zap.Int64("empty_acquire_count", st.EmptyAcquireCount()))

	return nil, fmt.Errorf("%w | acquired %d of %d", ErrPoolBusy, st.AcquiredConns(), st.MaxConns())
}

func hasFreeConn(pool *pgxpool.Pool) bool {
	st := pool.Stat()

	return st.AcquiredConns()+st.ConstructingConns() < st.MaxConns()
}

func (p *PgxPool) healthDatabaseConcurrency() int {
	if p.healthDBConcurrency > 0 {
		return p.healthDBConcurrency
	}

	return defaultHealthDBConcurrency
}

// forEachLimited calls fn for every item, at most limit at a time, and waits.
func forEachLimited[T any](limit int, items []T, fn func(i int, item T)) {
	var g errgroup.Group

	g.SetLimit(max(limit, 1))

	for i, it := range items {
		g.Go(func() error {
			fn(i, it)

			return nil
		})
	}

	_ = g.Wait()
}
