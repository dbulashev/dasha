cold_target AS (
    SELECT o.idx, c.oid, c.relkind, c.relispartition, o.writes
    FROM unnest($SCHEMAS::text[], $TABLES::text[], $WRITES::bigint[])
         WITH ORDINALITY AS o(schema_name, rel_name, writes, idx)
    JOIN pg_namespace n ON n.nspname = o.schema_name
    JOIN pg_class c ON c.relnamespace = n.oid AND c.relname = o.rel_name
),
cold_leaf AS (
    SELECT t.idx, pt.relid
    FROM cold_target t
    CROSS JOIN LATERAL pg_partition_tree(t.oid) pt
    WHERE pt.isleaf
    UNION ALL
    SELECT t.idx, t.oid
    FROM cold_target t
    WHERE t.relkind <> 'p' AND NOT t.relispartition
),
cold_confirmed AS (
    SELECT t.idx
    FROM cold_target t
    JOIN cold_leaf l USING (idx)
    LEFT JOIN pg_stat_user_tables s ON s.relid = l.relid
    GROUP BY t.idx, t.writes
    HAVING COALESCE(sum(s.n_tup_ins + s.n_tup_upd + s.n_tup_del), 0) = t.writes
),
cold AS (
    SELECT l.idx, l.relid
    FROM cold_leaf l
    JOIN cold_confirmed USING (idx)
),
cold_rel AS (
    SELECT idx, relid FROM cold
    UNION ALL
    SELECT cold.idx, c.reltoastrelid
    FROM cold JOIN pg_class c ON c.oid = cold.relid
    WHERE c.reltoastrelid <> 0
)
