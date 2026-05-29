-- Tables with autovacuum_enabled=false in reloptions. Inline detail for the
-- tables_with_autovacuum_off recommendation.
SELECT
    n.nspname AS schema_name,
    c.relname AS table_name,
    array_to_string(c.reloptions, ', ') AS reloptions
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind IN ('r', 'm')
  AND c.reloptions IS NOT NULL
  AND 'autovacuum_enabled=false' = ANY (c.reloptions)
ORDER BY n.nspname, c.relname
LIMIT 50
