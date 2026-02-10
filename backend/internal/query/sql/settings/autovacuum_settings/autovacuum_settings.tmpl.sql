SELECT name, setting, unit, source
FROM pg_catalog.pg_settings
WHERE name IN (
  'autovacuum', 'autovacuum_max_workers', 'autovacuum_vacuum_cost_limit',
  'autovacuum_vacuum_scale_factor', 'autovacuum_analyze_scale_factor',
  'autovacuum_vacuum_cost_delay', 'autovacuum_work_mem',
  'autovacuum_analyze_threshold', 'autovacuum_naptime',
  'autovacuum_vacuum_insert_scale_factor', 'autovacuum_vacuum_insert_threshold',
  'autovacuum_vacuum_threshold', 'vacuum_cost_limit'
)
ORDER BY name
