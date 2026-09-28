package autosnapshot

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/hotobjects"
)

type hotFakeRepo struct {
	fakeRepo
	tables []hotobjects.AnchorRow
	reset  *time.Time
}

func (f *hotFakeRepo) GetHotSampleTables(context.Context, string, string, string, *string, *string) ([]hotobjects.AnchorRow, *time.Time, bool, error) {
	return f.tables, f.reset, false, nil
}

func (f *hotFakeRepo) GetHotSampleIndexes(context.Context, string, string, string, *string, *string) ([]hotobjects.AnchorRow, *time.Time, bool, error) {
	return nil, f.reset, false, nil
}

type hotFakeStore struct {
	Store
	anchors    map[string]hotobjects.AnchorRow
	windows    map[string]hotobjects.HostWindow
	windowsErr error
	snaps      []hotobjects.Snapshot
	stored     map[string][]hotobjects.AnchorRow
}

func (f *hotFakeStore) GetHotAnchors(context.Context, string, string, string) (map[string]hotobjects.AnchorRow, error) {
	return f.anchors, nil
}

func (f *hotFakeStore) GetLatestHotWindows(context.Context, string, string, []string, time.Time) (map[string]hotobjects.HostWindow, error) {
	return f.windows, f.windowsErr
}

func (f *hotFakeStore) InsertHotSnapshotWithAnchors(
	_ context.Context, snap hotobjects.Snapshot, anchors map[string][]hotobjects.AnchorRow,
) (uuid.UUID, error) {
	f.snaps = append(f.snaps, snap)
	f.stored = anchors

	return uuid.New(), nil
}

func TestTakeHotSnapshotCarriesQuietAndObserved(t *testing.T) {
	t.Parallel()

	reset := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	quiet := time.Date(2026, 9, 10, 3, 0, 0, 0, time.UTC)
	observed := time.Date(2026, 9, 5, 3, 0, 0, 0, time.UTC)

	idle := hotobjects.AnchorRow{ //nolint:exhaustruct
		Kind: hotobjects.KindTable, Schema: "public", Object: "archive",
		Counters: hotobjects.Counters{"n_tup_ins": 5},
	}

	anchor := idle
	anchor.CapturedAt = quiet
	anchor.StatsReset = &reset
	anchor.QuietSince = &quiet

	repo := &hotFakeRepo{tables: []hotobjects.AnchorRow{idle}, reset: &reset} //nolint:exhaustruct
	store := &hotFakeStore{                                                   //nolint:exhaustruct
		anchors: map[string]hotobjects.AnchorRow{hotobjects.Key(idle.Kind, idle.Schema, idle.Object): anchor},
		windows: map[string]hotobjects.HostWindow{"h1": {ObservedSince: &observed}}, //nolint:exhaustruct
	}

	d := newIOTestDaemon(repo, store, zap.NewNop())
	cfg := Config{HotTopN: 10, HotRetentionDays: 180}            //nolint:exhaustruct
	cl := config.Cluster{Name: "c1", Hosts: []config.Host{"h1"}} //nolint:exhaustruct

	d.takeHotSnapshot(t.Context(), cfg, cl, "db")

	require.Len(t, store.snaps, 1)
	w := store.snaps[0].Windows["h1"]
	require.NotNil(t, w.ObservedSince)
	assert.Equal(t, observed, *w.ObservedSince)

	require.Len(t, store.stored["h1"], 1)
	require.NotNil(t, store.stored["h1"][0].QuietSince)
	assert.Equal(t, quiet, *store.stored["h1"][0].QuietSince)
}

func TestTakeHotSnapshotSkipsOnWindowsError(t *testing.T) {
	t.Parallel()

	repo := &hotFakeRepo{}                                 //nolint:exhaustruct
	store := &hotFakeStore{windowsErr: errors.New("boom")} //nolint:exhaustruct

	d := newIOTestDaemon(repo, store, zap.NewNop())
	cl := config.Cluster{Name: "c1", Hosts: []config.Host{"h1"}} //nolint:exhaustruct

	d.takeHotSnapshot(t.Context(), Config{HotTopN: 10, HotRetentionDays: 180}, cl, "db") //nolint:exhaustruct

	assert.Empty(t, store.snaps)
}
