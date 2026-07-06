-- Sessions whose backend_xmin is oldest — they pin the MVCC horizon and
-- prevent VACUUM from removing dead tuples. Inline detail for the
-- horizon_lag_xids recommendation.
SELECT
    pid,
    COALESCE(usename, '') AS usename,
    state,
    COALESCE(wait_event_type, '') AS wait_event_type,
    COALESCE(wait_event, '') AS wait_event,
    COALESCE(EXTRACT(EPOCH FROM (now() - xact_start)), 0)::float8 AS xact_duration_seconds,
    backend_xmin::text AS backend_xmin,
    COALESCE(query, '') AS query
FROM pg_stat_activity
WHERE backend_xmin IS NOT NULL
ORDER BY backend_xmin
LIMIT $1 OFFSET $2
