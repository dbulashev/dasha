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
type Storage struct {
	pool   *pgxpool.Pool
	logger *zap.Logger
}

// New creates a Storage connected to the configured DSN.
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

	logger.Info("snapshot storage connected")

	return &Storage{pool: pool, logger: logger}, nil
}

// Close closes the underlying connection pool.
func (s *Storage) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}
