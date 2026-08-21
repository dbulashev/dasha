SELECT n.nspname                    AS schema,
       c.relname                    AS name,
       ic.relname                   AS index_name,
       am.amname                    AS method,
       i.indisunique                AS is_unique,
       i.indisprimary               AS is_primary,
       i.indisvalid                 AS is_valid,
       (i.indpred IS NOT NULL)      AS is_partial,
       (i.indexprs IS NOT NULL)     AS has_expressions,
       COALESCE((
           SELECT array_agg(a.attname::text ORDER BY k.ord)
           FROM unnest(i.indkey::int2[]) WITH ORDINALITY AS k(attnum, ord)
               JOIN pg_catalog.pg_attribute a
                   ON a.attrelid = i.indrelid AND a.attnum = k.attnum
           WHERE k.ord <= i.indnkeyatts
       ), ARRAY[]::text[])          AS key_columns,
       pg_catalog.pg_get_expr(i.indpred, i.indrelid) AS predicate
FROM pg_catalog.pg_index i
    JOIN pg_catalog.pg_class c ON c.oid = i.indrelid
    JOIN pg_catalog.pg_class ic ON ic.oid = i.indexrelid
    JOIN pg_catalog.pg_am am ON am.oid = ic.relam
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind IN ('r', 'p', 'm')
  AND c.relpersistence <> 't'
  AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
  AND n.nspname NOT LIKE 'pg\_temp%'
  AND n.nspname NOT LIKE 'pg\_toast\_temp%'
ORDER BY n.nspname, c.relname, ic.relname
LIMIT $1
