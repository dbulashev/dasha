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
-- Per-table autovacuum eligibility (reloption-aware), see health_score.tmpl.sql.
table_autovac AS (
    SELECT
        s.n_dead_tup,
        s.n_live_tup,
        s.n_ins_since_vacuum,
        s.last_vacuum,
        s.last_autovacuum,
        EXTRACT(EPOCH FROM (now() - GREATEST(s.last_vacuum, s.last_autovacuum))) / 3600.0 AS vacuum_age_hours,
        COALESCE(ro.vac_base, g.vac_base) + COALESCE(ro.vac_sf, g.vac_sf) * GREATEST(c.reltuples, 0) AS vacuum_threshold,
        COALESCE(ro.ins_base, g.ins_base) + COALESCE(ro.ins_sf, g.ins_sf) * GREATEST(c.reltuples, 0) AS insert_threshold
    FROM pg_stat_user_tables s
    JOIN pg_class c ON c.oid = s.relid
    CROSS JOIN (
        SELECT
            (SELECT setting::bigint FROM pg_settings WHERE name = 'autovacuum_vacuum_threshold')          AS vac_base,
            (SELECT setting::float  FROM pg_settings WHERE name = 'autovacuum_vacuum_scale_factor')        AS vac_sf,
            (SELECT setting::bigint FROM pg_settings WHERE name = 'autovacuum_vacuum_insert_threshold')    AS ins_base,
            (SELECT setting::float  FROM pg_settings WHERE name = 'autovacuum_vacuum_insert_scale_factor') AS ins_sf
    ) g
    -- Per-table reloptions overrides, pivoted in one pass (see health_score.tmpl.sql).
    LEFT JOIN LATERAL (
        SELECT
            (max(option_value) FILTER (WHERE option_name = 'autovacuum_vacuum_threshold'))::bigint         AS vac_base,
            (max(option_value) FILTER (WHERE option_name = 'autovacuum_vacuum_scale_factor'))::float        AS vac_sf,
            (max(option_value) FILTER (WHERE option_name = 'autovacuum_vacuum_insert_threshold'))::bigint   AS ins_base,
            (max(option_value) FILTER (WHERE option_name = 'autovacuum_vacuum_insert_scale_factor'))::float AS ins_sf
        FROM pg_options_to_table(c.reloptions)
    ) ro ON true
),
maintenance_metrics AS (
    SELECT
        COALESCE(MAX(age(datfrozenxid)), 0)::bigint AS max_xid_age,
        (
            SELECT COUNT(*) FROM table_autovac
            WHERE n_dead_tup > vacuum_threshold OR n_ins_since_vacuum > insert_threshold
        )::int AS vacuum_backlog_tables,
        COALESCE((
            SELECT MAX(vacuum_age_hours) FROM table_autovac
            WHERE n_dead_tup > vacuum_threshold OR n_ins_since_vacuum > insert_threshold
        ), 0)::float8 AS max_overdue_vacuum_age_hours,
        (
            SELECT COUNT(*) FROM table_autovac
            WHERE n_live_tup + n_dead_tup > 10000
              AND last_vacuum IS NULL AND last_autovacuum IS NULL
        )::int AS tables_never_vacuumed
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
    m.vacuum_backlog_tables,
    m.max_overdue_vacuum_age_hours,
    m.tables_never_vacuumed
FROM performance_metrics p,
     storage_metrics s,
     maintenance_metrics m
