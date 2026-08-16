WITH RECURSIVE roots AS (
    SELECT c.oid AS rel, c.oid AS root
    FROM pg_catalog.pg_class c
    WHERE c.relkind = 'p' AND NOT c.relispartition
    UNION ALL
    SELECT i.inhrelid, r.root
    FROM pg_catalog.pg_inherits i
        JOIN roots r ON i.inhparent = r.rel
),
rolled AS (
    SELECT COALESCE(r.root, s.relid)                AS rel,
           sum(COALESCE(s.n_tup_ins, 0))::bigint    AS inserted,
           sum(COALESCE(s.n_tup_upd, 0))::bigint    AS updated,
           sum(COALESCE(s.n_tup_del, 0))::bigint    AS deleted,
           sum(COALESCE(s.seq_scan, 0))::bigint     AS seq_scans,
           sum(COALESCE(s.idx_scan, 0))::bigint     AS idx_scans
    FROM pg_catalog.pg_stat_user_tables s
        LEFT JOIN roots r ON r.rel = s.relid
    GROUP BY COALESCE(r.root, s.relid)
)
SELECT n.nspname AS schema,
       c.relname AS name,
       d.inserted,
       d.updated,
       d.deleted,
       d.seq_scans,
       d.idx_scans
FROM rolled d
    JOIN pg_catalog.pg_class c ON c.oid = d.rel
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
ORDER BY n.nspname, c.relname
LIMIT $1
