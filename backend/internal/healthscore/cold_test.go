package healthscore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/health"
	"github.com/dbulashev/dasha/internal/hotobjects"
	"github.com/dbulashev/dasha/internal/metrics"
)

type fakeColdStore struct {
	calls int
	err   error
	since time.Time
}

func (f *fakeColdStore) GetColdTables(_ context.Context, _, _ string, since time.Time) (hotobjects.ColdSets, error) {
	f.calls++
	f.since = since

	if f.err != nil {
		return hotobjects.ColdSets{}, f.err
	}

	return hotobjects.ColdSets{WindowDays: 7}, nil //nolint:exhaustruct
}

func TestColdCache(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	store := &fakeColdStore{} //nolint:exhaustruct
	c := newColdCache(store)
	c.now = func() time.Time { return now }
	target := metrics.TargetRef{Cluster: "c1", Instance: "h1"}

	assert.Equal(t, 7, c.get(t.Context(), target).WindowDays)
	assert.Equal(t, now.Add(-hotobjects.ColdMaxAge), store.since)

	c.get(t.Context(), target)
	assert.Equal(t, 1, store.calls, "served from cache within the TTL")

	now = now.Add(coldCacheTTL)
	c.get(t.Context(), target)
	assert.Equal(t, 2, store.calls, "expired entry is re-read")

	store.err = errors.New("boom")
	other := metrics.TargetRef{Cluster: "c1", Instance: "h2"}
	assert.Equal(t, hotobjects.ColdError, c.get(t.Context(), other).Status)
	c.get(t.Context(), other)
	assert.Equal(t, 4, store.calls, "errors are not cached")
}

func TestColdCacheNoStorage(t *testing.T) {
	t.Parallel()

	var s Scorer

	assert.Equal(t, hotobjects.ColdNoStorage, s.ColdTables(t.Context(), metrics.TargetRef{}).Status) //nolint:exhaustruct
}

func TestBuildColdFacts(t *testing.T) {
	t.Parallel()

	captured := time.Date(2026, 9, 27, 3, 0, 0, 0, time.UTC)
	set := hotobjects.ColdSet{ //nolint:exhaustruct
		CapturedAt: captured,
		Tables: []hotobjects.ColdTable{
			{Schema: "public", Table: "a", QuietSince: captured.Add(-41*24*time.Hour - time.Hour)},
			{Schema: "archive", Table: "b", QuietSince: captured.Add(-8 * 24 * time.Hour)},
		},
	}

	rows := []dto.HealthScoreColdTable{
		{Idx: 2, DeadRatio: 14.2, RelfrozenxidAge: 171_000_000, SizeBytes: 1 << 30},
		{Idx: 1, NeverVacuumed: true},
		{Idx: 9},
	}

	got := buildColdFacts(set, 7, rows, 15)

	assert.Equal(t, 7, got.WindowDays)
	assert.Equal(t, []health.ColdMaintenanceTable{
		{Schema: "archive", Table: "b", SizeBytes: 1 << 30, DeadRatio: 14.2, RelfrozenxidAge: 171_000_000, NoWritesDays: 8},
		{Schema: "public", Table: "a", NeverVacuumed: true, NoWritesDays: 41},
	}, got.Tables, "order kept, unknown idx dropped")
	assert.Equal(t, 13, got.More)
}
