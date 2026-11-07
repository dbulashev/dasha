SELECT
    pg_has_role('pg_read_all_stats', 'USAGE')
        OR COALESCE((SELECT rolsuper FROM pg_roles WHERE rolname = current_user), false),
    current_setting('compute_query_id'),
    {{ .StatsPreloaded }},
    COALESCE((
        SELECT setting::bool
        FROM pg_catalog.pg_settings
        WHERE name = ANY($2::text[]) AND vartype = 'bool'
        ORDER BY array_position($2::text[], name)
        LIMIT 1
    ), true)
