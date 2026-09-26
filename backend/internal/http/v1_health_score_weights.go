package http

import (
	"context"
	"fmt"
	"time"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/health"
)

func (s *Handlers) GetHealthScoreWeights(
	ctx context.Context,
	req serverhttp.GetHealthScoreWeightsRequestObject,
) (serverhttp.GetHealthScoreWeightsResponseObject, error) {
	resp, err := s.getHealthScoreWeightsResponse(ctx, req.Params.ClusterName)
	if err != nil {
		return nil, fmt.Errorf("GetHealthScoreWeights | %w", err)
	}

	return serverhttp.GetHealthScoreWeights200JSONResponse(resp), nil
}

func (s *Handlers) PutHealthScoreWeights(
	ctx context.Context,
	req serverhttp.PutHealthScoreWeightsRequestObject,
) (serverhttp.PutHealthScoreWeightsResponseObject, error) {
	if req.Body == nil {
		return serverhttp.PutHealthScoreWeights400Response{}, nil
	}

	w := health.Weights{
		Connections:   req.Body.Connections,
		Performance:   req.Body.Performance,
		Storage:       req.Body.Storage,
		Replication:   req.Body.Replication,
		Maintenance:   req.Body.Maintenance,
		Horizon:       req.Body.Horizon,
		WalCheckpoint: req.Body.WalCheckpoint,
		Locks:         req.Body.Locks,
	}

	if err := w.Validate(); err != nil {
		return serverhttp.PutHealthScoreWeights400Response{}, nil
	}

	if s.storage == nil {
		return nil, fmt.Errorf("PutHealthScoreWeights | storage is not configured")
	}

	if err := s.storage.UpsertHealthWeights(ctx, req.Params.ClusterName, w.Normalize(), ""); err != nil {
		return nil, fmt.Errorf("PutHealthScoreWeights | %w", err)
	}

	resp, err := s.getHealthScoreWeightsResponse(ctx, req.Params.ClusterName)
	if err != nil {
		return nil, fmt.Errorf("PutHealthScoreWeights | %w", err)
	}

	return serverhttp.PutHealthScoreWeights200JSONResponse(resp), nil
}

func (s *Handlers) DeleteHealthScoreWeights(
	ctx context.Context,
	req serverhttp.DeleteHealthScoreWeightsRequestObject,
) (serverhttp.DeleteHealthScoreWeightsResponseObject, error) {
	if s.storage != nil {
		if err := s.storage.DeleteHealthWeights(ctx, req.Params.ClusterName); err != nil {
			return nil, fmt.Errorf("DeleteHealthScoreWeights | %w", err)
		}
	}

	resp, err := s.getHealthScoreWeightsResponse(ctx, req.Params.ClusterName)
	if err != nil {
		return nil, fmt.Errorf("DeleteHealthScoreWeights | %w", err)
	}

	return serverhttp.DeleteHealthScoreWeights200JSONResponse(resp), nil
}

func (s *Handlers) getHealthScoreWeightsResponse(
	ctx context.Context,
	clusterName string,
) (serverhttp.HealthScoreWeights, error) {
	var (
		w         health.Weights
		source    = serverhttp.Default
		updatedAt *time.Time
		updatedBy *string
	)

	if s.storage != nil {
		rec, err := s.storage.GetHealthWeights(ctx, clusterName)
		if err != nil {
			return serverhttp.HealthScoreWeights{}, err
		}

		if rec != nil {
			w = rec.Weights
			source = serverhttp.Override
			ts := rec.UpdatedAt
			updatedAt = &ts

			if rec.UpdatedBy != "" {
				by := rec.UpdatedBy
				updatedBy = &by
			}
		}
	}

	if source == serverhttp.Default {
		w = health.DefaultWeights()
	}

	return serverhttp.HealthScoreWeights{
		Connections:   w.Connections,
		Performance:   w.Performance,
		Storage:       w.Storage,
		Replication:   w.Replication,
		Maintenance:   w.Maintenance,
		Horizon:       w.Horizon,
		WalCheckpoint: w.WalCheckpoint,
		Locks:         w.Locks,
		Source:        source,
		UpdatedAt:     updatedAt,
		UpdatedBy:     updatedBy,
	}, nil
}
