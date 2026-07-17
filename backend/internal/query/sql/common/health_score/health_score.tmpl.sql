WITH connection_metrics AS (
    SELECT
        COUNT(*) FILTER (WHERE state IS NOT NULL AND pid != pg_backend_pid()) AS total_connections,
        COUNT(*) FILTER (WHERE state = 'active' AND pid != pg_backend_pid()) AS active_connections,
        COUNT(*) FILTER (
            WHERE state = 'idle in transaction'
            AND pid != pg_backend_pid()
            AND now() - xact_start > interval '30 seconds'
        ) AS idle_in_transaction,
        COALESCE(
            EXTRACT(EPOCH FROM (now() - MIN(xact_start) FILTER (WHERE xact_start IS NOT NULL AND pid != pg_backend_pid()))),
            0
        )::float8 AS longest_transaction_seconds,
        (SELECT setting::int FROM pg_settings WHERE name = 'max_connections') AS max_connections
    FROM pg_stat_activity
),
performance_metrics AS (
    SELECT
        COALESCE(
            round(
                100.0 * sum(heap_blks_hit) /
                nullif(sum(heap_blks_hit) + sum(heap_blks_read), 0),
                2
            ),
            100
        )::float8 AS cache_hit_ratio
    FROM pg_statio_user_tables
),
storage_metrics AS (
    SELECT
        COALESCE(MAX(
            CASE WHEN n_live_tup + n_dead_tup > 10000
                 THEN round(100.0 * n_dead_tup / nullif(n_live_tup + n_dead_tup, 0), 2)
                 ELSE 0
            END
        ), 0)::float8 AS max_dead_ratio,
        COALESCE(AVG(
            CASE WHEN n_live_tup + n_dead_tup > 10000
                 THEN round(100.0 * n_dead_tup / nullif(n_live_tup + n_dead_tup, 0), 2)
                 ELSE NULL
            END
        ), 0)::float8 AS avg_dead_ratio,
        COUNT(*) FILTER (
            WHERE n_live_tup + n_dead_tup > 10000
            AND round(100.0 * n_dead_tup / nullif(n_live_tup + n_dead_tup, 0), 2) > 20
        )::int AS tables_high_bloat
    FROM pg_stat_user_tables
),
replication_metrics AS (
    SELECT
        COUNT(*)::int AS replica_count,
        COALESCE(MAX(EXTRACT(EPOCH FROM replay_lag)), 0)::float8 AS max_replay_lag_seconds,
        COALESCE(
            CASE WHEN NOT pg_is_in_recovery()
                 THEN MAX((pg_current_wal_lsn() - replay_lsn)::bigint)
                 ELSE 0
            END,
            0
        )::bigint AS max_lag_bytes,
        COUNT(*) FILTER (WHERE state != 'streaming')::int AS disconnected_replicas
    FROM pg_stat_replication
),
-- Per-table autovacuum/analyze eligibility, mirroring PostgreSQL's own trigger
-- formula (cf. describe_vacuum_stats): a table is "due" when its change counter
-- exceeds threshold + scale_factor * reltuples. Per-table reloptions override the
-- global GUCs (COALESCE), so custom per-table autovacuum settings are respected.
table_autovac AS (
    SELECT
        s.n_dead_tup,
        s.n_live_tup,
        s.n_ins_since_vacuum,
        s.n_mod_since_analyze,
        s.last_vacuum,
        s.last_autovacuum,
        GREATEST(s.last_analyze, s.last_autoanalyze) AS last_any_analyze,
        GREATEST(c.reltuples, 0)::bigint AS reltuples,
        EXTRACT(EPOCH FROM (now() - GREATEST(s.last_vacuum, s.last_autovacuum))) / 3600.0 AS vacuum_age_hours,
        COALESCE(ro.vac_base, g.vac_base) + COALESCE(ro.vac_sf, g.vac_sf) * est.row_estimate AS vacuum_threshold,
        COALESCE(ro.ins_base, g.ins_base) + COALESCE(ro.ins_sf, g.ins_sf) * est.row_estimate AS insert_threshold,
        COALESCE(ro.ana_base, g.ana_base) + COALESCE(ro.ana_sf, g.ana_sf) * est.row_estimate AS analyze_threshold
    FROM pg_stat_user_tables s
    JOIN pg_class c ON c.oid = s.relid
    -- Row estimate for the trigger formula: autovacuum itself uses reltuples,
    -- but that goes stale when ANALYZE never runs (e.g. autovacuum_enabled=false)
    -- — a table can sit at 100% dead rows while the formula still counts rows
    -- long deleted. The lower of reltuples and current live tuples lets either
    -- estimate trip the trigger (same convention as maintenance/autovacuum_summary).
    CROSS JOIN LATERAL (
        SELECT LEAST(GREATEST(c.reltuples, 0), s.n_live_tup) AS row_estimate
    ) est
    CROSS JOIN (
        SELECT
            (SELECT setting::bigint FROM pg_settings WHERE name = 'autovacuum_vacuum_threshold')          AS vac_base,
            (SELECT setting::float  FROM pg_settings WHERE name = 'autovacuum_vacuum_scale_factor')        AS vac_sf,
            (SELECT setting::bigint FROM pg_settings WHERE name = 'autovacuum_vacuum_insert_threshold')    AS ins_base,
            (SELECT setting::float  FROM pg_settings WHERE name = 'autovacuum_vacuum_insert_scale_factor') AS ins_sf,
            (SELECT setting::bigint FROM pg_settings WHERE name = 'autovacuum_analyze_threshold')          AS ana_base,
            (SELECT setting::float  FROM pg_settings WHERE name = 'autovacuum_analyze_scale_factor')       AS ana_sf
    ) g
    -- Per-table reloptions overrides, pivoted in one pass: a single
    -- pg_options_to_table() call per table instead of one per option.
    LEFT JOIN LATERAL (
        SELECT
            (max(option_value) FILTER (WHERE option_name = 'autovacuum_vacuum_threshold'))::bigint         AS vac_base,
            (max(option_value) FILTER (WHERE option_name = 'autovacuum_vacuum_scale_factor'))::float        AS vac_sf,
            (max(option_value) FILTER (WHERE option_name = 'autovacuum_vacuum_insert_threshold'))::bigint   AS ins_base,
            (max(option_value) FILTER (WHERE option_name = 'autovacuum_vacuum_insert_scale_factor'))::float AS ins_sf,
            (max(option_value) FILTER (WHERE option_name = 'autovacuum_analyze_threshold'))::bigint         AS ana_base,
            (max(option_value) FILTER (WHERE option_name = 'autovacuum_analyze_scale_factor'))::float       AS ana_sf
        FROM pg_options_to_table(c.reloptions)
    ) ro ON true
),
maintenance_metrics AS (
    -- max_xid_age is a per-DB scalar from pg_database; kept as its own subquery so
    -- an empty pg_stat_user_tables cannot zero out a dangerous XID age. The vacuum
    -- queue is derived from table_autovac (PG's own thresholds), so large static
    -- tables/partitions never inflate it.
    SELECT
        (
            SELECT COALESCE(age(datfrozenxid), 0)::bigint
            FROM pg_database
            WHERE datname = current_database()
        ) AS max_xid_age,
        -- vacuum queue depth: tables eligible for autovacuum right now, by the
        -- dead-tuple trigger OR the insert-only trigger (append-only tables).
        (
            SELECT COUNT(*) FROM table_autovac
            WHERE n_dead_tup > vacuum_threshold OR n_ins_since_vacuum > insert_threshold
        )::int AS vacuum_backlog_tables,
        -- oldest vacuum among queued tables: high only when autovacuum is genuinely
        -- behind (eligible but unvacuumed for a long time).
        (
            SELECT COALESCE(MAX(vacuum_age_hours), 0) FROM table_autovac
            WHERE n_dead_tup > vacuum_threshold OR n_ins_since_vacuum > insert_threshold
        )::float8 AS max_overdue_vacuum_age_hours,
        (
            SELECT COUNT(*) FROM table_autovac
            WHERE n_live_tup + n_dead_tup > 10000
              AND last_vacuum IS NULL AND last_autovacuum IS NULL
        )::int AS tables_never_vacuumed
),
-- Horizon: oldest backend_xmin in the cluster pins the MVCC horizon for
-- VACUUM in every database, so this scan is cluster-wide — no datname
-- filter. The drill-down (health_score_horizon_blocking_sessions) shares
-- the same scope so the head number matches the listed sessions.
horizon_metrics AS (
    SELECT
        COALESCE(MAX(age(backend_xmin))::bigint, 0) AS horizon_lag_xids
    FROM pg_stat_activity
    WHERE backend_xmin IS NOT NULL
),
-- Per-table maintenance: scans pg_class once for relfrozenxid + storage params.
per_table_metrics AS (
    SELECT
        COUNT(*) FILTER (
            WHERE 'autovacuum_enabled=false' = ANY(COALESCE(reloptions, ARRAY[]::text[]))
        )::int AS tables_with_autovacuum_off,
        COALESCE(MAX(age(relfrozenxid))::bigint, 0) AS max_relfrozenxid_age
    FROM pg_class
    WHERE relkind IN ('r','m','t')
),
-- GUC checks for autovacuum hygiene and IO timing tracking.
-- Pulled from pg_settings (one scan) instead of current_setting() to avoid
-- any session-level overrides; setting is text 'on'/'off' for boolean GUCs.
guc_metrics AS (
    SELECT
        COALESCE(BOOL_OR(name = 'autovacuum'      AND setting = 'on'), false) AS autovacuum_enabled,
        COALESCE(BOOL_OR(name = 'track_counts'    AND setting = 'on'), false) AS track_counts_enabled,
        COALESCE(BOOL_OR(name = 'track_io_timing' AND setting = 'on'), false) AS track_io_timing_enabled
    FROM pg_settings
    WHERE name IN ('autovacuum', 'track_counts', 'track_io_timing')
),
-- Checkpoint stats: base template targets the newest supported PG (17+) which
-- exposes counters in pg_stat_checkpointer. The 170000/ override covers
-- earlier PG versions still reading them from pg_stat_bgwriter.
checkpoint_metrics AS (
    SELECT
        COALESCE(num_timed, 0)::bigint AS timed_checkpoints,
        COALESCE(num_requested, 0)::bigint AS requested_checkpoints
    FROM pg_stat_checkpointer
    LIMIT 1
),
-- Locks: current snapshot of heavy lock waiters + deadlocks counter.
-- Wait-event 'Lock' covers heavy locks (relation/tuple/transactionid/...).
lock_activity AS (
    SELECT
        COUNT(*) FILTER (
            WHERE wait_event_type = 'Lock' AND state = 'active'
        )::int AS active_lock_waiters,
        -- FILTER attaches directly to the aggregate (MAX), not to the EXTRACT wrapper.
        COALESCE(
            EXTRACT(EPOCH FROM MAX(now() - state_change) FILTER (
                WHERE wait_event_type = 'Lock'
            )), 0
        )::float8 AS longest_lock_wait_seconds
    FROM pg_stat_activity
),
lock_pool AS (
    SELECT
        COUNT(*) FILTER (WHERE NOT granted)::int AS ungranted_locks,
        COUNT(*)::int AS heavyweight_total
    FROM pg_locks
),
deadlock_metrics AS (
    SELECT COALESCE(SUM(deadlocks), 0)::bigint AS deadlocks_total
    FROM pg_stat_database
),
locks_guc AS (
    SELECT setting::int AS max_locks_per_transaction
    FROM pg_settings
    WHERE name = 'max_locks_per_transaction'
),
-- HOT updates: high ratio means most updates stay on the same page, indexes
-- don't grow extra entries. Newpage-ratio measures HOT-chain breaks (only on
-- PG 16+; the 170000/ override sets it to 0 since the column does not exist
-- in PG 14/15).
hot_update_metrics AS (
    SELECT
        COALESCE(
            SUM(n_tup_hot_upd)::float8 / NULLIF(SUM(n_tup_upd), 0),
            1.0
        )::float8 AS hot_update_ratio,
        COALESCE(
            SUM(n_tup_newpage_upd)::float8 / NULLIF(SUM(n_tup_upd), 0),
            0
        )::float8 AS newpage_update_ratio
    FROM pg_stat_user_tables
    WHERE n_tup_upd > 1000
),
-- Tables with stale planner stats: modified past their (reloption-aware)
-- auto-analyze threshold AND not analyzed in 3h — autoanalyze should have
-- fired and did not. Reuses table_autovac so the threshold honours per-table
-- reloptions, consistent with the vacuum queue. The full threshold is required:
-- below it autoanalyze never fires by design, so a cold table with a one-off
-- sub-threshold batch of changes would be flagged forever. The 3h window is an
-- anti-flap guard (a healthy autovacuum picks an eligible table up within
-- minutes; 3h absorbs workers busy on huge tables and long ANALYZE runs).
stale_stats_metric AS (
    SELECT COUNT(*)::int AS stale_planner_stats_tables
    FROM table_autovac
    WHERE reltuples > 0
      AND n_mod_since_analyze > analyze_threshold
      AND COALESCE(last_any_analyze, '-infinity'::timestamptz) < now() - interval '3 hours'
),
wal_level_metric AS (
    SELECT setting AS wal_level
    FROM pg_settings
    WHERE name = 'wal_level'
),
logical_slots AS (
    SELECT
        COALESCE(COUNT(*) FILTER (WHERE slot_type = 'logical' AND active), 0)::int AS active_count
    FROM pg_replication_slots
)
SELECT
    -- recovery state: standbys cannot run autovacuum/ANALYZE, so the maintenance
    -- category is dropped (its weight is redistributed) when this is true.
    pg_is_in_recovery() AS in_recovery,
    -- connections
    c.total_connections,
    c.active_connections,
    c.idle_in_transaction,
    c.longest_transaction_seconds,
    c.max_connections,
    -- performance
    p.cache_hit_ratio,
    g.track_io_timing_enabled,
    -- storage
    s.max_dead_ratio,
    s.avg_dead_ratio,
    s.tables_high_bloat,
    -- replication
    r.replica_count,
    r.max_replay_lag_seconds,
    r.max_lag_bytes,
    r.disconnected_replicas,
    -- maintenance
    m.max_xid_age,
    m.vacuum_backlog_tables,
    m.max_overdue_vacuum_age_hours,
    m.tables_never_vacuumed,
    g.autovacuum_enabled,
    g.track_counts_enabled,
    pt.tables_with_autovacuum_off,
    pt.max_relfrozenxid_age,
    -- horizon
    h.horizon_lag_xids,
    -- wal & checkpoint
    cp.timed_checkpoints,
    cp.requested_checkpoints,
    -- locks
    la.active_lock_waiters,
    la.longest_lock_wait_seconds,
    lp.ungranted_locks,
    dl.deadlocks_total,
    lp.heavyweight_total,
    lg.max_locks_per_transaction,
    -- minor (P3)
    hot.hot_update_ratio,
    hot.newpage_update_ratio,
    ss.stale_planner_stats_tables,
    wl.wal_level,
    ls.active_count AS logical_slots_active
FROM connection_metrics c,
     performance_metrics p,
     storage_metrics s,
     replication_metrics r,
     maintenance_metrics m,
     horizon_metrics h,
     per_table_metrics pt,
     guc_metrics g,
     checkpoint_metrics cp,
     lock_activity la,
     lock_pool lp,
     deadlock_metrics dl,
     locks_guc lg,
     hot_update_metrics hot,
     stale_stats_metric ss,
     wal_level_metric wl,
     logical_slots ls
