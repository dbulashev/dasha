package autosnapshot

import (
	"context"
	"errors"
	"testing"
	"time"
)

type statusStore struct {
	err  error
	last map[string]time.Time
}

func (s statusStore) GetAutosnapshotConfig(context.Context) (Config, error) {
	return Config{Enabled: true}, s.err
}

func (s statusStore) GetLeaderInfo(context.Context) (LeaderInfo, error) {
	return LeaderInfo{IsAlive: true}, s.err
}

func (s statusStore) LastHotSnapshotAt(context.Context) (map[string]time.Time, error) {
	return s.last, s.err
}

func (statusStore) ListTriggerEvents(context.Context, TriggerEventFilter) ([]TriggerEvent, error) {
	return nil, nil
}

func (statusStore) SummarizeTriggerEvents(context.Context) ([]ClusterSummary, error) {
	return nil, nil
}

func TestStatusTakesNewestHotSnapshot(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	st := NewStatusReader(statusStore{last: map[string]time.Time{
		"a.db": t0,
		"b.db": t0.Add(time.Hour),
	}}).Status(t.Context())

	if !st.Enabled || st.Leader == nil || !st.Leader.IsAlive {
		t.Fatalf("status = %+v", st)
	}

	if st.LastHotSnapshotAt == nil || !st.LastHotSnapshotAt.Equal(t0.Add(time.Hour)) {
		t.Fatalf("last hot snapshot = %v, want %v", st.LastHotSnapshotAt, t0.Add(time.Hour))
	}
}

func TestStatusOmitsWhatCannotBeRead(t *testing.T) {
	t.Parallel()

	st := NewStatusReader(statusStore{err: errors.New("down")}).Status(t.Context())

	if st.Enabled || st.Leader != nil || st.LastHotSnapshotAt != nil {
		t.Fatalf("status on failing storage = %+v, want zero", st)
	}
}
