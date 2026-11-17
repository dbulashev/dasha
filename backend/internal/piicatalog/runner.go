package piicatalog

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/pkg/cronsched"
)

type Store interface {
	ListPIIStatus(ctx context.Context) (map[Key]Status, error)
	// Compares set hashes and, on a difference, writes a version, the SCD2
	// pair changes and the status in one transaction.
	ApplyPIISnapshot(ctx context.Context, k Key, snap Snapshot) (ApplyResult, error)
	RecordPIIFailure(ctx context.Context, k Key, attemptAt time.Time, errMsg string) error
	PIIRetention(ctx context.Context, cutoff, auditCutoff time.Time) (RetentionStats, error)
}

const (
	tickInterval       = 60 * time.Second
	clusterConcurrency = 4
	retentionInterval  = 24 * time.Hour
)

type Runner struct {
	cfg       config.PIICatalogConfig
	sched     cron.Schedule
	clusters  config.Clusters
	collector *Collector
	store     Store
	logger    *zap.Logger
	clock     func() time.Time

	lastRetention time.Time
}

func NewRunner(
	cfg config.PIICatalogConfig,
	clusters config.Clusters,
	repo Repo,
	store Store,
	logger *zap.Logger,
) (*Runner, error) {
	sched, err := cronsched.Parse(cfg.Schedule)
	if err != nil {
		return nil, fmt.Errorf("pii_catalog.schedule: invalid cron expression %q: %w", cfg.Schedule, err)
	}

	return &Runner{
		cfg:       cfg,
		sched:     sched,
		clusters:  clusters,
		collector: NewCollector(repo, cfg.ExcludeSchemas, cfg.StatementTimeout, logger),
		store:     store,
		logger:    logger,
		clock:     time.Now,
	}, nil
}

// Run blocks until ctx is cancelled. Due-ness is derived from the persisted
// last_attempt_at, so a restart or leader change does not trigger an extra sweep.
func (r *Runner) Run(ctx context.Context) {
	r.logger.Info("pii_catalog: runner started", zap.String("schedule", r.cfg.Schedule))

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	for {
		r.runOnce(ctx)

		select {
		case <-ctx.Done():
			r.logger.Info("pii_catalog: runner stopped")

			return
		case <-ticker.C:
		}
	}
}

func (r *Runner) now() time.Time {
	return r.clock().UTC()
}

func (r *Runner) runOnce(ctx context.Context) {
	now := r.now()

	status, err := r.store.ListPIIStatus(ctx)
	if err != nil {
		r.logger.Warn("pii_catalog: list status failed", zap.Error(err))

		return
	}

	cls, err := r.clusters.Get(ctx)
	if err != nil {
		r.logger.Warn("pii_catalog: get clusters failed", zap.Error(err))

		return
	}

	sem := make(chan struct{}, clusterConcurrency)

	var wg sync.WaitGroup

	for _, cl := range cls {
		due := r.dueDatabases(cl, status, now)
		if len(due) == 0 {
			continue
		}

		select {
		case <-ctx.Done():
		case sem <- struct{}{}:
			wg.Add(1)

			go func() {
				defer wg.Done()
				defer func() { <-sem }()

				r.collectClusterSafe(ctx, cl, due, now)
			}()
		}
	}

	wg.Wait()

	if ctx.Err() == nil {
		r.maybeRunRetention(ctx, now)
	}
}

func (r *Runner) dueDatabases(cl config.Cluster, status map[Key]Status, now time.Time) []string {
	var due []string

	seen := make(map[string]struct{}, len(cl.Databases))

	for _, db := range cl.Databases {
		if _, dup := seen[string(db)]; dup {
			continue
		}

		seen[string(db)] = struct{}{}

		s, ok := status[Key{Cluster: string(cl.Name), Database: string(db)}]
		if !ok || s.LastAttemptAt == nil || cronsched.Due(r.sched, *s.LastAttemptAt, now) {
			due = append(due, string(db))
		}
	}

	return due
}

// collectClusterSafe guards the host probing; per-database panics are caught
// in collectDatabaseSafe.
func (r *Runner) collectClusterSafe(ctx context.Context, cl config.Cluster, databases []string, attemptAt time.Time) {
	defer func() {
		if rec := recover(); rec != nil {
			r.logger.Error("pii_catalog: recovered panic processing cluster",
				zap.String("cluster", string(cl.Name)), zap.Any("panic", rec), zap.Stack("stack"))
		}
	}()

	hosts := r.collector.orderHosts(ctx, cl)

	for _, db := range databases {
		if ctx.Err() != nil {
			return
		}

		r.collectDatabaseSafe(ctx, string(cl.Name), hosts, db, attemptAt)
	}
}

func (r *Runner) collectDatabaseSafe(ctx context.Context, cluster string, hosts []host, database string, attemptAt time.Time) {
	k := Key{Cluster: cluster, Database: database}

	defer func() {
		if rec := recover(); rec != nil {
			r.logger.Error("pii_catalog: recovered panic collecting database",
				zap.String("cluster", cluster), zap.String("database", database),
				zap.Any("panic", rec), zap.Stack("stack"))

			r.recordFailure(ctx, k, attemptAt, "internal error")
		}
	}()

	budget := time.Duration(max(len(hosts), 1)) * (r.cfg.StatementTimeout + attemptSlack)

	dctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	start := time.Now()

	snap, err := r.collector.collect(dctx, cluster, hosts, database, attemptAt)
	if err != nil {
		if ctx.Err() != nil {
			return
		}

		r.logger.Warn("pii_catalog: collect failed",
			zap.String("cluster", cluster), zap.String("database", database), zap.Error(err))
		r.recordFailure(ctx, k, attemptAt, err.Error())

		return
	}

	// A storage failure is not recorded as an attempt: the database stays due
	// and is retried on the next tick.
	res, err := r.store.ApplyPIISnapshot(ctx, k, snap)
	if err != nil {
		r.logger.Warn("pii_catalog: store snapshot failed",
			zap.String("cluster", cluster), zap.String("database", database), zap.Error(err))

		return
	}

	r.logger.Info("pii_catalog: collected",
		zap.String("cluster", cluster),
		zap.String("database", database),
		zap.String("source_instance", snap.SourceInstance),
		zap.Int("pairs", len(snap.Pairs)),
		zap.Int64("version_id", res.VersionID),
		zap.Int("added", res.Added),
		zap.Int("removed", res.Removed),
		zap.Duration("duration", time.Since(start)))
}

func (r *Runner) recordFailure(ctx context.Context, k Key, attemptAt time.Time, msg string) {
	if err := r.store.RecordPIIFailure(ctx, k, attemptAt, msg); err != nil {
		r.logger.Warn("pii_catalog: record failure failed",
			zap.String("cluster", k.Cluster), zap.String("database", k.Database), zap.Error(err))
	}
}

func (r *Runner) maybeRunRetention(ctx context.Context, now time.Time) {
	if !r.lastRetention.IsZero() && now.Sub(r.lastRetention) < retentionInterval {
		return
	}

	r.lastRetention = now

	stats, err := r.store.PIIRetention(ctx,
		now.AddDate(0, 0, -r.cfg.RetentionDays),
		now.AddDate(0, 0, -r.cfg.AuditRetentionDays))
	if err != nil {
		r.logger.Warn("pii_catalog: retention failed", zap.Error(err))

		return
	}

	r.logger.Info("pii_catalog: retention done",
		zap.Int64("pairs", stats.Pairs),
		zap.Int64("versions", stats.Versions),
		zap.Int64("access_log", stats.AccessLog),
		zap.Int64("orphan_databases", stats.OrphanDatabases))
}
