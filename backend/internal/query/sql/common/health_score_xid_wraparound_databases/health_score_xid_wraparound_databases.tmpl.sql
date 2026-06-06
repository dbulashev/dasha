-- Per-database transaction-ID age. Inline detail for the
-- xid_wraparound_risk recommendation: the score raises a single
-- worst-case number, this lists where to look first.
SELECT
    datname,
    age(datfrozenxid)::bigint AS xid_age
FROM pg_database
WHERE datallowconn AND NOT datistemplate
ORDER BY xid_age DESC
LIMIT 10
