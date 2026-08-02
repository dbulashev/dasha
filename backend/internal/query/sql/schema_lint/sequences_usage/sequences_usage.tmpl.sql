-- numeric cast: seqmax - seqmin overflows bigint on full-range sequences.
-- The privilege is asked directly: pg_sequences keeps the row either way and
-- nulls last_value inside a CASE, so a missing value alone cannot tell "never
-- called" (NULL, and seqstart is right) from "not allowed to look" (NULL, and
-- seqstart would report an exhausted sequence as untouched).
SELECT
    n.nspname                                       AS schema_name,
    c.relname                                       AS object_name,
    COALESCE(sv.last_value, s.seqstart)             AS last_value,
    (pg_catalog.has_sequence_privilege(c.oid, 'SELECT,USAGE')
     OR pg_catalog.pg_has_role(c.relowner, 'USAGE')) AS last_value_known,
    s.seqmax                                        AS max_value,
    s.seqmin                                        AS min_value,
    CASE
        WHEN s.seqincrement > 0
            THEN 100.0 * (s.seqmax::numeric - COALESCE(sv.last_value, s.seqstart)::numeric)
                 / NULLIF(s.seqmax::numeric - s.seqmin::numeric, 0)
        ELSE 100.0 * (COALESCE(sv.last_value, s.seqstart)::numeric - s.seqmin::numeric)
                 / NULLIF(s.seqmax::numeric - s.seqmin::numeric, 0)
    END                                             AS free_pct,
    COALESCE(dn.nspname || '.' || dc.relname || '.' || da.attname, '') AS owned_by,
    COALESCE(format_type(da.atttypid, NULL), '')    AS owned_column_type
FROM pg_catalog.pg_sequence s
    JOIN pg_catalog.pg_class     c ON c.oid = s.seqrelid
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    LEFT JOIN pg_catalog.pg_sequences sv
           ON sv.schemaname = n.nspname AND sv.sequencename = c.relname
    LEFT JOIN pg_catalog.pg_depend d
           ON d.classid = 'pg_class'::regclass AND d.objid = c.oid AND d.deptype IN ('a', 'i')
    LEFT JOIN pg_catalog.pg_class     dc ON dc.oid = d.refobjid
    LEFT JOIN pg_catalog.pg_namespace dn ON dn.oid = dc.relnamespace
    LEFT JOIN pg_catalog.pg_attribute da ON da.attrelid = d.refobjid AND da.attnum = d.refobjsubid
WHERE NOT s.seqcycle
  AND n.nspname NOT LIKE 'pg\_%'
  AND n.nspname <> 'information_schema'
ORDER BY free_pct NULLS FIRST
LIMIT $1
