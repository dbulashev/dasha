-- Always one row at least, so the total survives an offset past the end; a NULL
-- table_name marks that the page itself is empty.
SELECT
    t.total,
    p.schema_name,
    p.table_name,
    p.score,
    p.xid_score,
    p.mxid_score,
    p.vacuum_score,
    p.vacuum_insert_score,
    p.analyze_score,
    p.do_vacuum,
    p.do_analyze,
    p.for_wraparound
FROM (SELECT count(*)::int AS total FROM pg_stat_autovacuum_scores) t
LEFT JOIN LATERAL (
    SELECT
        n.nspname                     AS schema_name,
        c.relname                     AS table_name,
        s.score::float8               AS score,
        s.xid_score::float8           AS xid_score,
        s.mxid_score::float8          AS mxid_score,
        s.vacuum_score::float8        AS vacuum_score,
        s.vacuum_insert_score::float8 AS vacuum_insert_score,
        s.analyze_score::float8       AS analyze_score,
        s.do_vacuum,
        s.do_analyze,
        s.for_wraparound,
        s.relid
    FROM pg_stat_autovacuum_scores s
    JOIN pg_class c     ON c.oid = s.relid
    JOIN pg_namespace n ON n.oid = c.relnamespace
    ORDER BY s.score DESC, s.relid
    LIMIT $1 OFFSET $2
) p ON true
ORDER BY p.score DESC, p.relid
