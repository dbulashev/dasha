package statlock

import (
	"context"
	"fmt"
	"time"
)

type HistoryStore interface {
	// GetLockSnapshotMetas includes the last capture before from as the
	// baseline of the first interval.
	GetLockSnapshotMetas(ctx context.Context, cluster, instance string, from, to time.Time) ([]Meta, error)
	GetLockSnapshotsAt(ctx context.Context, cluster, instance string, at []time.Time) ([]Snapshot, error)
	// LockSnapshotRange returns nils when the host has never been captured.
	LockSnapshotRange(ctx context.Context, cluster, instance string) (earliest, latest *time.Time, err error)
}

type History struct {
	Metas            []Meta
	Earliest, Latest *time.Time
	Buckets          []Bucket
}

type HistoryReader struct {
	store HistoryStore
}

func NewHistoryReader(store HistoryStore) *HistoryReader {
	return &HistoryReader{store: store}
}

// Read lays [from, to] out into at most points buckets, loading only the
// captures the bucket plan names.
func (r *HistoryReader) Read(ctx context.Context, cluster, instance string, from, to time.Time, points int) (History, error) {
	metas, err := r.store.GetLockSnapshotMetas(ctx, cluster, instance, from, to)
	if err != nil {
		return History{}, fmt.Errorf("lock snapshot metas: %w", err)
	}

	earliest, latest, err := r.store.LockSnapshotRange(ctx, cluster, instance)
	if err != nil {
		return History{}, fmt.Errorf("lock snapshot range: %w", err)
	}

	plan := PlanBuckets(metas, points)

	var snaps []Snapshot

	if at := plan.At(); len(at) > 0 {
		if snaps, err = r.store.GetLockSnapshotsAt(ctx, cluster, instance, at); err != nil {
			return History{}, fmt.Errorf("lock snapshots: %w", err)
		}
	}

	return History{
		Metas:    metas,
		Earliest: earliest,
		Latest:   latest,
		Buckets:  Assemble(plan, snaps),
	}, nil
}
