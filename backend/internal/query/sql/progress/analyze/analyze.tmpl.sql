SELECT
    pid,
    datname,
    relid::regclass::text AS table_name,
    phase,
    sample_blks_total,
    sample_blks_scanned,
    ext_stats_total,
    ext_stats_computed,
    current_child_table_relid::regclass AS current_child_table
FROM
    pg_stat_progress_analyze;
