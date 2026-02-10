SELECT
    schemaname AS schema,
    relname AS table,
    last_vacuum,
    last_autovacuum,
    last_analyze,
    last_autoanalyze,
    n_dead_tup AS dead_rows,
    n_live_tup AS live_rows
FROM
    pg_stat_user_tables
ORDER BY
    1, 2
