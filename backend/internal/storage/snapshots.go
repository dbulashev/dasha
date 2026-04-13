package storage

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/pkg/sanitize"
	"github.com/dbulashev/dasha/internal/version"
)

const currentJSONVersion = 1

// SnapshotListItem is a summary row returned by List.
type SnapshotListItem struct {
	ID           uuid.UUID
	CreatedAt    time.Time
	DashaVersion string
	JsonVersion  int
}

// CreateSnapshot stores a pgss snapshot and returns its id and timestamp.
func (s *Storage) CreateSnapshot(
	ctx context.Context,
	clusterName, instance, database string,
	reports []dto.QueryReport,
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

	// Build JSON: replace Query with query_hash, store texts separately.
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

	var id uuid.UUID

	err = tx.QueryRow(ctx, `
		INSERT INTO snapshots (cluster_name, instance, database, dasha_version, json_version, report_data, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`,
		clusterName, instance, database, version.GetBuildNumber(), currentJSONVersion, data, now,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, time.Time{}, fmt.Errorf("storage: insert snapshot: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, time.Time{}, fmt.Errorf("storage: commit: %w", err)
	}

	return id, now, nil
}

// ListSnapshots returns snapshot summaries for a given cluster/instance/database.
func (s *Storage) ListSnapshots(
	ctx context.Context,
	clusterName, instance, database string,
) ([]SnapshotListItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, created_at, dasha_version, json_version
		FROM snapshots
		WHERE cluster_name = $1 AND instance = $2 AND database = $3
		ORDER BY created_at DESC
		LIMIT 100`,
		clusterName, instance, database,
	)
	if err != nil {
		return nil, fmt.Errorf("storage: list snapshots: %w", err)
	}
	defer rows.Close()

	var items []SnapshotListItem

	for rows.Next() {
		var item SnapshotListItem
		if err := rows.Scan(&item.ID, &item.CreatedAt, &item.DashaVersion, &item.JsonVersion); err != nil {
			return nil, fmt.Errorf("storage: scan snapshot: %w", err)
		}

		items = append(items, item)
	}

	return items, rows.Err()
}

// GetSnapshot returns query report data from a stored snapshot.
func (s *Storage) GetSnapshot(ctx context.Context, id uuid.UUID) ([]dto.QueryReport, error) {
	var (
		data      []byte
		createdAt time.Time
	)

	err := s.pool.QueryRow(ctx,
		`SELECT report_data, created_at FROM snapshots WHERE id = $1`, id,
	).Scan(&data, &createdAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil //nolint:nilnil
		}

		return nil, fmt.Errorf("storage: get snapshot: %w", err)
	}

	type reportJSON struct {
		dto.QueryReport
		QueryHash string `json:"QueryHash"`
	}

	var items []reportJSON
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("storage: unmarshal report: %w", err)
	}

	// Collect unique hashes and resolve texts.
	hashes := make([]string, 0, len(items))
	for _, item := range items {
		hashes = append(hashes, item.QueryHash)
	}

	textMap, err := s.resolveQueryTexts(ctx, hashes, createdAt)
	if err != nil {
		return nil, err
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

	return reports, nil
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
