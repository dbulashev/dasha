SELECT
    queryid,
    pg_size_pretty(sum(wal_bytes)) AS wal_volume,
    sum(wal_bytes) AS wal_bytes,
    left(query, 64) AS query_trunc
FROM pg_stat_statements
GROUP BY queryid, query
ORDER BY sum(wal_bytes) DESC
LIMIT 10;
