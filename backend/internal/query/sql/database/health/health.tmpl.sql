SELECT
    deadlocks,
    conflicts,
    checksum_failures,
    checksum_last_failure,
    xact_commit,
    xact_rollback,
    CASE WHEN (xact_commit + xact_rollback) > 0
         THEN xact_rollback::float8 / (xact_commit + xact_rollback)
         ELSE 0
    END AS rollback_ratio,
    stats_reset
FROM pg_stat_database
WHERE datname = current_database()
