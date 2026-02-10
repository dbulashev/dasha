-- duplicate
SELECT
    pid,
    phase
FROM
    pg_stat_progress_vacuum
WHERE
    datname = current_database()
