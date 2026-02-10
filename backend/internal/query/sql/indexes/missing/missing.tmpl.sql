SELECT
    schemaname AS schema,
    relname AS table,
    CASE WHEN idx_scan > 0
        THEN round(100.0 * idx_scan / (seq_scan + idx_scan), 1)
        ELSE NULL
        END AS percent_of_times_index_used,
    n_live_tup AS estimated_rows
FROM
    pg_stat_user_tables
WHERE
    idx_scan > 0
  AND (100 * idx_scan / (seq_scan + idx_scan)) < 95
  AND n_live_tup >= 10000
ORDER BY
    n_live_tup DESC,
    relname ASC
