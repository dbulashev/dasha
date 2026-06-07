package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dbulashev/dasha/internal/autosnapshot"
)

// leaderLockKey is the key used for pg_try_advisory_lock across all autosnapshot daemons.
const leaderLockKey int64 = 0x7461736a // "tasj"

// GetAutosnapshotConfig returns the global auto-snapshot config.
func (s *Storage) GetAutosnapshotConfig(ctx context.Context) (autosnapshot.Config, error) {
	var (
		cfg          autosnapshot.Config
		pollInterval time.Duration
		maxFreq      time.Duration
		lockInterval time.Duration
		defaultsJSON []byte
	)

	err := s.pool.QueryRow(ctx, `
		SELECT enabled, poll_interval, max_snapshot_frequency,
		       retention_bytes, retention_min_days, min_baseline_active,
		       capture_locks, lock_probe_count, lock_probe_interval,
		       reset_query_stats,
		       defaults, updated_at, updated_by
		FROM autosnapshot_config_global WHERE id = 1`,
	).Scan(
		&cfg.Enabled, &pollInterval, &maxFreq,
		&cfg.RetentionBytes, &cfg.RetentionMinDays, &cfg.MinBaselineActive,
		&cfg.CaptureLocks, &cfg.LockProbeCount, &lockInterval,
		&cfg.ResetQueryStats,
		&defaultsJSON, &cfg.UpdatedAt, &cfg.UpdatedBy,
	)
	if err != nil {
		return cfg, fmt.Errorf("storage: get autosnapshot config: %w", err)
	}

	cfg.PollInterval = pollInterval
	cfg.MaxSnapshotFrequency = maxFreq
	cfg.LockProbeInterval = lockInterval

	var raw struct {
		ActivitySpike struct {
			Enabled            bool   `json:"enabled"`
			WindowSize         string `json:"window_size"`
			ActiveThresholdPct int    `json:"active_threshold_pct"`
			SpikeDuration      string `json:"spike_duration"`
			RecoveryDuration   string `json:"recovery_duration"`
			DeferredInterval   string `json:"deferred_interval"`
		} `json:"activity_spike"`
		RoleChange struct {
			Enabled   bool   `json:"enabled"`
			Direction string `json:"direction"`
		} `json:"role_change"`
	}

	if err := json.Unmarshal(defaultsJSON, &raw); err != nil {
		return cfg, fmt.Errorf("storage: unmarshal defaults: %w", err)
	}

	cfg.Defaults.ActivitySpike.Enabled = raw.ActivitySpike.Enabled
	cfg.Defaults.ActivitySpike.ActiveThresholdPct = raw.ActivitySpike.ActiveThresholdPct

	cfg.Defaults.ActivitySpike.WindowSize, err = time.ParseDuration(raw.ActivitySpike.WindowSize)
	if err != nil {
		return cfg, fmt.Errorf("storage: parse ActivitySpike.WindowSize %q: %w", raw.ActivitySpike.WindowSize, err)
	}

	cfg.Defaults.ActivitySpike.SpikeDuration, err = time.ParseDuration(raw.ActivitySpike.SpikeDuration)
	if err != nil {
		return cfg, fmt.Errorf("storage: parse ActivitySpike.SpikeDuration %q: %w", raw.ActivitySpike.SpikeDuration, err)
	}

	// Optional (0 = disabled): only parse when set.
	if raw.ActivitySpike.RecoveryDuration != "" {
		cfg.Defaults.ActivitySpike.RecoveryDuration, err = time.ParseDuration(raw.ActivitySpike.RecoveryDuration)
		if err != nil {
			return cfg, fmt.Errorf("storage: parse ActivitySpike.RecoveryDuration %q: %w", raw.ActivitySpike.RecoveryDuration, err)
		}
	}

	if raw.ActivitySpike.DeferredInterval != "" {
		cfg.Defaults.ActivitySpike.DeferredInterval, err = time.ParseDuration(raw.ActivitySpike.DeferredInterval)
		if err != nil {
			return cfg, fmt.Errorf("storage: parse ActivitySpike.DeferredInterval %q: %w", raw.ActivitySpike.DeferredInterval, err)
		}
	}

	cfg.Defaults.RoleChange.Enabled = raw.RoleChange.Enabled
	cfg.Defaults.RoleChange.Direction = autosnapshot.Direction(raw.RoleChange.Direction)

	return cfg, nil
}

// SetAutosnapshotConfig replaces the global config.
func (s *Storage) SetAutosnapshotConfig(ctx context.Context, cfg autosnapshot.Config, updatedBy string) error {
	defaults := map[string]any{
		"activity_spike": map[string]any{
			"enabled":              cfg.Defaults.ActivitySpike.Enabled,
			"window_size":          cfg.Defaults.ActivitySpike.WindowSize.String(),
			"active_threshold_pct": cfg.Defaults.ActivitySpike.ActiveThresholdPct,
			"spike_duration":       cfg.Defaults.ActivitySpike.SpikeDuration.String(),
			"recovery_duration":    cfg.Defaults.ActivitySpike.RecoveryDuration.String(),
			"deferred_interval":    cfg.Defaults.ActivitySpike.DeferredInterval.String(),
		},
		"role_change": map[string]any{
			"enabled":   cfg.Defaults.RoleChange.Enabled,
			"direction": string(cfg.Defaults.RoleChange.Direction),
		},
	}

	data, err := json.Marshal(defaults)
	if err != nil {
		return fmt.Errorf("storage: marshal defaults: %w", err)
	}

	_, err = s.pool.Exec(ctx, `
		UPDATE autosnapshot_config_global
		SET enabled = $1, poll_interval = $2, max_snapshot_frequency = $3,
		    retention_bytes = $4, retention_min_days = $5, min_baseline_active = $6,
		    capture_locks = $7, lock_probe_count = $8, lock_probe_interval = $9,
		    reset_query_stats = $10,
		    defaults = $11::jsonb, updated_at = now(), updated_by = $12
		WHERE id = 1`,
		cfg.Enabled, cfg.PollInterval, cfg.MaxSnapshotFrequency,
		cfg.RetentionBytes, cfg.RetentionMinDays, cfg.MinBaselineActive,
		cfg.CaptureLocks, cfg.LockProbeCount, cfg.LockProbeInterval,
		cfg.ResetQueryStats,
		data, nullStringPtr(updatedBy),
	)
	if err != nil {
		return fmt.Errorf("storage: set autosnapshot config: %w", err)
	}

	return nil
}

// GetClusterOverride returns the per-cluster override or an empty override if absent.
func (s *Storage) GetClusterOverride(ctx context.Context, clusterName string) (autosnapshot.ClusterOverride, error) {
	var (
		result       autosnapshot.ClusterOverride
		overrideJSON []byte
	)

	err := s.pool.QueryRow(ctx, `
		SELECT cluster_name, overrides, updated_at, updated_by
		FROM autosnapshot_config_cluster WHERE cluster_name = $1`,
		clusterName,
	).Scan(&result.ClusterName, &overrideJSON, &result.UpdatedAt, &result.UpdatedBy)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return autosnapshot.ClusterOverride{
				ClusterName: clusterName,
				Overrides:   map[string]any{},
			}, nil
		}

		return result, fmt.Errorf("storage: get cluster override: %w", err)
	}

	if len(overrideJSON) > 0 {
		if err := json.Unmarshal(overrideJSON, &result.Overrides); err != nil {
			return result, fmt.Errorf("storage: unmarshal override: %w", err)
		}
	}

	if result.Overrides == nil {
		result.Overrides = map[string]any{}
	}

	return result, nil
}

// SetClusterOverride upserts per-cluster overrides. Empty map removes the row.
func (s *Storage) SetClusterOverride(ctx context.Context, clusterName string, overrides map[string]any, updatedBy string) error {
	if len(overrides) == 0 {
		if _, err := s.pool.Exec(ctx,
			`DELETE FROM autosnapshot_config_cluster WHERE cluster_name = $1`, clusterName,
		); err != nil {
			return fmt.Errorf("storage: delete cluster override: %w", err)
		}

		return nil
	}

	data, err := json.Marshal(overrides)
	if err != nil {
		return fmt.Errorf("storage: marshal overrides: %w", err)
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO autosnapshot_config_cluster (cluster_name, overrides, updated_by)
		VALUES ($1, $2::jsonb, $3)
		ON CONFLICT (cluster_name) DO UPDATE
		SET overrides = EXCLUDED.overrides,
		    updated_at = now(),
		    updated_by = EXCLUDED.updated_by`,
		clusterName, data, nullStringPtr(updatedBy),
	)
	if err != nil {
		return fmt.Errorf("storage: set cluster override: %w", err)
	}

	return nil
}

// ListClusterOverrides returns all per-cluster overrides keyed by cluster name.
func (s *Storage) ListClusterOverrides(ctx context.Context) (map[string]map[string]any, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT cluster_name, overrides FROM autosnapshot_config_cluster`,
	)
	if err != nil {
		return nil, fmt.Errorf("storage: list cluster overrides: %w", err)
	}
	defer rows.Close()

	result := map[string]map[string]any{}

	for rows.Next() {
		var (
			name string
			data []byte
		)

		if err := rows.Scan(&name, &data); err != nil {
			return nil, fmt.Errorf("storage: scan cluster override: %w", err)
		}

		m := map[string]any{}
		if len(data) > 0 {
			if err := json.Unmarshal(data, &m); err != nil {
				return nil, fmt.Errorf("storage: unmarshal override: %w", err)
			}
		}

		result[name] = m
	}

	return result, rows.Err()
}

// InsertTriggerEvent records an event to the trigger_events table.
func (s *Storage) InsertTriggerEvent(ctx context.Context, e autosnapshot.TriggerEvent) error {
	if err := s.ensurePartitions(ctx, time.Now().UTC()); err != nil {
		return err
	}

	ctxJSON, err := json.Marshal(e.TriggerContext)
	if err != nil {
		return fmt.Errorf("storage: marshal trigger context: %w", err)
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO trigger_events
		(cluster_name, instance, database, trigger_type, outcome, snapshot_id, trigger_context, error_message)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8)`,
		e.ClusterName, e.Instance, e.Database,
		string(e.TriggerType), string(e.Outcome),
		e.SnapshotID, ctxJSON, e.ErrorMessage,
	)
	if err != nil {
		return fmt.Errorf("storage: insert trigger event: %w", err)
	}

	return nil
}

// ListTriggerEvents returns events filtered by optional fields, paginated.
// No total count is returned: the frontend pages with the project-standard
// "hasMore = full page" heuristic, so a COUNT(*) per request is avoided.
func (s *Storage) ListTriggerEvents(ctx context.Context, f autosnapshot.TriggerEventFilter) ([]autosnapshot.TriggerEvent, error) {
	var (
		where  []string
		args   []any
		argIdx = 1
	)

	if f.ClusterName != "" {
		where = append(where, fmt.Sprintf("cluster_name ILIKE '%%' || $%d || '%%'", argIdx))
		args = append(args, f.ClusterName)
		argIdx++
	}

	if f.Outcome != "" {
		where = append(where, fmt.Sprintf("outcome = $%d", argIdx))
		args = append(args, f.Outcome)
		argIdx++
	}

	if f.TriggerType != "" {
		where = append(where, fmt.Sprintf("trigger_type = $%d", argIdx))
		args = append(args, f.TriggerType)
		argIdx++
	}

	if f.From != nil {
		where = append(where, fmt.Sprintf("created_at >= $%d", argIdx))
		args = append(args, *f.From)
		argIdx++
	}

	if f.To != nil {
		where = append(where, fmt.Sprintf("created_at < $%d", argIdx))
		args = append(args, *f.To)
		argIdx++
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}

	args = append(args, limit, f.Offset)

	q := fmt.Sprintf(`
		SELECT id, created_at, cluster_name, instance, database,
		       trigger_type, outcome, snapshot_id, trigger_context, error_message
		FROM trigger_events
		%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d`, whereClause, argIdx, argIdx+1)

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: list trigger events: %w", err)
	}
	defer rows.Close()

	var items []autosnapshot.TriggerEvent

	for rows.Next() {
		var (
			e       autosnapshot.TriggerEvent
			typ     string
			outcome string
			ctxData []byte
		)

		if err := rows.Scan(
			&e.ID, &e.CreatedAt, &e.ClusterName, &e.Instance, &e.Database,
			&typ, &outcome, &e.SnapshotID, &ctxData, &e.ErrorMessage,
		); err != nil {
			return nil, fmt.Errorf("storage: scan trigger event: %w", err)
		}

		e.TriggerType = autosnapshot.TriggerType(typ)
		e.Outcome = autosnapshot.Outcome(outcome)

		if len(ctxData) > 0 {
			if err := json.Unmarshal(ctxData, &e.TriggerContext); err != nil {
				return nil, fmt.Errorf("storage: unmarshal trigger context: %w", err)
			}
		}

		items = append(items, e)
	}

	return items, rows.Err()
}

// SummarizeTriggerEvents returns per-cluster snapshot/error counts (all retained
// history), ordered by snapshot count desc — the summary tab's data source.
func (s *Storage) SummarizeTriggerEvents(ctx context.Context) ([]autosnapshot.ClusterSummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			cluster_name,
			count(*) FILTER (WHERE outcome = 'snapshot_created')                                      AS snapshots,
			count(*) FILTER (WHERE outcome = 'snapshot_created' AND trigger_type = 'activity_spike')  AS activity_spike,
			count(*) FILTER (WHERE outcome = 'snapshot_created' AND trigger_type = 'role_change')      AS role_change,
			count(*) FILTER (WHERE outcome = 'error')                                                  AS errors
		FROM trigger_events
		GROUP BY cluster_name
		ORDER BY snapshots DESC, errors DESC, cluster_name`)
	if err != nil {
		return nil, fmt.Errorf("storage: summarize trigger events: %w", err)
	}
	defer rows.Close()

	var out []autosnapshot.ClusterSummary

	for rows.Next() {
		var c autosnapshot.ClusterSummary
		if err := rows.Scan(&c.ClusterName, &c.Snapshots, &c.ActivitySpike, &c.RoleChange, &c.Errors); err != nil {
			return nil, fmt.Errorf("storage: scan cluster summary: %w", err)
		}

		out = append(out, c)
	}

	return out, rows.Err()
}

// EnqueuePendingSnapshot schedules a deferred snapshot for a host (one pending per
// host — a newer spike replaces the old due time).
func (s *Storage) EnqueuePendingSnapshot(ctx context.Context, p autosnapshot.PendingSnapshot, dueAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO autosnapshot_pending (cluster_name, instance, database, due_at, reason)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (cluster_name, instance)
		DO UPDATE SET database = EXCLUDED.database, due_at = EXCLUDED.due_at, reason = EXCLUDED.reason`,
		p.ClusterName, p.Instance, p.Database, dueAt, p.Reason,
	)
	if err != nil {
		return fmt.Errorf("storage: enqueue pending snapshot: %w", err)
	}

	return nil
}

// DeletePendingSnapshot cancels a host's pending deferred snapshot — used once the
// spike has resolved and the drop snapshot already captured the incident, so the
// deferred follow-up would only snapshot the quiet aftermath.
func (s *Storage) DeletePendingSnapshot(ctx context.Context, clusterName, instance string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM autosnapshot_pending WHERE cluster_name = $1 AND instance = $2`,
		clusterName, instance,
	)
	if err != nil {
		return fmt.Errorf("storage: delete pending snapshot: %w", err)
	}

	return nil
}

// ClaimDuePendingSnapshots atomically removes and returns all pending snapshots
// whose due_at has passed (DELETE ... RETURNING, so they are taken exactly once).
func (s *Storage) ClaimDuePendingSnapshots(ctx context.Context) ([]autosnapshot.PendingSnapshot, error) {
	rows, err := s.pool.Query(ctx, `
		DELETE FROM autosnapshot_pending
		WHERE due_at <= now()
		RETURNING cluster_name, instance, database, reason`)
	if err != nil {
		return nil, fmt.Errorf("storage: claim due pending snapshots: %w", err)
	}
	defer rows.Close()

	var out []autosnapshot.PendingSnapshot

	for rows.Next() {
		var p autosnapshot.PendingSnapshot
		if err := rows.Scan(&p.ClusterName, &p.Instance, &p.Database, &p.Reason); err != nil {
			return nil, fmt.Errorf("storage: scan pending snapshot: %w", err)
		}

		out = append(out, p)
	}

	return out, rows.Err()
}

// LastAutoSnapshotAt returns the timestamp of the latest auto snapshot per cluster,
// used to enforce max_snapshot_frequency debounce across daemon restarts.
func (s *Storage) LastAutoSnapshotAt(ctx context.Context) (map[string]time.Time, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT cluster_name, MAX(created_at)
		FROM snapshots
		WHERE reason LIKE 'auto:%'
		GROUP BY cluster_name`,
	)
	if err != nil {
		return nil, fmt.Errorf("storage: last auto snapshot: %w", err)
	}
	defer rows.Close()

	result := map[string]time.Time{}

	for rows.Next() {
		var (
			name string
			t    time.Time
		)

		if err := rows.Scan(&name, &t); err != nil {
			return nil, fmt.Errorf("storage: scan last auto snapshot: %w", err)
		}

		result[name] = t
	}

	return result, rows.Err()
}

// TryAcquireLeaderLock tries to take the cross-instance advisory lock.
// Returns true on success. Must be held for the lifetime of the daemon.
func (s *Storage) TryAcquireLeaderLock(ctx context.Context) (*pgx.Conn, bool, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("storage: acquire conn: %w", err)
	}

	var acquired bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, leaderLockKey).Scan(&acquired); err != nil {
		conn.Release()
		return nil, false, fmt.Errorf("storage: try advisory lock: %w", err)
	}

	if !acquired {
		conn.Release()
		return nil, false, nil
	}

	// Hijack removes the connection from the pool so the session-level advisory
	// lock is held on a dedicated conn the caller owns and Closes (Leader.Release).
	// Using conn.Conn() instead would leak the pool checkout.
	return conn.Hijack(), true, nil
}

// UpdateLeaderHeartbeat writes the current leader identity and heartbeat time.
func (s *Storage) UpdateLeaderHeartbeat(ctx context.Context, instanceID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE autosnapshot_leader SET instance_id = $1, last_heartbeat = now() WHERE id = 1`,
		instanceID,
	)
	if err != nil {
		return fmt.Errorf("storage: update leader heartbeat: %w", err)
	}

	return nil
}

// GetLeaderInfo returns the current leader identity + liveness for UI.
func (s *Storage) GetLeaderInfo(ctx context.Context) (autosnapshot.LeaderInfo, error) {
	var (
		info          autosnapshot.LeaderInfo
		lastHeartbeat *time.Time
	)

	err := s.pool.QueryRow(ctx,
		`SELECT instance_id, last_heartbeat FROM autosnapshot_leader WHERE id = 1`,
	).Scan(&info.InstanceID, &lastHeartbeat)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return info, nil
		}

		return info, fmt.Errorf("storage: get leader info: %w", err)
	}

	info.LastHeartbeat = lastHeartbeat
	if lastHeartbeat != nil {
		info.IsAlive = time.Since(*lastHeartbeat) < autosnapshot.LeaderLivenessThreshold
	}

	return info, nil
}

// PartitionSize is re-exported from autosnapshot to keep storage callers working.
type PartitionSize = autosnapshot.PartitionSize

// ComputePartitionSizes returns per-day totals across all partitioned tables, sorted oldest first.
func (s *Storage) ComputePartitionSizes(ctx context.Context) ([]PartitionSize, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT substring(c.relname FROM '[0-9]+$') AS day_key,
		       SUM(pg_total_relation_size(c.oid))::bigint AS total
		FROM pg_class c
		WHERE c.relkind = 'r'
		  AND (c.relname LIKE 'snapshots_________'
		       OR c.relname LIKE 'query_texts_________'
		       OR c.relname LIKE 'trigger_events_________')
		GROUP BY day_key
		ORDER BY day_key ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("storage: compute partition sizes: %w", err)
	}
	defer rows.Close()

	var out []PartitionSize

	for rows.Next() {
		var (
			dayKey string
			size   int64
		)

		if err := rows.Scan(&dayKey, &size); err != nil {
			return nil, fmt.Errorf("storage: scan partition size: %w", err)
		}

		day, err := time.Parse("20060102", dayKey)
		if err != nil {
			continue
		}

		out = append(out, PartitionSize{Day: day, TotalSize: size})
	}

	return out, rows.Err()
}

// DropDayPartitions drops all partitioned-table partitions for the given day.
func (s *Storage) DropDayPartitions(ctx context.Context, day time.Time) error {
	dayStr := day.Format("20060102")

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin drop: %w", err)
	}

	defer tx.Rollback(ctx) //nolint:errcheck

	for _, table := range partitionedTables {
		q := fmt.Sprintf(`DROP TABLE IF EXISTS %s_%s`, table, dayStr)
		if _, err := tx.Exec(ctx, q); err != nil {
			return fmt.Errorf("storage: drop %s_%s: %w", table, dayStr, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit drop: %w", err)
	}

	return nil
}

func nullStringPtr(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}
