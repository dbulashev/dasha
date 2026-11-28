package http

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/repository"
	"github.com/dbulashev/dasha/internal/statlock"
)

// GetLockStatsHistory serves the pg_stat_lock delta series of one host over a
// period, one series per lock type.
func (s *Handlers) GetLockStatsHistory(
	ctx context.Context,
	req serverhttp.GetLockStatsHistoryRequestObject,
) (serverhttp.GetLockStatsHistoryResponseObject, error) {
	if s.lockHistory == nil {
		return serverhttp.GetLockStatsHistory501JSONResponse{Reason: serverhttp.FeatureUnavailableNoStorage}, nil
	}

	cluster, instance := req.Params.ClusterName, req.Params.Instance

	// Any other error means the host is unreachable; its stored history is
	// still readable.
	vNum, err := s.repo.GetServerVersionNum(ctx, cluster, instance)

	switch {
	case errors.Is(err, repository.ErrNotFound):
		return serverhttp.GetLockStatsHistory404Response{}, nil
	case err == nil && vNum < statlock.MinVersionNum:
		return serverhttp.GetLockStatsHistory501JSONResponse{Reason: serverhttp.FeatureUnavailableUnsupportedVersion}, nil
	}

	from, to := historyWindow(req.Params.From, req.Params.To)

	hist, err := s.lockHistory.Read(ctx, cluster, instance, from, to, historyPoints(req.Params.Points))
	if err != nil {
		return nil, fmt.Errorf("GetLockStatsHistory | %w", err)
	}

	return serverhttp.GetLockStatsHistory200JSONResponse{
		Meta:   lockStatsMeta(instance, hist.Earliest, hist.Latest, hist.Metas),
		Series: lockStatsSeries(hist.Buckets),
	}, nil
}

// lockStatsSeries transposes the buckets into one series per lock type. Every
// series carries a point for every bucket, so series align by index.
func lockStatsSeries(buckets []statlock.Bucket) []serverhttp.LockStatsSeries {
	perBucket := make([]map[string]statlock.Row, 0, len(buckets))
	types := make([]string, 0)
	seen := map[string]struct{}{}

	for _, b := range buckets {
		rows := make(map[string]statlock.Row, len(b.Rows))

		for _, r := range b.Rows {
			rows[r.LockType] = r

			if _, ok := seen[r.LockType]; !ok {
				seen[r.LockType] = struct{}{}

				types = append(types, r.LockType)
			}
		}

		perBucket = append(perBucket, rows)
	}

	out := make([]serverhttp.LockStatsSeries, 0, len(types))

	for _, lt := range types {
		points := make([]serverhttp.LockStatsPoint, 0, len(buckets))

		for i, b := range buckets {
			r := perBucket[i][lt]

			points = append(points, serverhttp.LockStatsPoint{
				From:             b.From,
				To:               b.To,
				DurationSeconds:  b.Duration.Seconds(),
				Complete:         !b.Partial,
				Waits:            r.Waits,
				WaitTimeMs:       r.WaitTimeMs,
				FastpathExceeded: r.FastpathExceeded,
			})
		}

		out = append(out, serverhttp.LockStatsSeries{Locktype: lt, Points: points})
	}

	return out
}

// lockStatsMeta reports the settings the hints have to name: their values at
// the newest capture and whether they moved inside the period.
func lockStatsMeta(instance string, earliest, latest *time.Time, metas []statlock.Meta) serverhttp.LockStatsHistoryMeta {
	meta := serverhttp.LockStatsHistoryMeta{
		Instance:   instance,
		EarliestAt: earliest,
		LatestAt:   latest,
	}

	if len(metas) == 0 {
		return meta
	}

	first, last := metas[0], metas[len(metas)-1]
	meta.DeadlockTimeoutMs = &last.DeadlockTimeoutMs
	meta.MaxLocksPerTransaction = &last.MaxLocksPerTransaction

	for _, m := range metas[1:] {
		if m.VersionNum/10000 != first.VersionNum/10000 {
			meta.VersionChanged = true
		}

		if m.DeadlockTimeoutMs != first.DeadlockTimeoutMs {
			meta.DeadlockTimeoutChanged = true
		}

		if m.MaxLocksPerTransaction != first.MaxLocksPerTransaction {
			meta.MaxLocksPerTransactionChanged = true
		}
	}

	return meta
}
