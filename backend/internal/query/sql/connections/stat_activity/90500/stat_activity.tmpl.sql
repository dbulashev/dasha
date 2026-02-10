SELECT
    pid,
    COALESCE(datname, '') AS database,
    COALESCE(usename, '') AS user,
    COALESCE(application_name, '') AS source,
    COALESCE(client_addr::text, '') AS ip,
    COALESCE(state, '') AS state,
    false AS ssl,
    '' AS backend_type
FROM
    pg_stat_activity
WHERE
    ($3 = '' OR usename ILIKE '%' || $3 || '%')
    AND ($4 = '' OR state = $4)
ORDER BY
    CASE state
        WHEN 'idle in transaction' THEN 0
        WHEN 'idle in transaction (aborted)' THEN 1
        WHEN 'active' THEN 2
        WHEN 'idle' THEN 3
        WHEN 'fastpath function call' THEN 4
        WHEN 'disabled' THEN 5
        ELSE 6
    END,
    pid
LIMIT $1 OFFSET $2
