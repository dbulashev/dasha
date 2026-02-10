SELECT
    queryid,
    to_char(
            interval '1 millisecond' * sum(total_exec_time),
            'HH24:MI:SS'
    ) AS exec_time,
    sum(total_exec_time) AS exec_time_ms,
    (100 * sum(
            shared_blk_read_time + shared_blk_write_time +
            temp_blk_read_time + temp_blk_write_time
           ) / sum(total_exec_time))::numeric(5,2)::text || ' / ' ||
    (100 * sum(total_exec_time - (
        shared_blk_read_time + shared_blk_write_time +
        temp_blk_read_time + temp_blk_write_time)
           ) / sum(total_exec_time))::numeric(5,2) AS "io / cpu, %",
    (100 * sum(
            shared_blk_read_time + shared_blk_write_time +
            temp_blk_read_time + temp_blk_write_time
           ) / sum(total_exec_time))::numeric(5,2) AS io_pct,
    (100 * sum(total_exec_time - (
        shared_blk_read_time + shared_blk_write_time +
        temp_blk_read_time + temp_blk_write_time)
           ) / sum(total_exec_time))::numeric(5,2) AS cpu_pct,
    left(query, 48) AS query_trunc
FROM pg_stat_statements
GROUP BY queryid, query ORDER BY sum(total_exec_time) DESC LIMIT 10;
