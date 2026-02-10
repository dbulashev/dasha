SELECT EXISTS(
    SELECT 1 FROM pg_catalog.pg_extension WHERE extname = 'pgstattuple'
) AS available
