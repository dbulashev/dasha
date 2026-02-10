SELECT 'logMinDurationTooLow' AS key, jsonb_build_object('value', setting) AS params
  FROM pg_catalog.pg_settings WHERE name = 'log_min_duration_statement' AND setting::int BETWEEN 0 AND 10
UNION ALL
SELECT 'collapseLimitDiffer', jsonb_build_object('name', name, 'value', setting)
  FROM pg_catalog.pg_settings WHERE name IN ('from_collapse_limit', 'join_collapse_limit') AND setting <> '8'
UNION ALL
SELECT 'hugePagesDisabled', jsonb_build_object('value', setting)
  FROM pg_catalog.pg_settings WHERE name = 'huge_pages' AND setting NOT IN ('on', 'try')
UNION ALL
SELECT 'suboptimalCompression', jsonb_build_object('name', name, 'value', setting)
  FROM pg_catalog.pg_settings WHERE name IN ('default_toast_compression', 'wal_compression') AND setting = 'pglz'
UNION ALL
SELECT 'autovacuumDisabled', jsonb_build_object('value', setting)
  FROM pg_catalog.pg_settings WHERE name = 'autovacuum' AND setting <> 'on'
UNION ALL
SELECT 'tuneParameter', jsonb_build_object('name', name, 'value', setting)
  FROM pg_catalog.pg_settings WHERE name IN (
    'autovacuum_analyze_scale_factor', 'autovacuum_analyze_threshold', 'autovacuum_naptime',
    'autovacuum_vacuum_insert_scale_factor', 'autovacuum_vacuum_insert_threshold',
    'autovacuum_vacuum_scale_factor', 'autovacuum_vacuum_threshold', 'vacuum_cost_limit'
  ) AND "source" = 'default'
UNION ALL
SELECT 'tuneCostLimit', jsonb_build_object('value', coalesce(
    NULLIF((SELECT setting FROM pg_catalog.pg_settings WHERE "name" = 'autovacuum_vacuum_cost_limit'), '-1'),
    (SELECT setting FROM pg_catalog.pg_settings WHERE "name" = 'vacuum_cost_limit')
  ))
  WHERE coalesce(
    NULLIF((SELECT setting FROM pg_catalog.pg_settings WHERE "name" = 'autovacuum_vacuum_cost_limit'), '-1'),
    (SELECT setting FROM pg_catalog.pg_settings WHERE "name" = 'vacuum_cost_limit')
  )::int < 1000
UNION ALL
SELECT 'tuneCostDelay', jsonb_build_object('value', setting)
  FROM pg_catalog.pg_settings WHERE name = 'autovacuum_vacuum_cost_delay' AND setting::int > 5
UNION ALL
SELECT 'tuneWorkMem', jsonb_build_object('value', coalesce(
    NULLIF((SELECT setting FROM pg_catalog.pg_settings WHERE "name" = 'autovacuum_work_mem'), '-1'),
    (SELECT setting FROM pg_catalog.pg_settings WHERE "name" = 'maintenance_work_mem')
  ))
  WHERE coalesce(
    NULLIF((SELECT setting FROM pg_catalog.pg_settings WHERE "name" = 'autovacuum_work_mem'), '-1'),
    (SELECT setting FROM pg_catalog.pg_settings WHERE "name" = 'maintenance_work_mem')
  )::int <= 65536
UNION ALL
SELECT 'tooManyCheckpoints', jsonb_build_object('req', checkpoints_req::text, 'timed', checkpoints_timed::text)
  FROM pg_stat_bgwriter
  WHERE checkpoints_req > 0.3 * checkpoints_timed
    AND checkpoints_req / GREATEST(DATE_PART('day', now() - stats_reset)::integer, 1) > 20
UNION ALL
SELECT 'highMaxwrittenClean', jsonb_build_object('value', (maxwritten_clean / GREATEST(DATE_PART('day', now() - stats_reset)::integer, 1))::text)
  FROM pg_stat_bgwriter
  WHERE maxwritten_clean / GREATEST(DATE_PART('day', now() - stats_reset)::integer, 1) > 1000
