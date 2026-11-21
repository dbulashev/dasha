package autosnapshot

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/dto"
)

type CaptureRepo interface {
	GetQueriesReport(ctx context.Context, clusterName, instanceName, databaseName string, excludeUsers []string, queryID *int64) ([]dto.QueryReport, error)
	PgssDatabase(ctx context.Context, clusterName, instanceName, databaseName string) (string, error)
	GetPgssStatsResetTime(ctx context.Context, clusterName, instanceName, databaseName string) (*dto.StatsResetTime, error)
	PgssSource(ctx context.Context, clusterName, instanceName, databaseName string) (string, error)
	blockedProber
}

type CaptureStore interface {
	CreateSnapshot(ctx context.Context, clusterName, instance, database string, reports []dto.QueryReport, opts SnapshotOpts) (uuid.UUID, time.Time, error)
}

type LockProbe struct {
	Count          int
	Interval       time.Duration
	BackgroundPeak *BackgroundPeak
}

// DefaultLockProbe matches the column defaults of autosnapshot_config_global.
var DefaultLockProbe = LockProbe{Count: 5, Interval: 500 * time.Millisecond} //nolint:exhaustruct

type CaptureRequest struct {
	Cluster  string
	Instance string

	// Database is a preference only: the snapshot records the database the
	// statistics were actually read through (Captured.Database).
	Database string

	Reason         string
	TriggerContext map[string]any
	Locks          *LockProbe // nil — no lock probes
}

type Captured struct {
	ID        uuid.UUID
	CreatedAt time.Time

	// Database is the provenance. It is set as soon as it is known, including
	// when CreateSnapshot fails, so the daemon can attribute its error event.
	Database string
}

// ErrEmptyReport: no readable extension anywhere on the host. Storing such a
// snapshot would only fill the selector with entries that explain nothing.
var ErrEmptyReport = errors.New("empty report: no query-statistics extension is readable in any database of the host")

// Capturer stores one instance-wide pgss snapshot for a host. pg_stat_statements
// holds the same instance-wide rows in every database that has the extension,
// so what varies is only the database it could be read through.
type Capturer struct {
	repo   CaptureRepo
	store  CaptureStore
	logger *zap.Logger
}

func NewCapturer(repo CaptureRepo, store CaptureStore, logger *zap.Logger) *Capturer {
	return &Capturer{repo: repo, store: store, logger: logger}
}

func (c *Capturer) Capture(ctx context.Context, req CaptureRequest) (Captured, error) {
	var out Captured

	reports, err := c.repo.GetQueriesReport(ctx, req.Cluster, req.Instance, req.Database, nil, nil)
	if err != nil {
		return out, fmt.Errorf("get report: %w", err)
	}

	if len(reports) == 0 {
		return out, ErrEmptyReport
	}

	// Recorded, not guessed: falling back to the requested database would label
	// the snapshot with numbers it did not come from.
	provenance, err := c.repo.PgssDatabase(ctx, req.Cluster, req.Instance, req.Database)
	if err != nil {
		return out, fmt.Errorf("pgss database: %w", err)
	}

	out.Database = provenance

	var pgssReset *time.Time
	if t, err := c.repo.GetPgssStatsResetTime(ctx, req.Cluster, req.Instance, req.Database); err == nil && t != nil {
		pgssReset = &t.Time
	}

	statsSource, err := c.repo.PgssSource(ctx, req.Cluster, req.Instance, req.Database)
	if err != nil {
		c.logger.Warn("stats source lookup failed; snapshot stored unlabelled",
			zap.String("cluster", req.Cluster),
			zap.String("instance", req.Instance),
			zap.Error(err))
	}

	var locks *LockCapture
	if req.Locks != nil {
		lc := CaptureLocks(ctx, c.repo, req.Cluster, req.Instance, provenance, dto.ScopeInstance,
			req.Locks.Count, req.Locks.Interval)
		lc.BackgroundPeak = req.Locks.BackgroundPeak
		locks = &lc
	}

	out.ID, out.CreatedAt, err = c.store.CreateSnapshot(ctx, req.Cluster, req.Instance, provenance, reports, SnapshotOpts{
		PgssStatsReset: pgssReset,
		StatsSource:    statsSource,
		Reason:         req.Reason,
		TriggerContext: req.TriggerContext,
		LocksData:      locks,
	})
	if err != nil {
		return out, fmt.Errorf("create snapshot: %w", err)
	}

	return out, nil
}

type ConfigReader interface {
	GetAutosnapshotConfig(ctx context.Context) (Config, error)
}

// ManualCapturer is the on-demand snapshot: lock probes, when asked for, use
// the configured probe settings.
type ManualCapturer struct {
	capturer *Capturer
	cfg      ConfigReader
}

func NewManualCapturer(capturer *Capturer, cfg ConfigReader) *ManualCapturer {
	return &ManualCapturer{capturer: capturer, cfg: cfg}
}

func (m *ManualCapturer) Capture(ctx context.Context, cluster, instance, database string, withLocks bool) (Captured, error) {
	req := CaptureRequest{ //nolint:exhaustruct
		Cluster:  cluster,
		Instance: instance,
		Database: database,
	}

	if withLocks {
		probe := DefaultLockProbe
		if cfg, err := m.cfg.GetAutosnapshotConfig(ctx); err == nil {
			probe = LockProbe{Count: cfg.LockProbeCount, Interval: cfg.LockProbeInterval} //nolint:exhaustruct
		}

		req.Locks = &probe
	}

	return m.capturer.Capture(ctx, req)
}
