SELECT
    (CASE
        WHEN pg_is_in_recovery() THEN pg_last_wal_replay_lsn()
        ELSE pg_current_wal_lsn()
    END)::text AS lsn,
    clock_timestamp() AS captured_at
