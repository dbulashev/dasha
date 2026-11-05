WITH a AS (
    SELECT pid, backend_type, datname, usename, application_name, client_addr, state,
           wait_event_type, wait_event, query_id, query, query_start, xact_start, backend_xmin
    FROM pg_stat_activity
    WHERE pid <> pg_backend_pid()
      AND CASE WHEN backend_type IN ('client backend', 'parallel worker')
               THEN state IN ('active', 'idle in transaction', 'idle in transaction (aborted)', 'fastpath function call')
               ELSE COALESCE(state, '') <> 'idle' AND wait_event_type IS DISTINCT FROM 'Activity'
          END
    ORDER BY backend_type NOT IN ('client backend', 'parallel worker')
    LIMIT $1
)
SELECT
    now(),
    a.pid,
    COALESCE(a.backend_type, ''),
    COALESCE(a.datname, ''),
    COALESCE(a.usename, ''),
    COALESCE(a.application_name, ''),
    COALESCE(host(a.client_addr), ''),
    COALESCE(a.state, ''),
    COALESCE(a.wait_event_type, ''),
    COALESCE(a.wait_event, ''),
    COALESCE(a.query_id, 0),
    COALESCE(a.query, ''),
    a.query_start,
    a.xact_start,
    a.backend_xmin IS NOT NULL
FROM (VALUES (1)) AS one
    LEFT JOIN a ON true
