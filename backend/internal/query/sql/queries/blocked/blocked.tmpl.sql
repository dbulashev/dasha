SELECT
    COALESCE(blockingl.relation::regclass::text,blockingl.locktype) as locked_item,
    blockeda.pid AS blocked_pid,
    blockeda.usename AS blocked_user,
    blockeda.query as blocked_query,
    age(now(), blockeda.query_start) AS blocked_duration,
    blockedl.mode as blocked_mode,
    blockinga.pid AS blocking_pid,
    blockinga.usename AS blocking_user,
    blockinga.state AS state_of_blocking_process,
    blockinga.query AS current_or_recent_query_in_blocking_process,
    age(now(), blockinga.query_start) AS blocking_duration,
    blockingl.mode as blocking_mode
FROM
    pg_catalog.pg_locks blockedl
        LEFT JOIN
    pg_stat_activity blockeda ON blockedl.pid = blockeda.pid
        LEFT JOIN
    pg_catalog.pg_locks blockingl ON blockedl.pid != blockingl.pid AND (
        blockingl.transactionid = blockedl.transactionid
            OR (blockingl.relation = blockedl.relation AND blockingl.locktype = blockedl.locktype)
        )
        LEFT JOIN
    pg_stat_activity blockinga ON blockingl.pid = blockinga.pid AND blockinga.datid = blockeda.datid
WHERE
    NOT blockedl.granted
  AND blockeda.query <> '<insufficient privilege>'
  AND blockeda.datname = current_database()
ORDER BY
    blocked_duration DESC
