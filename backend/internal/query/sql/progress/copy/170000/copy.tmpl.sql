WITH locked_rels AS MATERIALIZED (
    SELECT
        relation
    FROM
        pg_locks
    WHERE
        mode = 'AccessExclusiveLock'
        AND relation IS NOT NULL
        AND database = (SELECT oid FROM pg_database WHERE datname = current_database())
),
s AS (
    SELECT
        p.pid,
        p.datname,
        p.relid,
        c.oid AS local_relid,
        c.oid IN (SELECT relation FROM locked_rels) AS locked,
        p.command,
        p.type,
        a.query,
        p.bytes_processed,
        p.bytes_total,
        p.tuples_processed,
        p.tuples_excluded,
        NULL::bigint AS tuples_skipped,
        NULLIF(c.reltuples, -1)::bigint AS table_tuples,
        EXTRACT(EPOCH FROM clock_timestamp() - a.query_start)::float8 AS elapsed_seconds
    FROM
        pg_stat_progress_copy p
        LEFT JOIN pg_stat_activity a ON a.pid = p.pid
        LEFT JOIN pg_class c ON c.oid = p.relid AND p.datname = current_database()
),
f AS (
    SELECT
        s.*,
        CASE
            WHEN s.bytes_total > 0
                THEN LEAST(s.bytes_processed::float8 / s.bytes_total, 1)
            WHEN s.command = 'COPY TO' AND s.table_tuples > 0
                THEN LEAST(s.tuples_processed::float8 / s.table_tuples, 1)
        END AS fraction
    FROM
        s
)
SELECT
    pid,
    datname,
    CASE
        WHEN local_relid IS NOT NULL THEN local_relid::regclass::text
        WHEN relid <> 0 THEN relid::text
    END AS table_name,
    command,
    type,
    query,
    bytes_processed,
    NULLIF(bytes_total, 0) AS bytes_total,
    tuples_processed,
    tuples_excluded,
    tuples_skipped,
    CASE
        WHEN local_relid IS NOT NULL AND NOT locked THEN pg_total_relation_size(local_relid)
    END AS table_size,
    table_tuples,
    elapsed_seconds,
    ROUND((fraction * 100)::numeric, 2)::float8 AS progress_percentage,
    CASE
        WHEN fraction > 0 AND fraction < 1 THEN elapsed_seconds * (1 - fraction) / fraction
    END AS eta_seconds
FROM
    f
ORDER BY
    pid;
