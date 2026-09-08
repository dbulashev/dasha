package http

import (
	"context"
	"errors"
	"fmt"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/logs"
	"github.com/dbulashev/dasha/internal/pkg/shortcut"
)

// upstreamMessage stands in for the log store's own error text, which names
// internal hosts and indices; the detail is logged instead.
const upstreamMessage = "log source error"

// GetLogs searches cluster logs through the log source bound to the cluster.
func (s *Handlers) GetLogs(
	ctx context.Context,
	req serverhttp.GetLogsRequestObject,
) (serverhttp.GetLogsResponseObject, error) {
	p := req.Params

	q := logs.SearchQuery{
		Cluster:    string(p.ClusterName),
		Stream:     string(p.ServiceType),
		From:       p.From,
		To:         p.To,
		Severities: deref(p.Severity),
		Host:       deref(p.Host),
		Include:    deref(p.Message),
		Exclude:    deref(p.Exclude),
		Database:   deref(p.Database),
		User:       deref(p.User),
		Dedup:      deref(p.Dedup),
		PageSize:   deref(p.PageSize),
		PageToken:  deref(p.PageToken),
	}

	res, err := s.logs.Search(ctx, q)
	if err != nil {
		switch {
		case errors.Is(err, logs.ErrNotFound):
			return serverhttp.GetLogs404Response{}, nil
		case errors.Is(err, logs.ErrInvalid):
			return serverhttp.GetLogs400Response{}, nil
		case errors.Is(err, logs.ErrUnsupported):
			return serverhttp.GetLogs501Response{}, nil
		case errors.Is(err, logs.ErrTimeout):
			return serverhttp.GetLogs504Response{}, nil
		case errors.Is(err, logs.ErrUpstream):
			return serverhttp.GetLogs502JSONResponse{Message: upstreamMessage}, nil
		default:
			// context.Canceled (client disconnect) lands here; the error
			// handler skips logging it.
			return nil, fmt.Errorf("GetLogs | %w", err)
		}
	}

	return mapLogResult(res), nil
}

func mapLogResult(res logs.SearchResult) serverhttp.GetLogs200JSONResponse {
	items := make([]serverhttp.LogEntry, 0, len(res.Items))
	for _, e := range res.Items {
		items = append(items, mapLogEntry(e, res.Dedup))
	}

	out := serverhttp.LogSearchResult{ //nolint:exhaustruct
		Items:   items,
		Dedup:   res.Dedup,
		Partial: res.Partial,
		Scanned: shortcut.Ptr(res.Scanned),
	}

	if res.NextPageToken != "" {
		out.NextPageToken = shortcut.Ptr(res.NextPageToken)
	}

	return serverhttp.GetLogs200JSONResponse(out)
}

func mapLogEntry(e logs.Entry, dedup bool) serverhttp.LogEntry {
	fields := e.Fields

	entry := serverhttp.LogEntry{ //nolint:exhaustruct
		Timestamp: e.Timestamp,
		Severity:  shortcut.Ptr(e.Severity),
		Hostname:  shortcut.Ptr(e.Hostname),
		Text:      shortcut.Ptr(e.Text),
		Database:  shortcut.Ptr(e.Database),
		User:      shortcut.Ptr(e.User),
		Fields:    &fields,
	}

	if dedup {
		entry.Count = shortcut.Ptr(e.Count)
		entry.FirstSeen = shortcut.Ptr(e.FirstSeen)
		entry.LastSeen = shortcut.Ptr(e.LastSeen)
	}

	return entry
}

func deref[T any](p *T) T {
	if p == nil {
		var zero T

		return zero
	}

	return *p
}

// GetLogsCheck probes the log source bound to a cluster so a misconfigured
// field map or an unreachable index is visible before anyone searches.
func (s *Handlers) GetLogsCheck(
	ctx context.Context,
	req serverhttp.GetLogsCheckRequestObject,
) (serverhttp.GetLogsCheckResponseObject, error) {
	res, err := s.logs.Check(ctx, string(req.Params.ClusterName), string(req.Params.ServiceType))
	if err != nil {
		switch {
		case errors.Is(err, logs.ErrNotFound):
			return serverhttp.GetLogsCheck404Response{}, nil
		case errors.Is(err, logs.ErrUnsupported):
			return serverhttp.GetLogsCheck501Response{}, nil
		case errors.Is(err, logs.ErrTimeout):
			return serverhttp.GetLogsCheck504Response{}, nil
		case errors.Is(err, logs.ErrUpstream):
			return serverhttp.GetLogsCheck502JSONResponse{Message: upstreamMessage}, nil
		default:
			return nil, fmt.Errorf("GetLogsCheck | %w", err)
		}
	}

	out := serverhttp.LogSourceCheck{
		Source:     res.Source,
		Stream:     res.Stream,
		Target:     res.Target,
		Documents:  shortcut.Ptr(res.Documents),
		Found:      mapOrNil(res.Found),
		FieldTypes: mapOrNil(res.Types),
		Missing:    nil,
		Sample:     mapOrNil(res.Sample),
	}

	if len(res.Missing) > 0 {
		out.Missing = &res.Missing
	}

	return serverhttp.GetLogsCheck200JSONResponse(out), nil
}

func mapOrNil(m map[string]string) *map[string]string {
	if len(m) == 0 {
		return nil
	}

	return &m
}
