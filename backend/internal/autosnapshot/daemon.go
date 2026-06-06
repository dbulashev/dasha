package autosnapshot

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/dto"
)

// Repo is the narrow repository interface the daemon needs.
// Declared locally to avoid pulling the full Repository into autosnapshot tests.
type Repo interface {
	GetActiveConnectionCount(ctx context.Context, clusterName, instanceName string) (int, error)
	GetInstanceInfo(ctx context.Context, clusterName, instanceName string) (dto.InstanceInfo, error)
	GetQueriesReport(ctx context.Context, clusterName, instanceName string, excludeUsers []string) ([]dto.QueryReport, error)
	GetPgssStatsResetTime(ctx context.Context, clusterName, instanceName, databaseName string) (*dto.StatsResetTime, error)
	ResetQueryStats(ctx context.Context, clusterName, instanceName, databaseName string) error
}

// Store is the narrow storage interface the daemon needs.
type Store interface {
	leaderStorage
	GetAutosnapshotConfig(ctx context.Context) (Config, error)
	ListClusterOverrides(ctx context.Context) (map[string]map[string]any, error)
	LastAutoSnapshotAt(ctx context.Context) (map[string]time.Time, error)
	InsertTriggerEvent(ctx context.Context, e TriggerEvent) error
	CreateSnapshot(ctx context.Context, clusterName, instance, database string, reports []dto.QueryReport, opts SnapshotOpts) (uuid.UUID, time.Time, error)

	ComputePartitionSizes(ctx context.Context) ([]PartitionSize, error)
	DropDayPartitions(ctx context.Context, day time.Time) error
}

// Daemon is the long-running auto-snapshot worker.
type Daemon struct {
	clusters             config.Clusters
	repo                 Repo
	store                Store
	logger               *zap.Logger
	resetQueryStatsAllow bool

	mu        sync.Mutex
	hosts     map[hostKey]*hostState
	lastAuto  map[string]time.Time
	lastRetry time.Time
}

type hostKey struct {
	Cluster  string
	Instance string
}

type hostState struct {
	windowSamples       []activitySample
	lastInRecovery      *bool
	aboveThresholdSince *time.Time
}

type activitySample struct {
	at    time.Time
	count int
}

// NewDaemon wires a daemon from its dependencies.
func NewDaemon(
	clusters config.Clusters,
	repo Repo,
	store Store,
	resetQueryStatsAllow bool,
	logger *zap.Logger,
) *Daemon {
	return &Daemon{
		clusters:             clusters,
		repo:                 repo,
		store:                store,
		logger:               logger,
		resetQueryStatsAllow: resetQueryStatsAllow,
		hosts:                map[hostKey]*hostState{},
	}
}

// Run acquires leader lock and loops until ctx is cancelled.
func (d *Daemon) Run(ctx context.Context) error {
	leader := NewLeader(d.store, d.logger)

	if err := leader.Acquire(ctx); err != nil {
		return fmt.Errorf("autosnapshot: acquire leader: %w", err)
	}

	defer leader.Release()

	hbCtx, cancelHB := context.WithCancel(ctx)
	defer cancelHB()

	go leader.RunHeartbeat(hbCtx)

	last, err := d.store.LastAutoSnapshotAt(ctx)
	if err != nil {
		d.logger.Warn("load last auto snapshot times failed", zap.Error(err))
		last = map[string]time.Time{}
	}

	d.lastAuto = last

	return d.loop(ctx)
}

func (d *Daemon) loop(ctx context.Context) error {
	interval := defaultPollInterval

	timer := time.NewTimer(interval)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			d.logger.Info("autosnapshot: shutdown")
			return nil
		case <-timer.C:
		}

		cfg, err := d.store.GetAutosnapshotConfig(ctx)
		if err != nil {
			d.logger.Warn("read config failed", zap.Error(err))
			timer.Reset(interval)

			continue
		}

		interval = effectivePollInterval(cfg.PollInterval)

		if cfg.Enabled {
			d.tick(ctx, cfg)
		}

		d.maybeRunRetention(ctx, cfg)

		timer.Reset(interval)
	}
}

const defaultPollInterval = 30 * time.Second

func effectivePollInterval(cfg time.Duration) time.Duration {
	if cfg < 5*time.Second {
		return 5 * time.Second
	}

	return cfg
}

// tick is called once per poll_interval. Triggers are implemented in triggers.go.
func (d *Daemon) tick(ctx context.Context, cfg Config) {
	overrides, err := d.store.ListClusterOverrides(ctx)
	if err != nil {
		d.logger.Warn("list cluster overrides failed", zap.Error(err))
		overrides = map[string]map[string]any{}
	}

	cls, err := d.clusters.Get(ctx)
	if err != nil {
		d.logger.Warn("get clusters failed", zap.Error(err))
		return
	}

	for _, cl := range cls {
		effective := cfg.EffectiveFor(overrides[string(cl.Name)])
		d.processCluster(ctx, cfg, effective, cl)
	}
}

