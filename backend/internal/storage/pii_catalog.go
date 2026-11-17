package storage

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dbulashev/dasha/internal/piicatalog"
)

func (s *Storage) ListPIIStatus(ctx context.Context) (map[piicatalog.Key]piicatalog.Status, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT cluster_name, database, current_version_id, set_hash, pair_count,
		       last_attempt_at, last_success_at, last_changed_at, last_error, source_instance
		FROM pii_catalog_status`)
	if err != nil {
		return nil, fmt.Errorf("storage: list pii status: %w", err)
	}
	defer rows.Close()

	out := map[piicatalog.Key]piicatalog.Status{}

	for rows.Next() {
		var (
			k  piicatalog.Key
			st piicatalog.Status
		)

		if err := rows.Scan(&k.Cluster, &k.Database, &st.CurrentVersionID, &st.SetHash, &st.PairCount,
			&st.LastAttemptAt, &st.LastSuccessAt, &st.LastChangedAt, &st.LastError, &st.SourceInstance); err != nil {
			return nil, fmt.Errorf("storage: scan pii status: %w", err)
		}

		out[k] = st
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: list pii status: %w", err)
	}

	return out, nil
}

func (s *Storage) ApplyPIISnapshot(
	ctx context.Context, k piicatalog.Key, snap piicatalog.Snapshot,
) (piicatalog.ApplyResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return piicatalog.ApplyResult{}, fmt.Errorf("storage: pii apply begin: %w", err)
	}

	defer tx.Rollback(ctx) //nolint:errcheck

	res, err := applyPIISnapshotTx(ctx, tx, k, snap)
	if err != nil {
		return piicatalog.ApplyResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return piicatalog.ApplyResult{}, fmt.Errorf("storage: pii apply commit: %w", err)
	}

	return res, nil
}

func applyPIISnapshotTx(
	ctx context.Context, tx pgx.Tx, k piicatalog.Key, snap piicatalog.Snapshot,
) (piicatalog.ApplyResult, error) {
	if _, err := tx.Exec(ctx, `
		INSERT INTO pii_catalog_status (cluster_name, database) VALUES ($1, $2)
		ON CONFLICT (cluster_name, database) DO NOTHING`, k.Cluster, k.Database); err != nil {
		return piicatalog.ApplyResult{}, fmt.Errorf("storage: pii status init: %w", err)
	}

	// FOR UPDATE serializes concurrent writers (several daemons without leader election).
	var (
		curVersion *int64
		curHash    []byte
	)

	if err := tx.QueryRow(ctx, `
		SELECT current_version_id, set_hash FROM pii_catalog_status
		WHERE cluster_name = $1 AND database = $2
		FOR UPDATE`, k.Cluster, k.Database).Scan(&curVersion, &curHash); err != nil {
		return piicatalog.ApplyResult{}, fmt.Errorf("storage: pii status lock: %w", err)
	}

	if curVersion != nil && bytes.Equal(curHash, snap.SetHash) {
		return piicatalog.ApplyResult{}, markPIIChecked(ctx, tx, k, snap)
	}

	rows, err := tx.Query(ctx, `
		SELECT pair_hash FROM pii_catalog_pair
		WHERE cluster_name = $1 AND database = $2 AND removed_at IS NULL`, k.Cluster, k.Database)
	if err != nil {
		return piicatalog.ApplyResult{}, fmt.Errorf("storage: pii current pairs: %w", err)
	}

	current, err := pgx.CollectRows(rows, pgx.RowTo[[]byte])
	if err != nil {
		return piicatalog.ApplyResult{}, fmt.Errorf("storage: pii current pairs: %w", err)
	}

	added, removed := piicatalog.Diff(current, snap)

	// Only the hash canonicalization changed.
	if curVersion != nil && len(added) == 0 && len(removed) == 0 {
		return piicatalog.ApplyResult{}, markPIIChecked(ctx, tx, k, snap)
	}

	var (
		versionID int64
		createdAt time.Time
	)

	if err := tx.QueryRow(ctx, `
		INSERT INTO pii_catalog_version
		    (cluster_name, database, set_hash, pair_count, added_count, removed_count, source_instance, server_version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at`,
		k.Cluster, k.Database, snap.SetHash, len(snap.Pairs), len(added), len(removed),
		snap.SourceInstance, snap.ServerVersion).Scan(&versionID, &createdAt); err != nil {
		return piicatalog.ApplyResult{}, fmt.Errorf("storage: pii insert version: %w", err)
	}

	if len(removed) > 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE pii_catalog_pair SET removed_version = $3, removed_at = $4
			WHERE cluster_name = $1 AND database = $2 AND removed_at IS NULL
			  AND pair_hash = ANY($5::bytea[])`,
			k.Cluster, k.Database, versionID, createdAt, removed); err != nil {
			return piicatalog.ApplyResult{}, fmt.Errorf("storage: pii remove pairs: %w", err)
		}
	}

	if len(added) > 0 {
		hashes := make([][]byte, len(added))
		names := make([]string, len(added))
		comments := make([]*string, len(added))

		for i, p := range added {
			hashes[i], names[i], comments[i] = p.Hash, p.Name, p.Comment
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO pii_catalog_pair
			    (cluster_name, database, pair_hash, column_name, column_comment, added_version, added_at)
			SELECT $1, $2, u.h, u.n, u.c, $3, $4
			FROM unnest($5::bytea[], $6::text[], $7::text[]) AS u(h, n, c)`,
			k.Cluster, k.Database, versionID, createdAt, hashes, names, comments); err != nil {
			return piicatalog.ApplyResult{}, fmt.Errorf("storage: pii add pairs: %w", err)
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE pii_catalog_status
		SET current_version_id = $3, set_hash = $4, pair_count = $5,
		    last_changed_at = $6, last_success_at = $6, last_attempt_at = $7,
		    last_error = NULL, source_instance = $8
		WHERE cluster_name = $1 AND database = $2`,
		k.Cluster, k.Database, versionID, snap.SetHash, len(snap.Pairs),
		createdAt, snap.AttemptAt, snap.SourceInstance); err != nil {
		return piicatalog.ApplyResult{}, fmt.Errorf("storage: pii update status: %w", err)
	}

	return piicatalog.ApplyResult{VersionID: versionID, Added: len(added), Removed: len(removed)}, nil
}

func markPIIChecked(ctx context.Context, tx pgx.Tx, k piicatalog.Key, snap piicatalog.Snapshot) error {
	if _, err := tx.Exec(ctx, `
		UPDATE pii_catalog_status
		SET set_hash = $3, pair_count = $4, last_attempt_at = $5, last_success_at = now(),
		    last_error = NULL, source_instance = $6
		WHERE cluster_name = $1 AND database = $2`,
		k.Cluster, k.Database, snap.SetHash, len(snap.Pairs), snap.AttemptAt, snap.SourceInstance); err != nil {
		return fmt.Errorf("storage: pii mark checked: %w", err)
	}

	return nil
}

func (s *Storage) RecordPIIFailure(ctx context.Context, k piicatalog.Key, attemptAt time.Time, errMsg string) error {
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO pii_catalog_status (cluster_name, database, last_attempt_at, last_error)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (cluster_name, database) DO UPDATE
		SET last_attempt_at = EXCLUDED.last_attempt_at, last_error = EXCLUDED.last_error`,
		k.Cluster, k.Database, attemptAt, errMsg); err != nil {
		return fmt.Errorf("storage: pii record failure: %w", err)
	}

	return nil
}

// PIIRetention never deletes the current pairs or the current version of a
// database that is still being attempted. A database is an orphan once nothing
// has attempted it since cutoff (gone from the config); last_attempt_at rather
// than last_success_at, so a live database failing for a long time keeps its
// last known catalog.
func (s *Storage) PIIRetention(ctx context.Context, cutoff, auditCutoff time.Time) (piicatalog.RetentionStats, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return piicatalog.RetentionStats{}, fmt.Errorf("storage: pii retention begin: %w", err)
	}

	defer tx.Rollback(ctx) //nolint:errcheck

	var st piicatalog.RetentionStats

	if err := tx.QueryRow(ctx, `
		WITH orphan AS (
		    DELETE FROM pii_catalog_status
		    WHERE coalesce(last_attempt_at, last_success_at) < $1
		    RETURNING cluster_name, database
		), dp AS (
		    DELETE FROM pii_catalog_pair p USING orphan o
		    WHERE p.cluster_name = o.cluster_name AND p.database = o.database
		    RETURNING 1
		), dv AS (
		    DELETE FROM pii_catalog_version v USING orphan o
		    WHERE v.cluster_name = o.cluster_name AND v.database = o.database
		    RETURNING 1
		)
		SELECT (SELECT count(*) FROM orphan), (SELECT count(*) FROM dp), (SELECT count(*) FROM dv)`,
		cutoff).Scan(&st.OrphanDatabases, &st.Pairs, &st.Versions); err != nil {
		return piicatalog.RetentionStats{}, fmt.Errorf("storage: pii retention orphans: %w", err)
	}

	tag, err := tx.Exec(ctx, `DELETE FROM pii_catalog_pair WHERE removed_at < $1`, cutoff)
	if err != nil {
		return piicatalog.RetentionStats{}, fmt.Errorf("storage: pii retention pairs: %w", err)
	}

	st.Pairs += tag.RowsAffected()

	tag, err = tx.Exec(ctx, `
		DELETE FROM pii_catalog_version v
		WHERE v.created_at < $1
		  AND NOT EXISTS (SELECT 1 FROM pii_catalog_status s WHERE s.current_version_id = v.id)`, cutoff)
	if err != nil {
		return piicatalog.RetentionStats{}, fmt.Errorf("storage: pii retention versions: %w", err)
	}

	st.Versions += tag.RowsAffected()

	tag, err = tx.Exec(ctx, `DELETE FROM pii_catalog_access_log WHERE at < $1`, auditCutoff)
	if err != nil {
		return piicatalog.RetentionStats{}, fmt.Errorf("storage: pii retention access log: %w", err)
	}

	st.AccessLog = tag.RowsAffected()

	if err := tx.Commit(ctx); err != nil {
		return piicatalog.RetentionStats{}, fmt.Errorf("storage: pii retention commit: %w", err)
	}

	return st, nil
}
