-- Heuristic: a 32/36-char varchar/bpchar named like an identifier is probably a
-- UUID kept as text. atttypmod carries the declared length plus the 4-byte header.
SELECT
    n.nspname AS schema_name,
    c.relname AS object_name,
    a.attname AS column_name,
    pg_catalog.format_type(a.atttypid, a.atttypmod) AS column_type
FROM pg_catalog.pg_attribute a
    JOIN pg_catalog.pg_class     c ON c.oid = a.attrelid
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE a.attnum > 0
  AND NOT a.attisdropped
  AND a.atttypid IN ('pg_catalog.varchar'::regtype, 'pg_catalog.bpchar'::regtype)
  AND a.atttypmod - 4 IN (32, 36)
  AND (a.attname ILIKE '%uid%' OR a.attname ILIKE '%\_id' OR a.attname = 'id')
  AND c.relkind IN ('r', 'p')
  AND c.relpersistence <> 't'
  AND n.nspname NOT LIKE 'pg\_%'
  AND n.nspname <> 'information_schema'
ORDER BY n.nspname, c.relname, a.attname
LIMIT $1
