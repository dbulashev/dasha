package mcpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dbulashev/dasha/gen/apiclient"
)

func ioFakeAPI(t *testing.T, history string, ioStatus int) *DashaClient {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/io/history":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(history))
		case "/api/io/current":
			if ioStatus != http.StatusOK {
				w.WriteHeader(ioStatus)

				return
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"instance":"h1","captured_at":"2026-08-29T10:00:00Z",` +
				`"version_num":170000,"track_io_timing":true,"rows":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	c, err := NewDashaClient(Config{DashaURL: srv.URL, Token: "t"}) //nolint:exhaustruct
	if err != nil {
		t.Fatalf("NewDashaClient: %v", err)
	}

	return c
}

// A fixed latest_at would start reading as window_after_history a day later.
func ioLiveMeta(earliest string) string {
	return `"meta":{"instance":"h1","earliest_at":"` + earliest + `","latest_at":"` +
		time.Now().UTC().Format(time.RFC3339) + `","track_io_timing":true,` +
		`"track_io_timing_changed":false,"version_changed":false}`
}

const ioHistoryJSON = `{
  "meta": {"instance":"h1","earliest_at":"2026-08-01T00:00:00Z","latest_at":"2026-08-29T10:00:00Z",
           "track_io_timing":true,"track_io_timing_changed":false,"version_changed":false},
  "series": [
    {"key":{"context":"normal"},
     "points":[{"from":"2026-08-29T09:00:00Z","to":"2026-08-29T10:00:00Z","duration_seconds":3600,
                "complete":true,"values":{"reads":100,"writes":50,"hits":1000,"fsyncs":0}}]},
    {"key":{"context":"vacuum"},
     "points":[{"from":"2026-08-29T09:00:00Z","to":"2026-08-29T10:00:00Z","duration_seconds":3600,
                "complete":true,"values":{"reads":900,"read_time":450,"hits":20}}]},
    {"key":{"context":"bulkwrite"},
     "points":[{"from":"2026-08-29T09:00:00Z","to":"2026-08-29T10:00:00Z","duration_seconds":3600,
                "complete":true,"values":{"hits":500}}]}
  ]}`

func ioSummaryOf(t *testing.T, c *DashaClient, a ioSummaryArgs) ioSummaryResult {
	t.Helper()

	got, err := ioSummary(context.Background(), c, a)
	if err != nil {
		t.Fatalf("ioSummary: %v", err)
	}

	res, ok := got.(ioSummaryResult)
	if !ok {
		t.Fatalf("ioSummary returned %T, want ioSummaryResult", got)
	}

	return res
}

func TestIOSummary_RanksAndDropsCacheOnlyRows(t *testing.T) {
	t.Parallel()

	c := ioFakeAPI(t, ioHistoryJSON, http.StatusOK)
	res := ioSummaryOf(t, c, ioSummaryArgs{Cluster: "demo", Instance: "h1"}) //nolint:exhaustruct

	// bulkwrite did no physical I/O — no row.
	if res.RowsTotal != 2 || res.RowsReturned != 2 {
		t.Fatalf("rows total/returned = %d/%d, want 2/2", res.RowsTotal, res.RowsReturned)
	}

	if res.Rows[0].Context != "vacuum" || res.Rows[1].Context != "normal" {
		t.Fatalf("row order = %q, %q; want vacuum first (heaviest)", res.Rows[0].Context, res.Rows[1].Context)
	}

	if res.Rows[0].IOOps != 900 || res.Rows[1].IOOps != 150 {
		t.Errorf("io_ops = %d, %d; want 900, 150", res.Rows[0].IOOps, res.Rows[1].IOOps)
	}

	if res.Rows[0].SharePct != 85.71 || res.Rows[1].SharePct != 14.29 {
		t.Errorf("share_pct = %v, %v; want 85.71, 14.29", res.Rows[0].SharePct, res.Rows[1].SharePct)
	}

	if res.Rows[0].OpsPerSecond != 0.25 {
		t.Errorf("ops_per_second = %v, want 0.25", res.Rows[0].OpsPerSecond)
	}

	// hits survive in the totals though the row is gone.
	if res.Totals["hits"] != 1520 {
		t.Errorf("totals[hits] = %d, want 1520 (every series, dropped ones included)", res.Totals["hits"])
	}

	// An explicit fsyncs:0 must not survive.
	if _, ok := res.Rows[1].Values["fsyncs"]; ok {
		t.Errorf("zero counters must be trimmed from values")
	}
}

func TestIOSummary_LatencyOnlyWhenMeasured(t *testing.T) {
	t.Parallel()

	c := ioFakeAPI(t, ioHistoryJSON, http.StatusOK)
	res := ioSummaryOf(t, c, ioSummaryArgs{Cluster: "demo", Instance: "h1"}) //nolint:exhaustruct

	if res.Rows[0].AvgReadMs == nil || *res.Rows[0].AvgReadMs != 0.5 {
		t.Errorf("vacuum avg_read_ms = %v, want 0.5", res.Rows[0].AvgReadMs)
	}

	// No read_time recorded: absent, never 0.00.
	if res.Rows[1].AvgReadMs != nil {
		t.Errorf("avg_read_ms = %v with no read_time, want absent", *res.Rows[1].AvgReadMs)
	}
}

func TestIOSummary_TopCutsTailAndReportsIt(t *testing.T) {
	t.Parallel()

	c := ioFakeAPI(t, ioHistoryJSON, http.StatusOK)
	res := ioSummaryOf(t, c, ioSummaryArgs{Cluster: "demo", Instance: "h1", Top: 1}) //nolint:exhaustruct

	if res.RowsReturned != 1 || res.RowsTotal != 2 {
		t.Fatalf("returned/total = %d/%d, want 1/2 so the model can see the tail was cut",
			res.RowsReturned, res.RowsTotal)
	}

	if res.RankedBy != ioRankedBy {
		t.Errorf("ranked_by = %q, want %q", res.RankedBy, ioRankedBy)
	}
}

func TestIOSummary_WindowComesFromTheData(t *testing.T) {
	t.Parallel()

	c := ioFakeAPI(t, ioHistoryJSON, http.StatusOK)
	res := ioSummaryOf(t, c, ioSummaryArgs{Cluster: "demo", Instance: "h1", Since: "24h"}) //nolint:exhaustruct

	if res.Window == nil {
		t.Fatal("window must be reported")
	}

	// Asked 24h, covered 1h: the window is the data's, not the request's.
	if got := res.Window.To.Sub(res.Window.From); got != time.Hour {
		t.Errorf("covered window = %v, want 1h", got)
	}

	if res.Window.DurationSeconds != 3600 {
		t.Errorf("duration_seconds = %v, want 3600 (longest series, not their sum)", res.Window.DurationSeconds)
	}

	if req := res.Requested.To.Sub(res.Requested.From); req != 24*time.Hour {
		t.Errorf("requested window = %v, want 24h", req)
	}
}

func TestIOSummary_EmptyReason(t *testing.T) {
	t.Parallel()

	const noHistory = `{"meta":{"instance":"h1","earliest_at":null,"latest_at":null,
      "track_io_timing":false,"track_io_timing_changed":false,"version_changed":false},"series":[]}`

	const laterHistory = `{"meta":{"instance":"h1","earliest_at":"2030-01-01T00:00:00Z",
      "latest_at":"2030-01-02T00:00:00Z","track_io_timing":true,
      "track_io_timing_changed":false,"version_changed":false},"series":[]}`

	// Every series is cache-only, so nothing survives the idle filter.
	idleOnly := `{` + ioLiveMeta("2026-08-01T00:00:00Z") + `,
      "series":[{"key":{"context":"normal"},"points":[{"from":"2026-08-29T09:00:00Z",
      "to":"2026-08-29T10:00:00Z","duration_seconds":3600,"complete":true,"values":{"hits":10}}]}]}`

	// History stops days before the window starts: the collector is dead.
	const staleHistory = `{"meta":{"instance":"h1","earliest_at":"2020-01-01T00:00:00Z",
      "latest_at":"2020-01-02T00:00:00Z","track_io_timing":true,
      "track_io_timing_changed":false,"version_changed":false},"series":[]}`

	// Snapshots exist on both sides of the window but none inside it.
	const gapHistory = `{"meta":{"instance":"h1","earliest_at":"2020-01-01T00:00:00Z",
      "latest_at":"2030-01-01T00:00:00Z","track_io_timing":true,
      "track_io_timing_changed":false,"version_changed":false},"series":[]}`

	// Every interval spans a reset, so nothing in the window is comparable.
	brokenEpoch := `{` + ioLiveMeta("2026-08-01T00:00:00Z") + `,
      "series":[{"key":{"context":"normal"},"points":[{"from":"2026-08-29T09:00:00Z",
      "to":"2026-08-29T10:00:00Z","duration_seconds":0,"complete":false,"values":{}}]}]}`

	tests := []struct {
		name     string
		history  string
		ioStatus int
		want     string
	}{
		{"pg 15 has no pg_stat_io", noHistory, http.StatusNotImplemented, "unsupported_version"},
		{"supported but not captured yet", noHistory, http.StatusOK, "no_snapshots"},
		{"the support probe was refused", noHistory, http.StatusForbidden, "support_unknown"},
		{"window ends before history starts", laterHistory, http.StatusOK, "window_before_history"},
		{"collector stopped before the window", staleHistory, http.StatusOK, "window_after_history"},
		{"no capture fell inside the window", gapHistory, http.StatusOK, "no_snapshots_in_window"},
		{"every interval spans a reset", brokenEpoch, http.StatusOK, "no_comparable_snapshots"},
		{"genuinely no physical I/O", idleOnly, http.StatusOK, "no_io"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := ioFakeAPI(t, tt.history, tt.ioStatus)
			res := ioSummaryOf(t, c, ioSummaryArgs{Cluster: "demo", Instance: "h1"}) //nolint:exhaustruct

			if res.EmptyReason != tt.want {
				t.Errorf("empty_reason = %q, want %q", res.EmptyReason, tt.want)
			}
		})
	}
}

// A filter miss must not read as "the instance did no I/O".
func TestIOSummary_FilteredEmptyIsNotNoIO(t *testing.T) {
	t.Parallel()

	nothingMatched := `{` + ioLiveMeta("2026-08-01T00:00:00Z") + `,"series":[]}`

	c := ioFakeAPI(t, nothingMatched, http.StatusOK)

	res := ioSummaryOf(t, c, ioSummaryArgs{ //nolint:exhaustruct
		Cluster: "demo", Instance: "h1", BackendType: "autovacuum",
	})

	if res.EmptyReason != "no_io_matching_filter" {
		t.Errorf("empty_reason = %q, want no_io_matching_filter", res.EmptyReason)
	}
}

// A cache-only instance is busy, not idle — the hits are the evidence.
func TestIOSummary_EmptyKeepsTotalsAndWindow(t *testing.T) {
	t.Parallel()

	cachedOnly := `{` + ioLiveMeta("2026-08-01T00:00:00Z") + `,
      "series":[{"key":{"context":"normal"},"points":[{"from":"2026-08-29T09:00:00Z",
      "to":"2026-08-29T10:00:00Z","duration_seconds":3600,"complete":true,
      "values":{"hits":50000000}}]}]}`

	c := ioFakeAPI(t, cachedOnly, http.StatusOK)
	res := ioSummaryOf(t, c, ioSummaryArgs{Cluster: "demo", Instance: "h1"}) //nolint:exhaustruct

	if res.EmptyReason != "no_io" {
		t.Fatalf("empty_reason = %q, want no_io", res.EmptyReason)
	}

	if res.Totals["hits"] != 50000000 {
		t.Errorf("totals[hits] = %d, want the counters of the dropped rows", res.Totals["hits"])
	}

	if res.Window == nil || res.Window.DurationSeconds != 3600 {
		t.Errorf("an empty result must still report what the data covered")
	}
}

const ioTrendJSON = `{
  "meta": {"instance":"h1","earliest_at":"2026-08-01T00:00:00Z","latest_at":"2026-08-29T10:00:00Z",
           "track_io_timing":true,"track_io_timing_changed":false,"version_changed":false},
  "series": [
    {"key":{"context":"vacuum"},
     "points":[
       {"from":"2026-08-29T07:00:00Z","to":"2026-08-29T08:00:00Z","duration_seconds":3600,
        "complete":true,"values":{"reads":500,"read_time":250,"writes":0}},
       {"from":"2026-08-29T08:00:00Z","to":"2026-08-29T09:00:00Z","duration_seconds":0,
        "complete":false,"values":{}},
       {"from":"2026-08-29T09:00:00Z","to":"2026-08-29T10:00:00Z","duration_seconds":3600,
        "complete":true,"values":{"reads":700}}]},
    {"key":{"context":"bulkwrite"},
     "points":[
       {"from":"2026-08-29T07:00:00Z","to":"2026-08-29T08:00:00Z","duration_seconds":3600,
        "complete":true,"values":{"hits":90}}]}
  ]}`

func TestIOTrend_GapsCarryNoValues(t *testing.T) {
	t.Parallel()

	c := ioFakeAPI(t, ioTrendJSON, http.StatusOK)

	got, err := ioTrend(context.Background(), c, ioTrendArgs{Cluster: "demo", Instance: "h1"}) //nolint:exhaustruct
	if err != nil {
		t.Fatalf("ioTrend: %v", err)
	}

	res, ok := got.(ioTrendResult)
	if !ok {
		t.Fatalf("ioTrend returned %T, want ioTrendResult", got)
	}

	// The cache-only series carries no I/O and must not be plotted.
	if len(res.Series) != 1 || deref(res.Series[0].Key.Context) != "vacuum" {
		t.Fatalf("series = %d, want only vacuum", len(res.Series))
	}

	if res.IncompletePoints != 1 {
		t.Errorf("incomplete_points = %d, want 1 (buckets, not series x buckets)", res.IncompletePoints)
	}

	gap := res.Series[0].Points[1]
	if gap.Complete {
		t.Fatal("the middle bucket spans a stats reset and must be incomplete")
	}

	// A zero here would read as a lull in the load.
	if gap.Values != nil {
		t.Errorf("a bucket that measured nothing must carry no values, got %v", gap.Values)
	}

	if gap.CoveragePct != 0 {
		t.Errorf("coverage_pct = %v, want 0 for a bucket that measured nothing", gap.CoveragePct)
	}

	if !res.Series[0].Points[0].Complete || res.Series[0].Points[0].Values["reads"] != 500 {
		t.Errorf("complete points must keep their counters")
	}

	if _, ok := res.Series[0].Points[2].Values["writes"]; ok {
		t.Errorf("zero counters must be trimmed inside a complete point")
	}
}

func TestIOTrend_TimeMetricsFollowTracking(t *testing.T) {
	t.Parallel()

	c := ioFakeAPI(t, ioTrendJSON, http.StatusOK)

	got, _ := ioTrend(context.Background(), c, ioTrendArgs{Cluster: "demo", Instance: "h1"}) //nolint:exhaustruct

	res, ok := got.(ioTrendResult)
	if !ok {
		t.Fatalf("ioTrend returned %T", got)
	}

	if !slices.Contains(res.Metrics, "read_time") {
		t.Errorf("metrics = %v, must include read_time when track_io_timing is on", res.Metrics)
	}

	off := `{"meta":{"instance":"h1","earliest_at":"2026-08-01T00:00:00Z","latest_at":"2026-08-29T10:00:00Z",
      "track_io_timing":false,"track_io_timing_changed":false,"version_changed":false},
      "series":[{"key":{"context":"normal"},"points":[{"from":"2026-08-29T09:00:00Z",
      "to":"2026-08-29T10:00:00Z","duration_seconds":3600,"complete":true,"values":{"reads":5}}]}]}`

	got2, _ := ioTrend(context.Background(), ioFakeAPI(t, off, http.StatusOK),
		ioTrendArgs{Cluster: "demo", Instance: "h1"}) //nolint:exhaustruct

	res2, ok := got2.(ioTrendResult)
	if !ok {
		t.Fatalf("ioTrend returned %T", got2)
	}

	if slices.Contains(res2.Metrics, "read_time") {
		t.Errorf("metrics = %v, must omit time metrics that are zero by construction", res2.Metrics)
	}
}

func TestIOSummaryParams_Defaults(t *testing.T) {
	t.Parallel()

	req, msg := ioSummaryParams(ioSummaryArgs{Cluster: "demo", Instance: "h1"}) //nolint:exhaustruct
	if msg != "" {
		t.Fatalf("ioSummaryParams(defaults) error = %q, want none", msg)
	}

	p := req.Params

	if p.Points == nil || *p.Points != 1 {
		t.Errorf("a summary must ask for exactly one bucket")
	}

	if p.GroupBy == nil || *p.GroupBy != apiclient.Context {
		t.Errorf("group_by = %v, want context", p.GroupBy)
	}

	if req.Top != ioSummaryDefaultTop {
		t.Errorf("top = %d, want %d", req.Top, ioSummaryDefaultTop)
	}

	if got := p.To.Sub(p.From); got != ioSummaryDefaultSince {
		t.Errorf("window = %v, want %v", got, ioSummaryDefaultSince)
	}

	if p.Context != nil || p.BackendType != nil || p.Object != nil {
		t.Errorf("unset filters must be omitted (nil)")
	}

	if req.Capped || req.Filtered {
		t.Errorf("a default call is neither capped nor filtered")
	}
}

func TestIOTrendParams_Defaults(t *testing.T) {
	t.Parallel()

	req, msg := ioTrendParams(ioTrendArgs{Cluster: "demo", Instance: "h1"}) //nolint:exhaustruct
	if msg != "" {
		t.Fatalf("ioTrendParams(defaults) error = %q, want none", msg)
	}

	if req.Params.Points == nil || *req.Params.Points != ioTrendDefaultPoints {
		t.Errorf("points must default to %d", ioTrendDefaultPoints)
	}

	if got := req.Params.To.Sub(req.Params.From); got != ioTrendDefaultSince {
		t.Errorf("window = %v, want %v", got, ioTrendDefaultSince)
	}

	// Filtering must not change the grouping.
	req2, _ := ioTrendParams(ioTrendArgs{Cluster: "demo", Instance: "h1", Context: "vacuum"}) //nolint:exhaustruct
	if req2.Params.GroupBy == nil || *req2.Params.GroupBy != apiclient.Context {
		t.Errorf("group_by must always be context")
	}

	if !req2.Filtered {
		t.Errorf("a call carrying a dimension filter must be marked filtered")
	}
}

func TestIOParams_Errors(t *testing.T) {
	t.Parallel()

	summary := []struct {
		name string
		args ioSummaryArgs
	}{
		{"bad group_by", ioSummaryArgs{Cluster: "c", Instance: "h", GroupBy: "object"}},               //nolint:exhaustruct
		{"top over cap", ioSummaryArgs{Cluster: "c", Instance: "h", Top: 500}},                        //nolint:exhaustruct
		{"bad since", ioSummaryArgs{Cluster: "c", Instance: "h", Since: "yesterday"}},                 //nolint:exhaustruct
		{"from without to", ioSummaryArgs{Cluster: "c", Instance: "h", From: "2026-07-10T12:00:00Z"}}, //nolint:exhaustruct
		// An unmatched filter would otherwise come back as "no I/O".
		{"bad context", ioSummaryArgs{Cluster: "c", Instance: "h", Context: "vacuuming"}}, //nolint:exhaustruct
		{"bad object", ioSummaryArgs{Cluster: "c", Instance: "h", Object: "temp"}},        //nolint:exhaustruct
	}

	for _, tt := range summary {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, msg := ioSummaryParams(tt.args); msg == "" {
				t.Errorf("ioSummaryParams(%s) must return a validation error", tt.name)
			}
		})
	}

	if _, msg := ioTrendParams(ioTrendArgs{Cluster: "c", Instance: "h", Points: 5000}); msg == "" { //nolint:exhaustruct
		t.Errorf("ioTrendParams must reject points over the cap")
	}

	if _, msg := ioTrendParams(ioTrendArgs{Cluster: "c", Instance: "h", Object: "temp"}); msg == "" { //nolint:exhaustruct
		t.Errorf("ioTrendParams must reject an object pg_stat_io cannot report")
	}
}

// Echoing the untrimmed request would overstate how far back it looked.
func TestIOParams_WindowCappedAtTheEndpointMaximum(t *testing.T) {
	t.Parallel()

	req, msg := ioTrendParams(ioTrendArgs{Cluster: "c", Instance: "h", Since: "90d"}) //nolint:exhaustruct
	if msg != "" {
		t.Fatalf("ioTrendParams('90d') error = %q, want none", msg)
	}

	if got := req.Params.To.Sub(req.Params.From); got != ioMaxWindow {
		t.Errorf("window = %v, want it clamped to %v", got, ioMaxWindow)
	}

	if !req.Capped {
		t.Errorf("a clamped window must be flagged so the model does not over-read it")
	}
}

// The io_trend schema recommends '7d', which time.ParseDuration cannot read.
func TestParseSince_AcceptsDays(t *testing.T) {
	t.Parallel()

	d, err := parseSince("7d")
	if err != nil {
		t.Fatalf("parseSince(7d): %v", err)
	}

	if d != 7*24*time.Hour {
		t.Errorf("parseSince(7d) = %v, want 168h", d)
	}

	if _, err := parseSince("yesterday"); err == nil {
		t.Errorf("parseSince must still reject prose")
	}
}

func TestResolveWindow_DefaultIsPerTool(t *testing.T) {
	t.Parallel()

	from, to, msg := resolveWindow("", "", "", ioTrendDefaultSince)
	if msg != "" {
		t.Fatalf("resolveWindow error = %q, want none", msg)
	}

	if got := to.Sub(from); got != ioTrendDefaultSince {
		t.Errorf("window = %v, want the caller's default %v", got, ioTrendDefaultSince)
	}
}

// Every I/O call can hit this 501 on a Dasha without snapshot storage.
func TestIOHistory_StorageOffExplainsItself(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
	}))
	defer srv.Close()

	c, err := NewDashaClient(Config{DashaURL: srv.URL, Token: "t"}) //nolint:exhaustruct
	if err != nil {
		t.Fatalf("NewDashaClient: %v", err)
	}

	_, err = ioSummary(context.Background(), c, ioSummaryArgs{Cluster: "demo", Instance: "h1"}) //nolint:exhaustruct
	if err == nil {
		t.Fatal("a 501 must surface as an error")
	}

	if !strings.Contains(err.Error(), "snapshot storage") {
		t.Errorf("error = %q, want it to name snapshot storage", err)
	}
}

const ioPartialJSON = `{
  "meta": {"instance":"h1","earliest_at":"2026-08-01T00:00:00Z","latest_at":"2026-08-29T10:00:00Z",
           "track_io_timing":false,"track_io_timing_changed":false,"version_changed":false},
  "series": [
    {"key":{"context":"normal"},
     "points":[{"from":"2026-08-29T09:00:00Z","to":"2026-08-29T10:00:00Z","duration_seconds":3300,
                "complete":false,"values":{"reads":5500,"writes":200}}]}
  ]}`

func ioTrendOf(t *testing.T, c *DashaClient, a ioTrendArgs) ioTrendResult {
	t.Helper()

	got, err := ioTrend(context.Background(), c, a)
	if err != nil {
		t.Fatalf("ioTrend: %v", err)
	}

	res, ok := got.(ioTrendResult)
	if !ok {
		t.Fatalf("ioTrend returned %T, want ioTrendResult", got)
	}

	return res
}

// Dropping a broken bucket's counters would hide 55 minutes of real I/O.
func TestIOTrend_PartialBucketKeepsWhatItMeasured(t *testing.T) {
	t.Parallel()

	res := ioTrendOf(t, ioFakeAPI(t, ioPartialJSON, http.StatusOK),
		ioTrendArgs{Cluster: "demo", Instance: "h1"}) //nolint:exhaustruct

	if len(res.Series) != 1 {
		t.Fatalf("series = %d, want 1 — a partial bucket with counters is not an idle series", len(res.Series))
	}

	pt := res.Series[0].Points[0]
	if pt.Complete {
		t.Fatal("the bucket spans a reset and must stay flagged incomplete")
	}

	if pt.Values["reads"] != 5500 {
		t.Errorf("values[reads] = %d, want 5500", pt.Values["reads"])
	}

	// Without the coverage the counters would be compared against a full hour.
	if pt.CoveragePct != 91.67 {
		t.Errorf("coverage_pct = %v, want 91.67 (3300s of a 3600s bucket)", pt.CoveragePct)
	}

	if res.IncompletePoints != 1 {
		t.Errorf("incomplete_points = %d, want 1", res.IncompletePoints)
	}

	if res.Points != 1 {
		t.Errorf("points = %d, want the buckets actually returned", res.Points)
	}
}

// Every bucket unmeasurable is a broken record, not a quiet instance.
func TestIOTrend_AllIncompleteIsNotNoIO(t *testing.T) {
	t.Parallel()

	allBroken := `{` + ioLiveMeta("2026-08-01T00:00:00Z") + `,
      "series":[{"key":{"context":"normal"},"points":[
        {"from":"2026-08-29T08:00:00Z","to":"2026-08-29T09:00:00Z","duration_seconds":0,
         "complete":false,"values":{}},
        {"from":"2026-08-29T09:00:00Z","to":"2026-08-29T10:00:00Z","duration_seconds":0,
         "complete":false,"values":{}}]}]}`

	res := ioTrendOf(t, ioFakeAPI(t, allBroken, http.StatusOK),
		ioTrendArgs{Cluster: "demo", Instance: "h1"}) //nolint:exhaustruct

	if res.EmptyReason != "no_comparable_snapshots" {
		t.Errorf("empty_reason = %q, want no_comparable_snapshots", res.EmptyReason)
	}

	if res.IncompletePoints != 2 {
		t.Errorf("incomplete_points = %d, want 2", res.IncompletePoints)
	}
}
