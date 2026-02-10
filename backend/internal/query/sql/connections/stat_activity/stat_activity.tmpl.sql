SELECT
    pg_stat_activity.pid,
    COALESCE(datname, '') AS database,
    COALESCE(usename, '') AS user,
    COALESCE(application_name, '') AS source,
    COALESCE(client_addr::text, '') AS ip,
    COALESCE(state, '') AS state,
    COALESCE(ssl, false) AS ssl,
    COALESCE(backend_type, '') AS backend_type
FROM
    pg_stat_activity
        LEFT JOIN
    pg_stat_ssl ON pg_stat_activity.pid = pg_stat_ssl.pid
WHERE
    ($3 = '' OR usename ILIKE '%' || $3 || '%')
    AND ($4 = '' OR state = $4)
ORDER BY
    CASE WHEN backend_type = 'client backend' THEN 0 ELSE 1 END,
    CASE state
        WHEN 'idle in transaction' THEN 0
        WHEN 'idle in transaction (aborted)' THEN 1
        WHEN 'active' THEN 2
        WHEN 'idle' THEN 3
        WHEN 'fastpath function call' THEN 4
        WHEN 'disabled' THEN 5
        ELSE 6
    END,
    pg_stat_activity.pid
LIMIT $1 OFFSET $2
