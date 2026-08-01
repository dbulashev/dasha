-- Go picks the code by relkind: 'S' is an unlogged sequence, the rest a relation.
SELECT
    n.nspname AS schema_name,
    c.relname AS object_name,
    c.relkind::text AS relkind
FROM pg_catalog.pg_class c
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE c.relpersistence = 'u'
  AND c.relkind IN ('r', 'p', 'm', 'S')
  AND n.nspname NOT LIKE 'pg\_%'
  AND n.nspname <> 'information_schema'
ORDER BY n.nspname, c.relname
LIMIT $1
