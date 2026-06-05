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

	addPgssStatsResetSQL = `
ALTER TABLE snapshots ADD COLUMN IF NOT EXISTS pgss_stats_reset timestamptz`

	createHealthScoreWeightsSQL = `
CREATE TABLE IF NOT EXISTS health_score_weights (
    cluster_name TEXT PRIMARY KEY,
    connections  DOUBLE PRECISION NOT NULL CHECK (connections >= 0),
    performance  DOUBLE PRECISION NOT NULL CHECK (performance >= 0),
    storage      DOUBLE PRECISION NOT NULL CHECK (storage     >= 0),
    replication  DOUBLE PRECISION NOT NULL CHECK (replication >= 0),
    maintenance  DOUBLE PRECISION NOT NULL CHECK (maintenance >= 0),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by   TEXT
)`

	addHealthScoreWeightsCategoriesSQL = `
ALTER TABLE health_score_weights
    ADD COLUMN IF NOT EXISTS horizon        DOUBLE PRECISION NOT NULL DEFAULT 0.10 CHECK (horizon        >= 0),
    ADD COLUMN IF NOT EXISTS wal_checkpoint DOUBLE PRECISION NOT NULL DEFAULT 0.10 CHECK (wal_checkpoint >= 0),
    ADD COLUMN IF NOT EXISTS locks          DOUBLE PRECISION NOT NULL DEFAULT 0.10 CHECK (locks          >= 0)`

	createAPITokensSQL = `
CREATE TABLE IF NOT EXISTS api_tokens (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash   bytea NOT NULL UNIQUE,
    token_prefix text  NOT NULL,
    name         text  NOT NULL,
    subject      text  NOT NULL,
    role         text  NOT NULL CHECK (role IN ('viewer','admin')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz,
    last_used_at timestamptz,
    revoked_at   timestamptz
)`

	createAPITokensSubjectIdxSQL = `
CREATE INDEX IF NOT EXISTS idx_api_tokens_subject ON api_tokens (subject)`
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

	// The migrate command connects as the DDL role, so both pools alias it.
	return &Storage{pool: pool, ddlPool: pool}, nil
}

func (s *Storage) migrate(ctx context.Context, logger *zap.Logger) error {
	for _, ddl := range []string{
		createSnapshotsSQL,
		createQueryTextsSQL,
		createSnapshotsIdxSQL,
		addPgssStatsResetSQL,
		createHealthScoreWeightsSQL,
		addHealthScoreWeightsCategoriesSQL,
		createAPITokensSQL,
		createAPITokensSubjectIdxSQL,
	} {
		if _, err := s.ddlPool.Exec(ctx, ddl); err != nil {
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
// Runs on the DDL pool (the migration role) so the DML-only service role does
// not need partition-creation privileges at snapshot-write time.
func (s *Storage) ensurePartitions(ctx context.Context, day time.Time) error {
	dayStr := day.Format("20060102")
	from := day.Format("2006-01-02")
	to := day.AddDate(0, 0, 1).Format("2006-01-02")

	for _, table := range []string{"snapshots", "query_texts"} {
		sql := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s_%s PARTITION OF %s FOR VALUES FROM ('%s') TO ('%s')`,
			table, dayStr, table, from, to,
		)

		if _, err := s.ddlPool.Exec(ctx, sql); err != nil {
			return fmt.Errorf("partition %s_%s: %w", table, dayStr, err)
		}
	}

	return nil
}
