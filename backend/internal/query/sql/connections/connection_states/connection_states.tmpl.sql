SELECT
    COALESCE(state, backend_type) AS state,
    COUNT(*) AS connections
FROM
    pg_stat_activity
GROUP BY
    1
ORDER BY
    2 DESC, 1
