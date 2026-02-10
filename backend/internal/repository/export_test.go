//go:build integration

package repository

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// NewTestPgxPool creates a PgxPool for integration tests with a pre-connected pool.
// It bypasses ensurePool/config.Clusters — the pool is injected directly.
func NewTestPgxPool(pool *pgxpool.Pool, logger *zap.Logger) *PgxPool {
	return &PgxPool{
		pools:  PgxPools{},
		logger: logger,
	}
}
