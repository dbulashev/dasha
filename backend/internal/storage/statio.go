package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/dbulashev/dasha/internal/statio"
)

// ioRowJSON is the stored shape of one pg_stat_io row. The keys are short
// because the whole matrix is repeated in every snapshot.
type ioRowJSON struct {
	B string           `json:"b"`
	O string           `json:"o"`
	C string           `json:"c"`
	V map[string]int64 `json:"v"`
}

// InsertIOSnapshot stores one host's raw cumulative pg_stat_io slice. Unlike a
// hot-objects capture there are no anchors to advance: the previous snapshot is
// the baseline, so a failed write costs one interval and nothing else.
func (s *Storage) InsertIOSnapshot(
	ctx context.Context,
	clusterName, instance string,
	snap statio.Snapshot,
) (uuid.UUID, error) {
	if err := s.ensureIOPartitions(ctx, snap.CapturedAt); err != nil {
		return uuid.Nil, err
	}

	rows := make([]ioRowJSON, 0, len(snap.Rows))
	for _, r := range snap.Rows {
		rows = append(rows, ioRowJSON{B: r.BackendType, O: r.Object, C: r.Context, V: r.Values})
	}

	body, err := json.Marshal(rows)
	if err != nil {
		return uuid.Nil, fmt.Errorf("storage: marshal io rows: %w", err)
	}

	var id uuid.UUID

	err = s.pool.QueryRow(ctx, `
		INSERT INTO io_snapshot (cluster_name, instance, captured_at, version_num, op_bytes,
		                         track_io_timing, stats_reset, rows)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)
		RETURNING id`,
		clusterName, instance, snap.CapturedAt, snap.VersionNum, snap.OpBytes,
		snap.TrackIOTiming, snap.StatsReset, jsonbArg(body),
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("storage: insert io snapshot: %w", err)
	}

	return id, nil
}

// LastIOSnapshotAt returns the newest capture time per cluster/instance — the
// daemon's schedule debounce.
func (s *Storage) LastIOSnapshotAt(ctx context.Context) (map[string]time.Time, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT cluster_name, instance, MAX(captured_at)
		FROM io_snapshot
		GROUP BY cluster_name, instance`)
	if err != nil {
		return nil, fmt.Errorf("storage: last io snapshot at: %w", err)
	}
	defer rows.Close()

	ret := make(map[string]time.Time)

	for rows.Next() {
		var (
			cluster, instance string
			at                time.Time
		)

		if err := rows.Scan(&cluster, &instance, &at); err != nil {
			return nil, fmt.Errorf("storage: scan last io snapshot: %w", err)
		}

		ret[cluster+"/"+instance] = at
	}

	return ret, rows.Err()
}

// GetIOSnapshots returns one host's snapshots covering [from, to], preceded by
// the last snapshot before `from` — without it the first interval inside the
// window would have no baseline and silently disappear.
func (s *Storage) GetIOSnapshots(
	ctx context.Context,
	clusterName, instance string,
	from, to time.Time,
) ([]statio.Snapshot, error) {
	rows, err := s.pool.Query(ctx, `
		(
		    SELECT captured_at, version_num, op_bytes, track_io_timing, stats_reset, rows
		    FROM io_snapshot
		    WHERE cluster_name = $1 AND instance = $2 AND captured_at < $3
		    ORDER BY captured_at DESC
		    LIMIT 1
		)
		UNION ALL
		(
		    SELECT captured_at, version_num, op_bytes, track_io_timing, stats_reset, rows
		    FROM io_snapshot
		    WHERE cluster_name = $1 AND instance = $2 AND captured_at >= $3 AND captured_at <= $4
		)
		ORDER BY captured_at`,
		clusterName, instance, from, to)
	if err != nil {
		return nil, fmt.Errorf("storage: get io snapshots: %w", err)
	}
	defer rows.Close()

	var ret []statio.Snapshot

	for rows.Next() {
		var (
			snap statio.Snapshot
			body []byte
		)

		if err := rows.Scan(&snap.CapturedAt, &snap.VersionNum, &snap.OpBytes,
			&snap.TrackIOTiming, &snap.StatsReset, &body); err != nil {
			return nil, fmt.Errorf("storage: scan io snapshot: %w", err)
		}

		var stored []ioRowJSON
		if err := json.Unmarshal(body, &stored); err != nil {
			return nil, fmt.Errorf("storage: unmarshal io rows: %w", err)
		}

		snap.Rows = make([]statio.Row, 0, len(stored))
		for _, r := range stored {
			snap.Rows = append(snap.Rows, statio.Row{
				Key:    statio.Key{BackendType: r.B, Object: r.O, Context: r.C},
				Values: r.V,
			})
		}

		ret = append(ret, snap)
	}

	return ret, rows.Err()
}

// IOSnapshotRange reports the oldest and newest stored capture of one host —
// what the UI needs to say from when history exists. Both are nil when the host
// has never been captured.
func (s *Storage) IOSnapshotRange(
	ctx context.Context,
	clusterName, instance string,
) (*time.Time, *time.Time, error) {
	var earliest, latest *time.Time

	err := s.pool.QueryRow(ctx, `
		SELECT MIN(captured_at), MAX(captured_at)
		FROM io_snapshot
		WHERE cluster_name = $1 AND instance = $2`,
		clusterName, instance,
	).Scan(&earliest, &latest)
	if err != nil {
		return nil, nil, fmt.Errorf("storage: io snapshot range: %w", err)
	}

	return earliest, latest, nil
}

// DropIOPartitionsBefore removes pg_stat_io day partitions older than the
// cutoff. Its own age-based retention: I/O history is far denser than the
// hot-objects one and loses value much faster.
func (s *Storage) DropIOPartitionsBefore(ctx context.Context, cutoff time.Time) error {
	return s.dropPartitionsBefore(ctx, ioPartitionedTables, cutoff)
}
