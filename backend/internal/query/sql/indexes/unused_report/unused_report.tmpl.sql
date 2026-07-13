-- Per-host evidence for the unused-index report: every non-unique index with its
-- scan counter AND the observation window the counter was accumulated over.
--
-- Deliberately separate from indexes/all_scans, which backs the existing
-- /api/indexes/unused contract — that query stays untouched.
--
-- idx_scan is a since-reset counter, so an absolute threshold is meaningless on its
-- own: 0 scans right after pg_stat_reset() says nothing, and 5 scans over two years
-- is effectively unused. The caller normalizes to a per-day rate over window_days
-- (same reasoning as the per-day rates in settings/analyze_settings).
--
-- window_days is fractional and NOT clamped, so the caller can tell a 2-hour window
-- from a 2-day one. When stats_reset IS NULL (never reset) it falls back to
-- postmaster start: statistics actually survive clean restarts, so this UNDERSTATES
-- the window — erring toward "not enough evidence", which is the safe direction.
--
-- Unique indexes are excluded: their idx_scan does not grow when the index is used
-- to enforce a constraint, so it can never prove them unused.
--
-- PARTITIONING. pg_stat_user_indexes only lists indexes whose table is an ordinary
-- relation, so a partitioned index (relkind 'I') never appears — only its per-partition
-- children do, each with its own counter. Judging those children individually is wrong
-- twice over: a cold partition legitimately shows zero scans (that is partition pruning
-- working), and a child index cannot be dropped anyway — PostgreSQL answers
--   ERROR: cannot drop index <child> because index <parent> requires it
--   HINT:  You can drop index <parent> instead.
-- and following that hint removes the index from EVERY partition, including the hot
-- ones. The only droppable unit is the top-level parent, so each row carries its root
-- index (root_schema/root_index/root_table) and the caller sums the children's scans
-- up to it. The walk is recursive because partitions can themselves be partitioned.
WITH RECURSIVE idx_up AS (
    SELECT inh.inhrelid AS idx, inh.inhparent AS anc, 1 AS lvl
    FROM pg_inherits inh
        INNER JOIN pg_class ic ON ic.oid = inh.inhrelid AND ic.relkind IN ('i', 'I')
    UNION ALL
    SELECT u.idx, inh.inhparent, u.lvl + 1
    FROM idx_up u
        INNER JOIN pg_inherits inh ON inh.inhrelid = u.anc
),
idx_root AS (
    SELECT DISTINCT ON (idx) idx, anc AS root
    FROM idx_up
    ORDER BY idx, lvl DESC
)
SELECT
    ui.schemaname AS schema,
    ui.relname AS table,
    ui.indexrelname AS index,
    COALESCE(rn.nspname, ui.schemaname) AS root_schema,
    COALESCE(rc.relname, ui.indexrelname) AS root_index,
    COALESCE(rt.relname, ui.relname) AS root_table,
    (r.root IS NOT NULL) AS is_partitioned,
    pg_relation_size(i.indexrelid) AS size_bytes,
    ui.idx_scan AS index_scans,
    d.stats_reset AS stats_reset,
    EXTRACT(EPOCH FROM (now() - COALESCE(d.stats_reset, pg_postmaster_start_time()))) / 86400.0 AS window_days,
    pg_is_in_recovery() AS in_recovery
FROM
    pg_stat_user_indexes ui
        INNER JOIN
    pg_index i ON ui.indexrelid = i.indexrelid
        LEFT JOIN
    idx_root r ON r.idx = i.indexrelid
        LEFT JOIN
    pg_class rc ON rc.oid = r.root
        LEFT JOIN
    pg_namespace rn ON rn.oid = rc.relnamespace
        LEFT JOIN
    pg_index ri ON ri.indexrelid = r.root
        LEFT JOIN
    pg_class rt ON rt.oid = ri.indrelid
        CROSS JOIN
    (SELECT stats_reset FROM pg_stat_database WHERE datname = current_database()) d
WHERE
    NOT i.indisunique
    AND ui.relid NOT IN (
        SELECT relation FROM pg_locks WHERE mode = 'AccessExclusiveLock' AND granted
    )
ORDER BY
    pg_relation_size(i.indexrelid) DESC,
    ui.relname ASC
