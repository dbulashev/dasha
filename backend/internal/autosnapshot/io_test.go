package autosnapshot

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/statio"
)

// The embedded interfaces stay nil on purpose: a call outside the tested path
// panics instead of silently passing.
type ioFakeRepo struct {
	Repo
	snap  *statio.Snapshot
	err   error
	calls int
}

func (f *ioFakeRepo) GetIOSample(context.Context, string, string) (*statio.Snapshot, error) {
	f.calls++

	return f.snap, f.err
}

type ioFakeStore struct {
	Store
	inserted []statio.Snapshot
}

func (f *ioFakeStore) LastIOSnapshotAt(context.Context) (map[string]time.Time, error) {
	return map[string]time.Time{}, nil
}

func (f *ioFakeStore) InsertIOSnapshot(_ context.Context, _, _ string, snap statio.Snapshot) (uuid.UUID, error) {
	f.inserted = append(f.inserted, snap)

	return uuid.New(), nil
}

type ioFakeClusters struct{ cls []config.Cluster }

func (f *ioFakeClusters) Get(context.Context) ([]config.Cluster, error) { return f.cls, nil }

func (f *ioFakeClusters) UpdateSource(string, []config.Cluster) []string { return nil }

func newIOTestDaemon(repo Repo, store Store, logger *zap.Logger) *Daemon {
	return &Daemon{ //nolint:exhaustruct
		repo:           repo,
		store:          store,
		logger:         logger,
		hosts:          map[hostKey]*hostState{},
		lastHotAttempt: map[string]time.Time{},
		lastIOAttempt:  map[string]time.Time{},
	}
}

func TestTakeIOSnapshotStores(t *testing.T) {
	t.Parallel()

	snap := &statio.Snapshot{
		VersionNum: 170004,
		Rows: []statio.Row{{
			Key:    statio.Key{BackendType: "client backend", Object: "relation", Context: "normal"},
			Values: map[string]int64{"reads": 7},
		}},
	}

	store := &ioFakeStore{}
	d := newIOTestDaemon(&ioFakeRepo{snap: snap}, store, zap.NewNop())

	d.takeIOSnapshot(context.Background(), "c1", "h1")

	if len(store.inserted) != 1 {
		t.Fatalf("stored %d snapshots, want 1", len(store.inserted))
	}

	if got := store.inserted[0].Rows[0].Values["reads"]; got != 7 {
		t.Errorf("stored reads = %d, want 7", got)
	}
}

// A host below PG 16 is a deployment fact, not a failure: it must not warn on
// every tick forever.
func TestTakeIOSnapshotSkipsUnsupportedVersionQuietly(t *testing.T) {
	t.Parallel()

	core, logs := observer.New(zapcore.WarnLevel)
	store := &ioFakeStore{}

	d := newIOTestDaemon(&ioFakeRepo{err: statio.ErrUnsupportedVersion}, store, zap.New(core))
	d.takeIOSnapshot(context.Background(), "c1", "h1")

	if logs.Len() != 0 {
		t.Errorf("unsupported host logged %d warnings, want none", logs.Len())
	}

	if len(store.inserted) != 0 {
		t.Errorf("stored %d snapshots for a host without pg_stat_io", len(store.inserted))
	}
}

func TestTakeIOSnapshotWarnsOnSampleFailure(t *testing.T) {
	t.Parallel()

	core, logs := observer.New(zapcore.WarnLevel)
	store := &ioFakeStore{}

	d := newIOTestDaemon(&ioFakeRepo{err: errors.New("connection refused")}, store, zap.New(core))
	d.takeIOSnapshot(context.Background(), "c1", "h1")

	if logs.FilterMessageSnippet("sample failed").Len() != 1 {
		t.Errorf("an unreachable host must be reported once, got %d entries", logs.Len())
	}

	if len(store.inserted) != 0 {
		t.Errorf("stored %d snapshots after a failed sample", len(store.inserted))
	}
}

func TestProcessIOSnapshotsHoldsScheduleWithoutStoredSnapshot(t *testing.T) {
	t.Parallel()

	repo := &ioFakeRepo{err: errors.New("host unreachable")}
	d := newIOTestDaemon(repo, &ioFakeStore{}, zap.NewNop())
	d.clusters = &ioFakeClusters{cls: []config.Cluster{oneHostCluster()}}

	cfg := Config{IOEnabled: true, IOSchedule: "*/5 * * * *"} //nolint:exhaustruct

	d.processIOSnapshots(context.Background(), cfg)
	d.processIOSnapshots(context.Background(), cfg)

	if repo.calls != 1 {
		t.Fatalf("expected one sample attempt per schedule tick, got %d", repo.calls)
	}
}
