# Health Score

One number from 0 to 100 for how a PostgreSQL instance is doing. 100 means nothing is wrong; the lower it goes, the more there is. It is made up of eight categories.

## Formula

```text
score = 100 − Σ (penalty_i × weight_i)
clamp(0..100)
if a critical condition is present: score = min(score, 30)
```

Each category earns a **penalty** between 0 and 100 that grows with its metric, without steps. The score is 100 minus those penalties, each taken with its category **weight**; the weights add up to one. Weights configured wrongly fall back to the defaults.

When there is nothing to measure on this instance, the category **does not count** towards the score and its weight is shared out among the rest — otherwise missing data would read as a problem:

- `replication` — when the instance has no replicas: there is no lag to measure.
- `maintenance` — on a replica (`pg_is_in_recovery() = true`). Autovacuum and ANALYZE do not run there, so vacuum age, XID age and the autovacuum settings describe the primary rather than the replica — and the primary is where the fixing happens. Those rules drop out of the recommendations as well.

### When the score drops straight into the red

An average across categories can hide a disaster. Imminent transaction-ID wraparound moves the `maintenance` category by at most its weight — around 15 points — so a database minutes from a forced shutdown would show ~85 out of 100 while sitting next to a HIGH wraparound recommendation. To stop the headline number from lying, any of the conditions below pulls the score into the red band (`min(score, 30)`):

- **transaction-ID wraparound at failsafe** — `max(age(datfrozenxid), age(relfrozenxid)) ≥ 1.6 B` (`vacuum_failsafe_age`), where PostgreSQL itself enters emergency VACUUM and skips index cleanup to race the ~2.1 B shutdown wall;
- **autovacuum globally off** (`autovacuum=off`) — dead tuples and XID age grow unbounded;
- **track_counts off** (`track_counts=off`) — autovacuum is blind and effectively never triggers;
- **sequence exhaustion** — under 5 % of a sequence's values left (the `schema_lint` error threshold), where INSERT is about to start failing;
- **a page failed its checksum** — any non-zero counter in `pg_stat_database`, which means data corruption;
- **host disk almost full** — the fullest filesystem ≥90 % used (metrics-backed mode).

The first four are checked on primaries only (`pg_is_in_recovery() = false`): a replica does not run autovacuum, gets its frozen-xid horizon from the primary and never advances a sequence — so both the red score and the work behind it belong to the primary. Corrupted pages and a full disk do not care about the role: they pull the score down on any instance, replicas included. All of these also show up as HIGH recommendations.

The same metrics are read in parallel by a **set of rules**, which produce the LOW / MEDIUM / HIGH recommendations with links to the relevant Dasha page. The score answers "how bad is it", the rules answer "what exactly to do". They share the metrics, but the mapping is not strict: `sequence_exhaustion` and `checksum_failures` carry no penalty of their own and move the score only through the red band, while `high_avg_dead_ratio` and `many_bloated_tables` show up as recommendations before the category penalty starts to grow.

## Categories and default weights

| Category        | Weight | What it measures                                                  |
|-----------------|--------|-------------------------------------------------------------------|
| `connections`   | 0.15   | Connection utilisation, idle-in-tx, long-running transactions     |
| `performance`   | 0.15   | Cache hit ratio, `track_io_timing`                                |
| `storage`       | 0.10   | Dead-tuple ratio, bloat, HOT-update efficiency                    |
| `replication`   | 0.15   | Lag (time and bytes), disconnected standbys                       |
| `maintenance`   | 0.15   | XID age, vacuum backlog & freshness, autovacuum/track_counts GUCs, ANALYZE  |
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
| replication    | `max_replay_lag_seconds`            | >1 s → >5 s → >30 s (up to 50 pts)              |
| replication    | `max_lag_bytes`                     | >100 MiB — up to 30 pts                         |
| replication    | `disconnected_replicas`             | 30 pts per replica, capped at 60                |
| maintenance    | `max(xid_age, relfrozenxid_age)`    | 200 M → 1.6 B → 2.1 B (escalates to 100)       |
| maintenance    | `vacuum_backlog_tables`             | >5 tables → +1.5 pts each, capped at 15         |
| maintenance    | `max_overdue_vacuum_age_hours`      | >168 h → >504 h → >1440 h (7/21/60 days)        |
| maintenance    | `tables_never_vacuumed`             | each table adds 5 pts, capped at 20             |
| maintenance    | `tables_with_autovacuum_off`        | 3 pts each, capped at 15                        |
| maintenance    | `stale_planner_stats_tables`        | 2 pts each, capped at 15                        |
| maintenance    | `autovacuum` / `track_counts` off   | maxes out the category (and pulls the score into the red) |
| horizon        | `horizon_lag_xids`                  | 1 M → 10 M → 100 M                               |
| wal_checkpoint | `requested / total_checkpoints`     | ≥5 % → ≥10 % → ≥20 %                            |
| wal_checkpoint | `wal_level` mismatch                | minimal+replicas 80 pts; logical+no slot 5 pts |
| locks          | weighted sum of all lock factors    | accumulative: the factors add up               |

The transaction-ID penalty is calibrated against PostgreSQL's freeze machinery: it starts at `autovacuum_freeze_max_age` (200 M, emergency autovacuum), reaches 80 at `vacuum_failsafe_age` (1.6 B) and 100 at the ~2.1 B shutdown wall — so it keeps climbing through the danger zone instead of flat-lining. The `relfrozenxid_age_outlier` rule shares the same curve via `max(datfrozenxid, relfrozenxid)`. Every rule below matches one of these penalties or one of the red-band conditions.

## Rules and how serious they are (recommendations)

A rule fires when its metric crosses the LOW, MEDIUM or HIGH threshold. What is shown depends on the scope:

- instance-wide categories (`connections`, `replication`, `horizon`, `wal_checkpoint`, `locks`) are not shown once a single database is selected;
- the whole `maintenance` category is hidden on a replica (`pg_is_in_recovery() = true`).

Each bullet: what's measured / how it's computed, then LOW / MEDIUM / HIGH thresholds.

### Connections
- `high_connection_ratio` — `count(*) from pg_stat_activity / max_connections`. Headroom before the server starts rejecting new sessions. Thresholds ≥0.60 / ≥0.80 / ≥0.95.
- `idle_in_transaction` — sessions in `pg_stat_activity` with `state='idle in transaction'` for over 30 seconds. Each one holds locks and pins the MVCC horizon, blocking VACUUM. Thresholds ≥2 / ≥5 / ≥10.
- `long_running_transaction` — `now() - xact_start` of the longest running transaction. Long transactions amplify bloat and prevent freezing. Thresholds ≥300 / ≥600 / ≥1800 seconds.

### Performance
- `low_cache_hit_ratio` — `heap_blks_hit / (heap_blks_hit + heap_blks_read)` over `pg_statio_user_tables`, in %. Share of page reads served from `shared_buffers` rather than the OS / disk. Thresholds <95 / <90 / <85.
- `track_io_timing_disabled` — GUC `track_io_timing` is off, so `pg_stat_statements.*_blk_*_time` are always zero and slow-query I/O cannot be analysed. LOW.

### Storage
- `high_max_dead_ratio` — worst per-table `n_dead_tup / NULLIF(n_live_tup + n_dead_tup, 0)` from `pg_stat_user_tables`, in %. Identifies a table autovacuum can't keep clean. Thresholds ≥10 / ≥20 / ≥30.
- `high_avg_dead_ratio` — same ratio averaged across tables with > 1000 live tuples. Background bloat level. Thresholds ≥5 / ≥15 / ≥25.
- `many_bloated_tables` — number of tables with a dead ratio above 20 %, counting only tables over 10,000 rows. Thresholds ≥5 / ≥10 / ≥20.
- `low_hot_update_ratio` — `n_tup_hot_upd / NULLIF(n_tup_upd, 0)` over all user tables. Lower means UPDATEs allocate new tuples and rewrite every index, bloating indexes. Thresholds <0.80 / <0.65 / <0.50.
- `high_newpage_update_ratio` — `n_tup_newpage_upd / NULLIF(n_tup_upd, 0)` (PG 16+). Share of UPDATEs that broke a HOT chain by placing the new tuple on a fresh page. Thresholds ≥0.05 / ≥0.10 / ≥0.20.
- `sequence_exhaustion` — worst sequence against the ceiling that actually applies: a bigint sequence owned by an `integer` column is measured against 2147483647, not against its own `maxvalue`. Once the values run out, INSERT fails. Thresholds are stated as percent of values still free and are shared with the Schema Checks page (`schema_lint.sequence_thresholds`): <20 % free / <10 % / <5 %. Fed by the metrics datasource when one is configured, otherwise read from the system catalog for every database of the instance.

### Replication
- `replication_lag_time` — `EXTRACT(EPOCH FROM replay_lag)` of the worst row in `pg_stat_replication`. How far behind in WAL replay any standby is. Thresholds ≥1 / ≥5 / ≥30 seconds.
- `replication_lag_bytes` — `pg_current_wal_lsn() - replay_lsn`, worst standby. Backlog of WAL still to apply. Thresholds ≥10 MB / ≥100 MB / ≥1 GB.
- `disconnected_replicas` — replicas configured in `dasha.yaml` (or discovered) but not present in `pg_stat_replication`. MEDIUM at one, HIGH at two or more.

### Maintenance
- `xid_wraparound_risk` — `max(age(datfrozenxid))` across `pg_database`. Number of transactions until wraparound forces shutdown. Calibrated against `autovacuum_freeze_max_age=200M` (autovacuum should already be in anti-wraparound mode) and the 2 B hard limit. Thresholds ≥150 M / ≥200 M / ≥1.6 B.
- `stale_vacuum` — oldest `last_vacuum`/`last_autovacuum` age, in days, **among the backlog tables** (those past their autovacuum trigger). Static / read-mostly tables never enter the queue, so they no longer false-positive. Detects stalled autovacuum. Thresholds ≥7 / ≥21 / ≥60 days.
- `vacuum_backlog` — tables currently past their autovacuum trigger: `n_dead_tup` over `autovacuum_vacuum_threshold + autovacuum_vacuum_scale_factor·reltuples`, or `n_ins_since_vacuum` over the insert threshold. Per-table `reloptions` override the global GUCs (PostgreSQL's own trigger). The vacuum-queue depth — a deep queue means autovacuum is outpaced. Thresholds ≥6 / ≥15 / ≥30 tables.
- `tables_never_vacuumed` — tables with both `last_vacuum IS NULL` and `last_autovacuum IS NULL`. Thresholds ≥1 / ≥2 / ≥5.
- `autovacuum_disabled` — global GUC `autovacuum=off`. Bloat and XID age grow unchecked. HIGH.
- `track_counts_disabled` — global GUC `track_counts=off`. Autovacuum has no statistics to act on and effectively stops. HIGH.
- `tables_with_autovacuum_off` — tables with `autovacuum_enabled=false` in `pg_class.reloptions`. Thresholds ≥1 / ≥5 / ≥20.
- `relfrozenxid_age_outlier` — worst per-table `age(relfrozenxid)` from `pg_class`. Per-table flavour of `xid_wraparound_risk`. Thresholds ≥150 M / ≥200 M / ≥1.6 B.
- `stale_planner_stats` — tables whose `n_mod_since_analyze` exceeds their (reloption-aware) auto-analyze threshold and that have not been analyzed in 3 h (planner has outdated stats). Thresholds ≥3 / ≥5 / ≥10 tables.

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
- `deadlocks_rate` — the `deadlocks` counter from `pg_stat_database`, accumulating since the last `pg_stat_database_reset`. There is no per-day rate here, so the fact itself is what counts: above zero is already worth a look at the log. LOW when the total is > 0.
- `lock_pool_saturation` — `count(*) from pg_locks` divided by `max_connections × max_locks_per_transaction` (size of the heavyweight-lock shared pool). Thresholds ≥0.4 / ≥0.6 / ≥0.8.

## Per-database detail

The "Databases" table collects the same metrics per database as it does for the instance: cache hit ratio, dead row versions, vacuum age. Each row is aggregated into a `DatabaseScore`. The rules are recomputed in the context of the selected database, and instance-wide categories are hidden. The table sorts by size or score, and a database can be pinned as the context for the recommendations.

## Metrics-backed mode (history, baseline, richer signals)

By default the score is a **point-in-time SQL snapshot**. When a Prometheus/VictoriaMetrics-compatible datasource is configured (`health_score.metrics` in `dasha.yaml`), Dasha computes the **score**, **recommendations** and a **trend** from time-series metrics instead. The fallback is scoped to the **score and recommendations only**: if the datasource is unavailable or a target is not mapped, those revert to the point-in-time SQL snapshot, and the `source` field on `GET /api/common/health-score` reports which path produced the number. The **trend/history** endpoint (`GET /api/common/health-score/history`) has **no snapshot fallback** — it is time-series-only and returns **404** when metrics mode is disabled, unavailable, or the target mapping is missing.

Catalog and GUC facts that a time-series datasource cannot express — per-table `autovacuum_enabled=false`, never-vacuumed tables, `relfrozenxid` age, planner-stat drift, `wal_level`, the `autovacuum`/`track_counts` GUCs, the MVCC horizon and lock-pool sizing — are **overlaid from the SQL snapshot** onto the metrics signals. That keeps the catalog rules working in metrics mode too: the per-table `tables_with_autovacuum_off` recommendation does not silently disappear when a datasource is attached. If the SQL snapshot cannot be read, Dasha falls back to the pure snapshot score rather than emit a metrics-only number with zeroed catalog facts — which would misread as, say, "autovacuum off". (The historical **trend** stays time-series-only — catalog facts are "now" values, so the gauge may sit slightly below the latest trend point by the catalog penalty.)

### Providers and label matching

The score consumes a **normalized signal set**; provider adapters translate each source's metrics and labels:

| Role | Self-managed | Managed (Yandex MDB) |
|------|--------------|----------------------|
| PG internals (`core`) | pgSCV | pgSCV (remote scrape) |
| Pooler | pgbouncer (via pgSCV) | YC pooler |
| Host | pgSCV system collector | YC host metrics |

Label schemes differ per provider/deployment, so **selector templates are configurable** per target (`selectors:` + `targets:`). `GET /api/common/health-score/datasource/status?cluster_name=…&instance=…` reports, per role, the chosen provider, the rendered selector and how many series matched (exactly one = OK) — use it to validate matching.

**Service-discovered clusters** (from `discovery:`, e.g. Yandex MDB) are auto-mapped from their discovery metadata, so they need no `targets:` entry: the host FQDN becomes `{{.Host}}`, the cloud resource id (MDB cluster id) `{{.Service}}`, the `folder_id` label `{{.Env}}` and the short host `{{.Container}}`; providers come from `providers_default` (e.g. `core: pgscv`, `pooler/host: yc_native`). Only the selector templates stay your customization surface. A static `targets:` entry always overrides the derived mapping; set `auto_map_discovered: false` to require explicit targets, or `discovery_env_label` to feed `{{.Env}}` from a different discovery label.

### Trend, seasonal baseline and dips

`GET /api/common/health-score/history?cluster_name=…&instance=…&from=…&to=…&step_seconds=…` returns the per-timestamp overall score, per-category scores and latency over `[from, to]`. The chart on `/health-score` plots score, baseline and latency with dips marked.

#### What "seasonal baseline" means

Database load is almost always **cyclic**: weekdays differ from weekends, day from night, Monday 09:00 from Sunday 03:00. A flat average or a fixed threshold ignores this — it either cries wolf at the nightly batch or misses a real slowdown during the peak. The seasonal baseline is the **expected value of a metric for a given point in the weekly cycle**, not a global average. It is built as:

1. **Split the history by hour of the week.** Every sample falls into one of **168 slots** (7 days × 24 hours): `hour_of_week = weekday*24 + hour` (UTC).
2. **Take the median of each slot** over a longer window, 28 days by default. A median rather than a mean, because it is not thrown off by outliers: one nightly `VACUUM` or a deploy will not move the baseline.

The result is a "week profile": the normal score (and latency) for each hour of each weekday.

#### How it is used

The current value is compared to **its own norm for this hour-of-week**, not to a global average:

- **Dips:** "it is Tuesday 14:00, score 70, but Tuesdays at 14:00 are normally 92 → a 22-point dip" → marked on the trend. A regular nightly batch that drops the score is *not* flagged (its norm is low too) — no false alarm.
- **Latency regression** → `performance`: `current latency / seasonal baseline` answers "is this query slower than usual *for this time of week*", which works on any workload because it compares against itself, not an absolute `50/200/1000 ms` threshold.

Example: 50 ms at Monday 14:00 (norm 45 ms) is barely above normal; the same 50 ms at Monday 03:00 (norm 12 ms) is ~4× the norm — a real anomaly. One value, two verdicts.

The baseline and the dips appear as history accumulates; until there is enough, the chart simply omits the baseline line and the dip markers.

### Richer signals (vs. the SQL snapshot)

- **Host CPU saturation** (`load_avg_15 / vCPU`) and **pooler saturation** (`server_conns / pool_size`) → `connections` — better pressure signals than `total / max_connections` on pooled setups.
- **Query-latency regression** → `performance`: windowed mean latency from `pg_stat_statements` compared to its own seasonal baseline (×1.5 / ×3 / ×6), so `performance` moves on real latency rather than just cache-hit ratio. Latency is always collected; the penalty needs a baseline.
- **Checksum failures** (data-page corruption) and **a sequence running out of values** → the red band plus a HIGH recommendation.
- **Sequential-scan regression** → `performance`: the rate of tuples read by seq scans vs its own seasonal baseline (×1.5 / ×3 / ×6) — a rise flags indexes going unused or stale planner stats (run `ANALYZE` / review indexes), without false-firing on normal analytical scans. Collected always; the penalty needs a baseline.
- **Host disk space** → `storage`: used/total of the fullest host filesystem (pgSCV `node_filesystem_*`, Yandex Cloud `disk_used_bytes`/`disk_total_bytes`). LOW/MED/HIGH at ≥70/80/90%, and from 90% the score goes into the red.

### Configuration

```yaml
health_score:
  metrics:
    enabled: true
    datasource:
      url: "http://victoria-metrics:8428"
      # auth (treat as secret): type none|bearer|basic, credentials via env
      auth: { type: bearer, token_from_env: DASHA_METRICS_DATASOURCE_TOKEN }
    providers_default: { core: pgscv, pooler: pgbouncer, host: pgscv_system }
    selectors: { … }   # per-provider label templates (sensible defaults shipped)
    targets:           # map each Dasha (cluster, instance) to datasource labels
      - { cluster: …, instance: …, env: …, service: …, host: …, container: … }
```

Datasource auth supports `token_from_env` (bearer) and `username` + `password_from_env` (basic), resolved from the environment like the other `*_from_env` secrets — so credentials are injected from a Secret rather than stored inline. `type: none` (default) needs no credentials.

