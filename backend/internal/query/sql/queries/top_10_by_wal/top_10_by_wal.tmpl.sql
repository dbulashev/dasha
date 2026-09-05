SELECT
    pss.queryid,
    COALESCE(d.datname, '') AS datname,
    pg_size_pretty(sum(pss.wal_bytes)) AS wal_volume,
    sum(pss.wal_bytes) AS wal_bytes,
    left(pss.query, 64) AS query_trunc
FROM {{ .Pgss }} pss
LEFT JOIN pg_catalog.pg_database d ON d.oid = pss.dbid
WHERE ($1::text IS NULL OR d.datname = $1)
  AND pss.queryid IS NOT NULL
GROUP BY pss.queryid, pss.query, d.datname
ORDER BY sum(pss.wal_bytes) DESC
LIMIT 10;
