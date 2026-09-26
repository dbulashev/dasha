package healthscore

import (
	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/health"
	"github.com/dbulashev/dasha/internal/metrics"
)

// overlayCatalogFacts fills metrics-derived RawMetrics with catalog/GUC facts a
// Prometheus-style datasource cannot express (per-table autovacuum/vacuum state,
// relfrozenxid age, planner-stat drift, GUCs, wal_level, MVCC horizon, lock-pool
// sizing, in-recovery). Time-series-derived fields keep their metrics values;
// only the gaps the collector leaves neutral are written, so catalog-only rules
// (e.g. tables_with_autovacuum_off) keep firing — and the score keeps penalising
// them — even when a datasource is configured.
func overlayCatalogFacts(raw *health.RawMetrics, m *dto.HealthScoreMetrics) {
	raw.InRecovery = m.InRecovery
	raw.Database = m.Database

	// Connections — longest transaction is a catalog/activity fact, not scraped.
	raw.LongestTransactionSeconds = m.LongestTransactionSeconds

	// Which database each activity-derived finding belongs to: pg_stat_activity
	// only, so the datasource never carries it.
	raw.LongestTransactionDatabase = m.LongestTransactionDatabase
	raw.IdleInTransactionDatabase = m.IdleInTransactionDatabase

	// Performance — track_io_timing GUC.
	raw.TrackIoTimingEnabled = m.TrackIoTimingEnabled

	// Storage — bloat-table count and newpage-update ratio.
	raw.TablesHighBloat = m.TablesHighBloat
	raw.NewpageUpdateRatio = m.NewpageUpdateRatio

	// Maintenance — per-table autovacuum/vacuum state, relfrozenxid, GUCs.
	// The vacuum queue (backlog + overdue age) is snapshot-only (no metrics
	// signal), so it must be overlaid here or both vacuum rules would silently
	// drop out in metrics mode and break score↔rules parity.
	raw.VacuumBacklogTables = m.VacuumBacklogTables
	raw.MaxOverdueVacuumAgeHours = m.MaxOverdueVacuumAgeHours
	raw.TablesNeverVacuumed = m.TablesNeverVacuumed
	raw.TablesWithAutovacuumOff = m.TablesWithAutovacuumOff
	raw.MaxRelfrozenxidAge = m.MaxRelfrozenxidAge
	raw.StalePlannerStatsTables = m.StalePlannerStatsTables
	raw.AutovacuumEnabled = m.AutovacuumEnabled
	raw.TrackCountsEnabled = m.TrackCountsEnabled

	// Horizon — oldest backend_xmin pinning VACUUM.
	raw.HorizonLagXids = m.HorizonLagXids
	raw.HorizonDatabase = m.HorizonDatabase

	// WAL / checkpoint configuration.
	raw.WalLevel = m.WalLevel
	raw.LogicalSlotsActive = m.LogicalSlotsActive

	// Locks — heavyweight-pool sizing and longest wait (catalog/activity).
	raw.LongestLockWaitSeconds = m.LongestLockWaitSeconds
	raw.HeavyweightLocksTotal = m.HeavyweightLocksTotal
	raw.MaxLocksPerTransaction = m.MaxLocksPerTransaction
}

// overlaySignalGaps backfills, from the SQL snapshot, every field whose signal
// the datasource did not carry. ToRawMetrics seeds absent inputs to a neutral
// (healthy-reading) value — 100% cache hit, zero dead ratio, no wraparound age —
// which is right while nothing better is known, but the snapshot read a few
// lines up holds the real numbers. Without this, partial coverage scores green
// and silently drops the matching rules: a target where only the node/pooler
// role resolves still reports matched > 0, so the degraded flag never trips.
func overlaySignalGaps(raw *health.RawMetrics, m *dto.HealthScoreMetrics, sig metrics.Signals) {
	// The five below feed metricsInstanceWideRules, which drop the database
	// attribution because a datasource series aggregates every database. The
	// snapshot values are per-database (pg_statio_user_tables,
	// pg_stat_user_tables, datfrozenxid of current_database()), so a rule fed
	// from here must keep naming the database it was read from.
	if !sig.Has(metrics.SigCacheHitRatio) {
		raw.CacheHitRatio = m.CacheHitRatio
		raw.CacheSampleBlocks = m.CacheSampleBlocks
		raw.MarkSnapshotBacked("low_cache_hit_ratio")
	}

	if !sig.Has(metrics.SigMaxDeadRatio) {
		raw.MaxDeadRatio = m.MaxDeadRatio
		raw.MarkSnapshotBacked("high_max_dead_ratio")
	}

	if !sig.Has(metrics.SigAvgDeadRatio) {
		raw.AvgDeadRatio = m.AvgDeadRatio
		raw.MarkSnapshotBacked("high_avg_dead_ratio")
	}

	if !sig.Has(metrics.SigHotUpdateRatio) {
		raw.HotUpdateRatio = m.HotUpdateRatio
		raw.MarkSnapshotBacked("low_hot_update_ratio")
	}

	if !sig.Has(metrics.SigXactsLeftWrap) {
		raw.MaxXidAge = m.MaxXidAge
		raw.MarkSnapshotBacked("xid_wraparound_risk")
	}

	if !sig.Has(metrics.SigDeadlocksTotal) {
		raw.DeadlocksTotal = m.DeadlocksTotal
	}

	if !sig.Has(metrics.SigLocksNotGranted) {
		raw.UngrantedLocks = m.UngrantedLocks
	}

	if !sig.Has(metrics.SigActiveLockWaiters) {
		raw.ActiveLockWaiters = m.ActiveLockWaiters
	}

	if !sig.Has(metrics.SigTotalConns) {
		raw.TotalConnections = m.TotalConnections
	}

	if !sig.Has(metrics.SigActiveConns) {
		raw.ActiveConnections = m.ActiveConnections
	}

	if !sig.Has(metrics.SigIdleInTx) {
		raw.IdleInTransaction = m.IdleInTransaction
	}

	if !sig.Has(metrics.SigMaxConns) {
		raw.MaxConnections = m.MaxConnections
	}

	if !sig.Has(metrics.SigTimedCheckpoints) {
		raw.TimedCheckpoints = m.TimedCheckpoints
	}

	if !sig.Has(metrics.SigRequestedCheckpoints) {
		raw.RequestedCheckpoints = m.RequestedCheckpoints
	}

	// Replication is one fact split across two signals, and ToRawMetrics infers
	// ReplicaCount from either — so the snapshot only wins when neither arrived,
	// otherwise a lag-bytes-only datasource would lose its replica.
	if !sig.Has(metrics.SigReplLagSeconds) && !sig.Has(metrics.SigReplLagBytes) {
		raw.ReplicaCount = m.ReplicaCount
		raw.MaxReplayLagSeconds = m.MaxReplayLagSeconds
		raw.MaxLagBytes = m.MaxLagBytes
		raw.DisconnectedReplicas = m.DisconnectedReplicas
	}
}
