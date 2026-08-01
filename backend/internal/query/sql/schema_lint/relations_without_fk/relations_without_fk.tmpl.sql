-- Tables on neither side of any foreign key.
SELECT
    n.nspname AS schema_name,
    c.relname AS object_name
FROM pg_catalog.pg_class c
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind IN ('r', 'p')
  AND c.relpersistence <> 't'
  AND n.nspname NOT LIKE 'pg\_%'
  AND n.nspname <> 'information_schema'
  AND NOT EXISTS (SELECT 1
                    FROM pg_catalog.pg_constraint fk
                   WHERE fk.contype = 'f'
                     AND (fk.conrelid = c.oid OR fk.confrelid = c.oid))
ORDER BY n.nspname, c.relname
LIMIT $1
