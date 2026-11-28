package autosnapshot

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/statio"
	"github.com/dbulashev/dasha/internal/statlock"
)

// The embedded interfaces stay nil on purpose: a call outside the tested path
// panics instead of silently passing.
type ioFakeRepo struct {
	Repo
	snap  *statio.Snapshot
	err   error
	calls int

	lockSnap  *statlock.Snapshot
	lockErr   error
	lockCalls int
}

func (f *ioFakeRepo) GetIOSample(context.Context, string, string) (*statio.Snapshot, error) {
	f.calls++

	return f.snap, f.err
}

func (f *ioFakeRepo) GetLockSample(context.Context, string, string) (*statlock.Snapshot, error) {
	f.lockCalls++

	if f.lockSnap == nil && f.lockErr == nil {
		return nil, statlock.ErrUnsupportedVersion
	}

	return f.lockSnap, f.lockErr
}

type ioFakeStore struct {
	Store
	last         map[string]time.Time
	inserted     []statio.Snapshot
	lockInserted []statlock.Snapshot
}

func (f *ioFakeStore) InsertLockSnapshot(_ context.Context, _, _ string, snap statlock.Snapshot) error {
	f.lockInserted = append(f.lockInserted, snap)

	return nil
}

func (f *ioFakeStore) LastIOSnapshotAt(context.Context) (map[string]time.Time, error) {
	last := map[string]time.Time{}
	for k, v := range f.last {
		last[k] = v
	}

	return last, nil
}

func (f *ioFakeStore) InsertIOSnapshot(_ context.Context, _, _ string, snap statio.Snapshot) error {
	f.inserted = append(f.inserted, snap)

	return nil
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
			BackendType: "client backend", Object: "relation", Context: "normal",
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

// Both sweeps of a test run at this instant, so they cannot straddle a */5 tick.
var midScheduleTick = time.Date(2026, 7, 19, 3, 2, 30, 0, time.UTC)

func TestProcessIOSnapshotsHoldsScheduleWithoutStoredSnapshot(t *testing.T) {
	t.Parallel()

	repo := &ioFakeRepo{err: errors.New("host unreachable")}
	d := newIOTestDaemon(repo, &ioFakeStore{}, zap.NewNop())
	d.clusters = &ioFakeClusters{cls: []config.Cluster{oneHostCluster()}}
	d.clock = func() time.Time { return midScheduleTick }

	cfg := Config{IOEnabled: true, IOSchedule: "*/5 * * * *"} //nolint:exhaustruct

	d.processIOSnapshots(context.Background(), cfg)
	d.processIOSnapshots(context.Background(), cfg)

	if repo.calls != 1 {
		t.Fatalf("expected one sample attempt per schedule tick, got %d", repo.calls)
	}
}

// The stored pg_stat_io time stays stale while its sample keeps failing; the
// lock capture riding on it must still wait for the next schedule tick.
func TestProcessIOSnapshotsHoldsScheduleWithStaleStoredSnapshot(t *testing.T) {
	t.Parallel()

	repo := &ioFakeRepo{
		err:      errors.New("pg_stat_io failed"),
		lockSnap: &statlock.Snapshot{VersionNum: 190001},
	}
	store := &ioFakeStore{last: map[string]time.Time{"c1/h1": midScheduleTick.Add(-time.Hour)}}
	d := newIOTestDaemon(repo, store, zap.NewNop())
	d.clusters = &ioFakeClusters{cls: []config.Cluster{oneHostCluster()}}
	d.clock = func() time.Time { return midScheduleTick }

	cfg := Config{IOEnabled: true, IOSchedule: "*/5 * * * *"} //nolint:exhaustruct

	d.processIOSnapshots(context.Background(), cfg)
	d.processIOSnapshots(context.Background(), cfg)

	if repo.calls != 1 || repo.lockCalls != 1 || len(store.lockInserted) != 1 {
		t.Fatalf("io calls = %d, lock calls = %d, lock stored = %d; want 1, 1, 1",
			repo.calls, repo.lockCalls, len(store.lockInserted))
	}
}

func TestTakeLockSnapshotStores(t *testing.T) {
	t.Parallel()

	snap := &statlock.Snapshot{
		VersionNum: 190001,
		Rows:       []statlock.Row{{LockType: "relation", Waits: 3}},
	}

	store := &ioFakeStore{}
	d := newIOTestDaemon(&ioFakeRepo{lockSnap: snap}, store, zap.NewNop())

	d.takeLockSnapshot(context.Background(), "c1", "h1")

	if len(store.lockInserted) != 1 || store.lockInserted[0].Rows[0].Waits != 3 {
		t.Fatalf("stored %+v, want the one sampled snapshot", store.lockInserted)
	}
}

func TestTakeLockSnapshotSkipsUnsupportedVersionQuietly(t *testing.T) {
	t.Parallel()

	core, logs := observer.New(zapcore.WarnLevel)
	store := &ioFakeStore{}

	d := newIOTestDaemon(&ioFakeRepo{lockErr: statlock.ErrUnsupportedVersion}, store, zap.New(core))
	d.takeLockSnapshot(context.Background(), "c1", "h1")

	if logs.Len() != 0 {
		t.Errorf("host without pg_stat_lock logged %d warnings, want none", logs.Len())
	}

	if len(store.lockInserted) != 0 {
		t.Errorf("stored %d snapshots for a host without pg_stat_lock", len(store.lockInserted))
	}
}

// The two captures of a tick are independent: a failed pg_stat_io sample must
// not cost the pg_stat_lock one.
func TestProcessIOSnapshotsTakesLockSnapshotWhenIOFails(t *testing.T) {
	t.Parallel()

	repo := &ioFakeRepo{
		err:      errors.New("pg_stat_io failed"),
		lockSnap: &statlock.Snapshot{VersionNum: 190001},
	}
	store := &ioFakeStore{}
	d := newIOTestDaemon(repo, store, zap.NewNop())
	d.clusters = &ioFakeClusters{cls: []config.Cluster{oneHostCluster()}}

	d.processIOSnapshots(context.Background(), Config{IOEnabled: true, IOSchedule: "*/5 * * * *"}) //nolint:exhaustruct

	if repo.lockCalls != 1 || len(store.lockInserted) != 1 {
		t.Fatalf("lock sample calls = %d, stored = %d; want 1 and 1", repo.lockCalls, len(store.lockInserted))
	}
}

// A host that spent the whole budget on the first capture would only burn a
// second one.
func TestProcessIOSnapshotsSkipsLockSnapshotOfAnUnreachableHost(t *testing.T) {
	t.Parallel()

	repo := &ioFakeRepo{
		err:      fmt.Errorf("get server version | %w", context.DeadlineExceeded),
		lockSnap: &statlock.Snapshot{VersionNum: 190001},
	}
	d := newIOTestDaemon(repo, &ioFakeStore{}, zap.NewNop())
	d.clusters = &ioFakeClusters{cls: []config.Cluster{oneHostCluster()}}

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	d.processIOSnapshots(ctx, Config{IOEnabled: true, IOSchedule: "*/5 * * * *"}) //nolint:exhaustruct

	if repo.lockCalls != 0 {
		t.Fatalf("lock sample calls = %d on an unreachable host, want 0", repo.lockCalls)
	}
}

// A child deadline (a busy pool, a slow query) leaves the host budget intact,
// so the lock capture still gets its own.
func TestProcessIOSnapshotsTakesLockSnapshotAfterChildDeadline(t *testing.T) {
	t.Parallel()

	repo := &ioFakeRepo{
		err:      fmt.Errorf("GetIOSample | %w", context.DeadlineExceeded),
		lockSnap: &statlock.Snapshot{VersionNum: 190001},
	}
	store := &ioFakeStore{}
	d := newIOTestDaemon(repo, store, zap.NewNop())
	d.clusters = &ioFakeClusters{cls: []config.Cluster{oneHostCluster()}}

	d.processIOSnapshots(context.Background(), Config{IOEnabled: true, IOSchedule: "*/5 * * * *"}) //nolint:exhaustruct

	if repo.lockCalls != 1 || len(store.lockInserted) != 1 {
		t.Fatalf("lock sample calls = %d, stored = %d; want 1 and 1", repo.lockCalls, len(store.lockInserted))
	}
}

func TestProcessIOSnapshotsSkipsLockSnapshotWhenDisabled(t *testing.T) {
	t.Parallel()

	repo := &ioFakeRepo{lockSnap: &statlock.Snapshot{VersionNum: 190001}}
	d := newIOTestDaemon(repo, &ioFakeStore{}, zap.NewNop())
	d.clusters = &ioFakeClusters{cls: []config.Cluster{oneHostCluster()}}

	d.processIOSnapshots(context.Background(), Config{IOEnabled: false, IOSchedule: "*/5 * * * *"}) //nolint:exhaustruct

	if repo.lockCalls != 0 {
		t.Fatalf("lock sample calls = %d with io snapshots disabled", repo.lockCalls)
	}
}
