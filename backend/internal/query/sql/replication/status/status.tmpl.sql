SELECT
    r.pid,
    r.usename,
    r.application_name,
    r.client_addr::text,
    r.state,
    r.sent_lsn::text,
    r.write_lsn::text,
    r.flush_lsn::text,
    r.replay_lsn::text,
    COALESCE(EXTRACT(EPOCH FROM r.write_lag), 0)::float8 AS write_lag_seconds,
    COALESCE(EXTRACT(EPOCH FROM r.flush_lag), 0)::float8 AS flush_lag_seconds,
    COALESCE(EXTRACT(EPOCH FROM r.replay_lag), 0)::float8 AS replay_lag_seconds,
    CASE WHEN NOT pg_is_in_recovery()
         THEN (pg_current_wal_lsn() - r.replay_lsn)::bigint
         ELSE NULL
    END AS replay_lag_bytes,
    r.sync_state,
    COALESCE(s.slot_name, '') AS slot_name
FROM pg_stat_replication r
LEFT JOIN pg_replication_slots s ON s.active_pid = r.pid
ORDER BY replay_lag_bytes DESC NULLS LAST
