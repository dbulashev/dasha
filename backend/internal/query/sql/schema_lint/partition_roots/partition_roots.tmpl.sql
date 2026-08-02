-- Descends from every root, so a child of a child maps to the root table,
-- not to its immediate parent.
WITH RECURSIVE roots AS (
    SELECT c.oid AS rel, c.oid AS root
    FROM pg_catalog.pg_class c
    WHERE c.relkind = 'p' AND NOT c.relispartition
    UNION ALL
    SELECT i.inhrelid, r.root
    FROM pg_catalog.pg_inherits i
        JOIN roots r ON i.inhparent = r.rel
)
SELECT cn.nspname AS child_schema,
       cc.relname AS child_name,
       rn.nspname AS root_schema,
       rc.relname AS root_name
FROM roots r
    JOIN pg_catalog.pg_class     cc ON cc.oid = r.rel
    JOIN pg_catalog.pg_namespace cn ON cn.oid = cc.relnamespace
    JOIN pg_catalog.pg_class     rc ON rc.oid = r.root
    JOIN pg_catalog.pg_namespace rn ON rn.oid = rc.relnamespace
WHERE r.rel <> r.root
ORDER BY child_schema, child_name
LIMIT $1
