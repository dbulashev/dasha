SELECT
    pid,
    phase,
    NULL::text AS started_by,
    NULL::text AS mode
FROM
    pg_stat_progress_vacuum
WHERE
    datname = current_database()
