package statio

import (
	"context"
	"testing"
	"time"
)

type fakeHistory struct {
	snaps  []Snapshot
	loaded []time.Time
}

func (f *fakeHistory) GetIOSnapshotMetas(context.Context, string, string, time.Time, time.Time) ([]Meta, error) {
	return metasOf(f.snaps), nil
}

func (f *fakeHistory) GetIOSnapshotsAt(_ context.Context, _, _ string, at []time.Time) ([]Snapshot, error) {
	f.loaded = at

	return loaded(f.snaps, at), nil
}

func (f *fakeHistory) IOSnapshotRange(context.Context, string, string) (*time.Time, *time.Time, error) {
	if len(f.snaps) == 0 {
		return nil, nil, nil
	}

	return &f.snaps[0].CapturedAt, &f.snaps[len(f.snaps)-1].CapturedAt, nil
}

func TestHistoryReaderLoadsOnlyPlannedCaptures(t *testing.T) {
	st := &fakeHistory{snaps: ioRun(11, 1, new(base.Add(-time.Hour)))}

	h, err := NewHistoryReader(st).Read(t.Context(), "c", "h", base, base.Add(time.Hour), 5)
	if err != nil {
		t.Fatal(err)
	}

	if len(h.Buckets) != 5 || len(st.loaded) != 6 {
		t.Fatalf("buckets = %d, loaded = %d; want 5 and 6", len(h.Buckets), len(st.loaded))
	}

	if len(h.Metas) != 11 || h.Earliest == nil || h.Latest == nil {
		t.Fatalf("metas = %d, range %v..%v", len(h.Metas), h.Earliest, h.Latest)
	}
}

func TestHistoryReaderWithoutCaptures(t *testing.T) {
	st := &fakeHistory{}

	h, err := NewHistoryReader(st).Read(t.Context(), "c", "h", base, base.Add(time.Hour), 5)
	if err != nil {
		t.Fatal(err)
	}

	if st.loaded != nil || len(h.Buckets) != 0 || h.Earliest != nil {
		t.Fatalf("history of a never captured host = %+v, loaded %v", h, st.loaded)
	}
}
