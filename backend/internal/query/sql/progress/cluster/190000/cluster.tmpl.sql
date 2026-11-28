SELECT
    pid,
    datname,
    relid::regclass::text AS table_name,
    command,
    phase,
    CASE WHEN cluster_index_relid = 0 THEN NULL
         ELSE cluster_index_relid::regclass::text
    END AS cluster_index,
    heap_tuples_scanned,
    heap_tuples_written,
    NULL::bigint AS heap_tuples_inserted,
    NULL::bigint AS heap_tuples_updated,
    NULL::bigint AS heap_tuples_deleted,
    heap_blks_total,
    heap_blks_scanned,
    index_rebuild_count
FROM
    pg_stat_progress_cluster;
