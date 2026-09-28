WITH {{.ColdCTE}}
SELECT
    s.schemaname AS schema,
    s.relname AS table,
    s.last_vacuum,
    s.last_autovacuum,
    s.last_analyze,
    s.last_autoanalyze,
    s.n_dead_tup AS dead_rows,
    s.n_live_tup AS live_rows,
    ci.idx::int AS cold_idx
FROM
    pg_stat_user_tables s
    LEFT JOIN LATERAL (SELECT min(cold.idx) AS idx FROM cold WHERE cold.relid = s.relid) ci ON true
WHERE
    ($1::text IS NULL OR s.relname ILIKE '%' || $1 || '%')
    AND ($7::text = 'all' OR ($7::text = 'cold') = (ci.idx IS NOT NULL))
ORDER BY
    1, 2
LIMIT $2 OFFSET $3
