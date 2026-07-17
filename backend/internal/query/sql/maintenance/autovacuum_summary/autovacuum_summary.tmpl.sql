-- Tables currently eligible for autovacuum / autoanalyze, mirroring PostgreSQL's
-- own trigger formula (threshold + scale_factor * reltuples) with per-table
-- reloptions overriding the global GUCs — same approach as health_score's
-- table_autovac. Running maintenance processes come from the progress views,
-- current database only (the page's scope).
WITH table_autovac AS (
    SELECT
        s.n_dead_tup,
        s.n_ins_since_vacuum,
        s.n_mod_since_analyze,
        COALESCE(ro.vac_base, g.vac_base) + COALESCE(ro.vac_sf, g.vac_sf) * est.row_estimate AS vacuum_threshold,
        COALESCE(ro.ins_base, g.ins_base) + COALESCE(ro.ins_sf, g.ins_sf) * est.row_estimate AS insert_threshold,
        COALESCE(ro.ana_base, g.ana_base) + COALESCE(ro.ana_sf, g.ana_sf) * est.row_estimate AS analyze_threshold
    FROM pg_stat_user_tables s
    JOIN pg_class c ON c.oid = s.relid
    -- Row estimate for the trigger formula: autovacuum itself uses
    -- pg_class.reltuples, but that goes stale when ANALYZE never runs (e.g.
    -- autovacuum_enabled=false) — a table can sit at 100% dead rows while the
    -- formula still counts rows long deleted. Taking the lower of reltuples
    -- and current live tuples lets either estimate trip the trigger, so such
    -- tables still surface as due.
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
flags AS (
    SELECT
        (n_dead_tup > vacuum_threshold OR n_ins_since_vacuum > insert_threshold) AS due_vacuum,
        n_mod_since_analyze > analyze_threshold AS due_analyze
    FROM table_autovac
)
SELECT
    COUNT(*) FILTER (WHERE due_vacuum AND NOT due_analyze)::int AS tables_due_vacuum_only,
    COUNT(*) FILTER (WHERE due_analyze AND NOT due_vacuum)::int AS tables_due_analyze_only,
    COUNT(*) FILTER (WHERE due_vacuum AND due_analyze)::int     AS tables_due_both,
    COUNT(*)::int                                               AS tables_total,
    (SELECT COUNT(*) FROM pg_stat_progress_vacuum  WHERE datname = current_database())::int AS running_vacuums,
    (SELECT COUNT(*) FROM pg_stat_progress_analyze WHERE datname = current_database())::int AS running_analyzes
FROM flags
