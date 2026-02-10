SELECT
    n.nspname AS schema,
    c.relname AS table,
    $1 - GREATEST(AGE(c.relfrozenxid), AGE(t.relfrozenxid)) AS transactions_left
FROM
    pg_class c
    INNER JOIN
    pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    LEFT JOIN
    pg_class t ON c.reltoastrelid = t.oid
WHERE
    c.relkind = 'r'
  AND (
   $1 -- max_value
   - GREATEST(AGE(c.relfrozenxid), AGE(t.relfrozenxid))) < $2
ORDER BY
    3, 1, 2
