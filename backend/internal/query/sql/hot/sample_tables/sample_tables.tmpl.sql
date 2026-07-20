-- Raw cumulative activity counters per user table, one row per rollup target.
-- A hash-partition leaf is folded into its parent (climb pg_inherits while the
-- parent is hash-partitioned; range/list and plain tables stay one row): hash
-- ranks are noise, and one anchor per parent beats thousands per leaf.
-- part_sig fingerprints the summed leaf set — the daemon skips the interval when
-- it changes, which a counter-regression check misses when other partitions grow.
WITH RECURSIVE climb AS (
    SELECT inh.inhrelid AS leaf, inh.inhparent AS cur, 1 AS lvl
    FROM pg_inherits inh
        INNER JOIN pg_partitioned_table pt ON pt.partrelid = inh.inhparent AND pt.partstrat = 'h'
    UNION ALL
    SELECT c.leaf, inh.inhparent, c.lvl + 1
    FROM climb c
        INNER JOIN pg_inherits inh ON inh.inhrelid = c.cur
        INNER JOIN pg_partitioned_table pt ON pt.partrelid = inh.inhparent AND pt.partstrat = 'h'
),
roll AS (
    SELECT DISTINCT ON (leaf) leaf, cur AS root
    FROM climb
    ORDER BY leaf, lvl DESC
)
SELECT
    COALESCE(rn.nspname, s.schemaname) AS schema,
    COALESCE(rc.relname, s.relname)    AS object,
    sum(pg_table_size(s.relid))        AS size_bytes,
    d.stats_reset,
    bool_or(pg_is_in_recovery())       AS in_recovery,
    md5(string_agg(s.relid::text, ',' ORDER BY s.relid)) AS part_sig,
    jsonb_build_object(
        'seq_scan',        sum(s.seq_scan),
        'seq_tup_read',    sum(s.seq_tup_read),
        'idx_scan',        sum(COALESCE(s.idx_scan, 0)),
        'idx_tup_fetch',   sum(COALESCE(s.idx_tup_fetch, 0)),
        'n_tup_ins',       sum(s.n_tup_ins),
        'n_tup_upd',       sum(s.n_tup_upd),
        'n_tup_del',       sum(s.n_tup_del),
        'n_tup_hot_upd',   sum(s.n_tup_hot_upd),
        'heap_blks_read',  sum(COALESCE(io.heap_blks_read, 0)),
        'heap_blks_hit',   sum(COALESCE(io.heap_blks_hit, 0)),
        'idx_blks_read',   sum(COALESCE(io.idx_blks_read, 0)),
        'idx_blks_hit',    sum(COALESCE(io.idx_blks_hit, 0)),
        'toast_blks_read', sum(COALESCE(io.toast_blks_read, 0)),
        'toast_blks_hit',  sum(COALESCE(io.toast_blks_hit, 0))
    ) AS counters
FROM pg_stat_user_tables s
    INNER JOIN pg_statio_user_tables io ON io.relid = s.relid
    LEFT JOIN roll r ON r.leaf = s.relid
    LEFT JOIN pg_class rc ON rc.oid = r.root
    LEFT JOIN pg_namespace rn ON rn.oid = rc.relnamespace
    CROSS JOIN (
        SELECT stats_reset FROM pg_stat_database WHERE datname = current_database()
    ) d
WHERE
    -- NULLs (the daemon's full sweep) match everything; the live-percentile
    -- endpoint narrows to a single object by its ROLLUP TARGET name.
    ($1::text IS NULL OR COALESCE(rn.nspname, s.schemaname) = $1)
    AND ($2::text IS NULL OR COALESCE(rc.relname, s.relname) = $2)
GROUP BY COALESCE(rn.nspname, s.schemaname), COALESCE(rc.relname, s.relname), d.stats_reset
