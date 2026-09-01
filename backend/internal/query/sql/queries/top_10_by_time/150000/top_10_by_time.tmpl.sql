SELECT
    t.queryid,
    t.datname,
    to_char(
            interval '1 millisecond' * t.exec_time_ms,
            'HH24:MI:SS'
    ) AS exec_time,
    t.exec_time_ms,
    t.io_pct::text || ' / ' || (100.00 - t.io_pct)::text AS "io / cpu, %",
    t.io_pct,
    100.00 - t.io_pct AS cpu_pct,
    t.query_trunc
FROM (
    SELECT
        pss.queryid,
        COALESCE(d.datname, '') AS datname,
        sum(pss.total_exec_time) AS exec_time_ms,
        round(least(COALESCE(100 * sum(
                pss.blk_read_time + pss.blk_write_time
               ) / nullif(sum(pss.total_exec_time), 0), 0), 100)::numeric, 2) AS io_pct,
        left(pss.query, 48) AS query_trunc
    FROM {{ .Pgss }} pss
    LEFT JOIN pg_catalog.pg_database d ON d.oid = pss.dbid
    WHERE $1::text IS NULL OR d.datname = $1
    GROUP BY pss.queryid, pss.query, d.datname
    ORDER BY sum(pss.total_exec_time) DESC
    LIMIT 10
) t
ORDER BY t.exec_time_ms DESC;
