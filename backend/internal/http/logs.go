package http

import (
	"context"
	"errors"
	"fmt"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/logs"
	"github.com/dbulashev/dasha/internal/pkg/shortcut"
)

// GetLogs searches Yandex Cloud cluster logs (PostgreSQL/pooler) via the MDB API.
func (s *Handlers) GetLogs(
	ctx context.Context,
	req serverhttp.GetLogsRequestObject,
) (serverhttp.GetLogsResponseObject, error) {
	p := req.Params

	q := logs.SearchQuery{
		Cluster:     string(p.ClusterName),
		ServiceType: logs.ParseServiceType(string(p.ServiceType)),
		From:        p.From,
		To:          p.To,
		Severities:  derefSlice(p.Severity),
		Host:        derefStr(p.Host),
		Message:     derefStr(p.Message),
		Database:    derefStr(p.Database),
		User:        derefStr(p.User),
		Dedup:       derefBool(p.Dedup),
		PageSize:    derefInt(p.PageSize),
		PageToken:   derefStr(p.PageToken),
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
			return serverhttp.GetLogs502Response{}, nil
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

func derefStr(p *string) string {
	if p == nil {
		return ""
	}

	return *p
}

func derefBool(p *bool) bool {
	if p == nil {
		return false
	}

	return *p
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}

	return *p
}

func derefSlice(p *[]string) []string {
	if p == nil {
		return nil
	}

	return *p
}
