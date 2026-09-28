WITH {{.ColdCTE}},
leaf_stats AS (
    SELECT c.idx,
           sum(s.n_dead_tup)::bigint AS n_dead,
           sum(s.n_live_tup + s.n_dead_tup)::bigint AS n_total,
           bool_or(s.n_live_tup + s.n_dead_tup > 10000
                   AND s.last_vacuum IS NULL AND s.last_autovacuum IS NULL) AS never_vacuumed
    FROM cold c
    JOIN pg_stat_user_tables s ON s.relid = c.relid
    GROUP BY c.idx
),
freeze_age AS (
    SELECT cr.idx, max(age(c.relfrozenxid))::bigint AS relfrozenxid_age
    FROM cold_rel cr
    JOIN pg_class c ON c.oid = cr.relid AND c.relkind IN ('r','m','t')
    GROUP BY cr.idx
),
picked AS (
    SELECT l.idx,
           COALESCE(round(100.0 * l.n_dead / nullif(l.n_total, 0), 2), 0)::float8 AS dead_ratio,
           l.never_vacuumed,
           COALESCE(f.relfrozenxid_age, 0) AS relfrozenxid_age
    FROM leaf_stats l
    LEFT JOIN freeze_age f USING (idx)
    CROSS JOIN (
        SELECT setting::bigint AS freeze_table_age FROM pg_settings WHERE name = 'vacuum_freeze_table_age'
    ) g
    WHERE (l.n_total > 10000 AND 100.0 * l.n_dead / nullif(l.n_total, 0) > 10)
       OR l.never_vacuumed
       OR COALESCE(f.relfrozenxid_age, 0) >= g.freeze_table_age
),
ranked AS (
    SELECT p.*, count(*) OVER () AS total
    FROM picked p
    ORDER BY p.relfrozenxid_age DESC, p.dead_ratio DESC, p.idx
    LIMIT $4
)
SELECT r.idx::int,
       r.dead_ratio,
       r.never_vacuumed,
       r.relfrozenxid_age,
       (SELECT COALESCE(sum(pg_table_size(c.relid)), 0) FROM cold c WHERE c.idx = r.idx)::bigint AS size_bytes,
       r.total::int
FROM ranked r
ORDER BY r.relfrozenxid_age DESC, r.dead_ratio DESC, r.idx
