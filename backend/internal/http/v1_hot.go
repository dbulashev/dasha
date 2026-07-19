package http

import (
	"context"
	"fmt"
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
	if s.storage == nil {
		return serverhttp.GetTablesHot501Response{}, nil
	}

	class := hotobjects.ClassReads
	if req.Params.Class != nil {
		class = hotobjects.Class(*req.Params.Class)
	}

	report, err := s.hotReport(ctx, req.Params.ClusterName, req.Params.Database,
		hotobjects.KindTable, class, req.Params.At, req.Params.Limit, req.Params.Offset)
	if err != nil {
		return nil, fmt.Errorf("GetTablesHot | %w", err)
	}

	if report == nil {
		return serverhttp.GetTablesHot404Response{}, nil
	}

	return serverhttp.GetTablesHot200JSONResponse(*report), nil
}

// GetIndexesHot serves the stored hot-indexes top for one class.
func (s *Handlers) GetIndexesHot(
	ctx context.Context,
	req serverhttp.GetIndexesHotRequestObject,
) (serverhttp.GetIndexesHotResponseObject, error) {
	if s.storage == nil {
		return serverhttp.GetIndexesHot501Response{}, nil
	}

	class := hotobjects.ClassReads
	if req.Params.Class != nil {
		class = hotobjects.Class(*req.Params.Class)
	}

	report, err := s.hotReport(ctx, req.Params.ClusterName, req.Params.Database,
		hotobjects.KindIndex, class, req.Params.At, req.Params.Limit, req.Params.Offset)
	if err != nil {
		return nil, fmt.Errorf("GetIndexesHot | %w", err)
	}

	if report == nil {
		return serverhttp.GetIndexesHot404Response{}, nil
	}

	return serverhttp.GetIndexesHot200JSONResponse(*report), nil
}

// hotDatesLimit bounds the day-selector list.
const hotDatesLimit = 60

// hotReport assembles a HotReport from storage; nil when no snapshot exists.
func (s *Handlers) hotReport(
	ctx context.Context,
	clusterName, database string,
	kind hotobjects.Kind,
	class hotobjects.Class,
	at *time.Time,
	limitPtr, offsetPtr *int,
) (*serverhttp.HotReport, error) {
	limit, offset := paginationDefaults(limitPtr, offsetPtr, defaultHotLimit)

	snap, err := s.storage.GetHotSnapshot(ctx, clusterName, database, at)
	if err != nil {
		return nil, err
	}

	if snap == nil {
		return nil, nil //nolint:nilnil
	}

	entries, err := s.storage.GetHotTop(ctx, snap.ID, snap.CapturedAt, kind, class, limit, offset)
	if err != nil {
		return nil, err
	}

	// PrevRank from the snapshot immediately preceding this one — a single
	// indexed lookup; trend arrows in the UI come from this.
	prevRanks := map[string]int{}

	if prev, err := s.storage.GetHotSnapshotBefore(ctx, clusterName, database, snap.CapturedAt); err == nil && prev != nil {
		prevRanks, _ = s.storage.GetHotRanks(ctx, prev.ID, prev.CapturedAt, kind, class)
	}

	dates, err := s.storage.ListHotSnapshotDates(ctx, clusterName, database, hotDatesLimit)
	if err != nil {
		return nil, err
	}

	windowDays := hotWindowDays(snap)

	out := serverhttp.HotReport{
		Snapshot: hotSnapshotMeta(snap, kind, class, dates),
		Entries:  make([]serverhttp.HotEntry, 0, len(entries)),
	}

	for _, e := range entries {
		out.Entries = append(out.Entries, hotEntry(e, kind, class, windowDays, prevRanks))
	}

	return &out, nil
}

// hotWindowDays returns the longest complete host window in days — the
// normalization base for per-day rates.
func hotWindowDays(snap *hotobjects.Snapshot) float64 {
	var maxDays float64

	for _, w := range snap.Windows {
		if !w.Complete {
			continue
		}

		if d := w.To.Sub(w.From).Hours() / 24; d > maxDays {
			maxDays = d
		}
	}

	return maxDays
}

func hotSnapshotMeta(
	snap *hotobjects.Snapshot,
	kind hotobjects.Kind,
	class hotobjects.Class,
	dates []time.Time,
) serverhttp.HotSnapshotMeta {
	windows := make(map[string]serverhttp.HotHostWindow, len(snap.Windows))

	for host, w := range snap.Windows {
		windows[host] = serverhttp.HotHostWindow{
			From:       w.From,
			To:         w.To,
			Complete:   w.Complete,
			StatsReset: w.StatsReset,
		}
	}

	coverage := snap.Coverage[class].Tables
	hist := snap.Histograms[class].Tables

	if kind == hotobjects.KindIndex {
		coverage = snap.Coverage[class].Indexes
		hist = snap.Histograms[class].Indexes
	}

	var outHist *serverhttp.HotHistogram
	if hist != nil {
		outHist = &serverhttp.HotHistogram{
			Deciles: hist.Deciles,
			Count:   hist.Count,
			Sum:     hist.Sum,
			Max:     hist.Max,
		}
	}

	return serverhttp.HotSnapshotMeta{
		CapturedAt:   snap.CapturedAt,
		Windows:      windows,
		HostsMissing: snap.HostsMissing,
		Coverage:     coverage,
		Histogram:    outHist,
		Dates:        &dates,
	}
}

func hotEntry(
	e hotobjects.TopEntry,
	kind hotobjects.Kind,
	class hotobjects.Class,
	windowDays float64,
	prevRanks map[string]int,
) serverhttp.HotEntry {
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

	rate := 0.0
	if windowDays > 0 {
		rate = float64(hotobjects.RankKey(kind, class, e.Delta)) / windowDays
	}

	var prevRank *int
	if r, ok := prevRanks[e.Schema+"."+e.Object]; ok {
		prevRank = &r
	}

	var tableName *string
	if e.TableName != "" {
		tableName = &e.TableName
	}

	return serverhttp.HotEntry{
		Rank:       e.Rank,
		PrevRank:   prevRank,
		Schema:     e.Schema,
		Object:     e.Object,
		TableName:  tableName,
		SizeBytes:  e.SizeBytes,
		Delta:      countersMap(e.Delta),
		RatePerDay: rate,
		PerHost:    perHost,
	}
}

func countersMap(c hotobjects.Counters) map[string]int64 {
	out := make(map[string]int64, len(c))

	for k, v := range c {
		out[k] = v
	}

	return out
}

// GetHotObjectHistory lists the days an object appeared in a stored top.
func (s *Handlers) GetHotObjectHistory(
	ctx context.Context,
	req serverhttp.GetHotObjectHistoryRequestObject,
) (serverhttp.GetHotObjectHistoryResponseObject, error) {
	if s.storage == nil {
		return serverhttp.GetHotObjectHistory501Response{}, nil
	}

	days := 30
	if req.Params.Days != nil && *req.Params.Days > 0 {
		days = *req.Params.Days
	}

	to := time.Now().UTC()
	from := to.AddDate(0, 0, -days)

	items, err := s.storage.GetHotObjectHistory(ctx,
		req.Params.ClusterName, req.Params.Database,
		hotKind(string(req.Params.Kind)), req.Params.Schema, req.Params.Object, from, to)
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

// GetHotPercentile computes an object's live delta against its anchors and
// projects the per-day rate onto the latest snapshot's tail histogram.
func (s *Handlers) GetHotPercentile(
	ctx context.Context,
	req serverhttp.GetHotPercentileRequestObject,
) (serverhttp.GetHotPercentileResponseObject, error) {
	if s.storage == nil {
		return serverhttp.GetHotPercentile501Response{}, nil
	}

	kind := hotKind(string(req.Params.Kind))

	class := hotobjects.ClassReads
	if req.Params.Class != nil {
		class = hotobjects.Class(*req.Params.Class)
	}

	snap, err := s.storage.GetHotSnapshot(ctx, req.Params.ClusterName, req.Params.Database, nil)
	if err != nil {
		return nil, fmt.Errorf("GetHotPercentile | %w", err)
	}

	if snap == nil {
		return serverhttp.GetHotPercentile404Response{}, nil
	}

	anchors, err := s.storage.GetHotAnchorsForObject(ctx,
		req.Params.ClusterName, req.Params.Database, kind, req.Params.Schema, req.Params.Object)
	if err != nil {
		return nil, fmt.Errorf("GetHotPercentile | anchors: %w", err)
	}

	if len(anchors) == 0 {
		return serverhttp.GetHotPercentile404Response{}, nil
	}

	rate := s.hotLiveRate(ctx, req, kind, class, anchors)

	hist := snap.Histograms[class].Tables
	if kind == hotobjects.KindIndex {
		hist = snap.Histograms[class].Indexes
	}

	// The histogram holds raw deltas accumulated over the snapshot window,
	// which the schedule makes arbitrary (5 minutes on the demo lab, an hour
	// or a day elsewhere). Scale the live per-day rate back into window units
	// before projecting, or the comparison is off by the window/day ratio.
	value := rate
	if days := hotWindowDays(snap); days > 0 {
		value = rate * days
	}

	percentile := hist.Percentile(value)

	ranks, err := s.storage.GetHotRanks(ctx, snap.ID, snap.CapturedAt, kind, class)
	if err != nil {
		return nil, fmt.Errorf("GetHotPercentile | ranks: %w", err)
	}

	_, inTop := ranks[req.Params.Schema+"."+req.Params.Object]

	return serverhttp.GetHotPercentile200JSONResponse{
		Percentile: percentile,
		RatePerDay: rate,
		InTop:      inTop,
		Class:      string(class),
	}, nil
}

// hotLiveRate sums per-host live deltas (skipping hosts with a broken epoch or
// unreachable ones) normalized by each host's own anchor age.
func (s *Handlers) hotLiveRate(
	ctx context.Context,
	req serverhttp.GetHotPercentileRequestObject,
	kind hotobjects.Kind,
	class hotobjects.Class,
	anchors []hotobjects.AnchorRow,
) float64 {
	now := time.Now().UTC()
	rate := 0.0

	for _, a := range anchors {
		var (
			rows  []hotobjects.AnchorRow
			reset *time.Time
			err   error
		)

		if kind == hotobjects.KindIndex {
			rows, reset, _, err = s.repo.GetHotSampleIndexes(ctx,
				req.Params.ClusterName, a.Instance, req.Params.Database, &req.Params.Schema, &req.Params.Object)
		} else {
			rows, reset, _, err = s.repo.GetHotSampleTables(ctx,
				req.Params.ClusterName, a.Instance, req.Params.Database, &req.Params.Schema, &req.Params.Object)
		}

		if err != nil || len(rows) == 0 {
			continue
		}

		if !sameReset(a.StatsReset, reset) {
			continue // epoch broke since the anchor; delta not measurable
		}

		d, ok := hotobjects.Delta(a.Counters, rows[0].Counters)
		if !ok {
			continue
		}

		days := now.Sub(a.CapturedAt).Hours() / 24
		if days <= 0 {
			continue
		}

		rate += float64(hotobjects.RankKey(kind, class, d)) / days
	}

	return rate
}

func sameReset(a, b *time.Time) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return a.Equal(*b)
	}
}

func hotKind(s string) hotobjects.Kind {
	if s == "index" {
		return hotobjects.KindIndex
	}

	return hotobjects.KindTable
}
