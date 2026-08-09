SELECT
    CASE
        WHEN blockingl.relation IS NULL THEN COALESCE(blockingl.locktype, '')
        WHEN blockingl.database IN (0, (SELECT oid FROM pg_catalog.pg_database WHERE datname = current_database()))
            THEN blockingl.relation::regclass::text
        ELSE blockingl.relation::text
    END AS locked_item,
    blockeda.pid AS blocked_pid,
    COALESCE(blockeda.datname, '') AS blocked_database,
    COALESCE(blockeda.usename, '') AS blocked_user,
    blockeda.query as blocked_query,
    age(now(), blockeda.query_start)::text AS blocked_duration,
    EXTRACT(EPOCH FROM age(now(), blockeda.query_start)) * 1000.0 AS blocked_duration_ms,
    blockedl.mode as blocked_mode,
    COALESCE(blockinga.pid, blockingl.pid, 0) AS blocking_pid,
    COALESCE(blockinga.usename, '') AS blocking_user,
    COALESCE(blockinga.state, '') AS state_of_blocking_process,
    COALESCE(blockinga.query, '') AS current_or_recent_query_in_blocking_process,
    COALESCE(age(now(), blockinga.query_start)::text, '') AS blocking_duration,
    EXTRACT(EPOCH FROM age(now(), blockinga.query_start)) * 1000.0 AS blocking_duration_ms,
    COALESCE(blockingl.mode, '') as blocking_mode
FROM
    pg_catalog.pg_locks blockedl
        LEFT JOIN
    pg_stat_activity blockeda ON blockedl.pid = blockeda.pid
        LEFT JOIN
    pg_catalog.pg_locks blockingl ON blockedl.pid != blockingl.pid AND (
        blockingl.transactionid = blockedl.transactionid
            OR (blockingl.relation = blockedl.relation
                AND blockingl.locktype = blockedl.locktype
                AND blockingl.database IS NOT DISTINCT FROM blockedl.database)
        )
        LEFT JOIN
    pg_stat_activity blockinga ON blockingl.pid = blockinga.pid AND blockinga.datid = blockeda.datid
WHERE
    NOT blockedl.granted
  AND blockeda.query <> '<insufficient privilege>'
  AND ($1::text IS NULL OR blockeda.datname = $1)
ORDER BY
    age(now(), blockeda.query_start) DESC
