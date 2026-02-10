SELECT
    nsp.nspname AS schema,
    rel.relname AS table,
    con.conname AS name,
    fnsp.nspname AS referenced_schema,
    frel.relname AS referenced_table
FROM
    pg_catalog.pg_constraint con
        INNER JOIN
    pg_catalog.pg_class rel ON rel.oid = con.conrelid
        LEFT JOIN
    pg_catalog.pg_class frel ON frel.oid = con.confrelid
        LEFT JOIN
    pg_catalog.pg_namespace nsp ON nsp.oid = con.connamespace
        LEFT JOIN
    pg_catalog.pg_namespace fnsp ON fnsp.oid = frel.relnamespace
WHERE
    con.convalidated = 'f'
