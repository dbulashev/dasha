-- PG 17+ replaced the dead-tuple row counters with TID-store memory counters
-- (dead_tuple_bytes / max_dead_tuple_bytes) and added num_dead_item_ids for
-- the collected count; a row-count limit no longer exists, hence NULL.
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
    max_dead_tuple_bytes
FROM
    pg_stat_progress_vacuum;
