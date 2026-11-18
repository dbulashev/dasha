package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dbulashev/dasha/internal/piicatalog"
)

func (s *Storage) ListPIIStatus(ctx context.Context) (map[piicatalog.Key]piicatalog.Status, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT cluster_name, database, set_hash, pair_count,
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

		if err := rows.Scan(&k.Cluster, &k.Database, &st.SetHash, &st.PairCount,
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

func (s *Storage) ApplyPIISnapshot(ctx context.Context, k piicatalog.Key, snap piicatalog.Snapshot) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("storage: pii apply begin: %w", err)
	}

	defer tx.Rollback(ctx) //nolint:errcheck

	changed, err := applyPIISnapshotTx(ctx, tx, k, snap)
	if err != nil {
		return false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("storage: pii apply commit: %w", err)
	}

	return changed, nil
}

func applyPIISnapshotTx(ctx context.Context, tx pgx.Tx, k piicatalog.Key, snap piicatalog.Snapshot) (bool, error) {
	cur, err := lockPIIStatusTx(ctx, tx, k)
	if err != nil {
		return false, err
	}

	next, changed, ok := piicatalog.ApplySuccess(cur, snap)
	if !ok {
		return false, nil
	}

	if changed {
		if _, err := tx.Exec(ctx, `DELETE FROM pii_catalog_pair WHERE cluster_name = $1 AND database = $2`,
			k.Cluster, k.Database); err != nil {
			return false, fmt.Errorf("storage: pii clear pairs: %w", err)
		}

		hashes := make([][]byte, len(snap.Pairs))
		names := make([]string, len(snap.Pairs))
		comments := make([]*string, len(snap.Pairs))

		for i, p := range snap.Pairs {
			hashes[i], names[i], comments[i] = p.Hash, p.Name, p.Comment
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO pii_catalog_pair (cluster_name, database, pair_hash, column_name, column_comment)
			SELECT $1, $2, u.h, u.n, u.c
			FROM unnest($3::bytea[], $4::text[], $5::text[]) AS u(h, n, c)`,
			k.Cluster, k.Database, hashes, names, comments); err != nil {
			return false, fmt.Errorf("storage: pii insert pairs: %w", err)
		}
	}

	if err := writePIIStatusTx(ctx, tx, k, next); err != nil {
		return false, err
	}

	return changed, nil
}

func (s *Storage) RecordPIIFailure(ctx context.Context, k piicatalog.Key, attemptAt time.Time, errMsg string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: pii failure begin: %w", err)
	}

	defer tx.Rollback(ctx) //nolint:errcheck

	cur, err := lockPIIStatusTx(ctx, tx, k)
	if err != nil {
		return err
	}

	next, ok := piicatalog.ApplyFailure(cur, attemptAt, errMsg)
	if !ok {
		return nil
	}

	if err := writePIIStatusTx(ctx, tx, k, next); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: pii failure commit: %w", err)
	}

	return nil
}

// lockPIIStatusTx creates the row if missing and locks it: FOR UPDATE
// serializes concurrent writers (several daemons without leader election).
func lockPIIStatusTx(ctx context.Context, tx pgx.Tx, k piicatalog.Key) (piicatalog.Status, error) {
	if _, err := tx.Exec(ctx, `
		INSERT INTO pii_catalog_status (cluster_name, database) VALUES ($1, $2)
		ON CONFLICT (cluster_name, database) DO NOTHING`, k.Cluster, k.Database); err != nil {
		return piicatalog.Status{}, fmt.Errorf("storage: pii status init: %w", err)
	}

	var st piicatalog.Status

	if err := tx.QueryRow(ctx, `
		SELECT set_hash, pair_count, last_attempt_at, last_success_at, last_changed_at, last_error, source_instance
		FROM pii_catalog_status
		WHERE cluster_name = $1 AND database = $2
		FOR UPDATE`, k.Cluster, k.Database).Scan(&st.SetHash, &st.PairCount,
		&st.LastAttemptAt, &st.LastSuccessAt, &st.LastChangedAt, &st.LastError, &st.SourceInstance); err != nil {
		return piicatalog.Status{}, fmt.Errorf("storage: pii status lock: %w", err)
	}

	return st, nil
}

func writePIIStatusTx(ctx context.Context, tx pgx.Tx, k piicatalog.Key, st piicatalog.Status) error {
	if _, err := tx.Exec(ctx, `
		UPDATE pii_catalog_status
		SET set_hash = $3, pair_count = $4, last_attempt_at = $5, last_success_at = $6,
		    last_changed_at = $7, last_error = $8, source_instance = $9
		WHERE cluster_name = $1 AND database = $2`,
		k.Cluster, k.Database, st.SetHash, st.PairCount, st.LastAttemptAt, st.LastSuccessAt,
		st.LastChangedAt, st.LastError, st.SourceInstance); err != nil {
		return fmt.Errorf("storage: pii update status: %w", err)
	}

	return nil
}

// PIIRetention deletes the databases not attempted since cutoff: they are gone
// from the configuration and service discovery. A database still attempted is
// kept even while its collection keeps failing.
func (s *Storage) PIIRetention(ctx context.Context, cutoff time.Time) (int64, error) {
	// Pairs go with the status row: ON DELETE CASCADE.
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM pii_catalog_status
		WHERE coalesce(last_attempt_at, last_success_at) < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("storage: pii retention: %w", err)
	}

	return tag.RowsAffected(), nil
}

// Repeatable read keeps the status row and the pairs on the same set.
var piiReadTx = pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly} //nolint:exhaustruct

// PIICatalog returns the current set ordered by the byte order of name, then comment.
func (s *Storage) PIICatalog(ctx context.Context, k piicatalog.Key) (piicatalog.Catalog, error) {
	tx, err := s.pool.BeginTx(ctx, piiReadTx)
	if err != nil {
		return piicatalog.Catalog{}, fmt.Errorf("storage: pii catalog begin: %w", err)
	}

	defer tx.Rollback(ctx) //nolint:errcheck

	var (
		cat              piicatalog.Catalog
		changed, checked *time.Time
	)

	err = tx.QueryRow(ctx, `
		SELECT set_hash, last_changed_at, last_success_at
		FROM pii_catalog_status
		WHERE cluster_name = $1 AND database = $2`, k.Cluster, k.Database).
		Scan(&cat.SetHash, &changed, &checked)
	if errors.Is(err, pgx.ErrNoRows) {
		return piicatalog.Catalog{}, piicatalog.ErrNoCatalog
	}

	if err != nil {
		return piicatalog.Catalog{}, fmt.Errorf("storage: pii status: %w", err)
	}

	if cat.SetHash == nil || changed == nil || checked == nil {
		return piicatalog.Catalog{}, piicatalog.ErrNoCatalog
	}

	cat.ChangedAt, cat.CheckedAt = *changed, *checked

	rows, err := tx.Query(ctx, `
		SELECT column_name, column_comment
		FROM pii_catalog_pair
		WHERE cluster_name = $1 AND database = $2
		ORDER BY column_name COLLATE "C", column_comment COLLATE "C" NULLS FIRST`, k.Cluster, k.Database)
	if err != nil {
		return piicatalog.Catalog{}, fmt.Errorf("storage: pii pairs: %w", err)
	}

	cat.Pairs, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (piicatalog.Pair, error) {
		var p piicatalog.Pair
		err := row.Scan(&p.Name, &p.Comment)

		return p, err
	})
	if err != nil {
		return piicatalog.Catalog{}, fmt.Errorf("storage: pii pairs: %w", err)
	}

	return cat, nil
}
