package autosnapshot

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
)

func (d *Daemon) processCluster(
	ctx context.Context,
	cfg Config,
	effective TriggerDefaults,
	cl config.Cluster,
) {
	firstDB := firstDatabase(cl)

	for _, host := range cl.Hosts {
		instance := string(host)
		key := hostKey{Cluster: string(cl.Name), Instance: instance}

		d.mu.Lock()
		state, ok := d.hosts[key]
		if !ok {
			state = &hostState{}
			d.hosts[key] = state
		}
		d.mu.Unlock()

		d.tickActivitySpike(ctx, cfg, effective.ActivitySpike, cl, instance, firstDB, state)
		d.tickRoleChange(ctx, cfg, effective.RoleChange, cl, instance, firstDB, state)
	}
}

func (d *Daemon) tickActivitySpike(
	ctx context.Context,
	cfg Config,
	t ActivitySpikeTrigger,
	cl config.Cluster,
	instance, database string,
	state *hostState,
) {
	if !t.Enabled {
		return
	}

	count, err := d.repo.GetActiveConnectionCount(ctx, string(cl.Name), instance)
	if err != nil {
		d.logger.Warn("activity probe failed",
			zap.String("cluster", string(cl.Name)),
			zap.String("instance", instance),
			zap.Error(err))

		return
	}

	now := time.Now().UTC()
	state.windowSamples = trimSamples(append(state.windowSamples, activitySample{at: now, count: count}), now.Add(-t.WindowSize))

	if len(state.windowSamples) < 2 {
		return
	}

	baseline := averageExcludingLast(state.windowSamples)

	threshold := baseline * float64(100+t.ActiveThresholdPct) / 100.0

	if baseline < float64(cfg.MinBaselineActive) {
		state.aboveThresholdSince = nil

		d.insertEvent(ctx, TriggerEvent{
			ClusterName: string(cl.Name),
			Instance:    instance,
			TriggerType: TriggerActivitySpike,
			Outcome:     OutcomeSkippedBelowBaseline,
			TriggerContext: map[string]any{
				"baseline":            baseline,
				"min_baseline_active": cfg.MinBaselineActive,
				"current":             count,
			},
		})

		return
	}

	if float64(count) < threshold {
		state.aboveThresholdSince = nil

		return
	}

	if state.aboveThresholdSince == nil {
		state.aboveThresholdSince = &now
		return
	}

	if now.Sub(*state.aboveThresholdSince) < t.SpikeDuration {
		return
	}

	if d.debounced(cl.Name, cfg.MaxSnapshotFrequency) {
		d.insertEvent(ctx, TriggerEvent{
			ClusterName: string(cl.Name),
			Instance:    instance,
			TriggerType: TriggerActivitySpike,
			Outcome:     OutcomeSkippedDebounce,
			TriggerContext: map[string]any{
				"baseline":      baseline,
				"peak_value":    count,
				"threshold_pct": t.ActiveThresholdPct,
			},
		})

		return
	}

	trigCtx := map[string]any{
		"trigger":       "activity_spike",
		"baseline":      baseline,
		"peak_value":    count,
		"threshold_pct": t.ActiveThresholdPct,
		"window_size":   t.WindowSize.String(),
		"duration":      now.Sub(*state.aboveThresholdSince).String(),
		"host":          instance,
	}

	d.takeSnapshot(ctx, cfg, cl, instance, database, TriggerActivitySpike, "auto:activity_spike", trigCtx)
	state.aboveThresholdSince = nil
}

func (d *Daemon) tickRoleChange(
	ctx context.Context,
	cfg Config,
	t RoleChangeTrigger,
	cl config.Cluster,
	instance, database string,
	state *hostState,
) {
	if !t.Enabled {
		return
	}

	info, err := d.repo.GetInstanceInfo(ctx, string(cl.Name), instance)
	if err != nil {
		d.logger.Warn("in_recovery probe failed",
			zap.String("cluster", string(cl.Name)),
			zap.String("instance", instance),
			zap.Error(err))

		return
	}

	current := info.InRecovery

	if state.lastInRecovery == nil {
		state.lastInRecovery = &current
		return
	}

	if *state.lastInRecovery == current {
		return
	}

	from, to := roleLabels(*state.lastInRecovery, current)
	direction := directionOf(*state.lastInRecovery, current)
	state.lastInRecovery = &current

	if !directionAllowed(t.Direction, direction) {
		d.insertEvent(ctx, TriggerEvent{
			ClusterName: string(cl.Name),
			Instance:    instance,
			TriggerType: TriggerRoleChange,
			Outcome:     OutcomeSkippedWrongDirection,
			TriggerContext: map[string]any{
				"from_role": from,
				"to_role":   to,
				"allowed":   string(t.Direction),
			},
		})

		return
	}

	if d.debounced(cl.Name, cfg.MaxSnapshotFrequency) {
		d.insertEvent(ctx, TriggerEvent{
			ClusterName: string(cl.Name),
			Instance:    instance,
			TriggerType: TriggerRoleChange,
			Outcome:     OutcomeSkippedDebounce,
			TriggerContext: map[string]any{
				"from_role": from,
				"to_role":   to,
			},
		})

		return
	}

	trigCtx := map[string]any{
		"trigger":   "role_change",
		"host":      instance,
		"from_role": from,
		"to_role":   to,
	}

	d.takeSnapshot(ctx, cfg, cl, instance, database, TriggerRoleChange, "auto:role_change", trigCtx)
}

func (d *Daemon) takeSnapshot(
	ctx context.Context,
	_ Config,
	cl config.Cluster,
	instance, database string,
	trigType TriggerType,
	reason string,
	trigCtx map[string]any,
) {
	reports, err := d.repo.GetQueriesReport(ctx, string(cl.Name), instance, nil)
	if err != nil {
		d.insertEvent(ctx, TriggerEvent{
			ClusterName:    string(cl.Name),
			Instance:       instance,
			TriggerType:    trigType,
			Outcome:        OutcomeError,
			TriggerContext: trigCtx,
			ErrorMessage:   strPtr(fmt.Sprintf("get report: %v", err)),
		})

		return
	}

	var pgssReset *time.Time
	if t, err := d.repo.GetPgssStatsResetTime(ctx, string(cl.Name), instance, database); err == nil && t != nil {
		pgssReset = &t.Time
	}

	id, createdAt, err := d.store.CreateSnapshot(ctx, string(cl.Name), instance, database, reports, SnapshotOpts{
		PgssStatsReset: pgssReset,
		Reason:         reason,
		TriggerContext: trigCtx,
	})
	if err != nil {
		d.insertEvent(ctx, TriggerEvent{
			ClusterName:    string(cl.Name),
			Instance:       instance,
			TriggerType:    trigType,
			Outcome:        OutcomeError,
			TriggerContext: trigCtx,
			ErrorMessage:   strPtr(fmt.Sprintf("create snapshot: %v", err)),
		})

		return
	}

	d.mu.Lock()
	if d.lastAuto == nil {
		d.lastAuto = map[string]time.Time{}
	}
	d.lastAuto[string(cl.Name)] = createdAt
	d.mu.Unlock()

	if d.resetQueryStatsAllow {
		if err := d.repo.ResetQueryStats(ctx, string(cl.Name), instance, database); err != nil {
			d.logger.Warn("pg_stat_statements_reset failed after snapshot",
				zap.String("cluster", string(cl.Name)),
				zap.String("instance", instance),
				zap.Error(err))
		}
	}

	snapID := id
	d.insertEvent(ctx, TriggerEvent{
		ClusterName:    string(cl.Name),
		Instance:       instance,
		TriggerType:    trigType,
		Outcome:        OutcomeSnapshotCreated,
		SnapshotID:     &snapID,
		TriggerContext: trigCtx,
	})

	d.logger.Info("auto snapshot created",
		zap.String("cluster", string(cl.Name)),
		zap.String("instance", instance),
		zap.String("reason", reason),
		zap.String("snapshot_id", id.String()))
}

func (d *Daemon) debounced(cluster config.ClusterName, maxFreq time.Duration) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	last, ok := d.lastAuto[string(cluster)]
	if !ok {
		return false
	}

	return time.Since(last) < maxFreq
}

func (d *Daemon) insertEvent(ctx context.Context, e TriggerEvent) {
	if err := d.store.InsertTriggerEvent(ctx, e); err != nil {
		d.logger.Warn("insert trigger event failed", zap.Error(err))
	}
}

func trimSamples(samples []activitySample, cutoff time.Time) []activitySample {
	i := 0
	for ; i < len(samples); i++ {
		if !samples[i].at.Before(cutoff) {
			break
		}
	}

	return samples[i:]
}

// averageExcludingLast returns the moving average of all but the most recent
// sample — used as the baseline against which the latest value is compared.
func averageExcludingLast(samples []activitySample) float64 {
	if len(samples) <= 1 {
		return 0
	}

	var sum int
	for _, s := range samples[:len(samples)-1] {
		sum += s.count
	}

	return float64(sum) / float64(len(samples)-1)
}

func roleLabels(prevInRecovery, curInRecovery bool) (string, string) {
	from := "master"
	if prevInRecovery {
		from = "replica"
	}

	to := "master"
	if curInRecovery {
		to = "replica"
	}

	return from, to
}

func directionOf(prevInRecovery, curInRecovery bool) Direction {
	if !prevInRecovery && curInRecovery {
		return DirectionMasterToReplica
	}

	return DirectionReplicaToMaster
}

func directionAllowed(allowed, actual Direction) bool {
	if allowed == DirectionBoth {
		return true
	}

	return allowed == actual
}

func firstDatabase(cl config.Cluster) string {
	if len(cl.Databases) == 0 {
		return "postgres"
	}

	return string(cl.Databases[0])
}

func strPtr(s string) *string {
	return &s
}
