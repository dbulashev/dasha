SELECT c.relname
FROM pg_catalog.pg_class c
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = $1
    AND c.relname ILIKE '%' || replace(replace(replace($2, '\', '\\'), '%', '\%'), '_', '\_') || '%'
    AND c.relkind IN ('r', 'p', 'v', 'm', 'f')
ORDER BY c.relname
LIMIT $3
