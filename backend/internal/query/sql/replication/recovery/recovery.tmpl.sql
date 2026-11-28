-- The constant on the left guarantees exactly one row: pg_stat_recovery is
-- empty outside recovery and hides its values without pg_read_all_stats, and
-- the flags beside it tell the two apart.
SELECT
    pg_is_in_recovery()                                           AS in_recovery,
    pg_has_role('pg_read_all_stats', 'USAGE')                     AS can_read_all_stats,
    r.pause_state,
    r.promote_triggered,
    r.last_replayed_end_lsn::text                                 AS last_replayed_end_lsn,
    r.last_replayed_tli::bigint                                   AS last_replayed_tli,
    r.replay_end_lsn::text                                        AS replay_end_lsn,
    r.replay_end_tli::bigint                                      AS replay_end_tli,
    r.recovery_last_xact_time,
    EXTRACT(EPOCH FROM now() - r.recovery_last_xact_time)::float8 AS since_last_xact_seconds,
    r.current_chunk_start_time
FROM (SELECT 1) one
LEFT JOIN pg_stat_recovery r ON true
