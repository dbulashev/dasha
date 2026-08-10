-- Tables with autovacuum_enabled=false in reloptions. Inline detail for the
-- tables_with_autovacuum_off recommendation.
SELECT
    n.nspname AS schema_name,
    c.relname AS table_name,
    array_to_string(c.reloptions, ', ') AS reloptions
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
-- Include TOAST tables ('t') so the list matches the per_table_metrics
-- counter in health_score.tmpl.sql, which scans relkind ('r','m','t').
WHERE c.relkind IN ('r', 'm', 't')
  AND c.reloptions IS NOT NULL
  -- reloptions keeps the spelling the user typed (off/0/f/no), so the option
  -- has to be parsed, not string-matched. CASE rather than a bare AND: the cast
  -- must not reach other options (fillfactor=70 would fail), and AND does not
  -- guarantee evaluation order.
  AND EXISTS (
      SELECT 1
      FROM pg_options_to_table(c.reloptions) o
      WHERE NOT CASE
          WHEN o.option_name = 'autovacuum_enabled'
          THEN o.option_value::boolean
          ELSE true
      END
  )
ORDER BY n.nspname, c.relname
LIMIT $1 OFFSET $2
