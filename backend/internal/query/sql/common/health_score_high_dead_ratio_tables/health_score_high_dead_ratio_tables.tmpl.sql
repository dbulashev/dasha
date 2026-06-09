-- Top tables by dead-tuple ratio. Inline detail for the high_max_dead_ratio
-- recommendation: list the worst offenders so VACUUM ANALYZE can target
-- them directly.
SELECT
    schemaname AS schema_name,
    relname AS table_name,
    n_live_tup::bigint AS live_tuples,
    n_dead_tup::bigint AS dead_tuples,
    (n_dead_tup::float8 / NULLIF(n_live_tup + n_dead_tup, 0))::float8 AS dead_ratio
FROM pg_stat_user_tables
-- Same lower bound as storage_metrics in health_score.tmpl.sql so the list
-- matches what actually drove the recommendation. Tiny tables with 100%
-- dead ratio (few stale rows) don't move the score and shouldn't crowd
-- out real offenders here either.
WHERE n_live_tup + n_dead_tup > 10000
ORDER BY dead_ratio DESC NULLS LAST, n_dead_tup DESC
LIMIT $1 OFFSET $2
