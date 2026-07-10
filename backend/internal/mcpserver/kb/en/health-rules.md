# Health score rules & thresholds

The health score is 0–100 (higher is better): `100 − Σ(category_penalty × weight)`.
Default category weights: connections 0.15, performance 0.15, replication 0.15,
maintenance 0.15, storage 0.10, horizon 0.10, wal_checkpoint 0.10, locks 0.10.
Score bands: ≥80 healthy, 40–79 degraded, <40 critical.

**Critical ceiling:** one catastrophic condition clamps the whole score to ≤30
regardless of other categories: XID age past the failsafe zone, any checksum
failure, a sequence ≥95% exhausted, or host disk ≥90% full.

**Key XID constants:** 150M — aggressive freezing starts
(vacuum_freeze_table_age); 200M — forced anti-wraparound autovacuum
(autovacuum_freeze_max_age); 1.6B — VACUUM failsafe (skips index cleanup);
~2.1B — server refuses new XIDs (downtime, single-user VACUUM).

Below: every rule ID as reported by get_health_recommendations, when it fires
(LOW / MED / HIGH), and the first action to take.

## connections (weight 0.15)

### high_connection_ratio
Used share of max_connections. LOW ≥60%, MED ≥80%, HIGH ≥95%.
First: `connections` — find who holds them (leaking app pool?); consider pgbouncer.

### idle_in_transaction
Sessions idle in transaction longer than 30s. LOW ≥2, MED ≥5, HIGH ≥10.
They hold locks and the xmin horizon (VACUUM cannot clean). First:
`running_queries` — find the oldest, terminate it (pg_terminate_backend),
set idle_in_transaction_session_timeout.

### long_running_transaction
Oldest transaction age. LOW ≥300s, MED ≥600s, HIGH ≥1800s.
First: `running_queries` — decide whether to terminate; long transactions
also hold the horizon (see horizon_lag_xids).

### host_cpu_saturation
15-min load average / vCPU (metrics mode only). LOW ≥1, MED ≥2, HIGH ≥4.
Run queue exceeds cores — latency grows. First: `top_queries` (by=time).

### pooler_saturation
Pooler server connections / pool capacity (metrics mode only).
LOW ≥50%, MED ≥60%, HIGH ≥80%. Clients start queueing. First: `connections`.

## performance (weight 0.15)

### low_cache_hit_ratio
Instance cache hit ratio. LOW <95%, MED <90%, HIGH <85% (relaxed on purpose:
large sequential scans on OLAP make lower values normal).
First: `top_queries` — find disk-heavy queries; consider shared_buffers (~25% RAM).

### track_io_timing_disabled
track_io_timing=off. Always LOW. Without it EXPLAIN ANALYZE and
pg_stat_statements lack I/O timings. Overhead is minimal on modern OS — enable it.

### latency_regression
Current query latency vs its seasonal baseline (metrics mode only).
LOW >1.5×, MED >3×, HIGH >6×. First: `top_queries` (by=time), compare with a
snapshot via `query_compare`.

### seq_scan_regression
Rows read by seq scans vs seasonal baseline (metrics mode only).
LOW >1.5×, MED >3×, HIGH >6×. Planner left an index (stale stats) or an index
is missing/invalid. First: `list_indexes` (kind=usage, then missing); ANALYZE hot tables.

## storage (weight 0.10)

### high_max_dead_ratio
Worst table dead-tuple ratio. LOW ≥10%, MED ≥20%, HIGH ≥30%.
First: `top_tables` / `describe_table` — VACUUM ANALYZE the worst table.

### high_avg_dead_ratio
Average dead ratio across tables. LOW ≥5%, MED ≥15%, HIGH ≥25%.
Autovacuum is not keeping up: tune autovacuum_vacuum_scale_factor / cost_limit.

### many_bloated_tables
Tables with dead ratio >20%. LOW ≥5, MED ≥10, HIGH ≥20. First: `vacuum_danger`
and per-table VACUUM.

### low_hot_update_ratio
Share of HOT updates. LOW <80%, MED <65%, HIGH <50%. Non-HOT updates touch
every index (bloat). Lower fillfactor (70–90%) on hot tables whose updates
do not change indexed columns.

### high_newpage_update_ratio
PG16+: updates that had to move to a new page (broken HOT chain).
LOW ≥5%, MED ≥10%, HIGH ≥20%. Lower fillfactor on the affected table.

### checksum_failures
Any data-page checksum failure. Always HIGH and clamps the score ≤30:
data corruption. Check disks and backups immediately.

### sequence_exhaustion
Worst sequence usage vs its type limit (e.g. int4 PK). LOW ≥75%, MED ≥85%,
HIGH ≥95% (≥95% also clamps score ≤30). Plan bigint migration before writes stop.

### host_disk_space
Fullest host filesystem (metrics mode only). LOW ≥70%, MED ≥80%, HIGH ≥90%
(≥90% clamps score ≤30 — a full data volume stops writes and can corrupt data).
Free WAL/logs/temp, check bloat and inactive replication slots (`get_replication`).

## replication (weight 0.15)

### replication_lag_time
Max standby replay lag. LOW ≥1s, MED ≥5s, HIGH ≥30s.
First: `get_replication` — check standby load (heavy SELECTs), network.

### replication_lag_bytes
Max standby lag in bytes. LOW ≥10MB, MED ≥100MB, HIGH ≥1GB.
Growing lag → check WAL apply speed on the standby, max_wal_size.

### disconnected_replicas
Standbys not streaming. MED =1, HIGH ≥2. Check network from standby to
primary and walreceiver status; an inactive slot retains WAL (disk risk).

## maintenance (weight 0.15) — dropped on standbys (autovacuum cannot run there)

### xid_wraparound_risk
Max database XID age. LOW ≥150M, MED ≥200M, HIGH ≥1.6B (see constants above;
past 1.6B the score is clamped ≤30). First: `vacuum_danger`; VACUUM FREEZE the
worst databases, kill horizon-holding transactions.

### relfrozenxid_age_outlier
Max per-table relfrozenxid age; same thresholds as xid_wraparound_risk.
Tables skipped by autovacuum freeze — find via `vacuum_danger`, VACUUM FREEZE them.

### stale_vacuum
Oldest table already past its autovacuum threshold but not vacuumed.
LOW ≥7 days, MED ≥21, HIGH ≥60. Autovacuum starved: tighten scale_factor for
large tables, check worker settings.

### vacuum_backlog
Tables currently eligible for autovacuum (queue depth). LOW ≥6, MED ≥15, HIGH ≥30.
Raise autovacuum_max_workers / cost_limit, lower cost_delay.

### tables_never_vacuumed
Tables never vacuumed at all. LOW ≥1, MED ≥2, HIGH ≥5. Run VACUUM ANALYZE;
check per-table autovacuum_enabled.

### autovacuum_disabled
autovacuum=off globally. Always HIGH. Dead tuples accumulate unbounded — enable it.

### track_counts_disabled
track_counts=off. Always HIGH: autovacuum is blind without usage stats — enable it.

### tables_with_autovacuum_off
Tables with reloptions autovacuum_enabled=false. Always LOW. Usually leftovers
from old migrations — verify each is intentional.

### stale_planner_stats
Tables changed >50% since last ANALYZE and not analyzed for >1 day.
LOW ≥3, MED ≥5, HIGH ≥10. Bad plans likely: ANALYZE them, or lower
autovacuum_analyze_scale_factor.

## horizon (weight 0.10)

### horizon_lag_xids
How far the oldest snapshot/transaction holds the vacuum horizon behind,
in transactions. LOW ≥1M, MED ≥10M, HIGH ≥100M. VACUUM sees dead tuples as
"not yet removable". First: `running_queries` — find the backend with the
oldest xmin (long transaction, idle-in-transaction, stale replication slot)
and terminate/fix it.

## wal_checkpoint (weight 0.10)

### requested_checkpoint_ratio
Share of requested (non-timed) checkpoints; evaluated only after ≥10 checkpoints.
LOW ≥5%, MED ≥10%, HIGH ≥20%. WAL fills max_wal_size before checkpoint_timeout:
raise max_wal_size. Healthy systems have almost only timed checkpoints.

### wal_level_minimal_with_replicas
wal_level=minimal while standbys are connected. Always HIGH: streaming
replication cannot work — set wal_level=replica (restart required).

### wal_level_logical_without_publications
wal_level=logical with no active logical slots. Always LOW: pure WAL overhead —
switch to replica unless logical replication is planned. Suppressed on managed
platforms (e.g. Yandex MDB) where wal_level is fixed by the provider.

## locks (weight 0.10)

### active_lock_waiters
Backends currently waiting on locks. LOW ≥1, MED ≥3, HIGH ≥10.
First: `blocked_queries` — walk the blocking tree, judge the blocker (never
kill the victim).

### longest_lock_wait_seconds
Longest current lock wait. LOW ≥10s, MED ≥30s, HIGH ≥60s. Typical causes:
long transaction, hot-row contention, table lock from non-CONCURRENTLY DDL.

### ungranted_locks
pg_locks rows with granted=false (queue length). LOW ≥2, MED ≥5, HIGH ≥15.

### deadlocks_rate
Deadlocks since pg_stat_database_reset. LOW when >0. Any deadlock is an app
bug (inconsistent lock ordering) — details are in the server log
(`search_logs` preset on Yandex MDB clusters).

### lock_pool_saturation
Heavyweight-lock pool usage (max_locks_per_transaction × max_connections).
LOW ≥50%, MED ≥60%, HIGH ≥80%. Overflow raises "out of shared memory" —
raise max_locks_per_transaction (restart) or reduce concurrency.
