package autosnapshot

import (
	"context"
	"fmt"
	"math"
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

	now := d.nowUTC()
	state.windowSamples = trimSamples(append(state.windowSamples, activitySample{at: now, count: count}), now.Add(-t.WindowSize))

	if len(state.windowSamples) < 2 {
		return
	}

	baseline := averageExcludingLast(state.windowSamples)

	threshold := baseline * float64(100+t.ActiveThresholdPct) / 100.0

	// Below the activity floor the spike rule is unreliable (a 2→3 jump is +50%);
	// skip it and debug-log (throttled) rather than persist a trigger_event.
	if baseline < float64(cfg.MinBaselineActive) {
		state.resetSpike()
		d.handleRecovery(ctx, cfg, t, cl, instance, database, state, now, count)

		if count >= cfg.MinBaselineActive && belowBaselineDue(state, now, cfg.MaxSnapshotFrequency) {
			state.lastBelowBaselineAt = &now

			d.logger.Debug("activity below baseline floor",
				zap.String("cluster", string(cl.Name)),
				zap.String("instance", instance),
				zap.Float64("baseline", baseline),
				zap.Int("current", count),
				zap.Int("min_baseline_active", cfg.MinBaselineActive))
		}

		return
	}

	switch state.advanceSpike(now, count, threshold, t.SpikeDuration) {
	case spikeAborted:
		d.handleRecovery(ctx, cfg, t, cl, instance, database, state, now, count)

		return
	case spikeStarted:
		state.recoveryBelowSince = nil
		// Freeze the recovery reference at the clean pre-spike threshold; capturing it
		// at fire time (baseline already inflated by the spike) makes the drop fire
		// mid-incident. Don't re-inflate it on a re-crossing within the same incident.
		if !state.inSpike {
			state.spikeThreshold = threshold
		}

		return
	case spikeForming:
		state.recoveryBelowSince = nil
	}

	// Sample blocked sessions while the spike forms, keeping only the running peak,
	// so the snapshot can report it even if the trigger fires after the storm subsided.
	if cfg.CaptureLocks {
		if n, err := d.repo.GetBlockedSessionCount(ctx, string(cl.Name), instance, database); err == nil && n > 0 {
			if state.lockPeak == nil || n > state.lockPeak.BlockedCount {
				state.lockPeak = &BackgroundPeak{BlockedCount: n, At: now}
			}
		}
	}

	if now.Sub(*state.aboveThresholdSince) < t.SpikeDuration {
		return
	}

	// Dips are tolerated one at a time, but the spike must still dominate the
	// candidate — otherwise load flapping around the threshold would qualify.
	coverage := state.spikeCoverage()
	if coverage < minSpikeCoverage {
		return
	}

	if d.debounced(hostKey{Cluster: string(cl.Name), Instance: instance}, cfg.MaxSnapshotFrequency) {
		d.logger.Debug("activity spike debounced",
			zap.String("cluster", string(cl.Name)),
			zap.String("instance", instance),
			zap.Float64("baseline", baseline),
			zap.Int("peak_value", state.spikePeak))

		return
	}

	trigCtx := map[string]any{
		"trigger":       "activity_spike",
		"baseline":      baseline,
		"peak_value":    state.spikePeak,
		"threshold_pct": t.ActiveThresholdPct,
		"window_size":   t.WindowSize.String(),
		"duration":      now.Sub(*state.aboveThresholdSince).String(),
		"coverage":      math.Round(coverage*100) / 100,
		"host":          instance,
	}

	err = d.takeSnapshot(ctx, cfg, cl, instance, database, TriggerActivitySpike, "auto:activity_spike", trigCtx, state.lockPeak)
	state.resetSpike()

	// On failure don't advance incident state or enqueue a deferred follow-up;
	// the spike retries on the next sustained crossing.
	if err != nil {
		return
	}

	state.inSpike = true
	state.recoveryBelowSince = nil

	if t.DeferredInterval > 0 {
		p := PendingSnapshot{ClusterName: string(cl.Name), Instance: instance, Database: database, Reason: "auto:deferred"}
		if err := d.store.EnqueuePendingSnapshot(ctx, p, now.Add(t.DeferredInterval)); err != nil {
			d.logger.Warn("enqueue deferred snapshot failed",
				zap.String("cluster", string(cl.Name)),
				zap.String("instance", instance),
				zap.Error(err))
		}
	}
}

// handleRecovery takes one activity_drop snapshot once a host that was in a spike
// stays below the FROZEN spike-onset threshold for t.RecoveryDuration. Judging
// against the frozen (not live) threshold avoids a false "recovered" mid-spike,
// since a sustained spike inflates the live moving-average threshold past the count.
func (d *Daemon) handleRecovery(
	ctx context.Context,
	cfg Config,
	t ActivitySpikeTrigger,
	cl config.Cluster,
	instance, database string,
	state *hostState,
	now time.Time,
	count int,
) {
	if t.RecoveryDuration <= 0 {
		state.inSpike = false
		state.recoveryBelowSince = nil

		return
	}

	if !state.inSpike {
		state.recoveryBelowSince = nil
		return
	}

	if float64(count) >= state.spikeThreshold {
		state.recoveryBelowSince = nil
		return
	}

	if state.recoveryBelowSince == nil {
		state.recoveryBelowSince = &now
		return
	}

	if now.Sub(*state.recoveryBelowSince) < t.RecoveryDuration {
		return
	}

	trigCtx := map[string]any{
		"trigger": "activity_drop",
		"host":    instance,
	}

	if err := d.takeSnapshot(ctx, cfg, cl, instance, database, TriggerActivityDrop, "auto:activity_drop", trigCtx, nil); err != nil {
		// keep inSpike so it retries; reset the debounce to wait recovery_duration again
		state.recoveryBelowSince = nil
		return
	}

	state.inSpike = false
	state.recoveryBelowSince = nil

	// Incident resolved — cancel the pending deferred (it only stays for spikes that never resolve).
	if err := d.store.DeletePendingSnapshot(ctx, string(cl.Name), instance); err != nil {
		d.logger.Warn("cancel pending deferred snapshot failed",
			zap.String("cluster", string(cl.Name)),
			zap.String("instance", instance),
			zap.Error(err))
	}
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
		d.logger.Debug("role change wrong direction",
			zap.String("cluster", string(cl.Name)),
			zap.String("instance", instance),
			zap.String("from_role", from),
			zap.String("to_role", to),
			zap.String("allowed", string(t.Direction)))

		return
	}

	if d.debounced(hostKey{Cluster: string(cl.Name), Instance: instance}, cfg.MaxSnapshotFrequency) {
		d.logger.Debug("role change debounced",
			zap.String("cluster", string(cl.Name)),
			zap.String("instance", instance),
			zap.String("from_role", from),
			zap.String("to_role", to))

		return
	}

	trigCtx := map[string]any{
		"trigger":   "role_change",
		"host":      instance,
		"from_role": from,
		"to_role":   to,
	}

	// Role change is a one-shot transition (lastInRecovery already advanced); a
	// failure is recorded as an error event inside takeSnapshot.
	_ = d.takeSnapshot(ctx, cfg, cl, instance, database, TriggerRoleChange, "auto:role_change", trigCtx, nil)
}

func (d *Daemon) takeSnapshot(
	ctx context.Context,
	cfg Config,
	cl config.Cluster,
	instance, database string,
	trigType TriggerType,
	reason string,
	trigCtx map[string]any,
	bgPeak *BackgroundPeak,
) error {
	reports, err := d.repo.GetQueriesReport(ctx, string(cl.Name), instance, nil)
	if err != nil {
		d.insertEvent(ctx, TriggerEvent{
			ClusterName:    string(cl.Name),
			Instance:       instance,
			Database:       &database,
			TriggerType:    trigType,
			Outcome:        OutcomeError,
			TriggerContext: trigCtx,
			ErrorMessage:   strPtr(fmt.Sprintf("get report: %v", err)),
		})

		return err
	}

	var pgssReset *time.Time
	if t, err := d.repo.GetPgssStatsResetTime(ctx, string(cl.Name), instance, database); err == nil && t != nil {
		pgssReset = &t.Time
	}

	// Locks are captured only for activity spikes (contention correlates with load,
	// not role changes). Best-effort: capture failure never fails the snapshot.
	var locks *LockCapture
	if cfg.CaptureLocks && trigType == TriggerActivitySpike {
		lc := CaptureLocks(ctx, d.repo, string(cl.Name), instance, database, cfg.LockProbeCount, cfg.LockProbeInterval)
		lc.BackgroundPeak = bgPeak
		locks = &lc
	}

	id, createdAt, err := d.store.CreateSnapshot(ctx, string(cl.Name), instance, database, reports, SnapshotOpts{
		PgssStatsReset: pgssReset,
		Reason:         reason,
		TriggerContext: trigCtx,
		LocksData:      locks,
	})
	if err != nil {
		d.insertEvent(ctx, TriggerEvent{
			ClusterName:    string(cl.Name),
			Instance:       instance,
			Database:       &database,
			TriggerType:    trigType,
			Outcome:        OutcomeError,
			TriggerContext: trigCtx,
			ErrorMessage:   strPtr(fmt.Sprintf("create snapshot: %v", err)),
		})

		return err
	}

	d.mu.Lock()
	if d.lastAuto == nil {
		d.lastAuto = map[hostKey]time.Time{}
	}
	d.lastAuto[hostKey{Cluster: string(cl.Name), Instance: instance}] = createdAt
	d.mu.Unlock()

	if cfg.ResetQueryStats {
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
		Database:       &database,
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

	return nil
}

func (d *Daemon) debounced(key hostKey, maxFreq time.Duration) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	last, ok := d.lastAuto[key]
	if !ok {
		return false
	}

	return d.nowUTC().Sub(last) < maxFreq
}

// belowBaselineDue reports whether enough time has passed since the last
// below-baseline event for this host to record another (throttle window).
func belowBaselineDue(state *hostState, now time.Time, every time.Duration) bool {
	return state.lastBelowBaselineAt == nil || now.Sub(*state.lastBelowBaselineAt) >= every
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
