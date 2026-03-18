SELECT
    pid,
    datname,
    relid::regclass::text AS table_name,
    command,
    phase,
    cluster_index_relid::regclass::text AS cluster_index,
    heap_tuples_scanned,
    heap_tuples_written,
    heap_blks_total,
    heap_blks_scanned,
    index_rebuild_count
FROM
    pg_stat_progress_cluster;
