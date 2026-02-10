SELECT
    pid,
    phase,
    backup_total,
    backup_streamed,
    CASE WHEN backup_total > 0
        THEN ROUND((backup_streamed::numeric / backup_total::numeric) * 100, 2)
        ELSE NULL
        END AS progress_percentage,
    tablespaces_total,
    tablespaces_streamed
FROM
    pg_stat_progress_basebackup;
