WITH cols AS (
    SELECT n.nspname                                AS schema,
           c.relname                                AS name,
           a.attname                                AS column_name,
           a.attnum                                 AS attnum,
           format_type(a.atttypid, a.atttypmod)     AS data_type,
           CASE WHEN t.typtype = 'd' THEN t.typbasetype ELSE a.atttypid END AS eff_type
    FROM pg_catalog.pg_attribute a
        JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
        JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
        JOIN pg_catalog.pg_type t ON t.oid = a.atttypid
    WHERE a.attnum > 0
      AND NOT a.attisdropped
      AND c.relkind IN ('r', 'p', 'm')
      AND c.relpersistence <> 't'
      AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
      AND n.nspname NOT LIKE 'pg\_temp%'
      AND n.nspname NOT LIKE 'pg\_toast\_temp%'
)
SELECT cols.schema,
       cols.name,
       cols.column_name,
       cols.data_type,
       EXISTS (
           SELECT 1
           FROM pg_catalog.pg_opclass o
               JOIN pg_catalog.pg_am am ON am.oid = o.opcmethod
           WHERE am.amname = 'btree'
             AND o.opcdefault
             AND (o.opcintype = cols.eff_type
                  OR EXISTS (SELECT 1
                             FROM pg_catalog.pg_cast cc
                             WHERE cc.castsource = cols.eff_type
                               AND cc.casttarget = o.opcintype
                               AND cc.castmethod = 'b'
                               AND cc.castcontext = 'i'))
       )                                    AS btree_indexable,
       COALESCE(st.found, false)            AS stats_known,
       COALESCE(st.n_distinct, 0)::float8   AS n_distinct,
       COALESCE(st.null_frac, 0)::float8    AS null_frac
FROM cols
    LEFT JOIN LATERAL (
        SELECT true AS found, s.n_distinct, s.null_frac
        FROM {{ .PgStatsView }} s
        WHERE s.schemaname = cols.schema
          AND s.tablename = cols.name
          AND s.attname = cols.column_name
        LIMIT 1
    ) st ON true
ORDER BY cols.schema, cols.name, cols.attnum
LIMIT $1
