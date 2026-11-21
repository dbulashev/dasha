package http

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sort"
	"time"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/hotobjects"
)

const defaultHotLimit = 30

// GetTablesHot serves the stored hot-tables top for one class.
func (s *Handlers) GetTablesHot(
	ctx context.Context,
	req serverhttp.GetTablesHotRequestObject,
) (serverhttp.GetTablesHotResponseObject, error) {
	if s.hot == nil {
		return serverhttp.GetTablesHot501Response{}, nil
	}

	report, err := s.hotReport(ctx, req.Params.ClusterName, req.Params.Database,
		hotobjects.KindTable, hotClass(req.Params.Class), req.Params.At, req.Params.Limit, req.Params.Offset)
	if errors.Is(err, hotobjects.ErrNoSnapshot) {
		return serverhttp.GetTablesHot404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetTablesHot | %w", err)
	}

	return serverhttp.GetTablesHot200JSONResponse(report), nil
}

// GetIndexesHot serves the stored hot-indexes top for one class.
func (s *Handlers) GetIndexesHot(
	ctx context.Context,
	req serverhttp.GetIndexesHotRequestObject,
) (serverhttp.GetIndexesHotResponseObject, error) {
	if s.hot == nil {
		return serverhttp.GetIndexesHot501Response{}, nil
	}

	report, err := s.hotReport(ctx, req.Params.ClusterName, req.Params.Database,
		hotobjects.KindIndex, hotClass(req.Params.Class), req.Params.At, req.Params.Limit, req.Params.Offset)
	if errors.Is(err, hotobjects.ErrNoSnapshot) {
		return serverhttp.GetIndexesHot404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetIndexesHot | %w", err)
	}

	return serverhttp.GetIndexesHot200JSONResponse(report), nil
}

func (s *Handlers) hotReport(
	ctx context.Context,
	clusterName, database string,
	kind hotobjects.Kind,
	class hotobjects.Class,
	at *time.Time,
	limitPtr, offsetPtr *int,
) (serverhttp.HotReport, error) {
	limit, offset := paginationDefaults(limitPtr, offsetPtr, defaultHotLimit)

	report, err := s.hot.Report(ctx, hotobjects.ReportQuery{
		Cluster:  clusterName,
		Database: database,
		Kind:     kind,
		Class:    class,
		At:       at,
		Limit:    limit,
		Offset:   offset,
	})
	if err != nil {
		return serverhttp.HotReport{}, err //nolint:exhaustruct
	}

	out := serverhttp.HotReport{
		Snapshot: hotSnapshotMeta(report),
		Entries:  make([]serverhttp.HotEntry, 0, len(report.Entries)),
	}

	for _, e := range report.Entries {
		out.Entries = append(out.Entries, hotEntry(e))
	}

	return out, nil
}

func hotSnapshotMeta(r hotobjects.Report) serverhttp.HotSnapshotMeta {
	snap := r.Snapshot
	windows := make(map[string]serverhttp.HotHostWindow, len(snap.Windows))

	for host, w := range snap.Windows {
		windows[host] = serverhttp.HotHostWindow{
			From:       w.From,
			To:         w.To,
			Complete:   w.Complete,
			StatsReset: w.StatsReset,
		}
	}

	var outHist *serverhttp.HotHistogram
	if h := r.Histogram; h != nil {
		outHist = &serverhttp.HotHistogram{
			Deciles: h.Deciles,
			Count:   h.Count,
			Sum:     h.Sum,
			Max:     h.Max,
		}
	}

	return serverhttp.HotSnapshotMeta{
		CapturedAt:   snap.CapturedAt,
		Windows:      windows,
		HostsMissing: snap.HostsMissing,
		Coverage:     r.Coverage,
		Histogram:    outHist,
		Dates:        emptyIfNil(r.Dates),
	}
}

func hotEntry(e hotobjects.ReportEntry) serverhttp.HotEntry {
	perHost := make([]serverhttp.HotHostDelta, 0, len(e.PerHost))

	for host, hd := range e.PerHost {
		perHost = append(perHost, serverhttp.HotHostDelta{
			Instance:   host,
			Complete:   hd.Complete,
			InRecovery: hd.InRecovery,
			Delta:      countersMap(hd.Delta),
		})
	}

	// Map iteration order is random; the UI counts on a stable host order.
	sort.Slice(perHost, func(i, j int) bool { return perHost[i].Instance < perHost[j].Instance })

	var tableName *string
	if e.TableName != "" {
		tableName = &e.TableName
	}

	return serverhttp.HotEntry{
		Rank:       e.Rank,
		PrevRank:   e.PrevRank,
		Schema:     e.Schema,
		Object:     e.Object,
		TableName:  tableName,
		SizeBytes:  e.SizeBytes,
		Delta:      countersMap(e.Delta),
		RatePerDay: e.RatePerDay,
		PerHost:    perHost,
	}
}

func countersMap(c hotobjects.Counters) map[string]int64 {
	out := make(map[string]int64, len(c))

	maps.Copy(out, c)

	return out
}

// GetHotObjectHistory lists the days an object appeared in a stored top.
func (s *Handlers) GetHotObjectHistory(
	ctx context.Context,
	req serverhttp.GetHotObjectHistoryRequestObject,
) (serverhttp.GetHotObjectHistoryResponseObject, error) {
	if s.hot == nil {
		return serverhttp.GetHotObjectHistory501Response{}, nil
	}

	days := 0
	if req.Params.Days != nil {
		days = *req.Params.Days
	}

	items, err := s.hot.History(ctx, req.Params.ClusterName, req.Params.Database,
		hotKind(string(req.Params.Kind)),
		hotobjects.ObjectRef{Schema: req.Params.Schema, Object: req.Params.Object}, days)
	if err != nil {
		return nil, fmt.Errorf("GetHotObjectHistory | %w", err)
	}

	ret := serverhttp.HotObjectHistory{Items: make([]serverhttp.HotObjectHistoryItem, 0, len(items))}

	for _, it := range items {
		ret.Items = append(ret.Items, serverhttp.HotObjectHistoryItem{
			CapturedAt: it.CapturedAt,
			Class:      string(it.Class),
			Rank:       it.Rank,
			SizeBytes:  it.SizeBytes,
			Delta:      countersMap(it.Delta),
		})
	}

	return serverhttp.GetHotObjectHistory200JSONResponse(ret), nil
}

// GetHotPercentile projects an object's live per-day rate onto the latest
// snapshot's tail histogram.
func (s *Handlers) GetHotPercentile(
	ctx context.Context,
	req serverhttp.GetHotPercentileRequestObject,
) (serverhttp.GetHotPercentileResponseObject, error) {
	if s.hot == nil {
		return serverhttp.GetHotPercentile501Response{}, nil
	}

	class := hotClass(req.Params.Class)

	p, err := s.hot.Percentile(ctx, req.Params.ClusterName, req.Params.Database,
		hotKind(string(req.Params.Kind)),
		hotobjects.ObjectRef{Schema: req.Params.Schema, Object: req.Params.Object}, class)

	switch {
	case errors.Is(err, hotobjects.ErrNoSnapshot), errors.Is(err, hotobjects.ErrNoAnchors):
		return serverhttp.GetHotPercentile404Response{}, nil
	case err != nil:
		return nil, fmt.Errorf("GetHotPercentile | %w", err)
	}

	return serverhttp.GetHotPercentile200JSONResponse{
		Percentile: p.Percentile,
		RatePerDay: p.RatePerDay,
		InTop:      p.InTop,
		Class:      string(class),
	}, nil
}

func hotClass[T ~string](s *T) hotobjects.Class {
	if s == nil {
		return hotobjects.ClassReads
	}

	return hotobjects.Class(*s)
}

func hotKind(s string) hotobjects.Kind {
	if s == "index" {
		return hotobjects.KindIndex
	}

	return hotobjects.KindTable
}
