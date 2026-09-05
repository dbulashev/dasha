WITH agg AS (
    SELECT
        pss.queryid,
        COALESCE(d.datname, '') AS datname,
        sum(pss.calls) AS calls,
        sum(pss.total_exec_time) AS total_exec_time,
        sum(pss.rows) AS rows,
        sum(pss.shared_blks_hit) AS shared_blks_hit,
        sum(pss.shared_blks_read) AS shared_blks_read,
        sum(pss.shared_blks_dirtied) AS shared_blks_dirtied,
        COALESCE(sum(pss.temp_blks_read), 0) AS temp_blks_read,
        COALESCE(sum(pss.temp_blks_written), 0) AS temp_blks_written,
        sum(pss.wal_records) AS wal_records
    FROM {{ .Pgss }} pss
    LEFT JOIN pg_catalog.pg_database d ON d.oid = pss.dbid
    WHERE ($1::text IS NULL OR d.datname = $1)
      AND pss.queryid IS NOT NULL
    GROUP BY pss.queryid, pss.dbid, d.datname
)
(SELECT 'calls' AS metric, queryid, datname, COALESCE(100.0 * calls / NULLIF(sum(calls) OVER(), 0), 0) AS pct FROM agg ORDER BY calls DESC LIMIT 10)
UNION ALL
(SELECT 'total_exec_time', queryid, datname, COALESCE(100.0 * total_exec_time / NULLIF(sum(total_exec_time) OVER(), 0), 0) FROM agg ORDER BY total_exec_time DESC LIMIT 10)
UNION ALL
(SELECT 'rows', queryid, datname, COALESCE(100.0 * rows / NULLIF(sum(rows) OVER(), 0), 0) FROM agg ORDER BY rows DESC LIMIT 10)
UNION ALL
(SELECT 'shared_blks_hit', queryid, datname, COALESCE(100.0 * shared_blks_hit / NULLIF(sum(shared_blks_hit) OVER(), 0), 0) FROM agg ORDER BY shared_blks_hit DESC LIMIT 10)
UNION ALL
(SELECT 'shared_blks_read', queryid, datname, COALESCE(100.0 * shared_blks_read / NULLIF(sum(shared_blks_read) OVER(), 0), 0) FROM agg ORDER BY shared_blks_read DESC LIMIT 10)
UNION ALL
(SELECT 'shared_blks_dirtied', queryid, datname, COALESCE(100.0 * shared_blks_dirtied / NULLIF(sum(shared_blks_dirtied) OVER(), 0), 0) FROM agg ORDER BY shared_blks_dirtied DESC LIMIT 10)
UNION ALL
(SELECT 'temp_blks_read', queryid, datname, COALESCE(100.0 * temp_blks_read / NULLIF(sum(temp_blks_read) OVER(), 0), 0) FROM agg ORDER BY temp_blks_read DESC LIMIT 10)
UNION ALL
(SELECT 'temp_blks_written', queryid, datname, COALESCE(100.0 * temp_blks_written / NULLIF(sum(temp_blks_written) OVER(), 0), 0) FROM agg ORDER BY temp_blks_written DESC LIMIT 10)
UNION ALL
(SELECT 'wal_records', queryid, datname, COALESCE(100.0 * wal_records / NULLIF(sum(wal_records) OVER(), 0), 0) FROM agg ORDER BY wal_records DESC LIMIT 10);
