-- Partial (indpred) and expression (attnum 0) unique indexes are not keys.
-- Only key columns count: indkey also lists INCLUDE columns, hence the ordinality
-- filter. unique_nullable answers "not one unique index can serve as a replica
-- identity", so a fully NOT NULL index next to a nullable one clears the flag.
-- Partitions stay in: the rollup to the root table is done in Go.
WITH usable_unique AS (
    SELECT i.indrelid,
           bool_and(a.attnotnull) AS all_not_null
    FROM pg_catalog.pg_index i
        CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS k(attnum, ord)
        LEFT JOIN pg_catalog.pg_attribute a
               ON a.attrelid = i.indrelid AND a.attnum = k.attnum
    WHERE i.indisunique AND i.indisvalid AND i.indpred IS NULL
      AND k.ord <= i.indnkeyatts
    GROUP BY i.indexrelid, i.indrelid
    HAVING bool_and(k.attnum <> 0)
)
SELECT
    n.nspname AS schema_name,
    c.relname AS object_name,
    EXISTS (SELECT 1 FROM usable_unique u WHERE u.indrelid = c.oid) AS has_unique,
    NOT EXISTS (SELECT 1 FROM usable_unique u
                 WHERE u.indrelid = c.oid AND u.all_not_null) AS unique_nullable
FROM pg_catalog.pg_class c
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind IN ('r', 'p')
  AND c.relpersistence <> 't'
  AND n.nspname NOT LIKE 'pg\_%'
  AND n.nspname <> 'information_schema'
  AND NOT EXISTS (SELECT 1
                    FROM pg_catalog.pg_constraint pc
                   WHERE pc.conrelid = c.oid AND pc.contype = 'p')
ORDER BY n.nspname, c.relname
LIMIT $1
