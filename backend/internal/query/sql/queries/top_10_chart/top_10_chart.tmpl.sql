WITH agg AS (
    SELECT
        queryid,
        sum(calls) AS calls,
        sum(total_exec_time) AS total_exec_time,
        sum(rows) AS rows,
        sum(shared_blks_hit) AS shared_blks_hit,
        sum(shared_blks_read) AS shared_blks_read,
        sum(shared_blks_dirtied) AS shared_blks_dirtied,
        COALESCE(sum(temp_blks_read), 0) AS temp_blks_read,
        COALESCE(sum(temp_blks_written), 0) AS temp_blks_written,
        sum(wal_records) AS wal_records
    FROM pg_stat_statements
    GROUP BY queryid
)
(SELECT 'calls' AS metric, queryid, COALESCE(100.0 * calls / NULLIF(sum(calls) OVER(), 0), 0) AS pct FROM agg ORDER BY calls DESC LIMIT 10)
UNION ALL
(SELECT 'total_exec_time', queryid, COALESCE(100.0 * total_exec_time / NULLIF(sum(total_exec_time) OVER(), 0), 0) FROM agg ORDER BY total_exec_time DESC LIMIT 10)
UNION ALL
(SELECT 'rows', queryid, COALESCE(100.0 * rows / NULLIF(sum(rows) OVER(), 0), 0) FROM agg ORDER BY rows DESC LIMIT 10)
UNION ALL
(SELECT 'shared_blks_hit', queryid, COALESCE(100.0 * shared_blks_hit / NULLIF(sum(shared_blks_hit) OVER(), 0), 0) FROM agg ORDER BY shared_blks_hit DESC LIMIT 10)
UNION ALL
(SELECT 'shared_blks_read', queryid, COALESCE(100.0 * shared_blks_read / NULLIF(sum(shared_blks_read) OVER(), 0), 0) FROM agg ORDER BY shared_blks_read DESC LIMIT 10)
UNION ALL
(SELECT 'shared_blks_dirtied', queryid, COALESCE(100.0 * shared_blks_dirtied / NULLIF(sum(shared_blks_dirtied) OVER(), 0), 0) FROM agg ORDER BY shared_blks_dirtied DESC LIMIT 10)
UNION ALL
(SELECT 'temp_blks_read', queryid, COALESCE(100.0 * temp_blks_read / NULLIF(sum(temp_blks_read) OVER(), 0), 0) FROM agg ORDER BY temp_blks_read DESC LIMIT 10)
UNION ALL
(SELECT 'temp_blks_written', queryid, COALESCE(100.0 * temp_blks_written / NULLIF(sum(temp_blks_written) OVER(), 0), 0) FROM agg ORDER BY temp_blks_written DESC LIMIT 10)
UNION ALL
(SELECT 'wal_records', queryid, COALESCE(100.0 * wal_records / NULLIF(sum(wal_records) OVER(), 0), 0) FROM agg ORDER BY wal_records DESC LIMIT 10);
