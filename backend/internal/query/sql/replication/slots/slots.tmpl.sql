SELECT
    slot_name,
    slot_type,
    COALESCE(database, '') AS database,
    active,
    COALESCE(wal_status, '') AS wal_status,
    safe_wal_size,
    CASE WHEN NOT pg_is_in_recovery()
         THEN (pg_current_wal_lsn() - restart_lsn)::bigint
         ELSE NULL
    END AS backlog_bytes
FROM pg_replication_slots
ORDER BY slot_name
