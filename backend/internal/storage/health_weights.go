package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dbulashev/dasha/internal/health"
	"github.com/dbulashev/dasha/internal/healthscore"
)

func (s *Storage) GetHealthWeights(ctx context.Context, clusterName string) (*healthscore.WeightsRecord, error) {
	var (
		rec       healthscore.WeightsRecord
		updatedBy *string
	)

	err := s.pool.QueryRow(ctx, `
		SELECT connections, performance, storage, replication, maintenance,
		       horizon, wal_checkpoint, locks,
		       updated_at, updated_by
		FROM health_score_weights
		WHERE cluster_name = $1`, clusterName,
	).Scan(
		&rec.Weights.Connections,
		&rec.Weights.Performance,
		&rec.Weights.Storage,
		&rec.Weights.Replication,
		&rec.Weights.Maintenance,
		&rec.Weights.Horizon,
		&rec.Weights.WalCheckpoint,
		&rec.Weights.Locks,
		&rec.UpdatedAt,
		&updatedBy,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil //nolint:nilnil
		}

		return nil, fmt.Errorf("storage: get health weights: %w", err)
	}

	if updatedBy != nil {
		rec.UpdatedBy = *updatedBy
	}

	return &rec, nil
}

func (s *Storage) UpsertHealthWeights(
	ctx context.Context,
	clusterName string,
	w health.Weights,
	updatedBy string,
) (healthscore.WeightsRecord, error) {
	var by *string
	if updatedBy != "" {
		by = &updatedBy
	}

	rec := healthscore.WeightsRecord{Weights: w, UpdatedBy: updatedBy} //nolint:exhaustruct

	err := s.pool.QueryRow(ctx, `
		INSERT INTO health_score_weights
			(cluster_name,
			 connections, performance, storage, replication, maintenance,
			 horizon, wal_checkpoint, locks,
			 updated_at, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now(), $10)
		ON CONFLICT (cluster_name) DO UPDATE SET
			connections    = EXCLUDED.connections,
			performance    = EXCLUDED.performance,
			storage        = EXCLUDED.storage,
			replication    = EXCLUDED.replication,
			maintenance    = EXCLUDED.maintenance,
			horizon        = EXCLUDED.horizon,
			wal_checkpoint = EXCLUDED.wal_checkpoint,
			locks          = EXCLUDED.locks,
			updated_at     = now(),
			updated_by     = EXCLUDED.updated_by
		RETURNING updated_at`,
		clusterName,
		w.Connections, w.Performance, w.Storage, w.Replication, w.Maintenance,
		w.Horizon, w.WalCheckpoint, w.Locks,
		by,
	).Scan(&rec.UpdatedAt)
	if err != nil {
		return healthscore.WeightsRecord{}, fmt.Errorf("storage: upsert health weights: %w", err)
	}

	return rec, nil
}

func (s *Storage) DeleteHealthWeights(ctx context.Context, clusterName string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM health_score_weights WHERE cluster_name = $1`,
		clusterName,
	)
	if err != nil {
		return fmt.Errorf("storage: delete health weights: %w", err)
	}

	return nil
}
