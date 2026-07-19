-- Raw cumulative activity counters for every user index; see sample_tables.
-- PostgreSQL keeps no per-index write counters, so indexes carry the reads and
-- io classes only.
SELECT
    s.schemaname AS schema,
    s.indexrelname AS object,
    s.relname AS table_name,
    pg_relation_size(s.indexrelid) AS size_bytes,
    d.stats_reset,
    pg_is_in_recovery() AS in_recovery,
    jsonb_build_object(
        'idx_scan',      s.idx_scan,
        'idx_tup_read',  s.idx_tup_read,
        'idx_tup_fetch', s.idx_tup_fetch,
        'idx_blks_read', COALESCE(io.idx_blks_read, 0),
        'idx_blks_hit',  COALESCE(io.idx_blks_hit, 0)
    ) AS counters
FROM pg_stat_user_indexes s
    INNER JOIN pg_statio_user_indexes io ON io.indexrelid = s.indexrelid
    CROSS JOIN (
        SELECT stats_reset FROM pg_stat_database WHERE datname = current_database()
    ) d
WHERE
    -- NULLs (the daemon's full sweep) match everything; the live-percentile
    -- endpoint narrows to a single object.
    ($1::text IS NULL OR s.schemaname = $1)
    AND ($2::text IS NULL OR s.indexrelname = $2)
