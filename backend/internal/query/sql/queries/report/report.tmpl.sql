WITH stst as (
    SELECT
        queryid,
        (array_agg(query))[1] AS query,
        sum(total_plan_time) AS total_plan_time,
        sum(total_exec_time) AS total_exec_time,
        min(min_plan_time) AS min_plan_time,
        max(max_plan_time) AS max_plan_time,
        avg(mean_plan_time) AS mean_plan_time,
        min(min_exec_time) AS min_exec_time,
        max(max_exec_time) AS max_exec_time,
        avg(mean_exec_time) AS mean_exec_time,
        sum(calls) AS calls,
        sum(rows) AS rows,
        sum(shared_blks_hit) AS shared_blks_hit,
        sum(shared_blks_read) AS shared_blks_read,
        sum(shared_blks_dirtied) AS shared_blks_dirtied,
        sum(shared_blks_written) AS shared_blks_written,
        COALESCE(sum(temp_blks_read),0) + COALESCE(sum(temp_blks_written),0) AS temp_blks,
        sum(shared_blk_read_time) AS blk_read_time,
        sum(shared_blk_write_time) AS blk_write_time,
        sum(temp_blk_read_time) AS temp_blk_read_time,
        sum(temp_blk_write_time) AS temp_blk_write_time,
        sum(wal_records) AS wal_records,
        sum(wal_fpi) AS wal_fpi,
        sum(wal_bytes) AS wal_bytes
    FROM pg_stat_statements pss
    JOIN pg_catalog.pg_roles r ON r.oid = pss.userid
    WHERE r.rolname != ALL($1::text[])
    GROUP BY queryid
),
     stst_ AS (
         SELECT
             queryid,
             query,
             rows,
             100.0 * (rows) / nullif( sum(rows) OVER () , 0) AS rows_pct,
             calls,
             100.0 * (calls) / nullif( sum(calls) OVER () , 0) AS calls_pct,
             total_plan_time,
             total_exec_time + total_plan_time AS total_time,
             total_exec_time,
             min_plan_time,
             max_plan_time,
             mean_plan_time,
             min_exec_time,
             max_exec_time,
             mean_exec_time,
             100.0 * (total_exec_time + total_plan_time) / nullif( sum(total_exec_time + total_plan_time) OVER () , 0) AS total_time_pct,
             blk_read_time + blk_write_time + temp_blk_read_time + temp_blk_write_time AS io_time,
             100.0 * (blk_read_time + blk_write_time + temp_blk_read_time + temp_blk_write_time) / nullif( sum(blk_read_time + blk_write_time + temp_blk_read_time + temp_blk_write_time) OVER () , 0) AS io_time_pct,
             CASE WHEN total_plan_time + total_exec_time - blk_read_time - blk_write_time - temp_blk_read_time - temp_blk_write_time >= 0
                  THEN total_plan_time + total_exec_time - blk_read_time - blk_write_time - temp_blk_read_time - temp_blk_write_time
             END AS cpu_time,
             100.0 * (shared_blks_hit) / NULLIF(shared_blks_hit + shared_blks_read, 0) AS cache_hit_ratio,
             shared_blks_dirtied,
             100.0 * (shared_blks_dirtied) / NULLIF(sum(shared_blks_dirtied) OVER (), 0) AS shared_blks_dirtied_pct,
             shared_blks_written,
             100.0 * (shared_blks_written) / NULLIF(sum(shared_blks_written) OVER (), 0) AS shared_blks_written_pct,
             wal_bytes,
             100.0 * (wal_bytes) / NULLIF(sum(wal_bytes) OVER (), 0) AS wal_bytes_pct,
             wal_records,
             wal_fpi,
             temp_blks,
             100.0 * (temp_blks) / NULLIF(sum(temp_blks) OVER (), 0) AS temp_blks_pct
         FROM stst
     ),
     stst__ AS (
         SELECT
             *,
             100.0 * cpu_time / nullif( sum(cpu_time) OVER () , 0) AS cpu_time_pct
         FROM stst_
     ),
     stst_v AS (
         SELECT
             *,
             ROW_NUMBER() OVER (ORDER BY ROWS DESC) <= 10 OR
             ROW_NUMBER() OVER (ORDER BY calls DESC) <= 10 OR
             ROW_NUMBER() OVER (ORDER BY total_plan_time DESC) <= 10 OR
             ROW_NUMBER() OVER (ORDER BY total_time DESC) <= 10 OR
             ROW_NUMBER() OVER (ORDER BY io_time DESC) <= 10 OR
             ROW_NUMBER() OVER (ORDER BY cpu_time DESC NULLS LAST) <= 10 OR
             ROW_NUMBER() OVER (ORDER BY shared_blks_dirtied DESC) <= 10 OR
             ROW_NUMBER() OVER (ORDER BY shared_blks_written DESC) <= 10 OR
             ROW_NUMBER() OVER (ORDER BY wal_bytes DESC) <= 10 OR
             ROW_NUMBER() OVER (ORDER BY temp_blks DESC) <= 10
                 AS visible
         FROM stst__
     )
SELECT queryid,
       query,
       rows,
       coalesce(rows_pct, 0) AS rows_pct,
       calls,
       coalesce(calls_pct, 0) AS calls_pct,
       total_time AS total_time_ms,
       coalesce(total_time_pct, 0) AS total_time_pct,
       total_exec_time AS exec_time_ms,
       min_exec_time AS min_exec_time_ms,
       max_exec_time AS max_exec_time_ms,
       mean_exec_time AS mean_exec_time_ms,
       total_plan_time AS plan_time_ms,
       min_plan_time AS min_plan_time_ms,
       max_plan_time AS max_plan_time_ms,
       mean_plan_time AS mean_plan_time_ms,
       io_time AS io_time_ms,
       coalesce(io_time_pct, 0) AS io_time_pct,
       cpu_time AS cpu_time_ms,
       cpu_time_pct,
       coalesce(cache_hit_ratio, 0) AS cache_hit_ratio,
       coalesce(shared_blks_dirtied_pct, 0) AS shared_blks_dirtied_pct,
       coalesce(shared_blks_written_pct, 0) AS shared_blks_written_pct,
       wal_bytes,
       coalesce(wal_bytes_pct, 0) AS wal_bytes_pct,
       wal_records,
       wal_fpi,
       temp_blks,
       coalesce(temp_blks_pct, 0) AS temp_blks_pct
FROM stst_v
WHERE visible
ORDER BY total_time DESC, rows DESC;
