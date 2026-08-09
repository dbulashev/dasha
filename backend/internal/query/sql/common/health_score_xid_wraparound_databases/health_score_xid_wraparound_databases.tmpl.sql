-- Per-database transaction-ID age. Inline detail for the
-- xid_wraparound_risk recommendation: the score raises a single
-- worst-case number, this lists where to look first.
SELECT
    datname,
    age(datfrozenxid)::bigint AS xid_age
FROM pg_database
-- No datallowconn/datistemplate filter: template0 and template1 are often the
-- oldest datfrozenxid in the cluster, so they must stay in the list.
ORDER BY xid_age DESC
LIMIT $1 OFFSET $2
