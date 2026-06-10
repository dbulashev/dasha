package http

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/metrics"
)

func (s *Handlers) GetHealthScoreDatasourceStatus(
	ctx context.Context,
	req serverhttp.GetHealthScoreDatasourceStatusRequestObject,
) (serverhttp.GetHealthScoreDatasourceStatusResponseObject, error) {
	if !s.metrics.Enabled() {
		return serverhttp.GetHealthScoreDatasourceStatus200JSONResponse{
			Enabled: false,
			Roles:   []serverhttp.HealthScoreDatasourceRole{},
		}, nil
	}

	diag, err := s.metrics.ValidateTarget(ctx, req.Params.ClusterName, req.Params.Instance)
	if errors.Is(err, metrics.ErrTargetNotMapped) {
		return serverhttp.GetHealthScoreDatasourceStatus404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetHealthScoreDatasourceStatus | %w", err)
	}

	roles := make([]serverhttp.HealthScoreDatasourceRole, 0, len(diag.Roles))
	for _, rd := range diag.Roles {
		role := serverhttp.HealthScoreDatasourceRole{
			Role:     string(rd.Role),
			Provider: string(rd.Provider),
			Metric:   rd.Metric,
			Selector: rd.Selector,
			Matched:  rd.Matched,
		}

		if rd.Err != "" {
			e := rd.Err
			role.Error = &e
		}

		if len(rd.Sample) > 0 {
			labels := rd.Sample
			role.SampleLabels = &labels
		}

		roles = append(roles, role)
	}

	return serverhttp.GetHealthScoreDatasourceStatus200JSONResponse{
		Enabled: true,
		Target:  diag.Target,
		Roles:   roles,
	}, nil
}

func (s *Handlers) GetHealthScoreHistory(
	ctx context.Context,
	req serverhttp.GetHealthScoreHistoryRequestObject,
) (serverhttp.GetHealthScoreHistoryResponseObject, error) {
	if !s.metrics.Enabled() {
		return serverhttp.GetHealthScoreHistory404Response{}, nil
	}

	weights, err := s.loadHealthWeights(ctx, req.Params.ClusterName)
	if err != nil {
		return nil, fmt.Errorf("GetHealthScoreHistory | loadHealthWeights | %w", err)
	}

	step := 5 * time.Minute
	if req.Params.StepSeconds != nil && *req.Params.StepSeconds > 0 {
		step = time.Duration(*req.Params.StepSeconds) * time.Second
	}

	h, err := s.metrics.History(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.From, req.Params.To, step, weights)
	if errors.Is(err, metrics.ErrTargetNotMapped) {
		return serverhttp.GetHealthScoreHistory404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetHealthScoreHistory | %w", err)
	}

	points := make([]serverhttp.HealthScoreHistoryPoint, 0, len(h.Points))
	for _, p := range h.Points {
		points = append(points, serverhttp.HealthScoreHistoryPoint{
			Time:       p.Time,
			Score:      p.Score,
			LatencyMs:  p.LatencyMs,
			Categories: p.Categories,
		})
	}

	baseline := make([]serverhttp.HealthScoreHistoryBaseline, 0, len(h.Baseline))
	for _, b := range h.Baseline {
		baseline = append(baseline, serverhttp.HealthScoreHistoryBaseline{
			Time:  b.Time,
			Value: b.Value,
		})
	}

	dips := make([]serverhttp.HealthScoreHistoryDip, 0, len(h.Dips))
	for _, d := range h.Dips {
		dips = append(dips, serverhttp.HealthScoreHistoryDip{
			Time:     d.Time,
			Value:    d.Value,
			Baseline: d.Baseline,
			Drop:     d.Drop,
		})
	}

	return serverhttp.GetHealthScoreHistory200JSONResponse{
		Points:            points,
		Baseline:          baseline,
		Dips:              dips,
		BaselineAvailable: h.BaselineEnough,
	}, nil
}
