SELECT
    pg_stat_get_db_stat_reset_time(oid) AS reset_time
FROM
    pg_database
WHERE
    datname = current_database()
