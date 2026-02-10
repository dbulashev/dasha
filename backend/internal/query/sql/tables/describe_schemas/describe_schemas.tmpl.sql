SELECT nspname
FROM pg_catalog.pg_namespace
WHERE nspname NOT LIKE 'pg_toast%'
    AND nspname NOT LIKE 'pg_temp%'
ORDER BY
    CASE WHEN nspname = 'public' THEN 0
         WHEN nspname NOT LIKE 'pg_%' AND nspname != 'information_schema' THEN 1
         ELSE 2
    END,
    nspname
