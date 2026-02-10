SELECT
    schemaname AS schema,
    relname AS table,
    indexrelname AS index,
    CASE WHEN coalesce(idx_blks_hit,0) + coalesce(idx_blks_read,0) = 0 THEN
             0
         ELSE
             ROUND(1.0 * coalesce(idx_blks_hit,0) / (coalesce(idx_blks_hit,0) + coalesce(idx_blks_read,0)), 2)
        END AS hit_rate
FROM
    pg_statio_user_indexes
ORDER BY
    3 DESC, 1
LIMIT $1 OFFSET $2
