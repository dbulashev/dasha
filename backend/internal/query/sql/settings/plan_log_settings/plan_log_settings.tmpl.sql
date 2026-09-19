SELECT name, setting
FROM pg_catalog.pg_settings
WHERE name IN (
  'auto_explain.log_min_duration', 'auto_explain.log_analyze',
  'auto_explain.log_format', 'auto_explain.log_level',
  'compute_query_id'
)
ORDER BY name
