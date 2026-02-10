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
WHERE
    ($1::text IS NULL OR relname ILIKE '%' || $1 || '%')
ORDER BY
    1, 2
LIMIT $2 OFFSET $3
