SELECT s.queryid,
       s.query,
       s.calls,
       s.total_exec_time,
       s.rows
FROM (
    SELECT pss.queryid                                              AS queryid,
           (array_agg(pss.query ORDER BY length(pss.query) DESC))[1] AS query,
           sum(pss.calls)                                           AS calls,
           sum(pss.total_exec_time)                                 AS total_exec_time,
           sum(pss.rows)                                            AS rows
    FROM {{ .Pgss }} pss
        JOIN pg_catalog.pg_roles r ON r.oid = pss.userid
    WHERE pss.dbid = (SELECT d.oid FROM pg_catalog.pg_database d WHERE d.datname = current_database())
      AND pss.queryid IS NOT NULL
      AND pss.query IS NOT NULL
      AND r.rolname != ALL($1::text[])
    GROUP BY pss.queryid
) s
ORDER BY s.total_exec_time DESC
LIMIT $2
