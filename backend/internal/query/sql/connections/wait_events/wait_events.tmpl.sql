SELECT
    wait_event_type,
    wait_event,
    COUNT(*) AS count
FROM pg_stat_activity
WHERE backend_type = 'client backend'
  AND wait_event IS NOT NULL
  AND NOT (wait_event_type = 'Client' AND wait_event = 'ClientRead')
GROUP BY wait_event_type, wait_event
ORDER BY count DESC
