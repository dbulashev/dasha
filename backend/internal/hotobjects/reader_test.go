package hotobjects

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type readStore struct {
	snap, prev *Snapshot
	prevErr    error
	top        []TopEntry
	ranks      map[uuid.UUID]map[ObjectRef]int
	anchors    []AnchorRow
	dates      []time.Time

	histFrom, histTo time.Time
}

func (f *readStore) GetHotSnapshot(context.Context, string, string, *time.Time) (*Snapshot, error) {
	return f.snap, nil
}

func (f *readStore) GetHotSnapshotBefore(context.Context, string, string, time.Time) (*Snapshot, error) {
	return f.prev, f.prevErr
}

func (f *readStore) GetHotTop(context.Context, uuid.UUID, time.Time, Kind, Class, int, int) ([]TopEntry, error) {
	return f.top, nil
}

func (f *readStore) GetHotRanks(_ context.Context, id uuid.UUID, _ time.Time, _ Kind, _ Class) (map[ObjectRef]int, error) {
	return f.ranks[id], nil
}

func (f *readStore) ListHotSnapshotDates(context.Context, string, string, int) ([]time.Time, error) {
	return f.dates, nil
}

func (f *readStore) GetHotAnchorsForObject(context.Context, string, string, Kind, string, string) ([]AnchorRow, error) {
	return f.anchors, nil
}

func (f *readStore) GetHotObjectHistory(
	_ context.Context, _, _ string, _ Kind, _, _ string, from, to time.Time,
) ([]HistoryEntry, error) {
	f.histFrom, f.histTo = from, to

	return nil, nil
}

type liveSample struct {
	rows  []AnchorRow
	reset *time.Time
	err   error
}

// liveSource answers per instance; tables and indexes share the script.
type liveSource map[string]liveSample

func (f liveSource) GetHotSampleTables(_ context.Context, _, instance, _ string, _, _ *string) ([]AnchorRow, *time.Time, bool, error) {
	s := f[instance]

	return s.rows, s.reset, false, s.err
}

func (f liveSource) GetHotSampleIndexes(ctx context.Context, cluster, instance, database string, schema, object *string) ([]AnchorRow, *time.Time, bool, error) {
	return f.GetHotSampleTables(ctx, cluster, instance, database, schema, object)
}

var readerNow = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

func newTestReader(store ReadStore, live LiveSource) *Reader {
	r := NewReader(store, live, zap.NewNop())
	r.now = func() time.Time { return readerNow }

	return r
}

func hostWindow(d time.Duration, complete bool) HostWindow {
	return HostWindow{From: readerNow.Add(-d), To: readerNow, Complete: complete} //nolint:exhaustruct
}

func topEntry(rank int, object string, delta Counters) TopEntry {
	return TopEntry{Rank: rank, Schema: "public", Object: object, Delta: delta} //nolint:exhaustruct
}

func sampled(rows []AnchorRow, reset *time.Time) liveSample {
	return liveSample{rows: rows, reset: reset, err: nil}
}

func snapshotWithWindow(window time.Duration) *Snapshot {
	return &Snapshot{ //nolint:exhaustruct
		ID:         uuid.New(),
		CapturedAt: readerNow,
		Windows: map[string]HostWindow{
			"h1": hostWindow(window, true),
			// An incomplete window must not set the normalization base.
			"h2": hostWindow(10*window, false),
		},
		Coverage: map[Class]KindRatio{ClassReads: {Tables: 0.7, Indexes: 0.3}},
		Histograms: map[Class]KindHistogram{ClassReads: {
			Tables: &Histogram{Deciles: []float64{1, 2, 3, 4, 5, 6, 7, 8, 9}, Max: 10}, //nolint:exhaustruct
		}},
	}
}

func TestReportPrevRankAndRate(t *testing.T) {
	t.Parallel()

	snap := snapshotWithWindow(48 * time.Hour)
	prev := &Snapshot{ID: uuid.New(), CapturedAt: readerNow.Add(-24 * time.Hour)} //nolint:exhaustruct

	store := &readStore{ //nolint:exhaustruct
		snap: snap,
		prev: prev,
		top: []TopEntry{
			topEntry(1, "orders", Counters{"seq_tup_read": 300, "idx_tup_fetch": 100}),
			topEntry(2, "users", Counters{"seq_tup_read": 10}),
		},
		ranks: map[uuid.UUID]map[ObjectRef]int{
			prev.ID: {{Schema: "public", Object: "orders"}: 3},
		},
	}

	rep, err := newTestReader(store, liveSource{}).Report(context.Background(), ReportQuery{ //nolint:exhaustruct
		Kind: KindTable, Class: ClassReads, Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, rep.Entries, 2)

	require.NotNil(t, rep.Entries[0].PrevRank)
	assert.Equal(t, 3, *rep.Entries[0].PrevRank)
	assert.Nil(t, rep.Entries[1].PrevRank, "not in the previous top")

	assert.InDelta(t, 200.0, rep.Entries[0].RatePerDay, 1e-9, "400 over a two-day window")
	assert.InDelta(t, 0.7, rep.Coverage, 1e-9)
	assert.Same(t, snap.Histograms[ClassReads].Tables, rep.Histogram)
}

func TestReportPrevSnapshotErrorKeepsReport(t *testing.T) {
	t.Parallel()

	store := &readStore{ //nolint:exhaustruct
		snap:    snapshotWithWindow(24 * time.Hour),
		prevErr: errors.New("boom"),
		top:     []TopEntry{topEntry(1, "orders", nil)},
	}

	rep, err := newTestReader(store, liveSource{}).Report(context.Background(), ReportQuery{ //nolint:exhaustruct
		Kind: KindTable, Class: ClassReads, Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, rep.Entries, 1)
	assert.Nil(t, rep.Entries[0].PrevRank)
}

func TestReportNoSnapshot(t *testing.T) {
	t.Parallel()

	_, err := newTestReader(&readStore{}, liveSource{}).Report(context.Background(), ReportQuery{}) //nolint:exhaustruct
	assert.ErrorIs(t, err, ErrNoSnapshot)
}

func anchorAt(instance string, reset *time.Time, sig string, reads int64) AnchorRow {
	return AnchorRow{ //nolint:exhaustruct
		Instance:   instance,
		CapturedAt: readerNow.Add(-48 * time.Hour),
		StatsReset: reset,
		PartSig:    sig,
		Counters:   Counters{"seq_tup_read": reads},
	}
}

func liveRow(sig string, reads int64) []AnchorRow {
	return []AnchorRow{{PartSig: sig, Counters: Counters{"seq_tup_read": reads}}} //nolint:exhaustruct
}

func TestPercentileSkipsUnmeasurableHosts(t *testing.T) {
	t.Parallel()

	epoch := readerNow.Add(-30 * 24 * time.Hour)
	newEpoch := readerNow.Add(-time.Hour)

	store := &readStore{ //nolint:exhaustruct
		snap: snapshotWithWindow(24 * time.Hour),
		anchors: []AnchorRow{
			anchorAt("ok", &epoch, "p1", 1000),
			anchorAt("reset", &epoch, "p1", 1000),
			anchorAt("down", &epoch, "p1", 1000),
			anchorAt("repartitioned", &epoch, "p1", 1000),
			anchorAt("recreated", &epoch, "p1", 1000),
			anchorAt("gone", &epoch, "p1", 1000),
		},
	}
	live := liveSource{
		"ok":            sampled(liveRow("p1", 3000), &epoch),
		"reset":         sampled(liveRow("p1", 9000), &newEpoch),
		"down":          {rows: nil, reset: nil, err: errors.New("connection refused")},
		"repartitioned": sampled(liveRow("p2", 9000), &epoch),
		"recreated":     sampled(liveRow("p1", 10), &epoch),
		"gone":          sampled(nil, &epoch),
	}

	p, err := newTestReader(store, live).Percentile(context.Background(), "c", "db",
		KindTable, ObjectRef{Schema: "public", Object: "orders"}, ClassReads)
	require.NoError(t, err)
	assert.InDelta(t, 1000.0, p.RatePerDay, 1e-9, "only the intact host: 2000 over two days")
}

func TestPercentileScalesRateToWindow(t *testing.T) {
	t.Parallel()

	for _, window := range []time.Duration{5 * time.Minute, time.Hour, 24 * time.Hour} {
		t.Run(window.String(), func(t *testing.T) {
			t.Parallel()

			snap := snapshotWithWindow(window)
			store := &readStore{ //nolint:exhaustruct
				snap:    snap,
				anchors: []AnchorRow{anchorAt("h1", nil, "", 0)},
			}
			live := liveSource{"h1": sampled(liveRow("", 2), nil)}

			p, err := newTestReader(store, live).Percentile(context.Background(), "c", "db",
				KindTable, ObjectRef{Schema: "public", Object: "orders"}, ClassReads)
			require.NoError(t, err)

			assert.InDelta(t, 1.0, p.RatePerDay, 1e-9)

			want := snap.Histograms[ClassReads].Tables.Percentile(window.Hours() / 24)
			assert.InDelta(t, want, p.Percentile, 1e-9)
		})
	}
}

func TestPercentileInTopDoesNotSplitOnDots(t *testing.T) {
	t.Parallel()

	snap := snapshotWithWindow(24 * time.Hour)
	store := &readStore{ //nolint:exhaustruct
		snap:    snap,
		anchors: []AnchorRow{anchorAt("h1", nil, "", 0)},
		ranks:   map[uuid.UUID]map[ObjectRef]int{snap.ID: {{Schema: "a.b", Object: "c"}: 1}},
	}
	live := liveSource{"h1": sampled(liveRow("", 0), nil)}
	r := newTestReader(store, live)

	p, err := r.Percentile(context.Background(), "c", "db", KindTable, ObjectRef{Schema: "a", Object: "b.c"}, ClassReads)
	require.NoError(t, err)
	assert.False(t, p.InTop)

	p, err = r.Percentile(context.Background(), "c", "db", KindTable, ObjectRef{Schema: "a.b", Object: "c"}, ClassReads)
	require.NoError(t, err)
	assert.True(t, p.InTop)
}

func TestPercentileNotFound(t *testing.T) {
	t.Parallel()

	ref := ObjectRef{Schema: "public", Object: "orders"}

	_, err := newTestReader(&readStore{}, liveSource{}).Percentile(context.Background(), "c", "db", KindTable, ref, ClassReads) //nolint:exhaustruct
	require.ErrorIs(t, err, ErrNoSnapshot)

	store := &readStore{snap: snapshotWithWindow(time.Hour)} //nolint:exhaustruct

	_, err = newTestReader(store, liveSource{}).Percentile(context.Background(), "c", "db", KindTable, ref, ClassReads)
	require.ErrorIs(t, err, ErrNoAnchors)
}

func TestHistoryDefaultDays(t *testing.T) {
	t.Parallel()

	store := &readStore{} //nolint:exhaustruct
	r := newTestReader(store, liveSource{})

	_, err := r.History(context.Background(), "c", "db", KindTable, ObjectRef{Schema: "public", Object: "orders"}, 0)
	require.NoError(t, err)
	assert.Equal(t, readerNow, store.histTo)
	assert.Equal(t, readerNow.AddDate(0, 0, -30), store.histFrom)

	_, err = r.History(context.Background(), "c", "db", KindTable, ObjectRef{Schema: "public", Object: "orders"}, 7)
	require.NoError(t, err)
	assert.Equal(t, readerNow.AddDate(0, 0, -7), store.histFrom)
}
