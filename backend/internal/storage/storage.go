package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
)

const connectTimeout = 5 * time.Second

// Storage provides access to the snapshot database.
//
// pool is the service connection used for all reads/writes (DML). ddlPool is the
// privileged connection used only for DDL — partition creation at write time and
// migrations. When no dedicated migration role is configured, ddlPool aliases
// pool, so a single-role install behaves exactly as before.
type Storage struct {
	pool    *pgxpool.Pool
	ddlPool *pgxpool.Pool
	logger  *zap.Logger
}

// New creates a Storage connected to the configured DSN(s).
// Returns nil if storage is not configured.
func New(ctx context.Context, cfg config.StorageConfig, logger *zap.Logger) (*Storage, error) {
	if !cfg.Enabled() {
		return nil, nil //nolint:nilnil
	}

	connCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	pool, err := pgxpool.New(connCtx, cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("storage: connect: %w", err)
	}

	if err := pool.Ping(connCtx); err != nil {
		pool.Close()

		return nil, fmt.Errorf("storage: ping: %w", err)
	}

	// Separate DDL pool (partition creation) when a dedicated migration role is
	// configured; otherwise reuse the service pool.
	ddlPool := pool

	if cfg.DSNMigration != "" {
		dp, err := pgxpool.New(connCtx, cfg.DSNMigration)
		if err != nil {
			pool.Close()

			return nil, fmt.Errorf("storage: connect migration: %w", err)
		}

		if err := dp.Ping(connCtx); err != nil {
			dp.Close()
			pool.Close()

			return nil, fmt.Errorf("storage: ping migration: %w", err)
		}

		ddlPool = dp
	}

	logger.Info("snapshot storage connected")

	return &Storage{pool: pool, ddlPool: ddlPool, logger: logger}, nil
}

// Close closes the underlying connection pools.
func (s *Storage) Close() {
	if s == nil {
		return
	}

	if s.ddlPool != nil && s.ddlPool != s.pool {
		s.ddlPool.Close()
	}

	if s.pool != nil {
		s.pool.Close()
	}
}
