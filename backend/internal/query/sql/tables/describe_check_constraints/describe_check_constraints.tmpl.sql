SELECT
    con.conname AS constraint_name,
    pg_catalog.pg_get_constraintdef(con.oid, true) AS definition
FROM pg_catalog.pg_constraint con
JOIN pg_catalog.pg_class c ON c.oid = con.conrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = $1
    AND c.relname = $2
    AND con.contype = 'c'
ORDER BY con.conname
