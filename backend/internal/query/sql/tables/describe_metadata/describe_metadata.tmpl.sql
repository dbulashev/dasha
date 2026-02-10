SELECT
    n.nspname AS schema_name,
    c.relname AS table_name,
    CASE c.relkind
        WHEN 'r' THEN 'table'
        WHEN 'p' THEN 'partitioned_table'
        WHEN 'S' THEN 'sequence'
        WHEN 'v' THEN 'view'
        WHEN 'm' THEN 'materialized_view'
        WHEN 'c' THEN 'composite_type'
        WHEN 'f' THEN 'foreign_table'
        WHEN 't' THEN 'toast_table'
        ELSE c.relkind::text
    END AS table_type,
    COALESCE(am.amname, '') AS access_method,
    COALESCE(ts.spcname, '') AS tablespace,
    COALESCE(array_to_string(c.reloptions, ', '), '') AS options,
    CASE WHEN c.relkind IN ('r', 'p', 'm', 'f') THEN pg_size_pretty(pg_total_relation_size(c.oid)) ELSE '' END AS size_total,
    CASE WHEN c.relkind IN ('r', 'p', 'm', 'f') THEN pg_size_pretty(pg_table_size(c.oid)) ELSE '' END AS size_table,
    CASE WHEN c.relkind IN ('r', 'p', 'm') THEN pg_size_pretty(COALESCE(pg_total_relation_size(c.reltoastrelid), 0)) ELSE '' END AS size_toast,
    CASE WHEN c.relkind IN ('r', 'p', 'm') THEN pg_size_pretty(pg_indexes_size(c.oid)) ELSE '' END AS size_indexes,
    c.reltuples::bigint AS estimated_rows,
    COALESCE((SELECT nullif(
               CASE WHEN COALESCE(n_tup_ins, 0) + COALESCE(n_tup_upd, 0) + COALESCE(n_tup_del, 0) > 0
                    THEN 'INS: ' || to_char(100.0 * COALESCE(n_tup_ins, 0) / (COALESCE(n_tup_ins, 0) + COALESCE(n_tup_upd, 0) + COALESCE(n_tup_del, 0)), 'FM990.0') || '%, '
                      || 'UPD: ' || to_char(100.0 * COALESCE(n_tup_upd, 0) / (COALESCE(n_tup_ins, 0) + COALESCE(n_tup_upd, 0) + COALESCE(n_tup_del, 0)), 'FM990.0') || '%, '
                      || 'DEL: ' || to_char(100.0 * COALESCE(n_tup_del, 0) / (COALESCE(n_tup_ins, 0) + COALESCE(n_tup_upd, 0) + COALESCE(n_tup_del, 0)), 'FM990.0') || '%'
                      || COALESCE(', HOT UPD: ' || to_char(100.0 * COALESCE(n_tup_hot_upd, 0) / nullif(COALESCE(n_tup_upd, 0), 0), 'FM990.0') || '%', '')
                    ELSE '' END
               || CASE WHEN COALESCE(seq_scan, 0) + COALESCE(idx_scan, 0) > 0
                    THEN CASE WHEN COALESCE(n_tup_ins, 0) + COALESCE(n_tup_upd, 0) + COALESCE(n_tup_del, 0) > 0 THEN ', ' ELSE '' END
                      || CASE WHEN COALESCE(idx_scan, 0) = 0 THEN 'SEQ_SCN: 100%'
                              WHEN COALESCE(seq_scan, 0) = 0 THEN 'IDX_SCN: 100%'
                              ELSE 'SEQ_SCN: ' || to_char(100.0 * COALESCE(seq_scan, 0) / (COALESCE(seq_scan, 0) + COALESCE(idx_scan, 0)), 'FM990.0') || '%'
                         END
                    ELSE '' END
               , '')
        FROM pg_stat_all_tables s
        WHERE s.relid = c.oid), '') AS stat_info,
    CASE WHEN c.relispartition THEN
        inhparent.relname || ' ' || pg_get_expr(c.relpartbound, c.oid)
    ELSE ''
    END AS partition_of
FROM pg_catalog.pg_class c
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
LEFT JOIN pg_catalog.pg_am am ON am.oid = c.relam
LEFT JOIN pg_catalog.pg_tablespace ts ON ts.oid = c.reltablespace
LEFT JOIN pg_catalog.pg_inherits inh ON inh.inhrelid = c.oid
LEFT JOIN pg_catalog.pg_class inhparent ON inhparent.oid = inh.inhparent
WHERE n.nspname = $1
    AND c.relname = $2
    AND c.relkind IN ('r', 'p', 'v', 'm', 'f', 'c')
