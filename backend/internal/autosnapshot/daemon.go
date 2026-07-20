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
	"github.com/dbulashev/dasha/internal/hotobjects"
)

// Repo is the narrow repository interface the daemon needs.
// Declared locally to avoid pulling the full Repository into autosnapshot tests.
type Repo interface {
	GetActiveConnectionCount(ctx context.Context, clusterName, instanceName string) (int, error)
	GetBlockedSessionCount(ctx context.Context, clusterName, instanceName, databaseName string) (int, error)
	GetInstanceInfo(ctx context.Context, clusterName, instanceName string) (dto.InstanceInfo, error)
	GetQueriesReport(ctx context.Context, clusterName, instanceName string, excludeUsers []string) ([]dto.QueryReport, error)
	GetQueriesBlocked(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.QueryBlocked, error)
	GetPgssStatsResetTime(ctx context.Context, clusterName, instanceName, databaseName string) (*dto.StatsResetTime, error)
	ResetQueryStats(ctx context.Context, clusterName, instanceName, databaseName string) error
	GetHotSampleTables(ctx context.Context, clusterName, instanceName, databaseName string, schema, object *string) ([]hotobjects.AnchorRow, *time.Time, bool, error)
	GetHotSampleIndexes(ctx context.Context, clusterName, instanceName, databaseName string, schema, object *string) ([]hotobjects.AnchorRow, *time.Time, bool, error)
}

// Store is the narrow storage interface the daemon needs.
type Store interface {
	leaderStorage
	GetAutosnapshotConfig(ctx context.Context) (Config, error)
	ListClusterOverrides(ctx context.Context) (map[string]map[string]any, error)
	LastAutoSnapshotAt(ctx context.Context) (map[string]time.Time, error)
	InsertTriggerEvent(ctx context.Context, e TriggerEvent) error
	CreateSnapshot(ctx context.Context, clusterName, instance, database string, reports []dto.QueryReport, opts SnapshotOpts) (uuid.UUID, time.Time, error)

	EnqueuePendingSnapshot(ctx context.Context, p PendingSnapshot, dueAt time.Time) error
	ClaimDuePendingSnapshots(ctx context.Context) ([]PendingSnapshot, error)
	DeletePendingSnapshot(ctx context.Context, clusterName, instance string) error

	ComputePartitionSizes(ctx context.Context) ([]PartitionSize, error)
	DropDayPartitions(ctx context.Context, day time.Time) error

	LastHotSnapshotAt(ctx context.Context) (map[string]time.Time, error)
	GetHotAnchors(ctx context.Context, clusterName, instance, database string) (map[string]hotobjects.AnchorRow, error)
	InsertHotSnapshotWithAnchors(ctx context.Context, snap hotobjects.Snapshot, anchors map[string][]hotobjects.AnchorRow) (uuid.UUID, error)
	DropHotPartitionsBefore(ctx context.Context, cutoff time.Time) error
}

// Daemon is the long-running auto-snapshot worker.
type Daemon struct {
	clusters       config.Clusters
	repo           Repo
	store          Store
	logger         *zap.Logger
	leaderElection bool

	mu               sync.Mutex
	hosts            map[hostKey]*hostState
	lastAuto         map[hostKey]time.Time
	lastRetry        time.Time
	lastHotRetention time.Time
}

type hostKey struct {
	Cluster  string
	Instance string
}

type hostState struct {
	windowSamples       []activitySample
	lastInRecovery      *bool
	aboveThresholdSince *time.Time
	lastBelowBaselineAt *time.Time      // throttles below-baseline events to avoid per-poll spam
	lockPeak            *BackgroundPeak // worst blocked-session count during a forming spike (hybrid lock capture)
	inSpike             bool            // a spike snapshot was taken and not yet resolved
	recoveryBelowSince  *time.Time      // debounce for the activity_drop (recovery) snapshot
	spikeThreshold      float64         // frozen threshold at spike onset — recovery is judged against it, not the live (inflated) one
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
	leaderElection bool,
	logger *zap.Logger,
) *Daemon {
	return &Daemon{
		clusters:       clusters,
		repo:           repo,
		store:          store,
		logger:         logger,
		leaderElection: leaderElection,
		hosts:          map[hostKey]*hostState{},
	}
}

// Run optionally acquires the advisory-lock leader (opt-in, storage.leader_election;
// off by default since the dedicated session is incompatible with transaction-pooling
// proxies), then loops until ctx is cancelled.
func (d *Daemon) Run(ctx context.Context) error {
	leader := NewLeader(d.store, d.logger)

	if d.leaderElection {
		if err := leader.Acquire(ctx); err != nil {
			return fmt.Errorf("autosnapshot: acquire leader: %w", err)
		}

		defer leader.Release()
	} else {
		d.logger.Info("autosnapshot: leader election disabled, running as single instance")
	}

	// Heartbeat always runs (a plain pooled UPDATE) so UI liveness works regardless
	// of leader election; in HA mode only the elected leader reaches here.
	hbCtx, cancelHB := context.WithCancel(ctx)
	defer cancelHB()

	go leader.RunHeartbeat(hbCtx)

	last, err := d.store.LastAutoSnapshotAt(ctx)
	if err != nil {
		d.logger.Warn("load last auto snapshot times failed", zap.Error(err))
		last = map[string]time.Time{}
	}

	d.lastAuto = d.seedDebounce(ctx, last)

	return d.loop(ctx)
}

// seedDebounce expands the per-cluster last-auto-snapshot times into the per-host
// debounce map (each host inherits the cluster's latest — a conservative restart seed).
func (d *Daemon) seedDebounce(ctx context.Context, perCluster map[string]time.Time) map[hostKey]time.Time {
	out := map[hostKey]time.Time{}

	cls, err := d.clusters.Get(ctx)
	if err != nil {
		d.logger.Warn("seed debounce: get clusters failed", zap.Error(err))

		return out
	}

	for _, cl := range cls {
		t, ok := perCluster[string(cl.Name)]
		if !ok {
			continue
		}

		for _, h := range cl.Hosts {
			out[hostKey{Cluster: string(cl.Name), Instance: string(h)}] = t
		}
	}

	return out
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
			d.processPending(ctx, cfg)
		}

		// Hot-objects snapshots are gated by their own flag: they are useful
		// even when pgss auto-snapshots are disabled.
		d.processHotSnapshots(ctx, cfg)

		d.maybeRunRetention(ctx, cfg)
		d.maybeRunHotRetention(ctx, cfg)

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
		d.processClusterSafe(ctx, cfg, effective, cl)
	}
}

// processClusterSafe bounds one cluster's sweep with a deadline and recovers
// panics, so one bad cluster can't hang or crash the whole daemon loop.
func (d *Daemon) processClusterSafe(ctx context.Context, cfg Config, effective TriggerDefaults, cl config.Cluster) {
	defer func() {
		if r := recover(); r != nil {
			d.logger.Error("autosnapshot: recovered panic processing cluster",
				zap.String("cluster", string(cl.Name)),
				zap.Any("panic", r),
				zap.Stack("stack"))
		}
	}()

	cctx, cancel := context.WithTimeout(ctx, clusterTickTimeout(cfg, len(cl.Hosts)))
	defer cancel()

	d.processCluster(cctx, cfg, effective, cl)
}

// Per-host slack on top of any lock-probe sleeps; see clusterTickTimeout.
const clusterTickBudgetPerHost = 30 * time.Second

// clusterTickTimeout is deliberately generous — it must exceed legitimate work
// (mainly the lock-probe sleeps, which scale with host count) so it only trips
// on a real hang, never on a slow-but-healthy cluster.
func clusterTickTimeout(cfg Config, hosts int) time.Duration {
	if hosts < 1 {
		hosts = 1
	}

	per := clusterTickBudgetPerHost
	if cfg.CaptureLocks {
		per += time.Duration(cfg.LockProbeCount) * cfg.LockProbeInterval
	}

	return time.Duration(hosts) * per
}

// processPending takes any deferred snapshots whose due time has passed.
func (d *Daemon) processPending(ctx context.Context, cfg Config) {
	// Resolve clusters first — ClaimDuePendingSnapshots is a destructive
	// DELETE ... RETURNING, so a failed cluster lookup must not drop the jobs.
	cls, err := d.clusters.Get(ctx)
	if err != nil {
		d.logger.Warn("pending: get clusters failed", zap.Error(err))
		return
	}

	byName := make(map[string]config.Cluster, len(cls))
	for _, cl := range cls {
		byName[string(cl.Name)] = cl
	}

	pending, err := d.store.ClaimDuePendingSnapshots(ctx)
	if err != nil {
		d.logger.Warn("claim pending snapshots failed", zap.Error(err))
		return
	}

	for _, p := range pending {
		cl, ok := byName[p.ClusterName]
		if !ok {
			continue // cluster removed since enqueue
		}

		d.takePendingSafe(ctx, cfg, cl, p)
	}
}

// takePendingSafe runs one deferred snapshot under the same per-host deadline and
// panic recovery as the live sweep. Deferred snapshots never capture locks, so the
// base per-host budget applies.
func (d *Daemon) takePendingSafe(ctx context.Context, cfg Config, cl config.Cluster, p PendingSnapshot) {
	defer func() {
		if r := recover(); r != nil {
			d.logger.Error("autosnapshot: recovered panic taking deferred snapshot",
				zap.String("cluster", p.ClusterName),
				zap.String("instance", p.Instance),
				zap.Any("panic", r),
				zap.Stack("stack"))
		}
	}()

	cctx, cancel := context.WithTimeout(ctx, clusterTickBudgetPerHost)
	defer cancel()

	trigCtx := map[string]any{"trigger": "deferred", "host": p.Instance}
	_ = d.takeSnapshot(cctx, cfg, cl, p.Instance, p.Database, TriggerDeferred, p.Reason, trigCtx, nil)
}
