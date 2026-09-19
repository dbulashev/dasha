package http

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/google/uuid"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/logs"
	"github.com/dbulashev/dasha/internal/logs/insights"
	"github.com/dbulashev/dasha/internal/pkg/mapstruct"
	"github.com/dbulashev/dasha/internal/pkg/shortcut"
)

// GetLogsPlansCompare reads the plans of two windows and lists the statements
// whose plans changed for the worse.
func (s *Handlers) GetLogsPlansCompare(
	ctx context.Context,
	req serverhttp.GetLogsPlansCompareRequestObject,
) (serverhttp.GetLogsPlansCompareResponseObject, error) {
	p := req.Params

	q := logs.CompareQuery{ //nolint:exhaustruct
		PlansQuery: logs.PlansQuery{ //nolint:exhaustruct
			Cluster: string(p.ClusterName),
			Stream:  string(p.ServiceType),
			From:    deref(p.From),
			To:      deref(p.To),
			Host:    deref(p.Host),
			Limit:   deref(p.Limit),
		},
		BaseFrom: p.BaselineFrom,
		BaseTo:   p.BaselineTo,
	}

	if p.ScanId != nil {
		q.ScanID = uuid.UUID(*p.ScanId)
	}

	if p.QueryId != nil {
		id, err := strconv.ParseInt(*p.QueryId, 10, 64)
		if err != nil {
			return serverhttp.GetLogsPlansCompare400Response{}, nil
		}

		q.QueryID, q.HasQueryID = id, true
	}

	res, err := s.logs.Compare(ctx, q)
	if err != nil {
		switch {
		case errors.Is(err, logs.ErrNotFound), errors.Is(err, logs.ErrDisabled):
			return serverhttp.GetLogsPlansCompare404Response{}, nil
		case errors.Is(err, logs.ErrInvalid):
			return serverhttp.GetLogsPlansCompare400Response{}, nil
		case errors.Is(err, logs.ErrUnsupported), errors.Is(err, logs.ErrNoStorage):
			return serverhttp.GetLogsPlansCompare501Response{}, nil
		case errors.Is(err, logs.ErrTimeout):
			return serverhttp.GetLogsPlansCompare504Response{}, nil
		case errors.Is(err, logs.ErrUpstream):
			return serverhttp.GetLogsPlansCompare502JSONResponse{Message: upstreamMessage}, nil
		default:
			return nil, fmt.Errorf("GetLogsPlansCompare | %w", err)
		}
	}

	return serverhttp.GetLogsPlansCompare200JSONResponse(mapLogComparison(res)), nil
}

func mapLogComparison(res logs.CompareResult) serverhttp.LogPlanComparison {
	out := serverhttp.LogPlanComparison{ //nolint:exhaustruct
		Partial:     res.Partial,
		Current:     mapComparedWindow(res.Current),
		Regressions: mapstruct.SliceMap(res.Regressions, mapLogRegression),
	}

	if res.Baseline != nil {
		out.Baseline = shortcut.Ptr(mapComparedWindow(*res.Baseline))
	}

	return out
}

func mapComparedWindow(w logs.ComparedWindow) serverhttp.LogComparedWindow {
	return serverhttp.LogComparedWindow{
		From:    w.From,
		To:      w.To,
		Summary: mapLogInsights(w.Result),
	}
}

func mapLogRegression(r insights.Regression) serverhttp.LogPlanRegression {
	out := serverhttp.LogPlanRegression{ //nolint:exhaustruct
		Severity:      serverhttp.LogPlanRegressionSeverity(r.Severity),
		Reasons:       mapLogRegressionReasons(r.Reasons),
		AddedHashes:   emptyIfNil(r.AddedHashes),
		RemovedHashes: emptyIfNil(r.RemovedHashes),
		AddedIndexes:  emptyIfNil(r.AddedIndexes),
		LostIndexes:   emptyIfNil(r.LostIndexes),
		Current:       mapPlanDurations(r.Current),
		Baseline:      mapPlanDurations(r.Baseline),
		CurrentCount:  r.CurrentCount,
		BaselineCount: r.BaselineCount,
		P50Ratio:      r.P50Ratio,
		P95Ratio:      r.P95Ratio,
	}

	out.QueryText, out.QueryTextOmittedBytes = clip(r.QueryText, planQueryTextLimit)

	if r.HasQueryID {
		out.QueryId = shortcut.Ptr(strconv.FormatInt(r.QueryID, 10))
	}

	return out
}

func mapLogRegressionReasons(reasons []string) []serverhttp.LogPlanRegressionReason {
	out := make([]serverhttp.LogPlanRegressionReason, 0, len(reasons))
	for _, r := range reasons {
		out = append(out, serverhttp.LogPlanRegressionReason(r))
	}

	return out
}
