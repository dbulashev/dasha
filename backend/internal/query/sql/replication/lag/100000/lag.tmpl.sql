SELECT CASE
           WHEN NOT pg_is_in_recovery() OR pg_last_xlog_receive_location() = pg_last_xlog_replay_location() THEN 0
           ELSE EXTRACT(EPOCH FROM NOW() - pg_last_xact_replay_timestamp())
           END
           AS replication_lag
