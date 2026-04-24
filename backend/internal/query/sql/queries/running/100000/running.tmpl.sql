SELECT pid::int,
       state,
       application_name                                                       AS source,
       age(NOW(), COALESCE(query_start, xact_start))                          AS duration,
       (wait_event IS NOT NULL)                                               AS waiting,
       query,
       COALESCE(query_start, xact_start)                                      AS started_at,
       EXTRACT(EPOCH FROM NOW() - COALESCE(query_start, xact_start)) * 1000.0 AS duration_ms,
       COALESCE(usename, '')                                                  AS user,
       ''::text                                                               AS backend_type
FROM pg_stat_activity
WHERE state <> 'idle'
  AND pid <> pg_backend_pid()
  AND datname = current_database()
  AND NOW() - COALESCE(query_start, xact_start) > ({{ .MinDuration }} * interval '1 millisecond')
  AND query <> '<insufficient privilege>'
ORDER BY COALESCE(query_start, xact_start) DESC