WITH idx as (SELECT n.nspname              AS schemaname,
                    indexrelid,
                    c.relname              AS tablename,
                    i.relname              AS indexname,
                    t.spcname              AS tablespace,
                    pg_get_indexdef(i.oid) AS indexdef
             FROM pg_index x
                      JOIN pg_class c ON c.oid = x.indrelid
                      JOIN pg_class i ON i.oid = x.indexrelid
                      LEFT JOIN pg_namespace n ON n.oid = c.relnamespace
                      LEFT JOIN pg_tablespace t ON t.oid = i.reltablespace
             WHERE (c.relkind = ANY (ARRAY ['r'::"char", 'm'::"char", 'p'::"char"]))
               AND (i.relkind = ANY (ARRAY ['i'::"char", 'I'::"char"])))
SELECT coalesce(tablespace, 'pg_default')           AS tablespace,
       schemaname || '.' || tablename               AS table,
       indexname                                    AS index,
       pg_size_pretty(pg_relation_size(indexrelid)) AS size,
       pg_relation_size(indexrelid)                 AS size_bytes
FROM idx
ORDER BY pg_relation_size(indexrelid) DESC
LIMIT 10
