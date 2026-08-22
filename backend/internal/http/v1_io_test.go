package http

import (
	"testing"
	"time"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/statio"
)

func ioBucket(from time.Time, partial bool, rows ...statio.Row) statio.Bucket {
	b := statio.Bucket{
		From:     from,
		To:       from.Add(time.Minute),
		Duration: time.Minute,
		Partial:  partial,
		Rows:     rows,
	}

	if partial {
		b.Duration = 0
	}

	return b
}

func ioRow(bt, obj, ctx string, reads int64) statio.Row {
	return statio.Row{
		Key:    statio.Key{BackendType: bt, Object: obj, Context: ctx},
		Values: map[string]int64{"reads": reads},
	}
}

func TestIOSeriesKeepsOnePointPerBucket(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 8, 22, 3, 0, 0, 0, time.UTC)

	buckets := []statio.Bucket{
		ioBucket(start, false, ioRow("client backend", "relation", "normal", 5)),
		// The vacuum context only appears in the second bucket; the normal one
		// must still get a point here so the two series share an x axis.
		ioBucket(start.Add(time.Minute), false, ioRow("autovacuum worker", "relation", "vacuum", 7)),
	}

	series := ioSeries(buckets, statio.Filter{}, statio.GroupByContext)
	if len(series) != 2 {
		t.Fatalf("series = %d, want 2", len(series))
	}

	for _, s := range series {
		if len(s.Points) != len(buckets) {
			t.Fatalf("series %v has %d points, want %d", s.Key, len(s.Points), len(buckets))
		}
	}

	byContext := map[string]serverhttp.IOSeries{}

	for _, s := range series {
		if s.Key.Context == nil {
			t.Fatalf("context grouping must name the context: %+v", s.Key)
		}

		if s.Key.BackendType != nil || s.Key.Object != nil {
			t.Errorf("summed dimensions must be absent from the key: %+v", s.Key)
		}

		byContext[*s.Key.Context] = s
	}

	if got := byContext["normal"].Points[1].Values["reads"]; got != 0 {
		t.Errorf("idle bucket carries reads = %d, want none", got)
	}

	if got := byContext["vacuum"].Points[1].Values["reads"]; got != 7 {
		t.Errorf("vacuum reads = %d, want 7", got)
	}
}

func TestIOSeriesMarksPartialBucketIncomplete(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 8, 22, 3, 0, 0, 0, time.UTC)
	buckets := []statio.Bucket{ioBucket(start, true, ioRow("client backend", "relation", "normal", 1))}

	series := ioSeries(buckets, statio.Filter{}, statio.GroupByContext)
	if len(series) != 1 {
		t.Fatalf("series = %d, want 1", len(series))
	}

	p := series[0].Points[0]
	if p.Complete {
		t.Error("a partial bucket must be reported as incomplete")
	}

	if p.DurationSeconds != 0 {
		t.Errorf("duration = %v, want 0 — no complete interval contributed", p.DurationSeconds)
	}
}

func TestIOSeriesFilters(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 8, 22, 3, 0, 0, 0, time.UTC)
	buckets := []statio.Bucket{ioBucket(start, false,
		ioRow("client backend", "relation", "normal", 1),
		ioRow("client backend", "temp relation", "normal", 2),
	)}

	series := ioSeries(buckets, statio.Filter{Object: "relation"}, statio.GroupByFull)
	if len(series) != 1 {
		t.Fatalf("series = %d, want only the filtered object", len(series))
	}

	if got := series[0].Points[0].Values["reads"]; got != 1 {
		t.Errorf("reads = %d, want 1", got)
	}
}

func TestIOHistoryMetaReportsEpochShifts(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 8, 22, 3, 0, 0, 0, time.UTC)
	earliest, latest := at.Add(-24*time.Hour), at

	snaps := []statio.Snapshot{
		{CapturedAt: at.Add(-2 * time.Minute), VersionNum: 170004, TrackIOTiming: false},
		{CapturedAt: at.Add(-time.Minute), VersionNum: 180001, TrackIOTiming: true},
	}

	meta := ioHistoryMeta("h1", &earliest, &latest, snaps)

	if !meta.TrackIoTimingChanged {
		t.Error("a toggled track_io_timing must be reported")
	}

	if !meta.VersionChanged {
		t.Error("a major upgrade must be reported")
	}

	if !meta.TrackIoTiming {
		t.Error("track_io_timing must reflect the newest capture")
	}

	if meta.EarliestAt == nil || !meta.EarliestAt.Equal(earliest) {
		t.Errorf("earliest_at = %v, want %v", meta.EarliestAt, earliest)
	}
}

func TestIOHistoryMetaWithoutSnapshots(t *testing.T) {
	t.Parallel()

	meta := ioHistoryMeta("h1", nil, nil, nil)
	if meta.TrackIoTimingChanged || meta.VersionChanged || meta.TrackIoTiming {
		t.Errorf("an empty period claims nothing: %+v", meta)
	}
}

func TestIOPointsClamped(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		in   *int
		want int
	}{
		"absent":   {nil, defaultIOPoints},
		"zero":     {new(int), defaultIOPoints},
		"in range": {ptrTo(50), 50},
		"over cap": {ptrTo(5000), maxIOPoints},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := ioPoints(tc.in); got != tc.want {
				t.Errorf("ioPoints = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestIOWindowBounded(t *testing.T) {
	t.Parallel()

	to := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)

	tests := map[string]struct {
		from     time.Time
		wantFrom time.Time
	}{
		"within cap": {to.Add(-6 * time.Hour), to.Add(-6 * time.Hour)},
		"over cap":   {to.AddDate(-1, 0, 0), to.Add(-maxIOWindow)},
		"inverted":   {to.Add(time.Hour), to},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			gotFrom, gotTo := ioWindow(tc.from, to)

			if !gotFrom.Equal(tc.wantFrom) {
				t.Errorf("from = %s, want %s", gotFrom, tc.wantFrom)
			}

			if !gotTo.Equal(to) {
				t.Errorf("to = %s, want %s", gotTo, to)
			}
		})
	}
}

func TestIOGroupBy(t *testing.T) {
	t.Parallel()

	if got := ioGroupBy(""); got != statio.GroupByContext {
		t.Errorf("absent group_by = %q, want context", got)
	}

	if got := ioGroupBy("nonsense"); got != statio.GroupByContext {
		t.Errorf("unknown group_by = %q, want context", got)
	}

	if got := ioGroupBy("full"); got != statio.GroupByFull {
		t.Errorf("group_by = %q, want full", got)
	}
}

func TestGetIOHistoryWithoutStorage(t *testing.T) {
	t.Parallel()

	h := &Handlers{} //nolint:exhaustruct

	resp, err := h.GetIOHistory(t.Context(), serverhttp.GetIOHistoryRequestObject{
		Params: serverhttp.GetIOHistoryParams{ //nolint:exhaustruct
			ClusterName: "c1",
			Instance:    "h1",
			From:        time.Now().Add(-time.Hour),
			To:          time.Now(),
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := resp.(serverhttp.GetIOHistory501Response); !ok {
		t.Fatalf("response = %T, want 501", resp)
	}
}

func ptrTo(v int) *int { return &v }
