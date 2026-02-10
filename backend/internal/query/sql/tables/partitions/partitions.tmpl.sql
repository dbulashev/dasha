SELECT nmsp_parent.nspname AS parent_schema,
       parent.relname AS parent,
       count(1) AS childs_count,
       COALESCE(sum(COALESCE(child.relpages, 0)) * 8192, 0) AS childs_size_bytes,
       pg_size_pretty(sum(COALESCE(child.relpages, 0)) * 8192) AS childs_size,
       COALESCE((avg(child.relpages) FILTER (WHERE child.relpages IS NOT NULL)) * 8192, 0)::bigint AS childs_avg_size_bytes,
       pg_size_pretty((avg(child.relpages) FILTER (WHERE child.relpages IS NOT NULL)) * 8192) AS childs_avg_size
FROM pg_inherits
         JOIN pg_class parent ON
    pg_inherits.inhparent = parent.oid
         JOIN pg_class child ON
    pg_inherits.inhrelid = child.oid
         JOIN pg_namespace nmsp_parent ON
    nmsp_parent.oid = parent.relnamespace
WHERE parent.relkind = 'p'
GROUP BY 1, 2;
