package autosnapshot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/dbulashev/dasha/internal/dto"
)

type captureRepo struct {
	reports     []dto.QueryReport
	provenance  string
	pgssDBErr   error
	sourceErr   error
	blockedDBs  []string
	blockedRows []dto.QueryBlocked
}

func (f *captureRepo) GetQueriesReport(context.Context, string, string, string, []string, *int64) ([]dto.QueryReport, error) {
	return f.reports, nil
}

func (f *captureRepo) PgssDatabase(context.Context, string, string, string) (string, error) {
	return f.provenance, f.pgssDBErr
}

func (f *captureRepo) GetPgssStatsResetTime(context.Context, string, string, string) (*dto.StatsResetTime, error) {
	return nil, nil
}

func (f *captureRepo) PgssSource(context.Context, string, string, string) (string, error) {
	if f.sourceErr != nil {
		return "", f.sourceErr
	}

	return "pg_stat_statements", nil
}

func (f *captureRepo) GetQueriesBlocked(_ context.Context, _, _, database, _ string) ([]dto.QueryBlocked, error) {
	f.blockedDBs = append(f.blockedDBs, database)

	return f.blockedRows, nil
}

type captureStore struct {
	err      error
	database string
	opts     SnapshotOpts
	calls    int
	cfg      Config
}

func (f *captureStore) CreateSnapshot(
	_ context.Context, _, _, database string, _ []dto.QueryReport, opts SnapshotOpts,
) (uuid.UUID, time.Time, error) {
	f.calls++
	f.database, f.opts = database, opts

	if f.err != nil {
		return uuid.Nil, time.Time{}, f.err
	}

	return uuid.New(), time.Now(), nil
}

func (f *captureStore) GetAutosnapshotConfig(context.Context) (Config, error) {
	return f.cfg, nil
}

func oneRowRepo() *captureRepo {
	return &captureRepo{ //nolint:exhaustruct
		reports:    []dto.QueryReport{{QueryID: 1, Datname: "app"}}, //nolint:exhaustruct
		provenance: "postgres",
	}
}

func TestCaptureProbesLocksThroughProvenance(t *testing.T) {
	t.Parallel()

	repo := oneRowRepo()
	repo.blockedRows = blockedRows(7)
	store := &captureStore{} //nolint:exhaustruct
	peak := &BackgroundPeak{BlockedCount: 3, At: time.Now()}

	c, err := NewCapturer(repo, store, zap.NewNop()).Capture(t.Context(), CaptureRequest{
		Cluster:        "c1",
		Instance:       "h1",
		Database:       "app",
		Reason:         "auto:activity_spike",
		TriggerContext: map[string]any{"trigger": "activity_spike"},
		Locks:          &LockProbe{Count: 1, Interval: time.Millisecond, BackgroundPeak: peak},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if c.Database != "postgres" || store.database != "postgres" {
		t.Fatalf("snapshot database = %q / %q, want provenance %q", c.Database, store.database, "postgres")
	}

	if len(repo.blockedDBs) != 1 || repo.blockedDBs[0] != "postgres" {
		t.Fatalf("lock probes went through %v, want [postgres]", repo.blockedDBs)
	}

	locks := store.opts.LocksData
	if locks == nil || !locks.Captured || locks.BlockedCount != 1 || locks.BackgroundPeak != peak {
		t.Fatalf("locks = %+v, want one captured blocked session with the background peak", locks)
	}

	if store.opts.Reason != "auto:activity_spike" || store.opts.TriggerContext["trigger"] != "activity_spike" {
		t.Fatalf("opts = %+v, want reason and trigger context passed through", store.opts)
	}
}

func TestCaptureWithoutLocks(t *testing.T) {
	t.Parallel()

	repo := oneRowRepo()
	store := &captureStore{} //nolint:exhaustruct

	_, err := NewCapturer(repo, store, zap.NewNop()).Capture(t.Context(), CaptureRequest{ //nolint:exhaustruct
		Cluster: "c1", Instance: "h1", Database: "app",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.blockedDBs) != 0 || store.opts.LocksData != nil {
		t.Fatalf("lock probes ran without being asked for: %v", repo.blockedDBs)
	}
}

func TestCaptureEmptyReport(t *testing.T) {
	t.Parallel()

	repo := oneRowRepo()
	repo.reports = nil
	store := &captureStore{} //nolint:exhaustruct

	_, err := NewCapturer(repo, store, zap.NewNop()).Capture(t.Context(), CaptureRequest{ //nolint:exhaustruct
		Cluster: "c1", Instance: "h1", Database: "app",
	})
	if !errors.Is(err, ErrEmptyReport) {
		t.Fatalf("err = %v, want ErrEmptyReport", err)
	}

	if store.calls != 0 {
		t.Fatalf("stored %d snapshots, want none", store.calls)
	}
}

func TestCaptureNotFoundSurvivesWrapping(t *testing.T) {
	t.Parallel()

	repo := oneRowRepo()
	repo.pgssDBErr = fmt.Errorf("resolve: %w", dto.ErrNotFound)

	c, err := NewCapturer(repo, &captureStore{}, zap.NewNop()).Capture(t.Context(), CaptureRequest{ //nolint:exhaustruct
		Cluster: "c1", Instance: "h1", Database: "app",
	})
	if !errors.Is(err, dto.ErrNotFound) {
		t.Fatalf("err = %v, want dto.ErrNotFound in the chain", err)
	}

	if !strings.HasPrefix(err.Error(), "pgss database: ") {
		t.Fatalf("err = %q, want the stage named", err)
	}

	if c.Database != "" {
		t.Fatalf("provenance = %q, want none before it is resolved", c.Database)
	}
}

// The daemon attributes its error event to the provenance, so it must be known
// even when the insert fails.
func TestCaptureCreateFailureKeepsProvenance(t *testing.T) {
	t.Parallel()

	store := &captureStore{err: errors.New("disk full")} //nolint:exhaustruct

	c, err := NewCapturer(oneRowRepo(), store, zap.NewNop()).Capture(t.Context(), CaptureRequest{ //nolint:exhaustruct
		Cluster: "c1", Instance: "h1", Database: "app",
	})
	if err == nil || err.Error() != "create snapshot: disk full" {
		t.Fatalf("err = %v, want %q", err, "create snapshot: disk full")
	}

	if c.Database != "postgres" {
		t.Fatalf("provenance = %q, want %q", c.Database, "postgres")
	}
}

func TestCaptureLogsStatsSourceFailure(t *testing.T) {
	t.Parallel()

	core, logs := observer.New(zapcore.WarnLevel)
	repo := oneRowRepo()
	repo.sourceErr = errors.New("permission denied")
	store := &captureStore{} //nolint:exhaustruct

	_, err := NewCapturer(repo, store, zap.New(core)).Capture(t.Context(), CaptureRequest{ //nolint:exhaustruct
		Cluster: "c1", Instance: "h1", Database: "app",
	})
	if err != nil {
		t.Fatalf("a failed stats source lookup must not fail the snapshot: %v", err)
	}

	if store.calls != 1 || store.opts.StatsSource != "" {
		t.Fatalf("calls = %d, source = %q, want one unlabelled snapshot", store.calls, store.opts.StatsSource)
	}

	if logs.Len() != 1 {
		t.Fatalf("got %d warnings, want 1", logs.Len())
	}
}

func TestManualCaptureUsesConfiguredProbes(t *testing.T) {
	t.Parallel()

	repo := oneRowRepo()
	store := &captureStore{cfg: Config{LockProbeCount: 2, LockProbeInterval: time.Millisecond}} //nolint:exhaustruct
	m := NewManualCapturer(NewCapturer(repo, store, zap.NewNop()), store)

	if _, err := m.Capture(t.Context(), "c1", "h1", "app", true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.blockedDBs) != 2 {
		t.Fatalf("ran %d lock probes, want 2 from the config", len(repo.blockedDBs))
	}

	if got := store.opts.LocksData.ProbeInterval; got != time.Millisecond.String() {
		t.Fatalf("probe interval = %s, want 1ms", got)
	}
}

func TestManualCaptureWithoutLocks(t *testing.T) {
	t.Parallel()

	repo := oneRowRepo()
	store := &captureStore{} //nolint:exhaustruct
	m := NewManualCapturer(NewCapturer(repo, store, zap.NewNop()), store)

	if _, err := m.Capture(t.Context(), "c1", "h1", "app", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.blockedDBs) != 0 || store.opts.LocksData != nil {
		t.Fatal("lock probes ran without being asked for")
	}
}
