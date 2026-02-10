SELECT
    c.relname,
    ic.relname idx_name,
    i.indisvalid,
    i.indisready,
    (SELECT string_agg(format('%I', c.conname), ',') FROM pg_catalog.pg_constraint AS c WHERE c.conindid = ic.oid)
FROM pg_catalog.pg_index AS i
         INNER JOIN pg_catalog.pg_class AS ic ON i.indexrelid = ic.oid
         INNER JOIN pg_catalog.pg_class AS c ON i.indrelid = c.oid
WHERE
    NOT i.indisvalid OR NOT i.indisready
ORDER BY 1, 2;
