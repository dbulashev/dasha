SELECT
    COALESCE(datname, backend_type)           AS database,
    COALESCE(usename, backend_type)           AS user,
    COALESCE(application_name, backend_type)  AS source,
    COALESCE(client_addr::text, backend_type) AS ip,
    COUNT(*) AS total_connections
FROM
    pg_stat_activity
GROUP BY
    1, 2, 3, 4
ORDER BY
    5 DESC, 1, 2, 3, 4
LIMIT $1 OFFSET $2