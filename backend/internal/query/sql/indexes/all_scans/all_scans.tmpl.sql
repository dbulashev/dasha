SELECT
    schemaname AS schema,
    relname AS table,
    indexrelname AS index,
    pg_relation_size(i.indexrelid) AS size_bytes,
    idx_scan as index_scans
FROM
    pg_stat_user_indexes ui
        INNER JOIN
    pg_index i ON ui.indexrelid = i.indexrelid
WHERE
    NOT indisunique
    AND ui.relid NOT IN (
        SELECT relation FROM pg_locks WHERE mode = 'AccessExclusiveLock' AND granted
    )
ORDER BY
    pg_relation_size(i.indexrelid) DESC,
    relname ASC
