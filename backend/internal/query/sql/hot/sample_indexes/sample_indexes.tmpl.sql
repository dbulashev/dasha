-- Raw cumulative counters per user index, one row per rollup target; see
-- hot/sample_tables for the hash-rollup and part_sig rationale. The climb gates
-- on the PARENT index's table being hash-partitioned. No per-index write
-- counters, so reads and io only.
WITH RECURSIVE climb AS (
    SELECT inh.inhrelid AS leaf, inh.inhparent AS cur, 1 AS lvl
    FROM pg_inherits inh
        INNER JOIN pg_index pi ON pi.indexrelid = inh.inhparent
        INNER JOIN pg_partitioned_table pt ON pt.partrelid = pi.indrelid AND pt.partstrat = 'h'
    UNION ALL
    SELECT c.leaf, inh.inhparent, c.lvl + 1
    FROM climb c
        INNER JOIN pg_inherits inh ON inh.inhrelid = c.cur
        INNER JOIN pg_index pi ON pi.indexrelid = inh.inhparent
        INNER JOIN pg_partitioned_table pt ON pt.partrelid = pi.indrelid AND pt.partstrat = 'h'
),
roll AS (
    SELECT DISTINCT ON (leaf) leaf, cur AS root
    FROM climb
    ORDER BY leaf, lvl DESC
)
SELECT
    COALESCE(rn.nspname, s.schemaname)   AS schema,
    COALESCE(rc.relname, s.indexrelname) AS object,
    COALESCE(rt.relname, s.relname)      AS table_name,
    sum(pg_relation_size(s.indexrelid))  AS size_bytes,
    d.stats_reset,
    bool_or(pg_is_in_recovery())         AS in_recovery,
    md5(string_agg(s.indexrelid::text, ',' ORDER BY s.indexrelid)) AS part_sig,
    jsonb_build_object(
        'idx_scan',      sum(s.idx_scan),
        'idx_tup_read',  sum(s.idx_tup_read),
        'idx_tup_fetch', sum(s.idx_tup_fetch),
        'idx_blks_read', sum(COALESCE(io.idx_blks_read, 0)),
        'idx_blks_hit',  sum(COALESCE(io.idx_blks_hit, 0))
    ) AS counters
FROM pg_stat_user_indexes s
    INNER JOIN pg_statio_user_indexes io ON io.indexrelid = s.indexrelid
    LEFT JOIN roll r ON r.leaf = s.indexrelid
    LEFT JOIN pg_class rc ON rc.oid = r.root
    LEFT JOIN pg_namespace rn ON rn.oid = rc.relnamespace
    LEFT JOIN pg_index ri ON ri.indexrelid = r.root
    LEFT JOIN pg_class rt ON rt.oid = ri.indrelid
    CROSS JOIN (
        SELECT stats_reset FROM pg_stat_database WHERE datname = current_database()
    ) d
WHERE
    ($1::text IS NULL OR COALESCE(rn.nspname, s.schemaname) = $1)
    AND ($2::text IS NULL OR COALESCE(rc.relname, s.indexrelname) = $2)
GROUP BY COALESCE(rn.nspname, s.schemaname), COALESCE(rc.relname, s.indexrelname),
         COALESCE(rt.relname, s.relname), d.stats_reset
