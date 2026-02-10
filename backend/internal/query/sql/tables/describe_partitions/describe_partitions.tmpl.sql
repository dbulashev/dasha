SELECT
    child_ns.nspname AS schema,
    child.relname AS name,
    pg_catalog.pg_get_expr(child.relpartbound, child.oid) AS partition_expression,
    pg_catalog.pg_relation_size(child.oid) AS size_bytes,
    pg_catalog.pg_size_pretty(pg_catalog.pg_relation_size(child.oid)) AS size
FROM pg_catalog.pg_class parent
JOIN pg_catalog.pg_namespace parent_ns ON parent_ns.oid = parent.relnamespace
JOIN pg_catalog.pg_inherits inh ON inh.inhparent = parent.oid
JOIN pg_catalog.pg_class child ON child.oid = inh.inhrelid
JOIN pg_catalog.pg_namespace child_ns ON child_ns.oid = child.relnamespace
WHERE parent_ns.nspname = $1
    AND parent.relname = $2
ORDER BY child.relname
LIMIT $3 OFFSET $4
