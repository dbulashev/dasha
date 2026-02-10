SELECT CASE
           WHEN NOT pg_is_in_recovery() OR pg_last_wal_receive_lsn() = pg_last_wal_replay_lsn() THEN 0
           ELSE EXTRACT(EPOCH FROM NOW() - pg_last_xact_replay_timestamp())
           END
           AS replication_lag
