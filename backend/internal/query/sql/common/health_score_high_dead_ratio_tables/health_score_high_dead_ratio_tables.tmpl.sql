-- Top tables by dead-tuple ratio. Inline detail for the high_max_dead_ratio
-- recommendation: list the worst offenders so VACUUM ANALYZE can target
-- them directly.
WITH {{.ColdCTE}}
SELECT
    s.schemaname AS schema_name,
    s.relname AS table_name,
    s.n_live_tup::bigint AS live_tuples,
    s.n_dead_tup::bigint AS dead_tuples,
    (s.n_dead_tup::float8 / NULLIF(s.n_live_tup + s.n_dead_tup, 0))::float8 AS dead_ratio,
    ci.idx::int AS cold_idx
FROM pg_stat_user_tables s
LEFT JOIN LATERAL (SELECT min(cold.idx) AS idx FROM cold WHERE cold.relid = s.relid) ci ON true
-- Same lower bound as storage_metrics in health_score.tmpl.sql so the list
-- matches what actually drove the recommendation. Tiny tables with 100%
-- dead ratio (few stale rows) don't move the score and shouldn't crowd
-- out real offenders here either.
WHERE s.n_live_tup + s.n_dead_tup > 10000
ORDER BY (ci.idx IS NOT NULL), dead_ratio DESC NULLS LAST, s.n_dead_tup DESC
LIMIT $4 OFFSET $5
