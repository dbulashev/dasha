WITH global_settings AS (
    SELECT
        (SELECT setting::bigint FROM pg_settings WHERE name = 'autovacuum_vacuum_threshold') AS vac_base,
        (SELECT setting::float FROM pg_settings WHERE name = 'autovacuum_vacuum_scale_factor') AS vac_sf,
        (SELECT setting::bigint FROM pg_settings WHERE name = 'autovacuum_analyze_threshold') AS ana_base,
        (SELECT setting::float FROM pg_settings WHERE name = 'autovacuum_analyze_scale_factor') AS ana_sf,
        (SELECT setting::bigint FROM pg_settings WHERE name = 'autovacuum_vacuum_insert_threshold') AS ins_base,
        (SELECT setting::float FROM pg_settings WHERE name = 'autovacuum_vacuum_insert_scale_factor') AS ins_sf
),
tbl AS (
    SELECT c.oid, c.reloptions
    FROM pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = $1 AND c.relname = $2
),
relopts AS (
    SELECT
        (SELECT option_value::bigint FROM pg_options_to_table((SELECT reloptions FROM tbl)) WHERE option_name = 'autovacuum_vacuum_threshold') AS vac_base,
        (SELECT option_value::float FROM pg_options_to_table((SELECT reloptions FROM tbl)) WHERE option_name = 'autovacuum_vacuum_scale_factor') AS vac_sf,
        (SELECT option_value::bigint FROM pg_options_to_table((SELECT reloptions FROM tbl)) WHERE option_name = 'autovacuum_analyze_threshold') AS ana_base,
        (SELECT option_value::float FROM pg_options_to_table((SELECT reloptions FROM tbl)) WHERE option_name = 'autovacuum_analyze_scale_factor') AS ana_sf,
        (SELECT option_value::bigint FROM pg_options_to_table((SELECT reloptions FROM tbl)) WHERE option_name = 'autovacuum_vacuum_insert_threshold') AS ins_base,
        (SELECT option_value::float FROM pg_options_to_table((SELECT reloptions FROM tbl)) WHERE option_name = 'autovacuum_vacuum_insert_scale_factor') AS ins_sf
)
SELECT
    s.last_vacuum,
    s.last_autovacuum,
    s.last_analyze,
    s.last_autoanalyze,
    COALESCE(s.n_dead_tup, 0) AS n_dead_tup,
    COALESCE(s.n_live_tup, 0) AS n_live_tup,
    COALESCE(s.n_mod_since_analyze, 0) AS n_mod_since_analyze,
    COALESCE(s.n_ins_since_vacuum, 0) AS n_ins_since_vacuum,
    (COALESCE(r.vac_base, g.vac_base) + COALESCE(r.vac_sf, g.vac_sf) * COALESCE(s.n_live_tup, 0))::bigint AS vacuum_threshold,
    (COALESCE(r.ana_base, g.ana_base) + COALESCE(r.ana_sf, g.ana_sf) * COALESCE(s.n_live_tup, 0))::bigint AS analyze_threshold,
    (COALESCE(r.ins_base, g.ins_base) + COALESCE(r.ins_sf, g.ins_sf) * COALESCE(s.n_live_tup, 0))::bigint AS insert_vacuum_threshold
FROM pg_stat_user_tables s
CROSS JOIN global_settings g
CROSS JOIN relopts r
WHERE s.relid = (SELECT oid FROM tbl)
