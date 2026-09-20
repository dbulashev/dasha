package http

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/logs"
	"github.com/dbulashev/dasha/internal/logs/insights"
	"github.com/dbulashev/dasha/internal/pkg/mapstruct"
	"github.com/dbulashev/dasha/internal/pkg/shortcut"
)

// GetLogsScan returns a stored scan: the same summary its own request answered
// with, read from the snapshot instead of the log source.
func (s *Handlers) GetLogsScan(
	ctx context.Context,
	req serverhttp.GetLogsScanRequestObject,
) (serverhttp.GetLogsScanResponseObject, error) {
	scan, err := s.logs.Snapshot(ctx, req.ScanId)
	if err != nil {
		switch {
		case errors.Is(err, logs.ErrNotFound), errors.Is(err, logs.ErrDisabled):
			return serverhttp.GetLogsScan404Response{}, nil
		case errors.Is(err, logs.ErrNoStorage):
			return serverhttp.GetLogsScan501Response{}, nil
		default:
			return nil, fmt.Errorf("GetLogsScan | %w", err)
		}
	}

	out := mapLogInsights(scan.Result)
	out.Scan = &serverhttp.LogScanInfo{ //nolint:exhaustruct
		Kind:        serverhttp.LogScanInfoKind(scan.Kind),
		ClusterName: scan.Cluster,
		ServiceType: serverhttp.LogScanInfoServiceType(scan.Stream),
		Host:        optString(scan.Host),
		From:        scan.From,
		To:          scan.To,
		CreatedAt:   scan.CreatedAt,
	}

	return serverhttp.GetLogsScan200JSONResponse(out), nil
}

// GetLogsScanGroups pages the plan groups of a stored scan without their trees.
func (s *Handlers) GetLogsScanGroups(
	ctx context.Context,
	req serverhttp.GetLogsScanGroupsRequestObject,
) (serverhttp.GetLogsScanGroupsResponseObject, error) {
	p := req.Params

	q := logs.GroupsQuery{ //nolint:exhaustruct
		ScanID:       req.ScanId,
		WithFindings: deref(p.WithFindings),
		Order:        string(deref(p.Order)),
		Limit:        deref(p.Limit),
		Offset:       deref(p.Offset),
	}

	if p.QueryId != nil {
		id, err := strconv.ParseInt(*p.QueryId, 10, 64)
		if err != nil {
			return serverhttp.GetLogsScanGroups400Response{}, nil
		}

		q.QueryID = &id
	}

	page, err := s.logs.SnapshotGroups(ctx, q)
	if err != nil {
		switch {
		case errors.Is(err, logs.ErrNotFound), errors.Is(err, logs.ErrDisabled):
			return serverhttp.GetLogsScanGroups404Response{}, nil
		case errors.Is(err, logs.ErrInvalid):
			return serverhttp.GetLogsScanGroups400Response{}, nil
		case errors.Is(err, logs.ErrNoStorage):
			return serverhttp.GetLogsScanGroups501Response{}, nil
		default:
			return nil, fmt.Errorf("GetLogsScanGroups | %w", err)
		}
	}

	return serverhttp.GetLogsScanGroups200JSONResponse{
		Total: page.Total,
		Items: mapstruct.SliceMap(page.Rows, mapLogPlanGroupRow),
	}, nil
}

// GetLogsScanGroup returns one plan group of a stored scan with its tree.
func (s *Handlers) GetLogsScanGroup(
	ctx context.Context,
	req serverhttp.GetLogsScanGroupRequestObject,
) (serverhttp.GetLogsScanGroupResponseObject, error) {
	group, err := s.logs.SnapshotGroup(ctx, req.ScanId, req.Ord)
	if err != nil {
		switch {
		case errors.Is(err, logs.ErrNotFound), errors.Is(err, logs.ErrDisabled):
			return serverhttp.GetLogsScanGroup404Response{}, nil
		case errors.Is(err, logs.ErrInvalid):
			return serverhttp.GetLogsScanGroup400Response{}, nil
		case errors.Is(err, logs.ErrNoStorage):
			return serverhttp.GetLogsScanGroup501Response{}, nil
		default:
			return nil, fmt.Errorf("GetLogsScanGroup | %w", err)
		}
	}

	return serverhttp.GetLogsScanGroup200JSONResponse(mapLogPlanGroup(group)), nil
}

func mapLogPlanGroupRow(r insights.GroupRow) serverhttp.LogPlanGroupRow {
	out := serverhttp.LogPlanGroupRow{ //nolint:exhaustruct
		Ord:       r.Ord,
		Hash:      r.Hash,
		Count:     r.Count,
		Durations: mapPlanDurations(r.Durations),
		FirstSeen: r.First,
		LastSeen:  r.Last,
		Findings:  mapstruct.SliceMap(r.Findings, mapPlanFinding),
	}

	out.QueryText, out.QueryTextOmittedBytes = clip(r.QueryText, planQueryTextLimit)

	if r.HasQueryID {
		out.QueryId = shortcut.Ptr(strconv.FormatInt(r.QueryID, 10))
	}

	return out
}
