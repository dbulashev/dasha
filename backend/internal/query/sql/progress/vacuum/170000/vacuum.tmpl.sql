-- PG < 17: dead tuples are counted in rows; the byte counters appear in PG 17+.
SELECT
    pid,
    datname,
    relid::regclass AS table_name,
    phase,
    heap_blks_total,
    heap_blks_scanned,
    heap_blks_vacuumed,
    index_vacuum_count,
    max_dead_tuples,
    num_dead_tuples,
    NULL::bigint AS dead_tuple_bytes,
    NULL::bigint AS max_dead_tuple_bytes
FROM
    pg_stat_progress_vacuum;
