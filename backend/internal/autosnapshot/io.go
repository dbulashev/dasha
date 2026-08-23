package autosnapshot

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/statio"
)

// processIOSnapshots runs one pg_stat_io sweep: every cluster×host whose cron
// schedule has fired since its last capture gets a fresh raw cumulative slice
// (plans/pg-stat-io-design.md). Runs on the leader inside the daemon loop.
//
// Unlike hot-objects there is no anchor to seed: the previous snapshot is the
// baseline, so the very first capture is already a full point of the series —
// it simply has nothing before it to measure against.
func (d *Daemon) processIOSnapshots(ctx context.Context, cfg Config) {
	if !cfg.IOEnabled {
		return
	}

	sched, err := ParseCronSchedule(cfg.IOSchedule)
	if err != nil {
		d.logger.Warn("io: invalid schedule, falling back to daily",
			zap.String("schedule", cfg.IOSchedule), zap.Error(err))

		sched = nil
	}

	last, err := d.store.LastIOSnapshotAt(ctx)
	if err != nil {
		d.logger.Warn("io: load last snapshot times failed", zap.Error(err))

		return
	}

	cls, err := d.clusters.Get(ctx)
	if err != nil {
		d.logger.Warn("io: get clusters failed", zap.Error(err))

		return
	}

	now := time.Now().UTC()

	for _, cl := range cls {
		for _, h := range cl.Hosts {
			host := string(h)
			key := string(cl.Name) + "/" + host

			if !d.dueForCapture(d.lastIOAttempt, sched, last, key, now) {
				continue
			}

			d.takeIOSnapshotSafe(ctx, cl, host)
		}
	}
}

// takeIOSnapshotSafe bounds one capture with the per-host budget and recovers
// panics, mirroring processClusterSafe.
func (d *Daemon) takeIOSnapshotSafe(ctx context.Context, cl config.Cluster, host string) {
	defer func() {
		if r := recover(); r != nil {
			d.logger.Error("io: recovered panic taking snapshot",
				zap.String("cluster", string(cl.Name)),
				zap.String("host", host),
				zap.Any("panic", r),
				zap.Stack("stack"))
		}
	}()

	cctx, cancel := context.WithTimeout(ctx, clusterTickBudgetPerHost)
	defer cancel()

	d.takeIOSnapshot(cctx, string(cl.Name), host)
}

func (d *Daemon) takeIOSnapshot(ctx context.Context, clusterName, host string) {
	snap, err := d.repo.GetIOSample(ctx, clusterName, host)

	// A host older than PG 16 has no pg_stat_io: a deployment fact, not a
	// failure, so it stays out of the warning stream every tick.
	if errors.Is(err, statio.ErrUnsupportedVersion) {
		d.logger.Debug("io: host has no pg_stat_io, skipping",
			zap.String("cluster", clusterName), zap.String("host", host))

		return
	}

	if err != nil {
		// Series are per host, so an unreachable host costs only its own
		// interval — the neighbours keep theirs.
		d.logger.Warn("io: sample failed",
			zap.String("cluster", clusterName), zap.String("host", host), zap.Error(err))

		return
	}

	if _, err := d.store.InsertIOSnapshot(ctx, clusterName, host, *snap); err != nil {
		d.logger.Warn("io: store snapshot failed",
			zap.String("cluster", clusterName), zap.String("host", host), zap.Error(err))

		return
	}

	d.logger.Debug("io: snapshot stored",
		zap.String("cluster", clusterName),
		zap.String("host", host),
		zap.Int("rows", len(snap.Rows)))
}

// maybeRunIORetention drops pg_stat_io partitions older than
// cfg.IORetentionDays. Age-based and independent from both the size-based pgss
// retention and the hot-objects one; runs at most once per retentionInterval.
func (d *Daemon) maybeRunIORetention(ctx context.Context, cfg Config) {
	if cfg.IORetentionDays <= 0 {
		return
	}

	d.mu.Lock()
	if !d.lastIORetention.IsZero() && time.Since(d.lastIORetention) < retentionInterval {
		d.mu.Unlock()

		return
	}
	d.lastIORetention = time.Now().UTC()
	d.mu.Unlock()

	cutoff := time.Now().UTC().AddDate(0, 0, -cfg.IORetentionDays)

	if err := d.store.DropIOPartitionsBefore(ctx, cutoff); err != nil {
		d.logger.Warn("io: retention failed", zap.Error(err))
	}
}
