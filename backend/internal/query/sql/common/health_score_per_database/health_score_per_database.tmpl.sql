WITH performance_metrics AS (
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
maintenance_metrics AS (
    SELECT
        COALESCE(MAX(age(datfrozenxid)), 0)::bigint AS max_xid_age,
        COALESCE((
            SELECT MAX(EXTRACT(EPOCH FROM (now() - GREATEST(last_vacuum, last_autovacuum))) / 3600.0)
            FROM pg_stat_user_tables
            WHERE n_live_tup + n_dead_tup > 10000
        ), 0)::float8 AS max_vacuum_age_hours,
        COALESCE((
            SELECT COUNT(*)
            FROM pg_stat_user_tables
            WHERE n_live_tup + n_dead_tup > 10000
              AND last_vacuum IS NULL
              AND last_autovacuum IS NULL
        ), 0)::int AS tables_never_vacuumed
    FROM pg_database
    WHERE datname = current_database()
)
SELECT
    current_database()::text AS database,
    pg_database_size(current_database())::bigint AS size_bytes,
    p.cache_hit_ratio,
    s.max_dead_ratio,
    s.avg_dead_ratio,
    s.tables_high_bloat,
    m.max_xid_age,
    m.max_vacuum_age_hours,
    m.tables_never_vacuumed
FROM performance_metrics p,
     storage_metrics s,
     maintenance_metrics m
