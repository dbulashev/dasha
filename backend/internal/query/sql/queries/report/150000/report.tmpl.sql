WITH stst as (
    SELECT
        pss.queryid,
        pss.dbid,
        (array_agg(pss.query))[1] AS query,
        array_agg(DISTINCT r.rolname) AS usernames,
        sum(pss.total_plan_time) AS total_plan_time,
        sum(pss.total_exec_time) AS total_exec_time,
        min(pss.min_plan_time) AS min_plan_time,
        max(pss.max_plan_time) AS max_plan_time,
        avg(pss.mean_plan_time) AS mean_plan_time,
        max(pss.stddev_plan_time) AS stddev_plan_time,
        min(pss.min_exec_time) AS min_exec_time,
        max(pss.max_exec_time) AS max_exec_time,
        avg(pss.mean_exec_time) AS mean_exec_time,
        max(pss.stddev_exec_time) AS stddev_exec_time,
        sum(pss.calls) AS calls,
        sum(pss.rows) AS rows,
        sum(pss.shared_blks_hit) AS shared_blks_hit,
        sum(pss.shared_blks_read) AS shared_blks_read,
        sum(pss.shared_blks_dirtied) AS shared_blks_dirtied,
        sum(pss.shared_blks_written) AS shared_blks_written,
        COALESCE(sum(pss.temp_blks_read),0) + COALESCE(sum(pss.temp_blks_written),0) AS temp_blks,
        sum(pss.blk_read_time) AS blk_read_time,
        sum(pss.blk_write_time) AS blk_write_time,
        sum(pss.wal_records) AS wal_records,
        sum(pss.wal_fpi) AS wal_fpi,
        sum(pss.wal_bytes) AS wal_bytes
    FROM {{ .Pgss }} pss
    JOIN pg_catalog.pg_roles r ON r.oid = pss.userid
    WHERE r.rolname != ALL($1::text[])
    GROUP BY pss.queryid, pss.dbid
),
     stst_ AS (
         SELECT
             s.queryid,
             s.dbid,
             COALESCE(d.datname, '') AS datname,
             s.query,
             s.usernames,
             s.rows,
             100.0 * (s.rows) / nullif( sum(s.rows) OVER (PARTITION BY s.dbid) , 0) AS rows_pct,
             100.0 * (s.rows) / nullif( sum(s.rows) OVER () , 0) AS rows_pct_instance,
             s.calls,
             100.0 * (s.calls) / nullif( sum(s.calls) OVER (PARTITION BY s.dbid) , 0) AS calls_pct,
             100.0 * (s.calls) / nullif( sum(s.calls) OVER () , 0) AS calls_pct_instance,
             s.total_plan_time,
             s.total_exec_time + s.total_plan_time AS total_time,
             s.total_exec_time,
             s.min_plan_time,
             s.max_plan_time,
             s.mean_plan_time,
             s.stddev_plan_time,
             s.min_exec_time,
             s.max_exec_time,
             s.mean_exec_time,
             s.stddev_exec_time,
             100.0 * (s.total_exec_time + s.total_plan_time) / nullif( sum(s.total_exec_time + s.total_plan_time) OVER (PARTITION BY s.dbid) , 0) AS total_time_pct,
             100.0 * (s.total_exec_time + s.total_plan_time) / nullif( sum(s.total_exec_time + s.total_plan_time) OVER () , 0) AS total_time_pct_instance,
             s.blk_read_time + s.blk_write_time AS io_time,
             100.0 * (s.blk_read_time + s.blk_write_time) / nullif( sum(s.blk_read_time + s.blk_write_time) OVER (PARTITION BY s.dbid) , 0) AS io_time_pct,
             100.0 * (s.blk_read_time + s.blk_write_time) / nullif( sum(s.blk_read_time + s.blk_write_time) OVER () , 0) AS io_time_pct_instance,
             CASE WHEN s.total_plan_time + s.total_exec_time - s.blk_read_time - s.blk_write_time >= 0
                  THEN s.total_plan_time + s.total_exec_time - s.blk_read_time - s.blk_write_time
             END AS cpu_time,
             100.0 * (s.shared_blks_hit) / NULLIF(s.shared_blks_hit + s.shared_blks_read, 0) AS cache_hit_ratio,
             s.shared_blks_dirtied,
             100.0 * (s.shared_blks_dirtied) / NULLIF(sum(s.shared_blks_dirtied) OVER (PARTITION BY s.dbid), 0) AS shared_blks_dirtied_pct,
             100.0 * (s.shared_blks_dirtied) / NULLIF(sum(s.shared_blks_dirtied) OVER (), 0) AS shared_blks_dirtied_pct_instance,
             s.shared_blks_written,
             100.0 * (s.shared_blks_written) / NULLIF(sum(s.shared_blks_written) OVER (PARTITION BY s.dbid), 0) AS shared_blks_written_pct,
             100.0 * (s.shared_blks_written) / NULLIF(sum(s.shared_blks_written) OVER (), 0) AS shared_blks_written_pct_instance,
             s.wal_bytes,
             100.0 * (s.wal_bytes) / NULLIF(sum(s.wal_bytes) OVER (PARTITION BY s.dbid), 0) AS wal_bytes_pct,
             100.0 * (s.wal_bytes) / NULLIF(sum(s.wal_bytes) OVER (), 0) AS wal_bytes_pct_instance,
             s.wal_records,
             s.wal_fpi,
             s.temp_blks,
             100.0 * (s.temp_blks) / NULLIF(sum(s.temp_blks) OVER (PARTITION BY s.dbid), 0) AS temp_blks_pct,
             100.0 * (s.temp_blks) / NULLIF(sum(s.temp_blks) OVER (), 0) AS temp_blks_pct_instance
         FROM stst s
         LEFT JOIN pg_catalog.pg_database d ON d.oid = s.dbid
     ),
     stst__ AS (
         SELECT
             *,
             100.0 * cpu_time / nullif( sum(cpu_time) OVER (PARTITION BY dbid) , 0) AS cpu_time_pct,
             100.0 * cpu_time / nullif( sum(cpu_time) OVER () , 0) AS cpu_time_pct_instance
         FROM stst_
     ),
     stst_v AS (
         SELECT
             *,
             ROW_NUMBER() OVER (PARTITION BY dbid ORDER BY ROWS DESC) <= 10 OR
             ROW_NUMBER() OVER (PARTITION BY dbid ORDER BY calls DESC) <= 10 OR
             ROW_NUMBER() OVER (PARTITION BY dbid ORDER BY total_plan_time DESC) <= 10 OR
             ROW_NUMBER() OVER (PARTITION BY dbid ORDER BY total_time DESC) <= 10 OR
             ROW_NUMBER() OVER (PARTITION BY dbid ORDER BY io_time DESC) <= 10 OR
             ROW_NUMBER() OVER (PARTITION BY dbid ORDER BY cpu_time DESC NULLS LAST) <= 10 OR
             ROW_NUMBER() OVER (PARTITION BY dbid ORDER BY shared_blks_dirtied DESC) <= 10 OR
             ROW_NUMBER() OVER (PARTITION BY dbid ORDER BY shared_blks_written DESC) <= 10 OR
             ROW_NUMBER() OVER (PARTITION BY dbid ORDER BY wal_bytes DESC) <= 10 OR
             ROW_NUMBER() OVER (PARTITION BY dbid ORDER BY temp_blks DESC) <= 10 OR
             ($2::bigint IS NOT NULL AND queryid = $2::bigint)
                 AS visible
         FROM stst__
         WHERE queryid IS NOT NULL
     )
SELECT queryid,
       query,
       usernames,
       datname,
       rows,
       coalesce(rows_pct, 0) AS rows_pct,
       coalesce(rows_pct_instance, 0) AS rows_pct_instance,
       calls,
       coalesce(calls_pct, 0) AS calls_pct,
       coalesce(calls_pct_instance, 0) AS calls_pct_instance,
       total_time AS total_time_ms,
       coalesce(total_time_pct, 0) AS total_time_pct,
       coalesce(total_time_pct_instance, 0) AS total_time_pct_instance,
       total_exec_time AS exec_time_ms,
       min_exec_time AS min_exec_time_ms,
       max_exec_time AS max_exec_time_ms,
       mean_exec_time AS mean_exec_time_ms,
       stddev_exec_time AS stddev_exec_time_ms,
       total_plan_time AS plan_time_ms,
       min_plan_time AS min_plan_time_ms,
       max_plan_time AS max_plan_time_ms,
       mean_plan_time AS mean_plan_time_ms,
       stddev_plan_time AS stddev_plan_time_ms,
       io_time AS io_time_ms,
       coalesce(io_time_pct, 0) AS io_time_pct,
       coalesce(io_time_pct_instance, 0) AS io_time_pct_instance,
       cpu_time AS cpu_time_ms,
       cpu_time_pct,
       cpu_time_pct_instance,
       coalesce(cache_hit_ratio, 0) AS cache_hit_ratio,
       coalesce(shared_blks_dirtied_pct, 0) AS shared_blks_dirtied_pct,
       coalesce(shared_blks_dirtied_pct_instance, 0) AS shared_blks_dirtied_pct_instance,
       coalesce(shared_blks_written_pct, 0) AS shared_blks_written_pct,
       coalesce(shared_blks_written_pct_instance, 0) AS shared_blks_written_pct_instance,
       wal_bytes,
       coalesce(wal_bytes_pct, 0) AS wal_bytes_pct,
       coalesce(wal_bytes_pct_instance, 0) AS wal_bytes_pct_instance,
       wal_records,
       wal_fpi,
       temp_blks,
       coalesce(temp_blks_pct, 0) AS temp_blks_pct,
       coalesce(temp_blks_pct_instance, 0) AS temp_blks_pct_instance
FROM stst_v
WHERE visible
ORDER BY total_time DESC, rows DESC;
