SELECT
    l.locktype,
    l.waits,
    round(l.wait_time)::bigint                                                  AS wait_time_ms,
    l.fastpath_exceeded,
    l.stats_reset,
    (SELECT setting::int FROM pg_settings WHERE name = 'deadlock_timeout')      AS deadlock_timeout_ms,
    current_setting('max_locks_per_transaction')::int                           AS max_locks_per_transaction
FROM pg_stat_lock l
