package http

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/logs"
)

// GetLogsPlans reads a window of cluster logs for auto_explain records alone,
// optionally for one statement.
func (s *Handlers) GetLogsPlans(
	ctx context.Context,
	req serverhttp.GetLogsPlansRequestObject,
) (serverhttp.GetLogsPlansResponseObject, error) {
	p := req.Params

	q := logs.PlansQuery{ //nolint:exhaustruct
		Cluster: string(p.ClusterName),
		Stream:  string(p.ServiceType),
		From:    p.From,
		To:      p.To,
		Host:    deref(p.Host),
		Limit:   deref(p.Limit),
	}

	if p.QueryId != nil {
		id, err := strconv.ParseInt(*p.QueryId, 10, 64)
		if err != nil {
			return serverhttp.GetLogsPlans400Response{}, nil
		}

		q.QueryID, q.HasQueryID = id, true
	}

	res, err := s.logs.Plans(ctx, q)
	if err != nil {
		switch {
		case errors.Is(err, logs.ErrNotFound), errors.Is(err, logs.ErrDisabled):
			return serverhttp.GetLogsPlans404Response{}, nil
		case errors.Is(err, logs.ErrInvalid):
			return serverhttp.GetLogsPlans400Response{}, nil
		case errors.Is(err, logs.ErrUnsupported):
			return serverhttp.GetLogsPlans501Response{}, nil
		case errors.Is(err, logs.ErrTimeout):
			return serverhttp.GetLogsPlans504Response{}, nil
		case errors.Is(err, logs.ErrUpstream):
			return serverhttp.GetLogsPlans502JSONResponse{Message: upstreamMessage}, nil
		default:
			return nil, fmt.Errorf("GetLogsPlans | %w", err)
		}
	}

	return serverhttp.GetLogsPlans200JSONResponse(mapLogInsights(res)), nil
}
