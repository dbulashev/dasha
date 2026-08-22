package storage

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dbulashev/dasha/internal/autosnapshot"
	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/pkg/sanitize"
	"github.com/dbulashev/dasha/internal/version"
)

// currentJSONVersion 2 stores one row per (queryid, database) with both share
// sets; version 1 rows are instance-wide with no database attribution and can
// only be read back as such.
const (
	currentJSONVersion = 2

	// ScopedJSONVersion is the first json_version whose rows name their database.
	ScopedJSONVersion = 2
)

// SnapshotListItem is a summary row returned by List.
type SnapshotListItem struct {
	ID             uuid.UUID
	CreatedAt      time.Time
	Database       string // database the snapshot was read through, not its content
	Databases      []string
	DashaVersion   string
	JsonVersion    int
	PgssStatsReset *time.Time
	StatsSource    *string // extension read through; nil when not recorded
	HasLocks       bool
	Reason         string // "manual" or "auto:<trigger_type>"
}

// SnapshotOpts is re-exported from autosnapshot to keep storage callers working.
type SnapshotOpts = autosnapshot.SnapshotOpts

// jsonbArg renders a marshaled JSON payload for a jsonb column as a string rather
// than []byte. Under pgx's simple query protocol — used when the storage pool
// reaches the DB through a transaction pooler (e.g. Odyssey) via
// default_query_exec_mode=simple — a []byte is interpolated as a bytea literal
// (\x...), which jsonb rejects; a string is interpolated as a text literal that
// coerces to jsonb. Nil maps to SQL NULL.
func jsonbArg(b []byte) any {
	if b == nil {
		return nil
	}

	return string(b)
}

// CreateSnapshot stores a pgss snapshot and returns its id and timestamp.
// Reason defaults to "manual" when empty.
func (s *Storage) CreateSnapshot(
	ctx context.Context,
	clusterName, instance, database string,
	reports []dto.QueryReport,
	opts SnapshotOpts,
) (uuid.UUID, time.Time, error) {
	now := time.Now().UTC()

	if err := s.ensurePartitions(ctx, now); err != nil {
		return uuid.Nil, time.Time{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, time.Time{}, fmt.Errorf("storage: begin tx: %w", err)
	}

	defer tx.Rollback(ctx) //nolint:errcheck

	dayStart := now.Truncate(24 * time.Hour)
	dayEnd := dayStart.Add(24 * time.Hour)

	type reportJSON struct {
		dto.QueryReport
		QueryHash string `json:"QueryHash"`
	}

	items := make([]reportJSON, 0, len(reports))

	batch := &pgx.Batch{}

	for _, r := range reports {
		text := sanitize.SQL(r.Query)
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(text)))

		batch.Queue(`
			INSERT INTO query_texts (query_hash, query_text, created_at)
			SELECT $1, $2, $3
			WHERE NOT EXISTS (
				SELECT 1 FROM query_texts
				WHERE query_hash = $1 AND created_at >= $4 AND created_at < $5
			)`, hash, text, now, dayStart, dayEnd)

		entry := reportJSON{QueryReport: r, QueryHash: hash}
		entry.Query = ""
		items = append(items, entry)
	}

	br := tx.SendBatch(ctx, batch)
	if err := br.Close(); err != nil {
		return uuid.Nil, time.Time{}, fmt.Errorf("storage: batch insert query_texts: %w", err)
	}

	data, err := json.Marshal(items)
	if err != nil {
		return uuid.Nil, time.Time{}, fmt.Errorf("storage: marshal report: %w", err)
	}

	reason := opts.Reason
	if reason == "" {
		reason = "manual"
	}

	var triggerCtx []byte
	if opts.TriggerContext != nil {
		triggerCtx, err = json.Marshal(opts.TriggerContext)
		if err != nil {
			return uuid.Nil, time.Time{}, fmt.Errorf("storage: marshal trigger context: %w", err)
		}
	}

	var locksData []byte
	if opts.LocksData != nil {
		locksData, err = json.Marshal(opts.LocksData)
		if err != nil {
			return uuid.Nil, time.Time{}, fmt.Errorf("storage: marshal locks data: %w", err)
		}
	}

	var id uuid.UUID

	err = tx.QueryRow(ctx, `
		INSERT INTO snapshots (cluster_name, instance, database, databases, dasha_version, json_version, report_data, created_at, pgss_stats_reset, reason, trigger_context, locks_data, stats_source)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10, $11::jsonb, $12::jsonb, $13)
		RETURNING id`,
		clusterName, instance, database, reportDatabases(reports), version.GetBuildNumber(), currentJSONVersion, jsonbArg(data), now, opts.PgssStatsReset, reason, jsonbArg(triggerCtx), jsonbArg(locksData), nullableText(opts.StatsSource),
	).Scan(&id)
	if err != nil {
		return uuid.Nil, time.Time{}, fmt.Errorf("storage: insert snapshot: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, time.Time{}, fmt.Errorf("storage: commit: %w", err)
	}

	return id, now, nil
}

func nullableText(v string) any {
	if v == "" {
		return nil
	}

	return v
}

// reportDatabases lists the databases represented in a report, sorted, so the
// list endpoint can say what a snapshot covers without opening its jsonb.
func reportDatabases(reports []dto.QueryReport) []string {
	seen := make(map[string]struct{}, 4) //nolint:mnd

	for _, r := range reports {
		if r.Datname != "" {
			seen[r.Datname] = struct{}{}
		}
	}

	return slices.Sorted(maps.Keys(seen))
}

// ListSnapshots returns snapshot summaries for a given cluster/instance. The
// listing is host-wide on purpose: a snapshot holds every database of the
// instance, so one taken through "postgres" must be visible while browsing any
// other database.
func (s *Storage) ListSnapshots(
	ctx context.Context,
	clusterName, instance string,
) ([]SnapshotListItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, created_at, database, COALESCE(databases, '{}'), dasha_version, json_version, pgss_stats_reset, locks_data IS NOT NULL, reason, stats_source
		FROM snapshots
		WHERE cluster_name = $1 AND instance = $2
		ORDER BY created_at DESC
		LIMIT 100`,
		clusterName, instance,
	)
	if err != nil {
		return nil, fmt.Errorf("storage: list snapshots: %w", err)
	}
	defer rows.Close()

	var items []SnapshotListItem

	for rows.Next() {
		var item SnapshotListItem
		if err := rows.Scan(&item.ID, &item.CreatedAt, &item.Database, &item.Databases, &item.DashaVersion, &item.JsonVersion, &item.PgssStatsReset, &item.HasLocks, &item.Reason, &item.StatsSource); err != nil {
			return nil, fmt.Errorf("storage: scan snapshot: %w", err)
		}

		items = append(items, item)
	}

	return items, rows.Err()
}

// GetSnapshot returns query report data from a stored snapshot along with its
// json_version: version 1 carries no database attribution, so the caller must
// serve it instance-wide rather than filtering it down to nothing.
func (s *Storage) GetSnapshot(ctx context.Context, id uuid.UUID) ([]dto.QueryReport, int, error) {
	var (
		data        []byte
		createdAt   time.Time
		jsonVersion int
	)

	err := s.pool.QueryRow(ctx,
		`SELECT report_data, created_at, json_version FROM snapshots WHERE id = $1`, id,
	).Scan(&data, &createdAt, &jsonVersion)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, 0, nil
		}

		return nil, 0, fmt.Errorf("storage: get snapshot: %w", err)
	}

	type reportJSON struct {
		dto.QueryReport
		QueryHash string `json:"QueryHash"`
	}

	var items []reportJSON
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, 0, fmt.Errorf("storage: unmarshal report: %w", err)
	}

	// Collect unique hashes and resolve texts.
	hashes := make([]string, 0, len(items))
	for _, item := range items {
		hashes = append(hashes, item.QueryHash)
	}

	textMap, err := s.resolveQueryTexts(ctx, hashes, createdAt)
	if err != nil {
		return nil, 0, err
	}

	reports := make([]dto.QueryReport, 0, len(items))

	for _, item := range items {
		r := item.QueryReport
		if text, ok := textMap[item.QueryHash]; ok {
			r.Query = text
		} else {
			r.Query = "[unknown]"
		}

		reports = append(reports, r)
	}

	return reports, jsonVersion, nil
}

// GetSnapshotLocks returns the raw locks_data jsonb for a snapshot. found is
// false when the snapshot is missing or carries no lock capture.
func (s *Storage) GetSnapshotLocks(ctx context.Context, id uuid.UUID) ([]byte, bool, error) {
	var data []byte

	err := s.pool.QueryRow(ctx,
		`SELECT locks_data FROM snapshots WHERE id = $1`, id,
	).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}

	if err != nil {
		return nil, false, fmt.Errorf("storage: get snapshot locks: %w", err)
	}

	if data == nil {
		return nil, false, nil
	}

	return data, true, nil
}

// resolveQueryTexts fetches query texts from the same daily partition as the snapshot.
func (s *Storage) resolveQueryTexts(
	ctx context.Context,
	hashes []string,
	snapshotTime time.Time,
) (map[string]string, error) {
	if len(hashes) == 0 {
		return nil, nil
	}

	dayStart := snapshotTime.Truncate(24 * time.Hour)
	dayEnd := dayStart.Add(24 * time.Hour)

	rows, err := s.pool.Query(ctx, `
		SELECT query_hash, query_text
		FROM query_texts
		WHERE query_hash = ANY($1)
		  AND created_at >= $2
		  AND created_at < $3`,
		hashes, dayStart, dayEnd,
	)
	if err != nil {
		return nil, fmt.Errorf("storage: resolve query texts: %w", err)
	}
	defer rows.Close()

	result := make(map[string]string, len(hashes))

	for rows.Next() {
		var hash, text string
		if err := rows.Scan(&hash, &text); err != nil {
			return nil, fmt.Errorf("storage: scan query text: %w", err)
		}

		result[hash] = text
	}

	return result, rows.Err()
}
