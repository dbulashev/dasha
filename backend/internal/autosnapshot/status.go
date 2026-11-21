package autosnapshot

import (
	"context"
	"fmt"
	"time"
)

type StatusStore interface {
	GetAutosnapshotConfig(ctx context.Context) (Config, error)
	GetLeaderInfo(ctx context.Context) (LeaderInfo, error)
	LastHotSnapshotAt(ctx context.Context) (map[string]time.Time, error)
	ListTriggerEvents(ctx context.Context, f TriggerEventFilter) ([]TriggerEvent, error)
	SummarizeTriggerEvents(ctx context.Context) ([]ClusterSummary, error)
}

// Status leaves out what could not be read: the status page must open even
// when part of the storage is unavailable.
type Status struct {
	Enabled bool
	Leader  *LeaderInfo

	// Newest scheduled hot-objects capture over all clusters. It is a separate
	// mechanism from the triggered pgss events, so it is reported on its own.
	LastHotSnapshotAt *time.Time
}

type StatusReader struct {
	store StatusStore
}

func NewStatusReader(store StatusStore) *StatusReader {
	return &StatusReader{store: store}
}

func (r *StatusReader) Status(ctx context.Context) Status {
	var st Status

	if cfg, err := r.store.GetAutosnapshotConfig(ctx); err == nil {
		st.Enabled = cfg.Enabled
	}

	if leader, err := r.store.GetLeaderInfo(ctx); err == nil {
		st.Leader = &leader
	}

	if last, err := r.store.LastHotSnapshotAt(ctx); err == nil {
		var newest time.Time

		for _, t := range last {
			if t.After(newest) {
				newest = t
			}
		}

		if !newest.IsZero() {
			st.LastHotSnapshotAt = &newest
		}
	}

	return st
}

func (r *StatusReader) TriggerEvents(ctx context.Context, f TriggerEventFilter) ([]TriggerEvent, error) {
	items, err := r.store.ListTriggerEvents(ctx, f)
	if err != nil {
		return nil, fmt.Errorf("trigger events: %w", err)
	}

	return items, nil
}

func (r *StatusReader) Summary(ctx context.Context) ([]ClusterSummary, error) {
	items, err := r.store.SummarizeTriggerEvents(ctx)
	if err != nil {
		return nil, fmt.Errorf("trigger summary: %w", err)
	}

	return items, nil
}
