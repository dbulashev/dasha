-- rel is referenced twice, so it is materialized once (no MATERIALIZED keyword: PG < 12).
WITH rel AS (
    SELECT c.oid
    FROM pg_catalog.pg_class     c
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    WHERE c.relkind IN ('r', 'p', 'm')
      AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
      AND n.nspname !~ '^pg_(toast_)?temp_'
      AND NOT (n.nspname LIKE ANY ($1::text[]))
      AND NOT EXISTS (
            SELECT 1 FROM pg_catalog.pg_depend dep
            WHERE dep.classid = 'pg_catalog.pg_class'::regclass
              AND dep.objid   = c.oid
              AND dep.deptype = 'e')
)
-- Own columns; attinhcount = 0 drops partition and INHERITS-child copies.
SELECT a.attname::text AS column_name,
       d.description   AS column_comment
FROM rel
JOIN pg_catalog.pg_attribute a ON a.attrelid = rel.oid
LEFT JOIN pg_catalog.pg_description d
       ON d.objoid   = a.attrelid
      AND d.classoid = 'pg_catalog.pg_class'::regclass
      AND d.objsubid = a.attnum
WHERE a.attnum > 0
  AND NOT a.attisdropped
  AND a.attinhcount = 0
UNION
-- Inherited columns with a comment of their own, driven from the small pg_description.
SELECT a.attname::text,
       d.description
FROM pg_catalog.pg_description d
JOIN rel                       ON rel.oid = d.objoid
JOIN pg_catalog.pg_attribute a ON a.attrelid = d.objoid AND a.attnum = d.objsubid
WHERE d.classoid = 'pg_catalog.pg_class'::regclass
  AND d.objsubid > 0
  AND a.attinhcount > 0
  AND NOT a.attisdropped
