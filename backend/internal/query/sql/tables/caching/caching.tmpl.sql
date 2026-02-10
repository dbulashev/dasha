SELECT
    schemaname AS schema,
    relname AS table,
    round(100.0 * COALESCE(heap_blks_hit, 0) / nullif(COALESCE(heap_blks_hit, 0) + COALESCE(heap_blks_read, 0), 0), 2) AS heap_cache_ratio,
    round(100.0 * COALESCE(idx_blks_hit, 0) / nullif(COALESCE(idx_blks_hit, 0) + COALESCE(idx_blks_read, 0), 0), 2) AS idx_cache_ratio,
    round(100.0 * COALESCE(toast_blks_hit, 0) / nullif(COALESCE(toast_blks_hit, 0) + COALESCE(toast_blks_read, 0), 0), 2) AS toast_cache_ratio,
    round(100.0 * COALESCE(tidx_blks_hit, 0) / nullif(COALESCE(tidx_blks_hit, 0) + COALESCE(tidx_blks_read, 0), 0), 2) AS tidx_cache_ratio
FROM pg_statio_user_tables
WHERE relid NOT IN (
    SELECT relation FROM pg_locks WHERE mode = 'AccessExclusiveLock' AND granted
)
AND coalesce(
    nullif(COALESCE(heap_blks_hit, 0) + COALESCE(heap_blks_read, 0), 0),
    nullif(COALESCE(idx_blks_hit, 0) + COALESCE(idx_blks_read, 0), 0),
    nullif(COALESCE(toast_blks_hit, 0) + COALESCE(toast_blks_read, 0), 0),
    nullif(COALESCE(tidx_blks_hit, 0) + COALESCE(tidx_blks_read, 0), 0)
) IS NOT NULL
ORDER BY pg_total_relation_size(relid) DESC
LIMIT $1 OFFSET $2
