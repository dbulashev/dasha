-- PG 19 keeps pg_stat_progress_cluster for compatibility, but it reports REPACK
-- as CLUSTER / VACUUM FULL; only pg_stat_progress_repack has the real command.
SELECT
    pid,
    datname,
    relid::regclass::text AS table_name,
    command,
    phase,
    CASE WHEN repack_index_relid = 0 THEN NULL
         ELSE repack_index_relid::regclass::text
    END AS cluster_index,
    heap_tuples_scanned,
    NULL::bigint AS heap_tuples_written,
    heap_tuples_inserted,
    heap_tuples_updated,
    heap_tuples_deleted,
    heap_blks_total,
    heap_blks_scanned,
    index_rebuild_count
FROM
    pg_stat_progress_repack;
