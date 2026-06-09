package metrics

import "fmt"

// QueryCatalog maps (provider, signal) to a MetricsQL template. Templates use
// fmt indexed verbs: %[1]s = inner label selector, %[2]s = rate window,
// %[3]s = optional role-exclusion fragment (leading comma, empty when unused).
type QueryCatalog struct {
	templates map[catalogKey]string
}

type catalogKey struct {
	provider Provider
	signal   SignalKind
}

// NewQueryCatalog returns the catalog seeded with the known templates.
func NewQueryCatalog() *QueryCatalog {
	t := map[catalogKey]string{
		// performance — pgSCV calls-weighted latency (ms = seconds*1000)
		{ProviderPgSCV, SigLatencyMs}: `1000 * sum(rate(postgres_statements_time_seconds_all_total{%[1]s}[%[2]s])) / ` +
			`clamp_min(sum(rate(postgres_statements_calls_total{%[1]s}[%[2]s])), 1)`,
		// performance — YC pooler p95 (already ms or s depending on source; normalized in provider)
		{ProviderYCNative, SigLatencyMs}: `pooler_query_0_95{%[1]s}`,

		// maintenance — wraparound countdown (worst across DBs)
		{ProviderPgSCV, SigXactsLeftWrap}: `min(postgres_xacts_left_before_wraparound{%[1]s})`,

		// data integrity — checksum failures rate over window
		{ProviderPgSCV, SigChecksumFailRate}: `sum(increase(postgres_database_checksum_failures_total{%[1]s}[%[2]s])) or on() vector(0)`,

		// === core categories (pgSCV; exact label names to confirm in demo-lab) ===
		// performance — buffer cache hit ratio (%)
		{ProviderPgSCV, SigCacheHitRatio}: `100 * sum(rate(postgres_database_blocks_total{%[1]s,access="hit"}[%[2]s])) / ` +
			`clamp_min(sum(rate(postgres_database_blocks_total{%[1]s}[%[2]s])), 1)`,
		// storage — worst per-table dead-tuple ratio (%)
		{ProviderPgSCV, SigMaxDeadRatio}: `100 * max(postgres_table_tuples_dead_total{%[1]s} / ` +
			`clamp_min(postgres_table_tuples_dead_total{%[1]s} + postgres_table_tuples_live_total{%[1]s}, 1))`,
		// maintenance — worst time since last vacuum (hours)
		{ProviderPgSCV, SigMaxVacuumAgeH}: `max(postgres_table_since_last_vacuum_seconds_total{%[1]s}) / 3600`,
		// storage — average dead-tuple ratio + HOT-update ratio
		{ProviderPgSCV, SigAvgDeadRatio}: `100 * avg(postgres_table_tuples_dead_total{%[1]s} / ` +
			`clamp_min(postgres_table_tuples_dead_total{%[1]s} + postgres_table_tuples_live_total{%[1]s}, 1))`,
		{ProviderPgSCV, SigHotUpdateRatio}: `sum(rate(postgres_table_tuples_hot_updated_total{%[1]s}[%[2]s])) / ` +
			`clamp_min(sum(rate(postgres_table_tuples_updated_total{%[1]s}[%[2]s])), 1)`,
		// performance — sequential-scan activity: tuples read by seq scans per second
		// (large tables dominate, so this weights "big-table seq scans" naturally).
		// Baselined seasonally; a regression flags missing index usage / stale stats.
		// Metric name per pgSCV internal/collector/postgres_tables.go.
		{ProviderPgSCV, SigSeqScanRate}: `sum(rate(postgres_table_seq_tup_read_total{%[1]s}[%[2]s]))`,
		// locks — cumulative deadlocks
		{ProviderPgSCV, SigDeadlocksTotal}: `sum(postgres_database_deadlocks_total{%[1]s})`,
		// wal/checkpoint — cumulative counts (the count-based penalty needs totals)
		{ProviderPgSCV, SigTimedCheckpoints}:     `sum(postgres_checkpoints_total{%[1]s,type="timed"})`,
		{ProviderPgSCV, SigRequestedCheckpoints}: `sum(postgres_checkpoints_total{%[1]s,type="req"})`,
		// locks — backends currently waiting on a heavyweight lock (label TBD in demo)
		{ProviderPgSCV, SigActiveLockWaiters}: `sum(postgres_activity_wait_events_in_flight{%[1]s,type="Lock"})`,
		// replication — worst lag (seconds / bytes)
		{ProviderPgSCV, SigReplLagSeconds}: `max(postgres_replication_lag_all_seconds{%[1]s})`,
		{ProviderPgSCV, SigReplLagBytes}:   `max(postgres_replication_lag_all_bytes{%[1]s})`,
		// locks — ungranted lock requests (snapshot)
		{ProviderPgSCV, SigLocksNotGranted}: `sum(postgres_locks_not_granted_in_flight{%[1]s})`,
		// connections
		{ProviderPgSCV, SigTotalConns}:  `sum(postgres_activity_connections_all_in_flight{%[1]s})`,
		{ProviderPgSCV, SigActiveConns}: `sum(postgres_activity_connections_in_flight{%[1]s,state="active"})`,
		{ProviderPgSCV, SigIdleInTx}:    `sum(postgres_activity_connections_in_flight{%[1]s,state="idle in transaction"})`,
		{ProviderPgSCV, SigMaxConns}:    `max(postgres_service_settings_info{%[1]s,name="max_connections"})`,

		// wal/checkpoint — requested checkpoints rate
		{ProviderPgSCV, SigCheckpointsReqRate}: `sum(rate(postgres_checkpoints_total{%[1]s,type="req"}[%[2]s]))`,
		{ProviderPgSCV, SigCheckpointsAllRate}: `sum(rate(postgres_checkpoints_total{%[1]s}[%[2]s]))`,

		// storage — bloat / dead (optional custom collectors)
		{ProviderPgSCV, SigMaxBloatRatio}: `max(postgres_table_bloat_ratio{%[1]s})`,

		// capacity — exhaustion (optional custom collectors)
		{ProviderPgSCV, SigSeqExhaustionMax}:  `max(postgres_schema_sequence_exhaustion_ratio{%[1]s})`,
		{ProviderPgSCV, SigTypeExhaustionMax}: `max(postgres_table_datatype_exhaustion_ratio{%[1]s})`,

		// host saturation — YC native (load / vcpu)
		{ProviderYCNative, SigLoadAvg15}: `max(load_avg_15min{%[1]s})`,
		{ProviderYCNative, SigNumVCPU}:   `max(n_cpus{%[1]s})`,
		// host disk — YC native: worst (fullest) mount, used/total (0..1).
		{ProviderYCNative, SigDiskUsedRatio}: `max(disk_used_bytes{%[1]s} / disk_total_bytes{%[1]s})`,
		// host saturation — pgSCV system collector (self-managed). NOTE: node_*
		// metrics may be host-scoped rather than per-service; confirm the label
		// scheme in demo-lab and adjust the pgscv_system selector if needed.
		{ProviderPgSCVSystem, SigLoadAvg15}: `max(node_load15{%[1]s})`,
		{ProviderPgSCVSystem, SigNumVCPU}:   `count(count by (cpu) (node_cpu_seconds_total{%[1]s}))`,
		// host disk — pgSCV system collector: worst real filesystem, used/total (0..1).
		// pgSCV exposes node_filesystem_bytes{usage="used"} and node_filesystem_bytes_total
		// (labels device/mountpoint/fstype; the collector already filters to ext3/ext4/
		// xfs/btrfs). The numerator carries an extra `usage` label, so join with
		// ignoring(usage). See pgSCV internal/collector/linux_filesystem.go.
		{ProviderPgSCVSystem, SigDiskUsedRatio}: `max(node_filesystem_bytes{%[1]s,usage="used"} / ` +
			`ignoring(usage) node_filesystem_bytes_total{%[1]s})`,

		// pooler saturation — self-managed pgbouncer
		{ProviderPgBouncer, SigPoolerClients}:  `sum(pgbouncer_client_connections_in_flight{%[1]s})`,
		{ProviderPgBouncer, SigPoolerServers}:  `sum(pgbouncer_pool_connections_in_flight{%[1]s})`,
		{ProviderPgBouncer, SigPoolerPoolSize}: `sum(pgbouncer_service_database_pool_size{%[1]s})`,
		// connection saturation — YC native exposes per-role session and conn_limit
		// gauges (RoleLabel = role). The worst role's sessions/conn_limit ratio is
		// computed in PromQL with service roles dropped via %[3]s; conn_limit>0 skips
		// roles left at the PG default -1 (unlimited). pool_size is a presence
		// sentinel (1 when any bounded role exists), so the generic servers/pool_size
		// saturation rule consumes the ready-made ratio unchanged.
		{ProviderYCNative, SigPoolerServers}:  `max(postgres_role_sessions{%[1]s%[3]s} / (postgres_role_conn_limit{%[1]s%[3]s} > 0))`,
		{ProviderYCNative, SigPoolerPoolSize}: `clamp_max(count(postgres_role_conn_limit{%[1]s%[3]s} > 0), 1)`,
	}

	return &QueryCatalog{templates: t}
}

// Expr renders the expression for (provider, signal). Returns ("", false) when
// the pair is not catalogued.
func (c *QueryCatalog) Expr(p Provider, s SignalKind, selector, window, exclude string) (string, bool) {
	tpl, ok := c.templates[catalogKey{p, s}]
	if !ok {
		return "", false
	}

	return fmt.Sprintf(tpl, selector, window, exclude), true
}

// Supports reports whether the catalog can build a query for (provider, signal).
func (c *QueryCatalog) Supports(p Provider, s SignalKind) bool {
	_, ok := c.templates[catalogKey{p, s}]

	return ok
}

// ValidationMetric returns the metric used to confirm a target matches under a
// provider (exactly one series expected), or "" when none is defined yet.
func ValidationMetric(p Provider) string {
	switch p {
	case ProviderPgSCV:
		return "postgres_up"
	case ProviderYCNative:
		// host role; pooler validated separately via pooler_is_alive in the matcher.
		return "n_cpus"
	case ProviderPgBouncer:
		return "pgbouncer_up"
	case ProviderPgSCVSystem:
		return "node_load15" // pgSCV system collector (loadaverage)
	default:
		return ""
	}
}
