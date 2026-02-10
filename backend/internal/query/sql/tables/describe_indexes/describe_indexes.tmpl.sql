SELECT
    c2.relname AS index_name,
    pg_catalog.pg_get_indexdef(i.indexrelid, 0, true) AS definition,
    i.indisprimary AS is_primary,
    i.indisunique AS is_unique,
    i.indisvalid AS is_valid,
    pg_catalog.pg_relation_size(c2.oid) AS size_bytes,
    pg_catalog.pg_size_pretty(pg_catalog.pg_relation_size(c2.oid)) AS size
FROM pg_catalog.pg_class c
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
JOIN pg_catalog.pg_index i ON i.indrelid = c.oid
JOIN pg_catalog.pg_class c2 ON c2.oid = i.indexrelid
WHERE n.nspname = $1
    AND c.relname = $2
ORDER BY i.indisprimary DESC, c2.relname
