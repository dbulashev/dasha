SELECT
    pss.queryid,
    COALESCE(d.datname, '') AS datname,
    to_char(
            interval '1 millisecond' * sum(pss.total_exec_time),
            'HH24:MI:SS'
    ) AS exec_time,
    sum(pss.total_exec_time) AS exec_time_ms,
    COALESCE(
        (100 * sum(
                pss.blk_read_time + pss.blk_write_time
               ) / nullif(sum(pss.total_exec_time), 0))::numeric(5,2)::text || ' / ' ||
        (100 * sum(pss.total_exec_time - (
            pss.blk_read_time + pss.blk_write_time)
               ) / nullif(sum(pss.total_exec_time), 0))::numeric(5,2)::text,
        '0.00 / 0.00'
    ) AS "io / cpu, %",
    COALESCE((100 * sum(
            pss.blk_read_time + pss.blk_write_time
           ) / nullif(sum(pss.total_exec_time), 0))::numeric(5,2), 0) AS io_pct,
    COALESCE((100 * sum(pss.total_exec_time - (
        pss.blk_read_time + pss.blk_write_time)
           ) / nullif(sum(pss.total_exec_time), 0))::numeric(5,2), 0) AS cpu_pct,
    left(pss.query, 48) AS query_trunc
FROM {{ .Pgss }} pss
LEFT JOIN pg_catalog.pg_database d ON d.oid = pss.dbid
WHERE $1::text IS NULL OR d.datname = $1
GROUP BY pss.queryid, pss.query, d.datname
ORDER BY sum(pss.total_exec_time) DESC LIMIT 10;
