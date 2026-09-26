package http

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/healthscore"
)

func (s *Handlers) GetHealthScoreFleet(
	ctx context.Context,
	req serverhttp.GetHealthScoreFleetRequestObject,
) (serverhttp.GetHealthScoreFleetResponseObject, error) {
	fr := healthscore.FleetRequest{}

	if req.Params.Limit != nil {
		fr.Limit = min(max(*req.Params.Limit, 1), config.MaxFleetLimit)
	}

	if req.Params.ClusterName != nil {
		fr.Clusters = *req.Params.ClusterName
	}

	if req.Params.Exhaustive != nil {
		fr.Exhaustive = *req.Params.Exhaustive
	}

	res, err := s.fleet.Worst(ctx, fr)
	if errors.Is(err, healthscore.ErrUnknownCluster) || errors.Is(err, healthscore.ErrExhaustiveNotAllowed) {
		return serverhttp.GetHealthScoreFleet400JSONResponse{Message: err.Error()}, nil
	}

	if errors.Is(err, healthscore.ErrFleetBusy) {
		return serverhttp.GetHealthScoreFleet503JSONResponse{
			Body:    serverhttp.ErrorMessage{Message: err.Error()},
			Headers: serverhttp.GetHealthScoreFleet503ResponseHeaders{RetryAfter: int(math.Ceil(s.fleet.RetryAfter().Seconds()))},
		}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetHealthScoreFleet | %w", err)
	}

	items := make([]serverhttp.HealthScoreFleetItem, 0, len(res.Items))

	for _, it := range res.Items {
		row := serverhttp.HealthScoreFleetItem{
			ClusterName: it.Target.Cluster,
			Instance:    it.Target.Instance,
			Score:       it.Score,
			Source:      serverhttp.HealthScoreFleetItemSource(it.Source),
			InRecovery:  it.InRecovery,
		}

		if it.MetricsDegraded {
			row.MetricsDegraded = &it.MetricsDegraded
		}

		if it.Err != "" {
			row.Error = &it.Err
		}

		items = append(items, row)
	}

	return serverhttp.GetHealthScoreFleet200JSONResponse{
		Items:              items,
		InstancesTotal:     res.InstancesTotal,
		InstancesScored:    res.InstancesScored,
		Candidates:         res.Candidates,
		Uncomputed:         res.Uncomputed,
		Incomplete:         res.Incomplete,
		MetricsUnavailable: res.MetricsUnavailable,
		ComputedAt:         res.ComputedAt,
		DurationMs:         res.Duration.Milliseconds(),
	}, nil
}
