SELECT ns.nspname as namespace,
       CASE c.relkind
           WHEN 'r' THEN 'Tables'
           WHEN 'i' THEN 'Indexes'
           WHEN 'S' THEN 'Sequences'
           WHEN 'v' THEN 'Views'
           WHEN 'm' THEN 'Mat Views'
           WHEN 'c' THEN 'Compose types'
           WHEN 't' THEN 'TOAST Tables'
           WHEN 'f' THEN 'Foreign Tables'
           WHEN 'p' THEN 'Partitioned table'
           WHEN 'I' THEN 'Partitioned index'
           ELSE relkind::text
           END AS kind,
       pg_size_pretty(sum(COALESCE(relpages, 0)) * 8192) AS approx_size,
       count(1) AS amount
FROM pg_class c
         JOIN pg_catalog.pg_namespace ns ON ns."oid" = c.relnamespace
    AND NOT EXISTS (
        SELECT 1 FROM pg_locks
        WHERE relation = c.oid AND mode = 'AccessExclusiveLock' AND granted
    )
GROUP BY 1, 2
ORDER BY sum(COALESCE(relpages, 0)) desc;
