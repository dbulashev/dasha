SELECT schemaname                       AS schema,
       relname                          AS name,
       COALESCE(n_tup_ins, 0)           AS inserted,
       COALESCE(n_tup_upd, 0)           AS updated,
       COALESCE(n_tup_del, 0)           AS deleted,
       COALESCE(seq_scan, 0)            AS seq_scans,
       COALESCE(idx_scan, 0)            AS idx_scans,
       COALESCE(n_live_tup, 0)          AS live_tuples
FROM pg_catalog.pg_stat_user_tables
ORDER BY schemaname, relname
LIMIT $1
