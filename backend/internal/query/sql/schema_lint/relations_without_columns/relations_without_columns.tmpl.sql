-- Relations with no live columns left (every one dropped, or never added).
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
                    FROM pg_catalog.pg_attribute a
                   WHERE a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped)
ORDER BY n.nspname, c.relname
LIMIT $1
