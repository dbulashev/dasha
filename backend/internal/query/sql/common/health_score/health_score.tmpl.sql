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
maintenance_metrics AS (
    SELECT
        COALESCE(MAX(age(datfrozenxid)), 0)::bigint AS max_xid_age,
        COALESCE(MAX(
            EXTRACT(EPOCH FROM (now() - GREATEST(last_vacuum, last_autovacuum))) / 3600.0
        ) FILTER (WHERE n_live_tup + n_dead_tup > 10000), 0)::float8 AS max_vacuum_age_hours,
        COUNT(*) FILTER (
            WHERE n_live_tup + n_dead_tup > 10000
            AND last_vacuum IS NULL
            AND last_autovacuum IS NULL
        )::int AS tables_never_vacuumed
    FROM pg_stat_user_tables, pg_database
    WHERE pg_database.datname = current_database()
),
-- Horizon: oldest backend_xmin gives the longest-held DB horizon in xid age.
horizon_metrics AS (
    SELECT
        COALESCE(MAX(age(backend_xmin))::bigint, 0) AS horizon_lag_xids
    FROM pg_stat_activity
    WHERE backend_xmin IS NOT NULL
      AND datname = current_database()
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
-- Tables with stale planner stats: significantly modified since last analyze
-- AND last analyze was over 24h ago. Threshold = 0.5 × autovacuum_analyze
-- formula so we catch tables that are well past «should have been analyzed».
stale_stats_metric AS (
    SELECT COUNT(*)::int AS stale_planner_stats_tables
    FROM pg_stat_user_tables st
    JOIN pg_class c ON c.oid = st.relid
    CROSS JOIN (
        SELECT
            (SELECT setting::int   FROM pg_settings WHERE name = 'autovacuum_analyze_threshold')    AS thr,
            (SELECT setting::float FROM pg_settings WHERE name = 'autovacuum_analyze_scale_factor') AS sf
    ) p
    WHERE GREATEST(c.reltuples, 0) > 0
      AND st.n_mod_since_analyze >
          0.5 * (p.thr + p.sf * GREATEST(c.reltuples, 0))
      AND GREATEST(
              COALESCE(st.last_analyze,     '-infinity'::timestamptz),
              COALESCE(st.last_autoanalyze, '-infinity'::timestamptz)
          ) < now() - interval '24 hours'
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
    m.max_vacuum_age_hours,
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
