package statio

import (
	"context"
	"fmt"
	"time"
)

type HistoryStore interface {
	// GetIOSnapshotMetas includes the last capture before from as the baseline
	// of the first interval.
	GetIOSnapshotMetas(ctx context.Context, cluster, instance string, from, to time.Time) ([]Meta, error)
	GetIOSnapshotsAt(ctx context.Context, cluster, instance string, at []time.Time) ([]Snapshot, error)
	// IOSnapshotRange returns nils when the host has never been captured.
	IOSnapshotRange(ctx context.Context, cluster, instance string) (earliest, latest *time.Time, err error)
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
	metas, err := r.store.GetIOSnapshotMetas(ctx, cluster, instance, from, to)
	if err != nil {
		return History{}, fmt.Errorf("io snapshot metas: %w", err)
	}

	earliest, latest, err := r.store.IOSnapshotRange(ctx, cluster, instance)
	if err != nil {
		return History{}, fmt.Errorf("io snapshot range: %w", err)
	}

	plan := PlanBuckets(metas, points)

	var snaps []Snapshot

	if at := plan.At(); len(at) > 0 {
		if snaps, err = r.store.GetIOSnapshotsAt(ctx, cluster, instance, at); err != nil {
			return History{}, fmt.Errorf("io snapshots: %w", err)
		}
	}

	return History{
		Metas:    metas,
		Earliest: earliest,
		Latest:   latest,
		Buckets:  plan.Assemble(snaps),
	}, nil
}
