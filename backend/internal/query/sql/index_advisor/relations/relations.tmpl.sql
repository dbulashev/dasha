WITH RECURSIVE roots AS (
    SELECT c.oid AS rel, c.oid AS root
    FROM pg_catalog.pg_class c
    WHERE c.relkind = 'p' AND NOT c.relispartition
    UNION ALL
    SELECT i.inhrelid, r.root
    FROM pg_catalog.pg_inherits i
        JOIN roots r ON i.inhparent = r.rel
),
root_rows AS (
    SELECT r.root AS rel, sum(GREATEST(c.reltuples, 0))::bigint AS rows
    FROM roots r
        JOIN pg_catalog.pg_class c ON c.oid = r.rel
    WHERE c.relkind <> 'p'
    GROUP BY r.root
)
SELECT n.nspname                                                       AS schema,
       c.relname                                                       AS name,
       c.relkind::text                                                 AS relkind,
       COALESCE(rr.rows, GREATEST(c.reltuples, 0)::bigint)             AS rows,
       c.relpages::bigint                                              AS pages,
       COALESCE(rn.nspname, '')                                        AS root_schema,
       COALESCE(rc.relname, '')                                        AS root_name
FROM pg_catalog.pg_class c
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    LEFT JOIN roots r ON r.rel = c.oid AND r.rel <> r.root
    LEFT JOIN pg_catalog.pg_class rc ON rc.oid = r.root
    LEFT JOIN pg_catalog.pg_namespace rn ON rn.oid = rc.relnamespace
    LEFT JOIN root_rows rr ON rr.rel = c.oid
WHERE c.relkind IN ('r', 'p')
  AND c.relpersistence <> 't'
  AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
  AND n.nspname NOT LIKE 'pg\_temp%'
  AND n.nspname NOT LIKE 'pg\_toast\_temp%'
ORDER BY n.nspname, c.relname
LIMIT $1
