SELECT COUNT(*) > 0 AS available FROM pg_available_extensions WHERE name = '{{ .Extension }}'
