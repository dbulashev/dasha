SELECT
    con.conname AS constraint_name,
    src_ns.nspname || '.' || src_cl.relname AS source_table,
    pg_catalog.pg_get_constraintdef(con.oid, true) AS definition
FROM pg_catalog.pg_constraint con
JOIN pg_catalog.pg_class c ON c.oid = con.confrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
JOIN pg_catalog.pg_class src_cl ON src_cl.oid = con.conrelid
JOIN pg_catalog.pg_namespace src_ns ON src_ns.oid = src_cl.relnamespace
WHERE n.nspname = $1
    AND c.relname = $2
    AND con.contype = 'f'
ORDER BY con.conname
