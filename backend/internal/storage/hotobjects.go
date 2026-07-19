package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dbulashev/dasha/internal/hotobjects"
)

// UpsertHotAnchors replaces the anchor slice of one cluster×host×database in a
// single transaction: every current object is upserted with the same
// captured_at, then rows that kept an older captured_at (objects that
// disappeared from the target DB) are deleted. Only non-indexed columns are
// updated, so the writes stay HOT-eligible (see the DDL comment).
func (s *Storage) UpsertHotAnchors(
	ctx context.Context,
	clusterName, instance, database string,
	capturedAt time.Time,
	rows []hotobjects.AnchorRow,
) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: hot anchors begin: %w", err)
	}

	defer tx.Rollback(ctx) //nolint:errcheck

	batch := &pgx.Batch{}

	for _, r := range rows {
		counters, err := json.Marshal(r.Counters)
		if err != nil {
			return fmt.Errorf("storage: marshal anchor counters: %w", err)
		}

		batch.Queue(`
			INSERT INTO hot_anchor (cluster_name, instance, database, kind, schema_name, object_name,
			                        table_name, captured_at, stats_reset, size_bytes, counters)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb)
			ON CONFLICT (cluster_name, instance, database, kind, schema_name, object_name)
			DO UPDATE SET table_name = EXCLUDED.table_name,
			              captured_at = EXCLUDED.captured_at,
			              stats_reset = EXCLUDED.stats_reset,
			              size_bytes = EXCLUDED.size_bytes,
			              counters = EXCLUDED.counters`,
			clusterName, instance, database, string(r.Kind), r.Schema, r.Object,
			nullIfEmpty(r.TableName), capturedAt, r.StatsReset, r.SizeBytes, jsonbArg(counters))
	}

	br := tx.SendBatch(ctx, batch)
	if err := br.Close(); err != nil {
		return fmt.Errorf("storage: upsert hot anchors: %w", err)
	}

	// Anything not touched by this batch kept its old captured_at: the object
	// is gone from the target database, so its anchor goes too.
	_, err = tx.Exec(ctx, `
		DELETE FROM hot_anchor
		WHERE cluster_name = $1 AND instance = $2 AND database = $3 AND captured_at < $4`,
		clusterName, instance, database, capturedAt)
	if err != nil {
		return fmt.Errorf("storage: prune hot anchors: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: hot anchors commit: %w", err)
	}

	return nil
}

// GetHotAnchors returns the anchor slice of one cluster×host×database as a map
// keyed by kind+schema+object for delta computation.
func (s *Storage) GetHotAnchors(
	ctx context.Context,
	clusterName, instance, database string,
) (map[string]hotobjects.AnchorRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT kind, schema_name, object_name, COALESCE(table_name, ''), captured_at, stats_reset, size_bytes, counters
		FROM hot_anchor
		WHERE cluster_name = $1 AND instance = $2 AND database = $3`,
		clusterName, instance, database)
	if err != nil {
		return nil, fmt.Errorf("storage: get hot anchors: %w", err)
	}
	defer rows.Close()

	ret := make(map[string]hotobjects.AnchorRow)

	for rows.Next() {
		var (
			a        hotobjects.AnchorRow
			kind     string
			counters []byte
		)

		if err := rows.Scan(&kind, &a.Schema, &a.Object, &a.TableName, &a.CapturedAt, &a.StatsReset, &a.SizeBytes, &counters); err != nil {
			return nil, fmt.Errorf("storage: scan hot anchor: %w", err)
		}

		a.Kind = hotobjects.Kind(kind)
		a.Instance = instance

		if err := json.Unmarshal(counters, &a.Counters); err != nil {
			return nil, fmt.Errorf("storage: unmarshal anchor counters: %w", err)
		}

		ret[hotobjects.Key(a.Kind, a.Schema, a.Object)] = a
	}

	return ret, rows.Err()
}

// GetHotAnchorsForObject returns one object's anchors across all hosts — the
// working set of the live-percentile computation.
func (s *Storage) GetHotAnchorsForObject(
	ctx context.Context,
	clusterName, database string,
	kind hotobjects.Kind,
	schema, object string,
) ([]hotobjects.AnchorRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT instance, COALESCE(table_name, ''), captured_at, stats_reset, size_bytes, counters
		FROM hot_anchor
		WHERE cluster_name = $1 AND database = $2 AND kind = $3 AND schema_name = $4 AND object_name = $5`,
		clusterName, database, string(kind), schema, object)
	if err != nil {
		return nil, fmt.Errorf("storage: get object anchors: %w", err)
	}
	defer rows.Close()

	var ret []hotobjects.AnchorRow

	for rows.Next() {
		a := hotobjects.AnchorRow{Kind: kind, Schema: schema, Object: object} //nolint:exhaustruct

		var counters []byte
		if err := rows.Scan(&a.Instance, &a.TableName, &a.CapturedAt, &a.StatsReset, &a.SizeBytes, &counters); err != nil {
			return nil, fmt.Errorf("storage: scan object anchor: %w", err)
		}

		if err := json.Unmarshal(counters, &a.Counters); err != nil {
			return nil, fmt.Errorf("storage: unmarshal anchor counters: %w", err)
		}

		ret = append(ret, a)
	}

	return ret, rows.Err()
}

// InsertHotSnapshot stores one capture (meta row + top rows) atomically.
func (s *Storage) InsertHotSnapshot(ctx context.Context, snap hotobjects.Snapshot) (uuid.UUID, error) {
	if err := s.ensureHotPartitions(ctx, snap.CapturedAt); err != nil {
		return uuid.Nil, err
	}

	windows, err := json.Marshal(snap.Windows)
	if err != nil {
		return uuid.Nil, fmt.Errorf("storage: marshal hot windows: %w", err)
	}

	coverage, err := json.Marshal(snap.Coverage)
	if err != nil {
		return uuid.Nil, fmt.Errorf("storage: marshal hot coverage: %w", err)
	}

	histogram, err := json.Marshal(snap.Histograms)
	if err != nil {
		return uuid.Nil, fmt.Errorf("storage: marshal hot histogram: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("storage: hot snapshot begin: %w", err)
	}

	defer tx.Rollback(ctx) //nolint:errcheck

	hostsMissing := snap.HostsMissing
	if hostsMissing == nil {
		hostsMissing = []string{}
	}

	var id uuid.UUID

	err = tx.QueryRow(ctx, `
		INSERT INTO hot_snapshot (cluster_name, database, captured_at, windows, hosts_missing, coverage, histogram)
		VALUES ($1, $2, $3, $4::jsonb, $5, $6::jsonb, $7::jsonb)
		RETURNING id`,
		snap.ClusterName, snap.Database, snap.CapturedAt,
		jsonbArg(windows), hostsMissing, jsonbArg(coverage), jsonbArg(histogram),
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("storage: insert hot snapshot: %w", err)
	}

	batch := &pgx.Batch{}

	for _, e := range snap.Top {
		delta, err := json.Marshal(e.Delta)
		if err != nil {
			return uuid.Nil, fmt.Errorf("storage: marshal top delta: %w", err)
		}

		perHost, err := json.Marshal(e.PerHost)
		if err != nil {
			return uuid.Nil, fmt.Errorf("storage: marshal top per-host: %w", err)
		}

		batch.Queue(`
			INSERT INTO hot_top (snapshot_id, captured_at, cluster_name, database, kind, class, rank,
			                     schema_name, object_name, table_name, size_bytes, delta, per_host)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13::jsonb)`,
			id, snap.CapturedAt, snap.ClusterName, snap.Database, string(e.Kind), string(e.Class), e.Rank,
			e.Schema, e.Object, nullIfEmpty(e.TableName), e.SizeBytes, jsonbArg(delta), jsonbArg(perHost))
	}

	br := tx.SendBatch(ctx, batch)
	if err := br.Close(); err != nil {
		return uuid.Nil, fmt.Errorf("storage: insert hot top: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, fmt.Errorf("storage: hot snapshot commit: %w", err)
	}

	return id, nil
}

// GetHotSnapshot returns snapshot metadata for a cluster×database: the latest
// one, or the exact capture when at is set (values come from
// ListHotSnapshotDates). Returns nil when none.
func (s *Storage) GetHotSnapshot(
	ctx context.Context,
	clusterName, database string,
	at *time.Time,
) (*hotobjects.Snapshot, error) {
	query := `
		SELECT id, captured_at, windows, hosts_missing, coverage, histogram
		FROM hot_snapshot
		WHERE cluster_name = $1 AND database = $2`
	args := []any{clusterName, database}

	if at != nil {
		query += ` AND captured_at = $3`
		args = append(args, *at)
	}

	query += ` ORDER BY captured_at DESC LIMIT 1`

	var (
		snap                         hotobjects.Snapshot
		windows, coverage, histogram []byte
	)

	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&snap.ID, &snap.CapturedAt, &windows, &snap.HostsMissing, &coverage, &histogram)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil
	}

	if err != nil {
		return nil, fmt.Errorf("storage: get hot snapshot: %w", err)
	}

	snap.ClusterName = clusterName
	snap.Database = database

	if err := json.Unmarshal(windows, &snap.Windows); err != nil {
		return nil, fmt.Errorf("storage: unmarshal hot windows: %w", err)
	}

	if err := json.Unmarshal(coverage, &snap.Coverage); err != nil {
		return nil, fmt.Errorf("storage: unmarshal hot coverage: %w", err)
	}

	if err := json.Unmarshal(histogram, &snap.Histograms); err != nil {
		return nil, fmt.Errorf("storage: unmarshal hot histogram: %w", err)
	}

	return &snap, nil
}

// GetHotSnapshotBefore returns the newest snapshot captured strictly before
// the given time; nil when none. Feeds the previous-rank (trend) lookup.
func (s *Storage) GetHotSnapshotBefore(
	ctx context.Context,
	clusterName, database string,
	before time.Time,
) (*hotobjects.Snapshot, error) {
	var (
		snap hotobjects.Snapshot
	)

	err := s.pool.QueryRow(ctx, `
		SELECT id, captured_at
		FROM hot_snapshot
		WHERE cluster_name = $1 AND database = $2 AND captured_at < $3
		ORDER BY captured_at DESC
		LIMIT 1`,
		clusterName, database, before,
	).Scan(&snap.ID, &snap.CapturedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil
	}

	if err != nil {
		return nil, fmt.Errorf("storage: get hot snapshot before: %w", err)
	}

	snap.ClusterName = clusterName
	snap.Database = database

	return &snap, nil
}

// ListHotSnapshotDates returns capture timestamps available for the selector,
// newest first.
func (s *Storage) ListHotSnapshotDates(
	ctx context.Context,
	clusterName, database string,
	limit int,
) ([]time.Time, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT captured_at FROM hot_snapshot
		WHERE cluster_name = $1 AND database = $2
		ORDER BY captured_at DESC
		LIMIT $3`,
		clusterName, database, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: list hot snapshot dates: %w", err)
	}
	defer rows.Close()

	var ret []time.Time

	for rows.Next() {
		var t time.Time
		if err := rows.Scan(&t); err != nil {
			return nil, fmt.Errorf("storage: scan hot snapshot date: %w", err)
		}

		ret = append(ret, t)
	}

	return ret, rows.Err()
}

// GetHotTop returns the stored top of one snapshot for a kind+class,
// rank-ordered with limit/offset.
func (s *Storage) GetHotTop(
	ctx context.Context,
	snapshotID uuid.UUID,
	capturedAt time.Time,
	kind hotobjects.Kind,
	class hotobjects.Class,
	limit, offset int,
) ([]hotobjects.TopEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT rank, schema_name, object_name, COALESCE(table_name, ''), size_bytes, delta, per_host
		FROM hot_top
		WHERE snapshot_id = $1 AND captured_at = $2 AND kind = $3 AND class = $4
		ORDER BY rank
		LIMIT $5 OFFSET $6`,
		snapshotID, capturedAt, string(kind), string(class), limit, offset)
	if err != nil {
		return nil, fmt.Errorf("storage: get hot top: %w", err)
	}
	defer rows.Close()

	var ret []hotobjects.TopEntry

	for rows.Next() {
		e := hotobjects.TopEntry{Kind: kind, Class: class} //nolint:exhaustruct

		var delta, perHost []byte
		if err := rows.Scan(&e.Rank, &e.Schema, &e.Object, &e.TableName, &e.SizeBytes, &delta, &perHost); err != nil {
			return nil, fmt.Errorf("storage: scan hot top: %w", err)
		}

		if err := json.Unmarshal(delta, &e.Delta); err != nil {
			return nil, fmt.Errorf("storage: unmarshal top delta: %w", err)
		}

		if err := json.Unmarshal(perHost, &e.PerHost); err != nil {
			return nil, fmt.Errorf("storage: unmarshal top per-host: %w", err)
		}

		ret = append(ret, e)
	}

	return ret, rows.Err()
}

// GetHotRanks returns rank by schema.object for one snapshot+kind+class — a
// single lookup that lets the API attach PrevRank without loading a second
// full snapshot.
func (s *Storage) GetHotRanks(
	ctx context.Context,
	snapshotID uuid.UUID,
	capturedAt time.Time,
	kind hotobjects.Kind,
	class hotobjects.Class,
) (map[string]int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT schema_name, object_name, rank
		FROM hot_top
		WHERE snapshot_id = $1 AND captured_at = $2 AND kind = $3 AND class = $4`,
		snapshotID, capturedAt, string(kind), string(class))
	if err != nil {
		return nil, fmt.Errorf("storage: get hot ranks: %w", err)
	}
	defer rows.Close()

	ret := make(map[string]int)

	for rows.Next() {
		var (
			schema, object string
			rank           int
		)

		if err := rows.Scan(&schema, &object, &rank); err != nil {
			return nil, fmt.Errorf("storage: scan hot rank: %w", err)
		}

		ret[schema+"."+object] = rank
	}

	return ret, rows.Err()
}

// GetHotObjectHistory returns the days an object appeared in a stored top.
func (s *Storage) GetHotObjectHistory(
	ctx context.Context,
	clusterName, database string,
	kind hotobjects.Kind,
	schema, object string,
	from, to time.Time,
) ([]hotobjects.HistoryEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT captured_at, class, rank, size_bytes, delta
		FROM hot_top
		WHERE cluster_name = $1 AND database = $2 AND kind = $3
		  AND schema_name = $4 AND object_name = $5
		  AND captured_at >= $6 AND captured_at < $7
		ORDER BY captured_at DESC, class`,
		clusterName, database, string(kind), schema, object, from, to)
	if err != nil {
		return nil, fmt.Errorf("storage: get hot object history: %w", err)
	}
	defer rows.Close()

	var ret []hotobjects.HistoryEntry

	for rows.Next() {
		var (
			e     hotobjects.HistoryEntry
			class string
			delta []byte
		)

		if err := rows.Scan(&e.CapturedAt, &class, &e.Rank, &e.SizeBytes, &delta); err != nil {
			return nil, fmt.Errorf("storage: scan hot history: %w", err)
		}

		e.Class = hotobjects.Class(class)

		if err := json.Unmarshal(delta, &e.Delta); err != nil {
			return nil, fmt.Errorf("storage: unmarshal history delta: %w", err)
		}

		ret = append(ret, e)
	}

	return ret, rows.Err()
}

// LastHotSnapshotAt returns the newest capture time per cluster×database —
// the daemon's debounce input (one query for all clusters, like
// LastAutoSnapshotAt).
func (s *Storage) LastHotSnapshotAt(ctx context.Context) (map[string]time.Time, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT cluster_name, database, MAX(captured_at)
		FROM hot_snapshot
		GROUP BY cluster_name, database`)
	if err != nil {
		return nil, fmt.Errorf("storage: last hot snapshot at: %w", err)
	}
	defer rows.Close()

	ret := make(map[string]time.Time)

	for rows.Next() {
		var (
			cluster, database string
			at                time.Time
		)

		if err := rows.Scan(&cluster, &database, &at); err != nil {
			return nil, fmt.Errorf("storage: scan last hot snapshot: %w", err)
		}

		ret[cluster+"/"+database] = at
	}

	return ret, rows.Err()
}

// DropHotPartitionsBefore removes hot-objects day partitions older than the
// cutoff. The hot group has its own age-based retention, independent from the
// size-based pgss retention.
func (s *Storage) DropHotPartitionsBefore(ctx context.Context, cutoff time.Time) error {
	rows, err := s.pool.Query(ctx, `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_inherits i ON i.inhrelid = c.oid
		JOIN pg_class p ON p.oid = i.inhparent
		WHERE p.relname = ANY($1)`,
		hotPartitionedTables)
	if err != nil {
		return fmt.Errorf("storage: list hot partitions: %w", err)
	}
	defer rows.Close()

	var names []string

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("storage: scan hot partition: %w", err)
		}

		names = append(names, name)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("storage: list hot partitions: %w", err)
	}

	cutoffDay := cutoff.UTC().Format("20060102")

	for _, name := range names {
		// Partition names end in the _YYYYMMDD suffix; string comparison on the
		// fixed-width digit suffix is date comparison.
		if len(name) < 9 || name[len(name)-9] != '_' {
			continue
		}

		suffix := name[len(name)-8:]
		if !isDigits(suffix) || suffix >= cutoffDay {
			continue
		}

		if _, err := s.ddlPool.Exec(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS %q`, name)); err != nil {
			return fmt.Errorf("storage: drop hot partition %s: %w", name, err)
		}
	}

	return nil
}

func isDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}

	return len(s) > 0
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}

	return s
}
