SELECT
    datname AS database,
    usename AS user,
    application_name AS source,
    client_addr AS ip,
    COUNT(*) AS total_connections
FROM
    pg_stat_activity
GROUP BY
    1, 2, 3, 4
ORDER BY
    5 DESC, 1, 2, 3, 4
LIMIT $1 OFFSET $2