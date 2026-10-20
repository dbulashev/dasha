WITH pos AS (
    SELECT
        CASE
            WHEN pg_is_in_recovery() THEN pg_last_wal_replay_lsn()
            ELSE pg_current_wal_lsn()
        END AS lsn,
        EXTRACT(EPOCH FROM clock_timestamp() - $2::timestamptz)::float8 AS elapsed_seconds
),
rate AS (
    SELECT
        lsn,
        elapsed_seconds,
        CASE
            WHEN elapsed_seconds > 0 AND lsn >= $1::pg_lsn
                THEN pg_wal_lsn_diff(lsn, $1::pg_lsn)::float8 / elapsed_seconds
        END AS wal_bytes_per_second
    FROM
        pos
),
keep AS (
    SELECT
        NULLIF(setting::bigint, -1) * 1024 * 1024 AS max_slot_wal_keep_size
    FROM
        pg_settings
    WHERE
        name = 'max_slot_wal_keep_size'
)
SELECT
    r.elapsed_seconds,
    r.wal_bytes_per_second,
    k.max_slot_wal_keep_size,
    s.slot_name,
    s.slot_type,
    COALESCE(s.database, '') AS database,
    s.active,
    COALESCE(s.wal_status, '') AS wal_status,
    pg_wal_lsn_diff(r.lsn, s.restart_lsn)::bigint AS retained_bytes,
    s.safe_wal_size,
    CASE
        WHEN s.safe_wal_size IS NULL THEN NULL
        WHEN s.safe_wal_size <= 0 THEN 0
        WHEN r.wal_bytes_per_second > 0 THEN s.safe_wal_size / r.wal_bytes_per_second
    END AS seconds_to_lost
FROM
    rate r
    LEFT JOIN keep k ON true
    LEFT JOIN pg_replication_slots s ON true
ORDER BY
    seconds_to_lost NULLS LAST,
    s.slot_name
