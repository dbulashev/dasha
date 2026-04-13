package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

const (
	partitionDaysAhead = 2
	createSnapshotsSQL = `
CREATE TABLE IF NOT EXISTS snapshots (
    id             uuid DEFAULT gen_random_uuid(),
    cluster_name   text NOT NULL,
    instance       text NOT NULL,
    database       text NOT NULL,
    dasha_version  text NOT NULL,
    json_version   int  NOT NULL,
    report_data    jsonb NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT snapshots_pkey PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at)`

	createQueryTextsSQL = `
CREATE TABLE IF NOT EXISTS query_texts (
    query_hash  text NOT NULL,
    query_text  text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT query_texts_pkey PRIMARY KEY (query_hash, created_at)
) PARTITION BY RANGE (created_at)`

	createSnapshotsIdxSQL = `
CREATE INDEX IF NOT EXISTS idx_snapshots_lookup
    ON snapshots (cluster_name, instance, database, created_at DESC)`
)

// Migrate creates parent tables and partitions for the next partitionDaysAhead days.
func Migrate(ctx context.Context, cfg string, logger *zap.Logger) error {
	s, err := newFromDSN(ctx, cfg)
	if err != nil {
		return err
	}
	defer s.Close()

	return s.migrate(ctx, logger)
}

func newFromDSN(ctx context.Context, dsn string) (*Storage, error) {
	if dsn == "" {
		return nil, fmt.Errorf("storage: DSN is empty")
	}

	connCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	pool, err := pgxpool.New(connCtx, dsn)
	if err != nil {
		return nil, fmt.Errorf("storage: connect: %w", err)
	}

	return &Storage{pool: pool}, nil
}

func (s *Storage) migrate(ctx context.Context, logger *zap.Logger) error {
	for _, ddl := range []string{createSnapshotsSQL, createQueryTextsSQL, createSnapshotsIdxSQL} {
		if _, err := s.pool.Exec(ctx, ddl); err != nil {
			return fmt.Errorf("storage: migrate: %w", err)
		}
	}

	logger.Info("parent tables created")

	now := time.Now().UTC()

	for i := range partitionDaysAhead {
		day := now.AddDate(0, 0, i)
		if err := s.ensurePartitions(ctx, day); err != nil {
			return fmt.Errorf("storage: create partition: %w", err)
		}
	}

	logger.Info("partitions created", zap.Int("days", partitionDaysAhead))

	return nil
}

// ensurePartitions creates daily partitions for both tables if they don't exist.
func (s *Storage) ensurePartitions(ctx context.Context, day time.Time) error {
	dayStr := day.Format("20060102")
	from := day.Format("2006-01-02")
	to := day.AddDate(0, 0, 1).Format("2006-01-02")

	for _, table := range []string{"snapshots", "query_texts"} {
		sql := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s_%s PARTITION OF %s FOR VALUES FROM ('%s') TO ('%s')`,
			table, dayStr, table, from, to,
		)

		if _, err := s.pool.Exec(ctx, sql); err != nil {
			return fmt.Errorf("partition %s_%s: %w", table, dayStr, err)
		}
	}

	return nil
}
