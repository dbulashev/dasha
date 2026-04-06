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
)
SELECT
    -- connections
    c.total_connections,
    c.active_connections,
    c.idle_in_transaction,
    c.longest_transaction_seconds,
    c.max_connections,
    -- performance
    p.cache_hit_ratio,
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
    m.tables_never_vacuumed
FROM connection_metrics c,
     performance_metrics p,
     storage_metrics s,
     replication_metrics r,
     maintenance_metrics m
