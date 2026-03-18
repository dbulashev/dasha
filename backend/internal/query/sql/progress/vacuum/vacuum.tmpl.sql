SELECT
    pid,
    datname,
    relid::regclass AS table_name,
    phase,
    heap_blks_total,
    heap_blks_scanned,
    heap_blks_vacuumed,
    index_vacuum_count,
    max_dead_tuple_bytes as max_dead_tuples,
    dead_tuple_bytes as num_dead_tuples
FROM
    pg_stat_progress_vacuum;
