SELECT
    pg_has_role('pg_read_all_stats', 'USAGE')
        OR COALESCE((SELECT rolsuper FROM pg_roles WHERE rolname = current_user), false),
    current_setting('compute_query_id'),
    COALESCE(
        (SELECT setting ~ 'pg_stat_statements' FROM pg_settings WHERE name = 'shared_preload_libraries'),
        true
    )
