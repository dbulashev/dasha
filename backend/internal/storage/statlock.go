package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dbulashev/dasha/internal/statlock"
)

type lockRowJSON struct {
	T  string `json:"t"`
	W  int64  `json:"w"`
	WT int64  `json:"wt"`
	FE int64  `json:"fe"`
}

// InsertLockSnapshot stores one host's raw cumulative pg_stat_lock slice. The
// previous snapshot is the baseline, so a failed write costs one interval.
func (s *Storage) InsertLockSnapshot(
	ctx context.Context,
	clusterName, instance string,
	snap statlock.Snapshot,
) error {
	if err := s.ensureIOPartitions(ctx, snap.CapturedAt, "lock_stat_snapshot"); err != nil {
		return err
	}

	rows := make([]lockRowJSON, 0, len(snap.Rows))
	for _, r := range snap.Rows {
		rows = append(rows, lockRowJSON{T: r.LockType, W: r.Waits, WT: r.WaitTimeMs, FE: r.FastpathExceeded})
	}

	body, err := json.Marshal(rows)
	if err != nil {
		return fmt.Errorf("storage: marshal lock rows: %w", err)
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO lock_stat_snapshot (cluster_name, instance, captured_at, version_num, stats_reset,
		                                deadlock_timeout_ms, max_locks_per_transaction, rows)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`,
		clusterName, instance, snap.CapturedAt, snap.VersionNum, snap.StatsReset,
		snap.DeadlockTimeoutMs, snap.MaxLocksPerTransaction, jsonbArg(body),
	)
	if err != nil {
		return fmt.Errorf("storage: insert lock snapshot: %w", err)
	}

	return nil
}

// GetLockSnapshotMetas returns the headers of one host's captures covering
// [from, to], preceded by the last one before from — the baseline of the first
// interval inside the window.
func (s *Storage) GetLockSnapshotMetas(
	ctx context.Context,
	clusterName, instance string,
	from, to time.Time,
) ([]statlock.Meta, error) {
	rows, err := s.pool.Query(ctx, `
		(
		    SELECT captured_at, version_num, stats_reset, deadlock_timeout_ms, max_locks_per_transaction
		    FROM lock_stat_snapshot
		    WHERE cluster_name = $1 AND instance = $2 AND captured_at < $3
		    ORDER BY captured_at DESC
		    LIMIT 1
		)
		UNION ALL
		(
		    SELECT captured_at, version_num, stats_reset, deadlock_timeout_ms, max_locks_per_transaction
		    FROM lock_stat_snapshot
		    WHERE cluster_name = $1 AND instance = $2 AND captured_at >= $3 AND captured_at <= $4
		)
		ORDER BY captured_at`,
		clusterName, instance, from, to)
	if err != nil {
		return nil, fmt.Errorf("storage: get lock snapshot metas: %w", err)
	}
	defer rows.Close()

	var ret []statlock.Meta

	for rows.Next() {
		var m statlock.Meta

		if err := rows.Scan(&m.CapturedAt, &m.VersionNum, &m.StatsReset,
			&m.DeadlockTimeoutMs, &m.MaxLocksPerTransaction); err != nil {
			return nil, fmt.Errorf("storage: scan lock snapshot meta: %w", err)
		}

		ret = append(ret, m)
	}

	return ret, rows.Err()
}

// GetLockSnapshotsAt loads the bodies of the named captures. The bounds go
// alongside the list so the planner can prune partitions.
func (s *Storage) GetLockSnapshotsAt(
	ctx context.Context,
	clusterName, instance string,
	at []time.Time,
) ([]statlock.Snapshot, error) {
	if len(at) == 0 {
		return nil, nil
	}

	rows, err := s.pool.Query(ctx, `
		SELECT captured_at, version_num, stats_reset, deadlock_timeout_ms, max_locks_per_transaction, rows
		FROM lock_stat_snapshot
		WHERE cluster_name = $1 AND instance = $2
		  AND captured_at >= $3 AND captured_at <= $4
		  AND captured_at = ANY($5::timestamptz[])
		ORDER BY captured_at`,
		clusterName, instance, at[0], at[len(at)-1], at)
	if err != nil {
		return nil, fmt.Errorf("storage: get lock snapshots: %w", err)
	}
	defer rows.Close()

	var ret []statlock.Snapshot

	for rows.Next() {
		var (
			snap statlock.Snapshot
			body []byte
		)

		if err := rows.Scan(&snap.CapturedAt, &snap.VersionNum, &snap.StatsReset,
			&snap.DeadlockTimeoutMs, &snap.MaxLocksPerTransaction, &body); err != nil {
			return nil, fmt.Errorf("storage: scan lock snapshot: %w", err)
		}

		var stored []lockRowJSON
		if err := json.Unmarshal(body, &stored); err != nil {
			return nil, fmt.Errorf("storage: unmarshal lock rows: %w", err)
		}

		snap.Rows = make([]statlock.Row, 0, len(stored))
		for _, r := range stored {
			snap.Rows = append(snap.Rows, statlock.Row{
				LockType: r.T, Waits: r.W, WaitTimeMs: r.WT, FastpathExceeded: r.FE,
			})
		}

		ret = append(ret, snap)
	}

	return ret, rows.Err()
}

// LockSnapshotRange reports the oldest and newest stored capture of one host;
// both are nil when the host has never been captured.
func (s *Storage) LockSnapshotRange(
	ctx context.Context,
	clusterName, instance string,
) (*time.Time, *time.Time, error) {
	var earliest, latest *time.Time

	err := s.pool.QueryRow(ctx, `
		SELECT MIN(captured_at), MAX(captured_at)
		FROM lock_stat_snapshot
		WHERE cluster_name = $1 AND instance = $2`,
		clusterName, instance,
	).Scan(&earliest, &latest)
	if err != nil {
		return nil, nil, fmt.Errorf("storage: lock snapshot range: %w", err)
	}

	return earliest, latest, nil
}
