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

	addSnapshotReasonSQL = `
ALTER TABLE snapshots ADD COLUMN IF NOT EXISTS reason text NOT NULL DEFAULT 'manual'`

	addSnapshotTriggerContextSQL = `
ALTER TABLE snapshots ADD COLUMN IF NOT EXISTS trigger_context jsonb`

	createAutosnapshotConfigGlobalSQL = `
CREATE TABLE IF NOT EXISTS autosnapshot_config_global (
    id                      smallint PRIMARY KEY CHECK (id = 1),
    enabled                 boolean NOT NULL DEFAULT false,
    poll_interval           interval NOT NULL DEFAULT '30s',
    max_snapshot_frequency  interval NOT NULL DEFAULT '1h',
    retention_bytes         bigint  NOT NULL DEFAULT 10737418240,
    retention_min_days      int     NOT NULL DEFAULT 7,
    min_baseline_active     int     NOT NULL DEFAULT 10,
    defaults                jsonb   NOT NULL,
    updated_at              timestamptz NOT NULL DEFAULT now(),
    updated_by              text
)`

	seedAutosnapshotConfigGlobalSQL = `
INSERT INTO autosnapshot_config_global (id, defaults)
VALUES (1, '{
    "activity_spike": {"enabled": true, "window_size": "5m", "active_threshold_pct": 50, "spike_duration": "5m"},
    "role_change":    {"enabled": true, "direction": "both"}
}'::jsonb)
ON CONFLICT (id) DO NOTHING`

	createAutosnapshotConfigClusterSQL = `
CREATE TABLE IF NOT EXISTS autosnapshot_config_cluster (
    cluster_name text PRIMARY KEY,
    overrides    jsonb NOT NULL DEFAULT '{}'::jsonb,
    updated_at   timestamptz NOT NULL DEFAULT now(),
    updated_by   text
)`

	createTriggerEventsSQL = `
CREATE TABLE IF NOT EXISTS trigger_events (
    id              uuid DEFAULT gen_random_uuid(),
    cluster_name    text NOT NULL,
    instance        text NOT NULL,
    database        text,
    trigger_type    text NOT NULL,
    outcome         text NOT NULL,
    snapshot_id     uuid,
    trigger_context jsonb NOT NULL DEFAULT '{}'::jsonb,
    error_message   text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT trigger_events_pkey PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at)`

	createTriggerEventsIdxSQL = `
CREATE INDEX IF NOT EXISTS idx_trigger_events_lookup
    ON trigger_events (cluster_name, created_at DESC)`

	createAutosnapshotLeaderSQL = `
CREATE TABLE IF NOT EXISTS autosnapshot_leader (
    id             smallint PRIMARY KEY CHECK (id = 1),
    instance_id    text,
    last_heartbeat timestamptz NOT NULL DEFAULT now()
)`

	seedAutosnapshotLeaderSQL = `
INSERT INTO autosnapshot_leader (id) VALUES (1)
ON CONFLICT (id) DO NOTHING`

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

	addSnapshotLocksDataSQL = `
ALTER TABLE snapshots ADD COLUMN IF NOT EXISTS locks_data jsonb`

	addAutosnapshotLockConfigSQL = `
ALTER TABLE autosnapshot_config_global
    ADD COLUMN IF NOT EXISTS capture_locks       boolean  NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS lock_probe_count    int      NOT NULL DEFAULT 5,
    ADD COLUMN IF NOT EXISTS lock_probe_interval interval NOT NULL DEFAULT '500ms'`

	addAutosnapshotResetConfigSQL = `
ALTER TABLE autosnapshot_config_global
    ADD COLUMN IF NOT EXISTS reset_query_stats boolean NOT NULL DEFAULT false`

	createAutosnapshotPendingSQL = `
CREATE TABLE IF NOT EXISTS autosnapshot_pending (
    cluster_name text        NOT NULL,
    instance     text        NOT NULL,
    database     text        NOT NULL,
    due_at       timestamptz NOT NULL,
    reason       text        NOT NULL,
    PRIMARY KEY (cluster_name, instance)
)`

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

	// #nosec G101 -- SQL DDL statement (the const name merely contains "Tokens"), not a credential
	createAPITokensSubjectIdxSQL = `
CREATE INDEX IF NOT EXISTS idx_api_tokens_subject ON api_tokens (subject)`

	// subject is the OIDC email — the same key api_tokens.subject stores, so a
	// user joins to the tokens they own. Rows appear on first sign-in; the table
	// is an audit of who has access, not an authorization source (roles still
	// come from the IdP on every login).
	createUsersSQL = `
CREATE TABLE IF NOT EXISTS users (
    subject       text PRIMARY KEY,
    name          text NOT NULL DEFAULT '',
    role          text NOT NULL DEFAULT 'viewer',
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_login_at timestamptz
)`
)

// partitionedTables lists the day-partitioned tables managed together —
// partitions for each table are created and dropped as a group per day.
var partitionedTables = []string{"snapshots", "query_texts", "trigger_events"}

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
		addSnapshotReasonSQL,
		addSnapshotTriggerContextSQL,
		createAutosnapshotConfigGlobalSQL,
		seedAutosnapshotConfigGlobalSQL,
		createAutosnapshotConfigClusterSQL,
		createTriggerEventsSQL,
		createTriggerEventsIdxSQL,
		createAutosnapshotLeaderSQL,
		seedAutosnapshotLeaderSQL,
		createHealthScoreWeightsSQL,
		addHealthScoreWeightsCategoriesSQL,
		addSnapshotLocksDataSQL,
		addAutosnapshotLockConfigSQL,
		addAutosnapshotResetConfigSQL,
		createAutosnapshotPendingSQL,
		createAPITokensSQL,
		createAPITokensSubjectIdxSQL,
		createUsersSQL,
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

// ensurePartitions creates daily partitions for all partitioned tables if they don't
// exist. Runs on the DDL pool (the migration role) so the DML-only service role does
// not need partition-creation privileges at snapshot-write time.
func (s *Storage) ensurePartitions(ctx context.Context, day time.Time) error {
	dayStr := day.Format("20060102")
	from := day.Format("2006-01-02")
	to := day.AddDate(0, 0, 1).Format("2006-01-02")

	for _, table := range partitionedTables {
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
