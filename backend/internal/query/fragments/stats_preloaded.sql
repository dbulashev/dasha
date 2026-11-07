EXISTS (
    SELECT FROM pg_catalog.pg_settings
    WHERE name = ANY($SETTINGS::text[]) AND vartype <> 'string'
)
