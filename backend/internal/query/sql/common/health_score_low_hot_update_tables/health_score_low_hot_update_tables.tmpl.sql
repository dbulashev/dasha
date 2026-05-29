-- Tables with the lowest HOT-update ratio (most UPDATEs that rewrite every
-- index). Inline detail for the low_hot_update_ratio recommendation.
SELECT
    schemaname AS schema_name,
    relname AS table_name,
    n_tup_upd::bigint AS updates,
    n_tup_hot_upd::bigint AS hot_updates,
    (n_tup_hot_upd::float8 / NULLIF(n_tup_upd, 0))::float8 AS hot_ratio
FROM pg_stat_user_tables
WHERE n_tup_upd > 1000
ORDER BY hot_ratio NULLS FIRST, n_tup_upd DESC
LIMIT 20
