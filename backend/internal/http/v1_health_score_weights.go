package http

import (
	"context"
	"errors"
	"fmt"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/health"
	"github.com/dbulashev/dasha/internal/healthscore"
)

func (s *Handlers) GetHealthScoreWeights(
	ctx context.Context,
	req serverhttp.GetHealthScoreWeightsRequestObject,
) (serverhttp.GetHealthScoreWeightsResponseObject, error) {
	v, err := s.scorer.WeightsView(ctx, req.Params.ClusterName)
	if err != nil {
		return nil, fmt.Errorf("GetHealthScoreWeights | %w", err)
	}

	return serverhttp.GetHealthScoreWeights200JSONResponse(mapHealthScoreWeights(v)), nil
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

	v, err := s.scorer.SetWeights(ctx, req.Params.ClusterName, w, userNameFromContext(ctx))
	switch {
	case errors.Is(err, healthscore.ErrInvalidWeights):
		return serverhttp.PutHealthScoreWeights400Response{}, nil
	case errors.Is(err, healthscore.ErrNoWeightsStore):
		return serverhttp.PutHealthScoreWeights501Response{}, nil
	case err != nil:
		return nil, fmt.Errorf("PutHealthScoreWeights | %w", err)
	}

	return serverhttp.PutHealthScoreWeights200JSONResponse(mapHealthScoreWeights(v)), nil
}

func (s *Handlers) DeleteHealthScoreWeights(
	ctx context.Context,
	req serverhttp.DeleteHealthScoreWeightsRequestObject,
) (serverhttp.DeleteHealthScoreWeightsResponseObject, error) {
	v, err := s.scorer.ResetWeights(ctx, req.Params.ClusterName)
	if err != nil {
		return nil, fmt.Errorf("DeleteHealthScoreWeights | %w", err)
	}

	return serverhttp.DeleteHealthScoreWeights200JSONResponse(mapHealthScoreWeights(v)), nil
}

func mapHealthScoreWeights(v healthscore.WeightsView) serverhttp.HealthScoreWeights {
	source := serverhttp.Default
	if v.Source == healthscore.WeightsOverride {
		source = serverhttp.Override
	}

	return serverhttp.HealthScoreWeights{
		Connections:   v.Weights.Connections,
		Performance:   v.Weights.Performance,
		Storage:       v.Weights.Storage,
		Replication:   v.Weights.Replication,
		Maintenance:   v.Weights.Maintenance,
		Horizon:       v.Weights.Horizon,
		WalCheckpoint: v.Weights.WalCheckpoint,
		Locks:         v.Weights.Locks,
		Source:        source,
		UpdatedAt:     v.UpdatedAt,
		UpdatedBy:     v.UpdatedBy,
	}
}
