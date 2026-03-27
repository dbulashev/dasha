SELECT
    a.attname AS column_name,
    pg_catalog.format_type(a.atttypid, a.atttypmod) AS data_type,
    COALESCE(
        CASE WHEN co.collname IS NOT NULL AND co.collname != 'default'
            THEN co.collname
        END, ''
    ) AS collation,
    NOT a.attnotnull AS nullable,
    COALESCE(pg_catalog.pg_get_expr(d.adbin, d.adrelid), '') AS default_value,
    CASE a.attstorage
        WHEN 'p' THEN 'plain'
        WHEN 'e' THEN 'external'
        WHEN 'x' THEN 'extended'
        WHEN 'm' THEN 'main'
        ELSE ''
    END AS storage,
    COALESCE(col_description(a.attrelid, a.attnum), '') AS description
FROM pg_catalog.pg_attribute a
LEFT JOIN pg_catalog.pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
LEFT JOIN pg_catalog.pg_collation co ON co.oid = a.attcollation AND a.attcollation <> 0
WHERE a.attrelid = (
    SELECT c.oid FROM pg_catalog.pg_class c
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = $1 AND c.relname = $2
)
AND a.attnum > 0
AND NOT a.attisdropped
ORDER BY a.attnum
