package mcpserver

import (
	"cmp"
	"context"
	"errors"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/dbulashev/dasha/gen/apiclient"
)

const (
	ioSummaryDefaultSince = time.Hour
	ioTrendDefaultSince   = 24 * time.Hour
	ioSummaryDefaultTop   = 20
	ioTrendDefaultPoints  = 24
	ioMaxTop              = 200
	ioMaxPoints           = 200

	// The history endpoint silently trims a longer window.
	ioMaxWindow = 31 * 24 * time.Hour
)

var ioRankMetrics = []string{"reads", "writes", "extends", "fsyncs"}

const ioRankedBy = "reads+writes+extends+fsyncs"

var ioTrendMetrics = []string{"reads", "read_bytes", "writes", "write_bytes", "extends"}

var ioTrendTimeMetrics = []string{"read_time", "write_time"}

// backend_type is absent on purpose: its set grows with every release.
var (
	ioContexts = []string{"normal", "vacuum", "bulkread", "bulkwrite", "init"}
	ioObjects  = []string{"relation", "temp relation", "wal"}
)

type ioRange struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

type ioSummaryWindow struct {
	From            time.Time `json:"from"`
	To              time.Time `json:"to"`
	DurationSeconds float64   `json:"duration_seconds"`
	Complete        bool      `json:"complete"`
}

type ioSummaryRow struct {
	BackendType  string           `json:"backend_type,omitempty"`
	Object       string           `json:"object,omitempty"`
	Context      string           `json:"context,omitempty"`
	IOOps        int64            `json:"io_ops"`
	SharePct     float64          `json:"share_pct"`
	OpsPerSecond float64          `json:"ops_per_second,omitempty"`
	AvgReadMs    *float64         `json:"avg_read_ms,omitempty"`
	AvgWriteMs   *float64         `json:"avg_write_ms,omitempty"`
	Values       map[string]int64 `json:"values"`
}

type ioSummaryResult struct {
	Requested    ioRange                 `json:"requested"`
	WindowCapped bool                    `json:"window_capped,omitempty"`
	Window       *ioSummaryWindow        `json:"window,omitempty"`
	Meta         apiclient.IOHistoryMeta `json:"meta"`
	GroupBy      string                  `json:"group_by"`
	RankedBy     string                  `json:"ranked_by"`
	RowsTotal    int                     `json:"rows_total"`
	RowsReturned int                     `json:"rows_returned"`
	Rows         []ioSummaryRow          `json:"rows"`
	Totals       map[string]int64        `json:"totals,omitempty"`
	EmptyReason  string                  `json:"empty_reason,omitempty"`
	EmptyDetail  string                  `json:"empty_detail,omitempty"`
}

// On an incomplete bucket the counters are real but cover only coverage_pct of
// its span.
type ioTrendPoint struct {
	At              time.Time        `json:"at"`
	DurationSeconds float64          `json:"duration_seconds"`
	Complete        bool             `json:"complete"`
	CoveragePct     float64          `json:"coverage_pct,omitempty"`
	Values          map[string]int64 `json:"values,omitempty"`
}

type ioTrendSeries struct {
	Key    apiclient.IOSeriesKey `json:"key"`
	Points []ioTrendPoint        `json:"points"`
}

type ioTrendResult struct {
	Requested        ioRange                 `json:"requested"`
	WindowCapped     bool                    `json:"window_capped,omitempty"`
	Window           *ioRange                `json:"window,omitempty"`
	Meta             apiclient.IOHistoryMeta `json:"meta"`
	GroupBy          string                  `json:"group_by"`
	Points           int                     `json:"points"`
	Metrics          []string                `json:"metrics"`
	IncompletePoints int                     `json:"incomplete_points"`
	Series           []ioTrendSeries         `json:"series"`
	EmptyReason      string                  `json:"empty_reason,omitempty"`
	EmptyDetail      string                  `json:"empty_detail,omitempty"`
}

type ioRequest struct {
	Params   *apiclient.GetIOHistoryParams
	Top      int
	Capped   bool
	Filtered bool
}

func ioWindow(since, from, to string, def time.Duration) (time.Time, time.Time, bool, string) {
	start, end, msg := resolveWindow(since, from, to, def)
	if msg != "" {
		return start, end, false, msg
	}

	if end.Sub(start) > ioMaxWindow {
		return end.Add(-ioMaxWindow), end, true, ""
	}

	return start, end, false, ""
}

func ioFilterMsg(ioContext, object string) string {
	if ioContext != "" && !slices.Contains(ioContexts, ioContext) {
		return "context must be one of: " + strings.Join(ioContexts, ", ")
	}

	if object != "" && !slices.Contains(ioObjects, object) {
		return "object must be one of: " + strings.Join(ioObjects, ", ")
	}

	return ""
}

// points=1 asks for one bucket covering the whole window.
func ioSummaryParams(a ioSummaryArgs) (ioRequest, string) {
	from, to, capped, msg := ioWindow(a.Since, a.From, a.To, ioSummaryDefaultSince)
	if msg != "" {
		return ioRequest{}, msg //nolint:exhaustruct
	}

	if m := ioFilterMsg(a.Context, a.Object); m != "" {
		return ioRequest{}, m //nolint:exhaustruct
	}

	groupBy := apiclient.GetIOHistoryParamsGroupBy(cmp.Or(a.GroupBy, string(apiclient.Context)))

	switch groupBy {
	case apiclient.Context, apiclient.BackendType, apiclient.Full:
	default:
		return ioRequest{}, "group_by must be 'context', 'backend_type' or 'full'" //nolint:exhaustruct
	}

	top := a.Top
	if top <= 0 {
		top = ioSummaryDefaultTop
	}

	if top > ioMaxTop {
		return ioRequest{}, "top must be 200 or less" //nolint:exhaustruct
	}

	one := 1

	return ioRequest{
		Params: &apiclient.GetIOHistoryParams{
			ClusterName: a.Cluster,
			Instance:    a.Instance,
			From:        from,
			To:          to,
			GroupBy:     &groupBy,
			Points:      &one,
			Context:     opt(a.Context),
			BackendType: opt(a.BackendType),
			Object:      opt(a.Object),
		},
		Top:      top,
		Capped:   capped,
		Filtered: a.Context != "" || a.BackendType != "" || a.Object != "",
	}, ""
}

func ioSummary(ctx context.Context, c *DashaClient, a ioSummaryArgs) (any, error) {
	req, msg := ioSummaryParams(a)
	if msg != "" {
		return nil, errors.New(msg)
	}

	hist, err := c.IOHistory(ctx, req.Params)
	if err != nil {
		return nil, err
	}

	out := ioSummaryResult{ //nolint:exhaustruct
		Requested:    ioRange{From: req.Params.From, To: req.Params.To},
		WindowCapped: req.Capped,
		Meta:         hist.Meta,
		GroupBy:      string(*req.Params.GroupBy),
		RankedBy:     ioRankedBy,
		Rows:         []ioSummaryRow{},
	}

	var (
		window ioSummaryWindow
		rows   []ioSummaryRow
		totals = map[string]int64{}
		seen   int
	)

	window.Complete = true

	for _, s := range hist.Series {
		seen += len(s.Points)

		values, span, duration, complete := ioFold(s.Points)
		if span.From.IsZero() {
			continue
		}

		ioMergeWindow(&window, span, duration, complete)

		for k, v := range values {
			totals[k] += v
		}

		if ioIdle(values) {
			continue
		}

		rows = append(rows, ioRow(s.Key, values))
	}

	if !window.From.IsZero() {
		out.Window = &window
	}

	out.Totals = ioTrimZeros(totals)

	if len(rows) == 0 {
		out.EmptyReason, out.EmptyDetail = ioEmptyReason(ctx, c, ioEmptyInput{
			Cluster:   a.Cluster,
			Instance:  a.Instance,
			Meta:      hist.Meta,
			Requested: out.Requested,
			Seen:      seen,
			Measured:  window.DurationSeconds > 0,
			Filtered:  req.Filtered,
		})

		return out, nil
	}

	ioFinishRows(rows, window)
	slices.SortStableFunc(rows, ioRowLess)

	out.RowsTotal = len(rows)

	if len(rows) > req.Top {
		rows = rows[:req.Top]
	}

	out.Rows = rows
	out.RowsReturned = len(rows)

	return out, nil
}

// duration counts only measurable spans — the denominator of any rate.
func ioFold(points []apiclient.IOPoint) (map[string]int64, ioRange, float64, bool) {
	var (
		values   = map[string]int64{}
		span     ioRange
		complete = true
		duration float64
	)

	for _, p := range points {
		if !p.Complete {
			complete = false
		}

		duration += p.DurationSeconds

		if span.From.IsZero() || p.From.Before(span.From) {
			span.From = p.From
		}

		if p.To.After(span.To) {
			span.To = p.To
		}

		for k, v := range p.Values {
			values[k] += v
		}
	}

	return values, span, duration, complete
}

// Series share the bucket grid: duration is the longest seen, not the sum.
func ioMergeWindow(w *ioSummaryWindow, span ioRange, duration float64, complete bool) {
	if w.From.IsZero() || span.From.Before(w.From) {
		w.From = span.From
	}

	if span.To.After(w.To) {
		w.To = span.To
	}

	w.DurationSeconds = max(w.DurationSeconds, duration)

	if !complete {
		w.Complete = false
	}
}

func ioIdle(values map[string]int64) bool {
	for k, v := range values {
		if k != "hits" && v != 0 {
			return false
		}
	}

	return true
}

func ioRow(key apiclient.IOSeriesKey, values map[string]int64) ioSummaryRow {
	row := ioSummaryRow{ //nolint:exhaustruct
		BackendType: deref(key.BackendType),
		Object:      deref(key.Object),
		Context:     deref(key.Context),
		Values:      ioTrimZeros(values),
	}

	for _, m := range ioRankMetrics {
		row.IOOps += values[m]
	}

	return row
}

func ioFinishRows(rows []ioSummaryRow, w ioSummaryWindow) {
	var total int64
	for _, r := range rows {
		total += r.IOOps
	}

	for i := range rows {
		r := &rows[i]

		if total > 0 {
			r.SharePct = round2(float64(r.IOOps) / float64(total) * 100)
		}

		if w.DurationSeconds > 0 {
			r.OpsPerSecond = round2(float64(r.IOOps) / w.DurationSeconds)
		}

		r.AvgReadMs = ioAvgMs(r.Values, "read_time", "reads")
		r.AvgWriteMs = ioAvgMs(r.Values, "write_time", "writes")
	}
}

// nil, not zero: with track_io_timing off every time counter is zero.
func ioAvgMs(values map[string]int64, timeKey, opsKey string) *float64 {
	t, ops := values[timeKey], values[opsKey]
	if t <= 0 || ops <= 0 {
		return nil
	}

	v := round2(float64(t) / float64(ops))

	return &v
}

func ioRowLess(a, b ioSummaryRow) int {
	if c := cmp.Compare(b.IOOps, a.IOOps); c != 0 {
		return c
	}

	return cmp.Or(
		cmp.Compare(a.BackendType, b.BackendType),
		cmp.Compare(a.Object, b.Object),
		cmp.Compare(a.Context, b.Context),
	)
}

func ioTrendParams(a ioTrendArgs) (ioRequest, string) {
	from, to, capped, msg := ioWindow(a.Since, a.From, a.To, ioTrendDefaultSince)
	if msg != "" {
		return ioRequest{}, msg //nolint:exhaustruct
	}

	if m := ioFilterMsg(a.Context, a.Object); m != "" {
		return ioRequest{}, m //nolint:exhaustruct
	}

	points := a.Points
	if points <= 0 {
		points = ioTrendDefaultPoints
	}

	if points > ioMaxPoints {
		return ioRequest{}, "points must be 200 or less" //nolint:exhaustruct
	}

	groupBy := apiclient.Context

	return ioRequest{ //nolint:exhaustruct
		Params: &apiclient.GetIOHistoryParams{
			ClusterName: a.Cluster,
			Instance:    a.Instance,
			From:        from,
			To:          to,
			GroupBy:     &groupBy,
			Points:      &points,
			Context:     opt(a.Context),
			BackendType: opt(a.BackendType),
			Object:      opt(a.Object),
		},
		Capped:   capped,
		Filtered: a.Context != "" || a.BackendType != "" || a.Object != "",
	}, ""
}

type ioTrendScan struct {
	Window     ioRange
	Incomplete map[int64]bool
	Measured   bool
	Seen       int
}

func ioTrend(ctx context.Context, c *DashaClient, a ioTrendArgs) (any, error) {
	req, msg := ioTrendParams(a)
	if msg != "" {
		return nil, errors.New(msg)
	}

	hist, err := c.IOHistory(ctx, req.Params)
	if err != nil {
		return nil, err
	}

	metrics := slices.Clone(ioTrendMetrics)
	if hist.Meta.TrackIoTiming || hist.Meta.TrackIoTimingChanged {
		metrics = append(metrics, ioTrendTimeMetrics...)
	}

	out := ioTrendResult{ //nolint:exhaustruct
		Requested:    ioRange{From: req.Params.From, To: req.Params.To},
		WindowCapped: req.Capped,
		Meta:         hist.Meta,
		GroupBy:      string(apiclient.Context),
		Metrics:      metrics,
		Series:       []ioTrendSeries{},
	}

	var series []ioTrendSeries

	scan := ioTrendScan{Incomplete: map[int64]bool{}} //nolint:exhaustruct

	for _, s := range hist.Series {
		pts, active := ioTrendPoints(s.Points, metrics, &scan)
		if !active {
			continue
		}

		series = append(series, ioTrendSeries{Key: s.Key, Points: pts})
	}

	out.IncompletePoints = len(scan.Incomplete)

	if !scan.Window.From.IsZero() {
		out.Window = &scan.Window
	}

	if len(series) == 0 {
		out.EmptyReason, out.EmptyDetail = ioEmptyReason(ctx, c, ioEmptyInput{
			Cluster:   a.Cluster,
			Instance:  a.Instance,
			Meta:      hist.Meta,
			Requested: out.Requested,
			Seen:      scan.Seen,
			Measured:  scan.Measured,
			Filtered:  req.Filtered,
		})

		return out, nil
	}

	slices.SortStableFunc(series, ioSeriesLess)

	out.Points = len(series[0].Points)
	out.Series = series

	return out, nil
}

func ioTrendPoints(points []apiclient.IOPoint, metrics []string, scan *ioTrendScan) ([]ioTrendPoint, bool) {
	out := make([]ioTrendPoint, 0, len(points))
	active := false

	for _, p := range points {
		scan.Seen++

		if scan.Window.From.IsZero() || p.From.Before(scan.Window.From) {
			scan.Window.From = p.From
		}

		if p.To.After(scan.Window.To) {
			scan.Window.To = p.To
		}

		pt := ioTrendPoint{ //nolint:exhaustruct
			At:              p.To,
			DurationSeconds: p.DurationSeconds,
			Complete:        p.Complete,
		}

		if !p.Complete {
			// The epoch break is instance-wide: count buckets, not series times buckets.
			scan.Incomplete[p.To.UnixNano()] = true
			pt.CoveragePct = ioCoverage(p)
		}

		if p.DurationSeconds > 0 {
			scan.Measured = true

			values := map[string]int64{}

			for _, m := range metrics {
				if v := p.Values[m]; v != 0 {
					values[m] = v
					active = true
				}
			}

			if len(values) > 0 {
				pt.Values = values
			}
		}

		out = append(out, pt)
	}

	return out, active
}

func ioCoverage(p apiclient.IOPoint) float64 {
	span := p.To.Sub(p.From).Seconds()
	if span <= 0 {
		return 0
	}

	return round2(p.DurationSeconds / span * 100)
}

func ioSeriesLess(a, b ioTrendSeries) int {
	if c := cmp.Compare(ioSeriesWeight(b), ioSeriesWeight(a)); c != 0 {
		return c
	}

	return cmp.Compare(deref(a.Key.Context), deref(b.Key.Context))
}

func ioSeriesWeight(s ioTrendSeries) int64 {
	var total int64

	for _, p := range s.Points {
		for _, m := range ioRankMetrics {
			total += p.Values[m]
		}
	}

	return total
}

type ioEmptyInput struct {
	Cluster   string
	Instance  string
	Meta      apiclient.IOHistoryMeta
	Requested ioRange
	Seen      int
	Measured  bool
	Filtered  bool
}

// The live probe runs only where no stored history exists at all.
func ioEmptyReason(ctx context.Context, c *DashaClient, in ioEmptyInput) (string, string) {
	if in.Meta.EarliestAt == nil {
		supported, err := c.IOSupported(ctx, in.Cluster, in.Instance)

		switch {
		case err != nil:
			return "support_unknown", err.Error()
		case !supported:
			return "unsupported_version", ""
		default:
			return "no_snapshots", ""
		}
	}

	// A filter is applied before the series are built: an empty response cannot
	// tell a filter miss from a window with no captures.
	switch {
	case in.Meta.EarliestAt.After(in.Requested.To):
		return "window_before_history", ""
	case in.Meta.LatestAt != nil && in.Meta.LatestAt.Before(in.Requested.From):
		return "window_after_history", ""
	case in.Seen == 0 && in.Filtered:
		return "no_io_matching_filter", ""
	case in.Seen == 0:
		return "no_snapshots_in_window", ""
	case !in.Measured:
		return "no_comparable_snapshots", ""
	}

	return "no_io", ""
}

// Absent means zero in a complete result.
func ioTrimZeros(values map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(values))

	for k, v := range values {
		if v != 0 {
			out[k] = v
		}
	}

	return out
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
