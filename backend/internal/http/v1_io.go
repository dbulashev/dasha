package http

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/repository"
	"github.com/dbulashev/dasha/internal/statio"
)

const (
	defaultIOPoints = 200
	maxIOPoints     = 1000

	// The matrix bodies of a window are read per bucket, but its capture
	// headers are read whole — hence a cap on how long a window may be.
	maxIOWindow = 31 * 24 * time.Hour
)

// GetIOCurrent serves one host's raw cumulative pg_stat_io slice. It needs no
// snapshot storage: this is what the live mode polls.
func (s *Handlers) GetIOCurrent(
	ctx context.Context,
	req serverhttp.GetIOCurrentRequestObject,
) (serverhttp.GetIOCurrentResponseObject, error) {
	snap, err := s.repo.GetIOSample(ctx, req.Params.ClusterName, req.Params.Instance)

	switch {
	case errors.Is(err, statio.ErrUnsupportedVersion):
		return serverhttp.GetIOCurrent501Response{}, nil
	case errors.Is(err, repository.ErrNotFound):
		return serverhttp.GetIOCurrent404Response{}, nil
	case err != nil:
		return nil, fmt.Errorf("GetIOCurrent | %w", err)
	}

	return serverhttp.GetIOCurrent200JSONResponse(ioSnapshotToAPI(req.Params.Instance, snap.Normalized())), nil
}

// GetIOHistory serves the delta series of one host over a period.
func (s *Handlers) GetIOHistory(
	ctx context.Context,
	req serverhttp.GetIOHistoryRequestObject,
) (serverhttp.GetIOHistoryResponseObject, error) {
	if s.storage == nil {
		return serverhttp.GetIOHistory501Response{}, nil
	}

	cluster, instance := req.Params.ClusterName, req.Params.Instance
	from, to := ioWindow(req.Params.From, req.Params.To)

	metas, err := s.storage.GetIOSnapshotMetas(ctx, cluster, instance, from, to)
	if err != nil {
		return nil, fmt.Errorf("GetIOHistory | %w", err)
	}

	earliest, latest, err := s.storage.IOSnapshotRange(ctx, cluster, instance)
	if err != nil {
		return nil, fmt.Errorf("GetIOHistory | %w", err)
	}

	plan := statio.PlanBuckets(metas, ioPoints(req.Params.Points))

	var snaps []statio.Snapshot

	if at := plan.At(); len(at) > 0 {
		if snaps, err = s.storage.GetIOSnapshotsAt(ctx, cluster, instance, at); err != nil {
			return nil, fmt.Errorf("GetIOHistory | %w", err)
		}
	}

	filter := statio.Filter{
		BackendType: deref(req.Params.BackendType),
		Object:      deref(req.Params.Object),
		Context:     deref(req.Params.Context),
	}

	out := serverhttp.IOHistory{
		Meta:   ioHistoryMeta(instance, earliest, latest, metas),
		Series: ioSeries(plan.Assemble(snaps), filter, ioGroupBy(string(deref(req.Params.GroupBy)))),
	}

	return serverhttp.GetIOHistory200JSONResponse(out), nil
}

// ioSeries reduces every bucket to the requested grouping and transposes the
// result into one series per key. Every series carries a point for every
// bucket — including the buckets where its key saw no activity — so the
// consumer can align series by index without re-deriving the time axis.
func ioSeries(buckets []statio.Bucket, filter statio.Filter, by statio.GroupBy) []serverhttp.IOSeries {
	perBucket := make([]map[statio.Key]map[string]int64, 0, len(buckets))
	keys := make([]statio.Key, 0)
	seen := map[statio.Key]struct{}{}

	for _, b := range buckets {
		rows := map[statio.Key]map[string]int64{}

		for _, r := range statio.Reduce(b.Rows, filter, by) {
			rows[r.Key] = r.Values

			if _, ok := seen[r.Key]; !ok {
				seen[r.Key] = struct{}{}

				keys = append(keys, r.Key)
			}
		}

		perBucket = append(perBucket, rows)
	}

	out := make([]serverhttp.IOSeries, 0, len(keys))

	for _, k := range keys {
		points := make([]serverhttp.IOPoint, 0, len(buckets))

		for i, b := range buckets {
			values := perBucket[i][k]
			if values == nil {
				values = map[string]int64{}
			}

			points = append(points, serverhttp.IOPoint{
				From:            b.From,
				To:              b.To,
				DurationSeconds: b.Duration.Seconds(),
				Complete:        !b.Partial,
				Values:          values,
			})
		}

		out = append(out, serverhttp.IOSeries{Key: ioSeriesKey(k), Points: points})
	}

	return out
}

func ioSeriesKey(k statio.Key) serverhttp.IOSeriesKey {
	key := serverhttp.IOSeriesKey{}

	if k.BackendType != "" {
		key.BackendType = &k.BackendType
	}

	if k.Object != "" {
		key.Object = &k.Object
	}

	if k.Context != "" {
		key.Context = &k.Context
	}

	return key
}

// ioHistoryMeta reports what the period itself says about its own readability:
// where stored history begins and ends, and whether the timing setting or the
// server version moved while it was being collected.
func ioHistoryMeta(instance string, earliest, latest *time.Time, metas []statio.Meta) serverhttp.IOHistoryMeta {
	meta := serverhttp.IOHistoryMeta{
		Instance:   instance,
		EarliestAt: earliest,
		LatestAt:   latest,
	}

	if len(metas) == 0 {
		return meta
	}

	meta.TrackIoTiming = metas[len(metas)-1].TrackIOTiming

	for _, m := range metas[1:] {
		if m.TrackIOTiming != metas[0].TrackIOTiming {
			meta.TrackIoTimingChanged = true
		}

		if m.VersionNum/10000 != metas[0].VersionNum/10000 {
			meta.VersionChanged = true
		}
	}

	setWALTiming(&meta, metas)

	return meta
}

// Captures older than the track_wal_io_timing column carry no value at all;
// they are skipped, so an unknown state never counts as a toggle.
func setWALTiming(meta *serverhttp.IOHistoryMeta, metas []statio.Meta) {
	var first *bool

	for _, m := range metas {
		if m.TrackWALIOTiming == nil {
			continue
		}

		switch {
		case first == nil:
			first = m.TrackWALIOTiming
		case *m.TrackWALIOTiming != *first:
			meta.TrackWalIoTimingChanged = true
		}

		meta.TrackWalIoTiming = *m.TrackWALIOTiming
	}
}

func ioSnapshotToAPI(instance string, snap statio.Snapshot) serverhttp.IOSnapshot {
	out := serverhttp.IOSnapshot{
		Instance:         instance,
		CapturedAt:       snap.CapturedAt,
		VersionNum:       snap.VersionNum,
		OpBytes:          snap.OpBytes,
		TrackIoTiming:    snap.TrackIOTiming,
		TrackWalIoTiming: snap.TrackWALIOTiming,
		StatsReset:       snap.StatsReset,
		Rows:             make([]serverhttp.IORow, 0, len(snap.Rows)),
	}

	for _, r := range snap.Rows {
		out.Rows = append(out.Rows, serverhttp.IORow{
			BackendType: r.BackendType,
			Object:      r.Object,
			Context:     r.Context,
			Values:      r.Values,
		})
	}

	return out
}

func ioGroupBy(raw string) statio.GroupBy {
	switch statio.GroupBy(raw) {
	case statio.GroupByBackendType:
		return statio.GroupByBackendType
	case statio.GroupByFull:
		return statio.GroupByFull
	case statio.GroupByContext:
		return statio.GroupByContext
	default:
		return statio.GroupByContext
	}
}

// ioWindow bounds the requested period. An inverted one collapses to empty
// rather than being read as "everything".
func ioWindow(from, to time.Time) (time.Time, time.Time) {
	if to.Before(from) {
		return to, to
	}

	if to.Sub(from) > maxIOWindow {
		from = to.Add(-maxIOWindow)
	}

	return from, to
}

func ioPoints(p *int) int {
	if p == nil || *p <= 0 {
		return defaultIOPoints
	}

	if *p > maxIOPoints {
		return maxIOPoints
	}

	return *p
}
