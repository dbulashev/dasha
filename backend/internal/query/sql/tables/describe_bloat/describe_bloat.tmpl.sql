SELECT
    table_len,
    pg_catalog.pg_size_pretty(table_len) AS table_len_pretty,
    approx_tuple_count,
    approx_tuple_len,
    pg_catalog.pg_size_pretty(approx_tuple_len) AS approx_tuple_len_pretty,
    approx_tuple_percent,
    dead_tuple_count,
    dead_tuple_len,
    pg_catalog.pg_size_pretty(dead_tuple_len) AS dead_tuple_len_pretty,
    dead_tuple_percent,
    approx_free_space,
    pg_catalog.pg_size_pretty(approx_free_space) AS approx_free_space_pretty,
    approx_free_percent
FROM pgstattuple_approx((quote_ident($1) || '.' || quote_ident($2))::regclass)
