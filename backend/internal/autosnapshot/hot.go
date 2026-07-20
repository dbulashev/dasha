package autosnapshot

import (
	"context"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/hotobjects"
)

// processHotSnapshots runs one hot-objects sweep: every cluster×database whose
// cron schedule has fired since its last capture gets a fresh delta snapshot
// (plans/hot-objects-design.md). Runs on the leader inside the daemon loop.
func (d *Daemon) processHotSnapshots(ctx context.Context, cfg Config) {
	if !cfg.HotEnabled {
		return
	}

	// A 5-field cron covers both shapes users need: a fixed time of day
	// ("0 3 * * *") and a frequency ("*/30 * * * *"). An unparsable value
	// falls back to daily rather than silently disabling the capture.
	sched, err := ParseHotSchedule(cfg.HotSchedule)
	if err != nil {
		d.logger.Warn("hot: invalid schedule, falling back to daily",
			zap.String("schedule", cfg.HotSchedule), zap.Error(err))

		sched = nil
	}

	last, err := d.store.LastHotSnapshotAt(ctx)
	if err != nil {
		d.logger.Warn("hot: load last snapshot times failed", zap.Error(err))

		return
	}

	cls, err := d.clusters.Get(ctx)
	if err != nil {
		d.logger.Warn("hot: get clusters failed", zap.Error(err))

		return
	}

	now := time.Now().UTC()

	for _, cl := range cls {
		for _, db := range cl.Databases {
			key := string(cl.Name) + "/" + string(db)

			// No snapshot yet → capture immediately: the first run only seeds
			// anchors, and the schedule takes over from there.
			if t, ok := last[key]; ok && !hotDue(sched, t, now) {
				continue
			}

			d.takeHotSnapshotSafe(ctx, cfg, cl, string(db))
		}
	}
}

// ParseHotSchedule parses the cron spec pinned to UTC unless the user names a
// zone explicitly. robfig/cron defaults a spec without a TZ prefix to the
// process's time.Local — 03:00 would then mean whatever zone the backend
// container happens to run in. UTC is the deterministic default; a local-time
// schedule is spelled "CRON_TZ=Europe/Moscow 0 3 * * *".
func ParseHotSchedule(spec string) (cron.Schedule, error) {
	if !strings.HasPrefix(spec, "TZ=") && !strings.HasPrefix(spec, "CRON_TZ=") {
		spec = "CRON_TZ=UTC " + spec
	}

	return cron.ParseStandard(spec)
}

// hotDue reports whether the schedule has fired between the last capture and
// now (nil schedule = the daily fallback).
func hotDue(sched cron.Schedule, last, now time.Time) bool {
	if sched == nil {
		return now.Sub(last) >= 24*time.Hour
	}

	return !now.Before(sched.Next(last))
}

// takeHotSnapshotSafe bounds one capture with the per-host budget and recovers
// panics, mirroring processClusterSafe.
func (d *Daemon) takeHotSnapshotSafe(ctx context.Context, cfg Config, cl config.Cluster, database string) {
	defer func() {
		if r := recover(); r != nil {
			d.logger.Error("hot: recovered panic taking snapshot",
				zap.String("cluster", string(cl.Name)),
				zap.String("database", database),
				zap.Any("panic", r),
				zap.Stack("stack"))
		}
	}()

	hosts := len(cl.Hosts)
	if hosts < 1 {
		hosts = 1
	}

	cctx, cancel := context.WithTimeout(ctx, time.Duration(hosts)*clusterTickBudgetPerHost)
	defer cancel()

	d.takeHotSnapshot(cctx, cfg, cl, database)
}

func (d *Daemon) takeHotSnapshot(ctx context.Context, cfg Config, cl config.Cluster, database string) {
	clusterName := string(cl.Name)
	capturedAt := time.Now().UTC()

	var (
		inputs  []hotobjects.BuildInput
		missing []string
	)

	for _, h := range cl.Hosts {
		host := string(h)

		tables, reset, inRecovery, err := d.repo.GetHotSampleTables(ctx, clusterName, host, database, nil, nil)
		if err != nil {
			d.logger.Warn("hot: sample tables failed",
				zap.String("cluster", clusterName), zap.String("host", host), zap.Error(err))
			missing = append(missing, host)

			continue
		}

		indexes, _, _, err := d.repo.GetHotSampleIndexes(ctx, clusterName, host, database, nil, nil)
		if err != nil {
			d.logger.Warn("hot: sample indexes failed",
				zap.String("cluster", clusterName), zap.String("host", host), zap.Error(err))
			missing = append(missing, host)

			continue
		}

		anchors, err := d.store.GetHotAnchors(ctx, clusterName, host, database)
		if err != nil {
			// Without the baseline the host cannot contribute deltas, and
			// overwriting its anchors would silently shorten the next window —
			// treat it as missing and leave the anchors alone.
			d.logger.Warn("hot: read anchors failed",
				zap.String("cluster", clusterName), zap.String("host", host), zap.Error(err))
			missing = append(missing, host)

			continue
		}

		inputs = append(inputs, hotobjects.BuildInput{
			Sample: hotobjects.HostSample{
				Instance:   host,
				CapturedAt: capturedAt,
				StatsReset: reset,
				InRecovery: inRecovery,
				Rows:       append(tables, indexes...),
			},
			Anchors: anchors,
		})
	}

	if len(inputs) == 0 {
		d.logger.Warn("hot: no host sampled, skipping snapshot",
			zap.String("cluster", clusterName), zap.String("database", database))

		return
	}

	snap := hotobjects.BuildSnapshot(clusterName, database, capturedAt, inputs, missing, cfg.HotTopN)

	// The snapshot and every host's anchor advance commit together: on failure
	// nothing is stored and the next tick retries from the same baseline, so no
	// interval is lost or (via a stored snapshot with stale anchors) counted twice.
	anchors := make(map[string][]hotobjects.AnchorRow, len(inputs))
	for _, in := range inputs {
		anchors[in.Sample.Instance] = in.Sample.Rows
	}

	if _, err := d.store.InsertHotSnapshotWithAnchors(ctx, snap, anchors); err != nil {
		d.logger.Warn("hot: store snapshot failed",
			zap.String("cluster", clusterName), zap.String("database", database), zap.Error(err))

		return
	}

	d.logger.Info("hot: snapshot stored",
		zap.String("cluster", clusterName),
		zap.String("database", database),
		zap.Int("hosts", len(inputs)),
		zap.Int("hosts_missing", len(missing)),
		zap.Int("top_entries", len(snap.Top)))
}

// maybeRunHotRetention drops hot-objects partitions older than
// cfg.HotRetentionDays. Age-based and independent from the size-based pgss
// retention; runs at most once per retentionInterval.
func (d *Daemon) maybeRunHotRetention(ctx context.Context, cfg Config) {
	if cfg.HotRetentionDays <= 0 {
		return
	}

	d.mu.Lock()
	if !d.lastHotRetention.IsZero() && time.Since(d.lastHotRetention) < retentionInterval {
		d.mu.Unlock()

		return
	}
	d.lastHotRetention = time.Now().UTC()
	d.mu.Unlock()

	cutoff := time.Now().UTC().AddDate(0, 0, -cfg.HotRetentionDays)

	if err := d.store.DropHotPartitionsBefore(ctx, cutoff); err != nil {
		d.logger.Warn("hot: retention failed", zap.Error(err))
	}
}
