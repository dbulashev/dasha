SELECT
    schemaname AS schema,
    relname AS table,
    CASE WHEN idx_scan + seq_scan > 0
        THEN round(100.0 * idx_scan / (idx_scan + seq_scan), 1)
        ELSE NULL
        END AS percent_of_times_index_used,
    n_live_tup AS estimated_rows
FROM
    pg_stat_user_tables
ORDER BY
    n_live_tup DESC,
    relname ASC
LIMIT $1 OFFSET $2
