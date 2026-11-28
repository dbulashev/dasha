-- duplicate
SELECT
    pid,
    phase,
    started_by,
    mode
FROM
    pg_stat_progress_vacuum
WHERE
    datname = current_database()
