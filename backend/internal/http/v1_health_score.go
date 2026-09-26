package http

import (
	"context"
	"fmt"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/healthscore"
	"github.com/dbulashev/dasha/internal/metrics"
)

func (s *Handlers) GetHealthScore(
	ctx context.Context,
	req serverhttp.GetHealthScoreRequestObject,
) (serverhttp.GetHealthScoreResponseObject, error) {
	score, err := s.scorer.Score(ctx, metrics.TargetRef{Cluster: req.Params.ClusterName, Instance: req.Params.Instance}, nil)
	if healthscore.IsNotFound(err) {
		return serverhttp.GetHealthScore404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetHealthScore | %w", err)
	}

	result := score.Result

	categories := make([]serverhttp.HealthScoreCategory, 0, len(result.Categories))
	for _, c := range result.Categories {
		categories = append(categories, serverhttp.HealthScoreCategory{
			Name:    string(c.Name),
			Score:   c.Score,
			Weight:  c.Weight,
			Penalty: c.Penalty,
			Details: c.Details,
		})
	}

	src := string(score.Source)
	metricsDegraded := score.MetricsDegraded

	return serverhttp.GetHealthScore200JSONResponse{
		Score:           result.Score,
		Categories:      categories,
		HasReplication:  result.HasReplication,
		InRecovery:      result.InRecovery,
		Source:          &src,
		MetricsDegraded: &metricsDegraded,
	}, nil
}

func (s *Handlers) GetHealthScoreRecommendations(
	ctx context.Context,
	req serverhttp.GetHealthScoreRecommendationsRequestObject,
) (serverhttp.GetHealthScoreRecommendationsResponseObject, error) {
	database := ""
	if req.Params.Database != nil {
		database = *req.Params.Database
	}

	recs, err := s.scorer.Recommendations(ctx, metrics.TargetRef{Cluster: req.Params.ClusterName, Instance: req.Params.Instance}, database)
	if healthscore.IsNotFound(err) {
		return serverhttp.GetHealthScoreRecommendations404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetHealthScoreRecommendations | %w", err)
	}

	out := make([]serverhttp.HealthScoreRecommendation, 0, len(recs))
	for _, r := range recs {
		var ctxPtr *map[string]any
		if len(r.Context) > 0 {
			c := r.Context
			ctxPtr = &c
		}

		var routePtr *string
		if r.RelatedRoute != "" {
			route := r.RelatedRoute
			routePtr = &route
		}

		var dbPtr *string
		if r.Database != "" {
			db := r.Database
			dbPtr = &db
		}

		out = append(out, serverhttp.HealthScoreRecommendation{
			RuleId:       r.RuleID,
			Category:     string(r.Category),
			Severity:     serverhttp.HealthScoreRecommendationSeverity(r.Severity),
			MetricValue:  r.MetricValue,
			Database:     dbPtr,
			Context:      ctxPtr,
			RelatedRoute: routePtr,
		})
	}

	return serverhttp.GetHealthScoreRecommendations200JSONResponse{
		Recommendations: out,
	}, nil
}
