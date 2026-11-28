SELECT
    pid,
    datname,
    relid::regclass AS table_name,
    phase,
    heap_blks_total,
    heap_blks_scanned,
    heap_blks_vacuumed,
    index_vacuum_count,
    NULL::bigint AS max_dead_tuples,
    num_dead_item_ids AS num_dead_tuples,
    dead_tuple_bytes,
    max_dead_tuple_bytes,
    started_by,
    mode
FROM
    pg_stat_progress_vacuum;
