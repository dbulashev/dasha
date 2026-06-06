# Health Score

A composite metric (0–100) summarising overall PostgreSQL instance health across eight categories. Higher is better.

## Formula

```text
score = 100 − Σ (penalty_i × weight_i)
clamp(0..100)
if a critical condition is present: score = min(score, 30)
```

For every category `i` Dasha computes a continuous **penalty** (0..100) from raw metrics and combines them with per-category **weights** that sum to 1.0. Weights are validated and normalised; invalid input falls back to defaults.

Categories with no signal on this instance are **dropped** and their weight is redistributed proportionally across the remaining categories, so missing signal does not artificially inflate or deflate the score:

- `replication` — dropped when the instance has no replicas.
- `maintenance` — dropped when `pg_is_in_recovery()` is true (the instance is a standby). Autovacuum and ANALYZE cannot run on a standby, so vacuum age, XID age and the maintenance GUCs reflect primary state observed from the replica — any action belongs on the primary. The corresponding rules are also hidden from recommendations.

### Critical conditions (score floor)

A plain weighted average dilutes catastrophes: imminent transaction-ID wraparound moves the `maintenance` category by at most its weight (~15 points), so a database minutes from a forced shutdown would otherwise still show ~85/100 next to a HIGH wraparound recommendation. To keep the headline number honest, any of the following clamps the score into the red band (`min(score, 30)`):

- **transaction-ID wraparound at failsafe** — `max(age(datfrozenxid), age(relfrozenxid)) ≥ 1.6 B` (`vacuum_failsafe_age`), where PostgreSQL itself enters emergency VACUUM and skips index cleanup to race the ~2.1 B shutdown wall;
- **autovacuum globally off** (`autovacuum=off`) — dead tuples and XID age grow unbounded;
- **track_counts off** (`track_counts=off`) — autovacuum is blind and effectively never triggers.

The floor is gated on primaries (`pg_is_in_recovery() = false`): a standby cannot run autovacuum and inherits its frozen-xid horizon from the primary, so the action — and the red score — belong there. These same conditions also surface as HIGH recommendations, so the number and the action list stay in lockstep.

In parallel, a **rules engine** evaluates the same metrics and emits a list of LOW / MEDIUM / HIGH recommendations with deep-links to the relevant Dasha page. Rules and penalties are independent: penalties feed the numeric score, rules feed the action list. Every rule has a matching score contribution (a penalty term or the floor), so a condition can never appear in the recommendation list while leaving the number untouched.

## Categories and default weights

| Category        | Weight | What it measures                                                  |
|-----------------|--------|-------------------------------------------------------------------|
| `connections`   | 0.15   | Connection utilisation, idle-in-tx, long-running transactions     |
| `performance`   | 0.15   | Cache hit ratio, `track_io_timing`                                |
| `storage`       | 0.10   | Dead-tuple ratio, bloat, HOT-update efficiency                    |
| `replication`   | 0.15   | Lag (time and bytes), disconnected standbys                       |
| `maintenance`   | 0.15   | XID age, vacuum freshness, autovacuum/track_counts GUCs, ANALYZE  |
| `horizon`       | 0.10   | MVCC horizon lag (oldest snapshot blocking VACUUM)                |
| `wal_checkpoint`| 0.10   | Requested vs. timed checkpoints, `wal_level` mismatch             |
| `locks`         | 0.10   | Lock-waiters, ungranted locks, deadlocks, lock-pool saturation    |


## Penalty thresholds (overview)

Penalties grow smoothly with the metric. A **breakpoint** is the metric value at which the slope of the penalty function changes: before the first one the penalty is zero, between the points it grows linearly, after the last one it reaches the category's maximum. The `→` arrows in the right column read exactly that way: first breakpoint → second → third.

| Category       | Metric                              | Breakpoints (no penalty → full penalty)        |
|----------------|--------------------------------------|------------------------------------------------|
| connections    | `total / max_connections`           | 0.60 → 0.80 → 0.95+                             |
| connections    | `idle_in_transaction` (count)       | linear 5 pts each, capped at 30                |
| connections    | `longest_transaction_seconds`       | >300 s, capped at 20 pts                        |
| performance    | `cache_hit_ratio` (%)               | ≥95 → ≥90 → ≥85 → below                         |
| performance    | `track_io_timing` off               | flat 5 pts (LOW)                               |
| storage        | `max_dead_ratio` (%)                | ≤20 → 20–30 → >30                               |
| storage        | `avg_dead_ratio` (%)                | >15 adds up to 30 pts                           |
| storage        | `tables_high_bloat` (count)         | >5 adds up to 30 pts                            |
| storage        | `hot_update_ratio`                  | <0.80 → <0.65 → <0.50 (5 / 15 / 30 pts)        |
| storage        | `newpage_update_ratio` (PG 16+)     | >0.05 → >0.10 → >0.20 (5 / 10 / 20 pts)        |
| replication    | `max_replay_lag_seconds`            | >10 s ramps up to full                          |
| replication    | `max_lag_bytes`                     | >16 MiB ramps up to full                        |
| replication    | `disconnected_replicas`             | each disconnect adds 25 pts                     |
| maintenance    | `max(xid_age, relfrozenxid_age)`    | 200 M → 1.6 B → 2.1 B (escalates to 100)       |
| maintenance    | `max_vacuum_age_hours`              | >168 h → >504 h → >1440 h (7/21/60 days)        |
| maintenance    | `tables_never_vacuumed`             | each table adds 5 pts, capped at 20             |
| maintenance    | `tables_with_autovacuum_off`        | 3 pts each, capped at 15                        |
| maintenance    | `stale_planner_stats_tables`        | 2 pts each, capped at 15                        |
| maintenance    | `autovacuum` / `track_counts` off   | saturates the category (also floors the score) |
| horizon        | `horizon_lag_xids`                  | 1 M → 10 M → 100 M                               |
| wal_checkpoint | `requested / total_checkpoints`     | ≥5 % → ≥10 % → ≥20 %                            |
| wal_checkpoint | `wal_level` mismatch                | minimal+replicas 80 pts; logical+no slot 5 pts |
| locks          | weighted sum of all lock factors    | see `penaltyLocks` (accumulative)              |

The transaction-ID penalty is calibrated against PostgreSQL's freeze machinery: it starts at `autovacuum_freeze_max_age` (200 M, emergency autovacuum), reaches 80 at `vacuum_failsafe_age` (1.6 B) and 100 at the ~2.1 B shutdown wall — so it keeps climbing through the danger zone instead of flat-lining. The `relfrozenxid_age_outlier` rule shares the same curve via `max(datfrozenxid, relfrozenxid)`. Every rule listed below maps to one of these penalty terms or to the critical floor, so the score and the recommendations cover the same conditions.

## Rules and severity (recommendations)

Rules emit when the metric crosses a discrete LOW / MEDIUM / HIGH threshold. They are filtered by scope:

- instance-only categories (`connections`, `replication`, `horizon`, `wal_checkpoint`, `locks`) are hidden on the per-database drill down (detail view);
- the whole `maintenance` category is hidden on standbys (`pg_is_in_recovery() = true`) — mirrors the maintenance-weight drop in the score.

Each bullet: what's measured / how it's computed, then LOW / MEDIUM / HIGH thresholds.

### Connections
- `high_connection_ratio` — `count(*) from pg_stat_activity / max_connections`. Headroom before the server starts rejecting new sessions. Thresholds ≥0.70 / ≥0.85 / ≥0.95.
- `idle_in_transaction` — sessions in `pg_stat_activity` with `state='idle in transaction'`. Each one holds locks and pins the MVCC horizon, blocking VACUUM. Thresholds ≥2 / ≥5 / ≥10.
- `long_running_transaction` — `now() - xact_start` of the longest running transaction. Long transactions amplify bloat and prevent freezing. Thresholds ≥300 / ≥600 / ≥1800 seconds.

### Performance
- `low_cache_hit_ratio` — `heap_blks_hit / (heap_blks_hit + heap_blks_read)` over `pg_statio_user_tables`, in %. Share of page reads served from `shared_buffers` rather than the OS / disk. Thresholds <95 / <90 / <85.
- `track_io_timing_disabled` — GUC `track_io_timing` is off, so `pg_stat_statements.*_blk_*_time` are always zero and slow-query I/O cannot be analysed. LOW.

### Storage
- `high_max_dead_ratio` — worst per-table `n_dead_tup / NULLIF(n_live_tup + n_dead_tup, 0)` from `pg_stat_user_tables`, in %. Identifies a table autovacuum can't keep clean. Thresholds ≥10 / ≥20 / ≥30.
- `high_avg_dead_ratio` — same ratio averaged across tables with > 1000 live tuples. Background bloat level. Thresholds ≥5 / ≥15 / ≥25.
- `many_bloated_tables` — number of tables whose dead ratio exceeds the autovacuum trigger (`autovacuum_vacuum_scale_factor`). Thresholds ≥5 / ≥10 / ≥20.
- `low_hot_update_ratio` — `n_tup_hot_upd / NULLIF(n_tup_upd, 0)` over all user tables. Lower means UPDATEs allocate new tuples and rewrite every index, bloating indexes. Thresholds <0.80 / <0.65 / <0.50.
- `high_newpage_update_ratio` — `n_tup_newpage_upd / NULLIF(n_tup_upd, 0)` (PG 16+). Share of UPDATEs that broke a HOT chain by placing the new tuple on a fresh page. Thresholds ≥0.05 / ≥0.10 / ≥0.20.

### Replication
- `replication_lag_time` — `EXTRACT(EPOCH FROM replay_lag)` of the worst row in `pg_stat_replication`. How far behind in WAL replay any standby is. Thresholds ≥10 / ≥60 / ≥300 seconds.
- `replication_lag_bytes` — `pg_current_wal_lsn() - replay_lsn`, worst standby. Backlog of WAL still to apply. Thresholds ≥16 MiB / ≥256 MiB / ≥1 GiB.
- `disconnected_replicas` — replicas configured in `dasha.yaml` (or discovered) but not present in `pg_stat_replication`. Thresholds ≥1 / ≥2 / ≥3.

### Maintenance
- `xid_wraparound_risk` — `max(age(datfrozenxid))` across `pg_database`. Number of transactions until wraparound forces shutdown. Calibrated against `autovacuum_freeze_max_age=200M` (autovacuum should already be in anti-wraparound mode) and the 2 B hard limit. Thresholds ≥150 M / ≥200 M / ≥1.6 B.
- `stale_vacuum` — days since the most recent `last_vacuum`/`last_autovacuum` over user tables. Detects stalled autovacuum. Thresholds ≥7 / ≥21 / ≥60 days.
- `tables_never_vacuumed` — tables with both `last_vacuum IS NULL` and `last_autovacuum IS NULL`. Thresholds ≥1 / ≥2 / ≥5.
- `autovacuum_disabled` — global GUC `autovacuum=off`. Bloat and XID age grow unchecked. HIGH.
- `track_counts_disabled` — global GUC `track_counts=off`. Autovacuum has no statistics to act on and effectively stops. HIGH.
- `tables_with_autovacuum_off` — tables with `autovacuum_enabled=false` in `pg_class.reloptions`. Thresholds ≥1 / ≥5 / ≥20.
- `relfrozenxid_age_outlier` — worst per-table `age(relfrozenxid)` from `pg_class`. Per-table flavour of `xid_wraparound_risk`. Thresholds ≥200 M / ≥500 M / ≥1 B.
- `stale_planner_stats` — tables whose `n_mod_since_analyze` is large relative to `n_live_tup` (planner has outdated stats). Thresholds ≥3 / ≥10 / ≥30 tables.

### Horizon
- `horizon_lag_xids` — `txid_current() - min(backend_xmin)` over `pg_stat_activity`. Number of transactions VACUUM cannot reclaim because some session still sees them (long tx, abandoned replication slot, prepared tx). Thresholds ≥1 M / ≥10 M / ≥100 M.

### WAL / checkpoints
- `requested_checkpoint_ratio` — `checkpoints_req / (checkpoints_req + checkpoints_timed)` from `pg_stat_bgwriter` (PG <17) / `pg_stat_checkpointer` (PG 17+). High share means `max_wal_size` is undersized or there's a write spike. Needs ≥10 samples. Thresholds ≥5 % / ≥10 % / ≥20 %.
- `wal_level_minimal_with_replicas` — GUC `wal_level=minimal` cannot drive physical replication; any standby is silently broken. HIGH.
- `wal_level_logical_without_publications` — GUC `wal_level=logical` is configured but `pg_publication` is empty; the extra WAL volume buys nothing. LOW.

### Locks
- `active_lock_waiters` — sessions in `pg_stat_activity` with `wait_event_type='Lock'`. They are blocked right now. Thresholds ≥1 / ≥3 / ≥10.
- `longest_lock_wait_seconds` — `EXTRACT(EPOCH FROM now() - state_change)` of the longest current Lock-wait. Thresholds ≥10 / ≥30 / ≥60 seconds.
- `ungranted_locks` — rows in `pg_locks` with `granted=false`. Queued lock requests piling up behind a holder. Thresholds ≥2 / ≥5 / ≥15.
- `deadlocks_rate` — `deadlocks` from `pg_stat_database` (cumulative since `pg_stat_database_reset`). Without per-day normalisation only "non-zero" is actionable. LOW when total > 0.
- `lock_pool_saturation` — `count(*) from pg_locks` divided by `max_connections × max_locks_per_transaction` (size of the heavyweight-lock shared pool). Thresholds ≥0.4 / ≥0.6 / ≥0.8.

## Drill down (per-database detail view)

The "Databases" table collects the same metrics on a per-DB basis as it does for the instance: cache hit ratio, dead tuples, vacuum age. Each row is aggregated into a `DatabaseScore`. The rules engine is re-run in database scope, hiding instance-only categories. The UI table is sortable by size or score and lets the user pin a database as the recommendation context.
