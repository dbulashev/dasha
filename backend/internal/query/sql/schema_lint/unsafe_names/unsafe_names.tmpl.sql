-- Names that force quoting or collide with keywords. The keyword list is
-- materialized once: an EXISTS over pg_get_keywords() per row re-runs the SRF
-- for every relation. Categories R and T are the ones PostgreSQL refuses
-- unquoted. Upper case is not flagged: it needs quoting too, but naming style
-- is not what this check is about.
WITH keywords AS MATERIALIZED (
    SELECT word FROM pg_catalog.pg_get_keywords() WHERE catcode IN ('R', 'T')
)
SELECT
    n.nspname AS schema_name,
    c.relname AS object_name,
    c.relkind::text AS relkind,
    (k.word IS NOT NULL) AS reserved
FROM pg_catalog.pg_class c
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    LEFT JOIN keywords k ON k.word = lower(c.relname)
WHERE c.relkind IN ('r', 'p', 'm', 'v', 'S', 'i', 'I')
  AND c.relpersistence <> 't'
  AND n.nspname NOT LIKE 'pg\_%'
  AND n.nspname <> 'information_schema'
  AND (c.relname ~ '[^A-Za-z0-9_]' OR k.word IS NOT NULL)
ORDER BY n.nspname, c.relname
LIMIT $1
