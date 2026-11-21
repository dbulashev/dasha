package hotobjects

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

var (
	ErrNoSnapshot = errors.New("hotobjects: no snapshot")
	ErrNoAnchors  = errors.New("hotobjects: object has no anchors")
)

// ReadStore is the stored side: snapshots, their tops and the anchors.
// GetHotSnapshot and GetHotSnapshotBefore return nil, nil when there is none.
type ReadStore interface {
	GetHotSnapshot(ctx context.Context, cluster, database string, at *time.Time) (*Snapshot, error)
	GetHotSnapshotBefore(ctx context.Context, cluster, database string, before time.Time) (*Snapshot, error)
	GetHotTop(ctx context.Context, id uuid.UUID, capturedAt time.Time, kind Kind, class Class, limit, offset int) ([]TopEntry, error)
	GetHotRanks(ctx context.Context, id uuid.UUID, capturedAt time.Time, kind Kind, class Class) (map[ObjectRef]int, error)
	ListHotSnapshotDates(ctx context.Context, cluster, database string, limit int) ([]time.Time, error)
	GetHotAnchorsForObject(ctx context.Context, cluster, database string, kind Kind, schema, object string) ([]AnchorRow, error)
	GetHotObjectHistory(ctx context.Context, cluster, database string, kind Kind, schema, object string, from, to time.Time) ([]HistoryEntry, error)
}

// LiveSource samples cumulative counters on a host right now.
type LiveSource interface {
	GetHotSampleTables(ctx context.Context, cluster, instance, database string, schema, object *string) ([]AnchorRow, *time.Time, bool, error)
	GetHotSampleIndexes(ctx context.Context, cluster, instance, database string, schema, object *string) ([]AnchorRow, *time.Time, bool, error)
}

type ReportQuery struct {
	Cluster  string
	Database string
	Kind     Kind
	Class    Class
	At       *time.Time // nil — the latest snapshot
	Limit    int
	Offset   int
}

type ReportEntry struct {
	TopEntry

	// PrevRank is the rank in the preceding snapshot; nil when the object was
	// not in its top or that snapshot could not be read.
	PrevRank   *int
	RatePerDay float64
}

type Report struct {
	Snapshot  *Snapshot
	Dates     []time.Time // newest first
	Histogram *Histogram
	Coverage  float64
	Entries   []ReportEntry
}

type Percentile struct {
	Percentile float64
	RatePerDay float64
	InTop      bool
}

const (
	// reportDatesLimit bounds the day-selector list.
	reportDatesLimit   = 60
	defaultHistoryDays = 30
)

type Reader struct {
	store  ReadStore
	live   LiveSource
	logger *zap.Logger
	now    func() time.Time
}

func NewReader(store ReadStore, live LiveSource, logger *zap.Logger) *Reader {
	return &Reader{store: store, live: live, logger: logger, now: time.Now}
}

// Report returns one page of a stored top with trend and per-day rates.
func (r *Reader) Report(ctx context.Context, q ReportQuery) (Report, error) {
	snap, err := r.store.GetHotSnapshot(ctx, q.Cluster, q.Database, q.At)
	if err != nil {
		return Report{}, err //nolint:exhaustruct
	}

	if snap == nil {
		return Report{}, ErrNoSnapshot //nolint:exhaustruct
	}

	top, err := r.store.GetHotTop(ctx, snap.ID, snap.CapturedAt, q.Kind, q.Class, q.Limit, q.Offset)
	if err != nil {
		return Report{}, err //nolint:exhaustruct
	}

	prevRanks := r.prevRanks(ctx, q, snap)

	dates, err := r.store.ListHotSnapshotDates(ctx, q.Cluster, q.Database, reportDatesLimit)
	if err != nil {
		return Report{}, err //nolint:exhaustruct
	}

	windowDays := snap.WindowDays()
	entries := make([]ReportEntry, 0, len(top))

	for _, e := range top {
		re := ReportEntry{TopEntry: e} //nolint:exhaustruct

		if windowDays > 0 {
			re.RatePerDay = float64(RankKey(q.Kind, q.Class, e.Delta)) / windowDays
		}

		if rank, ok := prevRanks[ObjectRef{Schema: e.Schema, Object: e.Object}]; ok {
			re.PrevRank = &rank
		}

		entries = append(entries, re)
	}

	return Report{
		Snapshot:  snap,
		Dates:     dates,
		Histogram: snap.Histogram(q.Kind, q.Class),
		Coverage:  snap.CoverageFor(q.Kind, q.Class),
		Entries:   entries,
	}, nil
}

// prevRanks reads the ranks of the snapshot preceding snap. Trend arrows are
// not worth failing the report over, so errors only reach the log.
func (r *Reader) prevRanks(ctx context.Context, q ReportQuery, snap *Snapshot) map[ObjectRef]int {
	prev, err := r.store.GetHotSnapshotBefore(ctx, q.Cluster, q.Database, snap.CapturedAt)
	if err != nil {
		r.logger.Warn("hot report: previous snapshot unavailable, serving without trend",
			zap.String("cluster", q.Cluster), zap.String("database", q.Database), zap.Error(err))

		return nil
	}

	if prev == nil {
		return nil
	}

	ranks, err := r.store.GetHotRanks(ctx, prev.ID, prev.CapturedAt, q.Kind, q.Class)
	if err != nil {
		r.logger.Warn("hot report: previous ranks unavailable, serving without trend",
			zap.String("cluster", q.Cluster), zap.String("database", q.Database), zap.Error(err))

		return nil
	}

	return ranks
}

// Percentile measures an object's live per-day rate against its anchors and
// projects it onto the latest snapshot's tail histogram.
func (r *Reader) Percentile(
	ctx context.Context,
	cluster, database string,
	kind Kind,
	ref ObjectRef,
	class Class,
) (Percentile, error) {
	snap, err := r.store.GetHotSnapshot(ctx, cluster, database, nil)
	if err != nil {
		return Percentile{}, err //nolint:exhaustruct
	}

	if snap == nil {
		return Percentile{}, ErrNoSnapshot //nolint:exhaustruct
	}

	anchors, err := r.store.GetHotAnchorsForObject(ctx, cluster, database, kind, ref.Schema, ref.Object)
	if err != nil {
		return Percentile{}, fmt.Errorf("anchors: %w", err) //nolint:exhaustruct
	}

	if len(anchors) == 0 {
		return Percentile{}, ErrNoAnchors //nolint:exhaustruct
	}

	rate := r.liveRate(ctx, cluster, database, kind, ref, class, anchors)

	// The histogram holds raw deltas accumulated over the snapshot window,
	// which the schedule makes arbitrary (5 minutes on the demo lab, an hour
	// or a day elsewhere). Scale the live per-day rate back into window units
	// before projecting, or the comparison is off by the window/day ratio.
	value := rate
	if days := snap.WindowDays(); days > 0 {
		value = rate * days
	}

	ranks, err := r.store.GetHotRanks(ctx, snap.ID, snap.CapturedAt, kind, class)
	if err != nil {
		return Percentile{}, fmt.Errorf("ranks: %w", err) //nolint:exhaustruct
	}

	_, inTop := ranks[ref]

	return Percentile{
		Percentile: snap.Histogram(kind, class).Percentile(value),
		RatePerDay: rate,
		InTop:      inTop,
	}, nil
}

// liveRate sums per-host live deltas, each normalized by that host's own
// anchor age. Hosts whose delta is not measurable are skipped; when all are,
// the rate is 0 and the log says why.
func (r *Reader) liveRate(
	ctx context.Context,
	cluster, database string,
	kind Kind,
	ref ObjectRef,
	class Class,
	anchors []AnchorRow,
) float64 {
	now := r.now().UTC()
	rate := 0.0

	for _, a := range anchors {
		log := r.logger.With(
			zap.String("cluster", cluster), zap.String("instance", a.Instance), zap.String("database", database),
			zap.String("schema", ref.Schema), zap.String("object", ref.Object))

		rows, reset, err := r.sample(ctx, cluster, a.Instance, database, kind, ref)
		if err != nil {
			log.Warn("hot percentile: host unreachable, skipped", zap.Error(err))

			continue
		}

		if len(rows) == 0 {
			log.Debug("hot percentile: object not found on host, skipped")

			continue
		}

		if !sameReset(a.StatsReset, reset) {
			log.Debug("hot percentile: stats epoch changed since the anchor, skipped")

			continue
		}

		if a.PartSig != rows[0].PartSig {
			log.Debug("hot percentile: partition set changed since the anchor, skipped")

			continue
		}

		d, ok := Delta(a.Counters, rows[0].Counters)
		if !ok {
			log.Debug("hot percentile: counters went backwards since the anchor, skipped")

			continue
		}

		days := now.Sub(a.CapturedAt).Hours() / 24
		if days <= 0 {
			continue
		}

		rate += float64(RankKey(kind, class, d)) / days
	}

	return rate
}

func (r *Reader) sample(
	ctx context.Context,
	cluster, instance, database string,
	kind Kind,
	ref ObjectRef,
) ([]AnchorRow, *time.Time, error) {
	get := r.live.GetHotSampleTables
	if kind == KindIndex {
		get = r.live.GetHotSampleIndexes
	}

	rows, reset, _, err := get(ctx, cluster, instance, database, &ref.Schema, &ref.Object)

	return rows, reset, err
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

// History lists the stored-top appearances of an object over the last days;
// days <= 0 means the default month.
func (r *Reader) History(
	ctx context.Context,
	cluster, database string,
	kind Kind,
	ref ObjectRef,
	days int,
) ([]HistoryEntry, error) {
	if days <= 0 {
		days = defaultHistoryDays
	}

	to := r.now().UTC()
	from := to.AddDate(0, 0, -days)

	return r.store.GetHotObjectHistory(ctx, cluster, database, kind, ref.Schema, ref.Object, from, to)
}
