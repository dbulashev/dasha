WITH db_size AS (
    SELECT pg_database_size(current_database()) AS size_bytes
)
SELECT
    size_bytes,
    pg_size_pretty(size_bytes) AS size_pretty
FROM db_size
