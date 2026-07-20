-- Raw cumulative activity counters for every user table, one row per table.
-- Deltas are computed by the hot-objects daemon against the stored anchor
-- slice (plans/hot-objects-design.md) — this query deliberately returns only
-- cumulatives plus the host's stats epoch.
SELECT
    s.schemaname AS schema,
    s.relname AS object,
    pg_table_size(s.relid) AS size_bytes,
    d.stats_reset,
    pg_is_in_recovery() AS in_recovery,
    jsonb_build_object(
        'seq_scan',        s.seq_scan,
        'seq_tup_read',    s.seq_tup_read,
        'idx_scan',        COALESCE(s.idx_scan, 0),
        'idx_tup_fetch',   COALESCE(s.idx_tup_fetch, 0),
        'n_tup_ins',       s.n_tup_ins,
        'n_tup_upd',       s.n_tup_upd,
        'n_tup_del',       s.n_tup_del,
        'n_tup_hot_upd',   s.n_tup_hot_upd,
        'heap_blks_read',  COALESCE(io.heap_blks_read, 0),
        'heap_blks_hit',   COALESCE(io.heap_blks_hit, 0),
        'idx_blks_read',   COALESCE(io.idx_blks_read, 0),
        'idx_blks_hit',    COALESCE(io.idx_blks_hit, 0),
        'toast_blks_read', COALESCE(io.toast_blks_read, 0),
        'toast_blks_hit',  COALESCE(io.toast_blks_hit, 0)
    ) AS counters
FROM pg_stat_user_tables s
    INNER JOIN pg_statio_user_tables io ON io.relid = s.relid
    CROSS JOIN (
        SELECT stats_reset FROM pg_stat_database WHERE datname = current_database()
    ) d
WHERE
    -- NULLs (the daemon's full sweep) match everything; the live-percentile
    -- endpoint narrows to a single object.
    ($1::text IS NULL OR s.schemaname = $1)
    AND ($2::text IS NULL OR s.relname = $2)
