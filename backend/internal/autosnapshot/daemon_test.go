package autosnapshot

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/hotobjects"
)

// fakeRepo scripts the two probes the tested paths touch; the rest are inert.
type fakeRepo struct {
	activeCount func(ctx context.Context) (int, error)
	report      func(ctx context.Context) ([]dto.QueryReport, error)
}

func (f *fakeRepo) GetActiveConnectionCount(ctx context.Context, _, _ string) (int, error) {
	if f.activeCount == nil {
		return 0, nil
	}

	return f.activeCount(ctx)
}

func (f *fakeRepo) GetBlockedSessionCount(context.Context, string, string, string) (int, error) {
	return 0, nil
}

func (f *fakeRepo) GetInstanceInfo(context.Context, string, string) (dto.InstanceInfo, error) {
	return dto.InstanceInfo{}, nil //nolint:exhaustruct
}

func (f *fakeRepo) GetQueriesReport(ctx context.Context, _, _ string, _ []string) ([]dto.QueryReport, error) {
	if f.report == nil {
		return nil, nil
	}

	return f.report(ctx)
}

func (f *fakeRepo) GetQueriesBlocked(context.Context, string, string, string) ([]dto.QueryBlocked, error) {
	return nil, nil
}

func (f *fakeRepo) GetPgssStatsResetTime(context.Context, string, string, string) (*dto.StatsResetTime, error) {
	return nil, nil
}

func (f *fakeRepo) ResetQueryStats(context.Context, string, string, string) error {
	return nil
}

func (f *fakeRepo) GetHotSampleTables(context.Context, string, string, string, *string, *string) ([]hotobjects.AnchorRow, *time.Time, bool, error) {
	return nil, nil, false, nil
}

func (f *fakeRepo) GetHotSampleIndexes(context.Context, string, string, string, *string, *string) ([]hotobjects.AnchorRow, *time.Time, bool, error) {
	return nil, nil, false, nil
}

func newTestDaemon(repo Repo, logger *zap.Logger) *Daemon {
	return &Daemon{ //nolint:exhaustruct
		repo:   repo,
		logger: logger,
		hosts:  map[hostKey]*hostState{},
	}
}

func oneHostCluster() config.Cluster {
	return config.Cluster{ //nolint:exhaustruct
		Name:      "c1",
		Hosts:     []config.Host{"h1"},
		Databases: []config.Database{"db1"},
	}
}

func TestClusterTickTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		captureLocks  bool
		probeCount    int
		probeInterval time.Duration
		hosts         int
		want          time.Duration
	}{
		{"no locks, single host", false, 0, 0, 1, 30 * time.Second},
		{"no locks, three hosts", false, 0, 0, 3, 90 * time.Second},
		{"locks, single host", true, 5, time.Second, 1, 35 * time.Second},
		{"locks, two hosts", true, 5, time.Second, 2, 70 * time.Second},
		{"zero hosts floored to one", false, 0, 0, 0, 30 * time.Second},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := Config{ //nolint:exhaustruct
				CaptureLocks:      tc.captureLocks,
				LockProbeCount:    tc.probeCount,
				LockProbeInterval: tc.probeInterval,
			}

			if got := clusterTickTimeout(cfg, tc.hosts); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestProcessClusterSafeRecoversPanic(t *testing.T) {
	t.Parallel()

	repo := &fakeRepo{activeCount: func(context.Context) (int, error) {
		panic("boom")
	}}

	core, logs := observer.New(zapcore.ErrorLevel)
	d := newTestDaemon(repo, zap.New(core))

	// A propagating panic would crash the test goroutine.
	d.processClusterSafe(context.Background(), validConfig(), validDefaults(), oneHostCluster())

	recovered := logs.FilterMessageSnippet("recovered panic").All()
	if len(recovered) != 1 {
		t.Fatalf("expected one recovered-panic log, got %d", len(recovered))
	}

	if got := recovered[0].ContextMap()["cluster"]; got != "c1" {
		t.Fatalf("expected cluster=c1 in log, got %v", got)
	}
}

func TestProcessClusterSafeAppliesDeadline(t *testing.T) {
	t.Parallel()

	var (
		gotDeadline time.Time
		hadDeadline bool
	)

	repo := &fakeRepo{activeCount: func(ctx context.Context) (int, error) {
		gotDeadline, hadDeadline = ctx.Deadline()

		return 0, nil
	}}

	d := newTestDaemon(repo, zap.NewNop())

	cfg := validConfig()
	start := time.Now()
	d.processClusterSafe(context.Background(), cfg, validDefaults(), oneHostCluster())

	if !hadDeadline {
		t.Fatal("expected the per-cluster context to carry a deadline")
	}

	want := clusterTickTimeout(cfg, 1)
	if budget := gotDeadline.Sub(start); budget < want-time.Second || budget > want+time.Second {
		t.Fatalf("deadline budget = %v, want ~%v", budget, want)
	}
}

func TestTakePendingSafeRecoversPanic(t *testing.T) {
	t.Parallel()

	var hadDeadline bool

	repo := &fakeRepo{report: func(ctx context.Context) ([]dto.QueryReport, error) {
		_, hadDeadline = ctx.Deadline()

		panic("boom")
	}}

	core, logs := observer.New(zapcore.ErrorLevel)
	d := newTestDaemon(repo, zap.New(core))
	p := PendingSnapshot{ClusterName: "c1", Instance: "h1", Database: "db1", Reason: "auto:deferred"}

	d.takePendingSafe(context.Background(), validConfig(), oneHostCluster(), p)

	if !hadDeadline {
		t.Fatal("expected the per-job context to carry a deadline")
	}

	recovered := logs.FilterMessageSnippet("taking deferred snapshot").All()
	if len(recovered) != 1 {
		t.Fatalf("expected one recovered-panic log, got %d", len(recovered))
	}

	if got := recovered[0].ContextMap()["instance"]; got != "h1" {
		t.Fatalf("expected instance=h1 in log, got %v", got)
	}
}
