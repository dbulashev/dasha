WITH locked_rels AS MATERIALIZED (
    SELECT relation FROM pg_locks
    WHERE mode = 'AccessExclusiveLock' AND granted
      AND database IN (0, (SELECT oid FROM pg_database WHERE datname = current_database()))
),
constants AS (SELECT current_setting('block_size')::numeric bs, 23 hdr, 8 ma),
     safe_tables AS MATERIALIZED (
         SELECT c.oid, c.relname, c.relkind, c.relpages, c.reltuples, c.reltoastrelid, c.reloptions, ns.nspname
         FROM pg_class c
                  JOIN pg_catalog.pg_namespace ns ON ns.oid = c.relnamespace
         WHERE c.relkind IN ('p', 'r', 'm')
           AND NOT EXISTS (SELECT 1 FROM locked_rels l WHERE l.relation = c.oid)
     ),
     index_pages AS (
         SELECT i.indrelid AS relid, sum(ic.relpages)::bigint AS pages
         FROM pg_index i
                  JOIN pg_class ic ON ic.oid = i.indexrelid
         GROUP BY i.indrelid
     ),
     candidates AS (
         SELECT s.*,
                s.relpages + coalesce(t.relpages, 0) + coalesce(ti.pages, 0) + coalesce(ip.pages, 0) AS approx_pages
         FROM safe_tables s
                  LEFT JOIN pg_class t ON t.oid = s.reltoastrelid AND t.relkind = 't'
                  LEFT JOIN index_pages ti ON ti.relid = s.reltoastrelid
                  LEFT JOIN index_pages ip ON ip.relid = s.oid
         ORDER BY approx_pages DESC
         LIMIT greatest($1 * 4, 50)
     ),
     sized_candidates AS (
         SELECT c.*, pg_total_relation_size(c.oid) AS total_bytes
         FROM candidates c
     ),
     top_tables AS (
         SELECT *
         FROM sized_candidates
         WHERE total_bytes > 500 * 2 ^ 10
         ORDER BY total_bytes DESC
         LIMIT $1
     ),
     top_columns AS (
         SELECT t.oid, t.nspname AS table_schema, t.relname AS table_name, a.attname AS column_name
         FROM top_tables t
                  JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum > 0 AND NOT a.attisdropped
     ),
     top_stats AS (
         SELECT s.schemaname, s.tablename, s.attname, s.null_frac, s.avg_width
         FROM {{ .PgStatsView }} s
                  JOIN top_tables t ON t.nspname = s.schemaname AND t.relname = s.tablename
     ),
     no_stats AS (SELECT c.table_schema,
                         c.table_name,
                         pg_table_size(c.oid)::numeric table_size
                  FROM top_columns c
                           JOIN pg_stat_user_tables psut ON psut.relid = c.oid
                           LEFT JOIN top_stats s ON s.schemaname = c.table_schema AND s.tablename = c.table_name AND
                                                    s.attname = c.column_name
                  WHERE s.attname IS NULL
                    AND c.table_schema NOT IN ('pg_catalog', 'information_schema')
                  GROUP BY c.table_schema, c.table_name, c.oid),
     null_headers AS (SELECT hdr + 1 + sum(CASE WHEN null_frac <> 0 THEN 1 ELSE 0 END) / 8 nullhdr,
                             sum((1 - null_frac) * avg_width)                              datawidth,
                             max(null_frac)                                                maxfracsum,
                             schemaname,
                             tablename,
                             hdr,
                             ma,
                             bs
                      FROM top_stats
                               CROSS JOIN constants
                               LEFT JOIN no_stats
                                         ON schemaname = no_stats.table_schema AND tablename = no_stats.table_name
                      WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
                        AND no_stats.table_name IS NULL
                      GROUP BY schemaname, tablename, hdr, ma, bs),
     data_headers AS (SELECT ma,
                             bs,
                             hdr,
                             schemaname,
                             tablename,
                             (datawidth + (hdr + ma - CASE WHEN hdr % ma = 0 THEN ma ELSE hdr % ma END))::numeric datahdr,
                             maxfracsum *
                             (nullhdr + ma - CASE WHEN nullhdr % ma = 0 THEN ma ELSE nullhdr % ma END)            nullhdr2
                      FROM null_headers),
     table_estimates AS (SELECT dh.schemaname,
                                dh.tablename,
                                dh.bs,
                                t.relpages * dh.bs                                            table_bytes,
                                ceil(t.reltuples * (datahdr + nullhdr2 + 4 + dh.ma -
                                                    CASE WHEN datahdr % dh.ma = 0 THEN dh.ma ELSE datahdr % dh.ma END) /
                                     (dh.bs - 20)) * dh.bs                                    expected_bytes,
                                t.reltoastrelid
                         FROM data_headers dh
                                  JOIN top_tables t ON t.nspname = dh.schemaname AND t.relname = dh.tablename
                         WHERE t.relkind = 'r'),
     estimates_with_toast AS (SELECT schemaname,
                                     tablename,
                                     table_bytes + coalesce(toast.relpages, 0) * bs               table_bytes,
                                     expected_bytes + ceil(coalesce(toast.reltuples, 0) / 4) * bs expected_bytes
                              FROM table_estimates
                                       LEFT JOIN pg_class toast
                                                 ON table_estimates.reltoastrelid = toast.oid AND toast.relkind = 't'),
     table_estimates_plus AS (SELECT current_database()         databasename,
                                     schemaname,
                                     tablename,
                                     CASE
                                         WHEN table_bytes > 0 THEN table_bytes::numeric
                                         ELSE NULL::numeric END table_bytes,
                                     CASE
                                         WHEN expected_bytes > 0 THEN expected_bytes::numeric
                                         ELSE NULL::numeric END expected_bytes,
                                     CASE
                                         WHEN expected_bytes > 0 AND table_bytes > 0 AND expected_bytes <= table_bytes
                                             THEN (table_bytes - expected_bytes)::numeric
                                         ELSE 0::numeric END    bloat_bytes
                              FROM estimates_with_toast
                              UNION ALL
                              SELECT current_database() databasename,
                                     table_schema,
                                     table_name,
                                     table_size,
                                     NULL::numeric,
                                     NULL::numeric
                              FROM no_stats),
     bloat_data AS (SELECT current_database()                     databasename,
                           schemaname,
                           tablename,
                           table_bytes,
                           expected_bytes,
                           round(bloat_bytes * 100 / table_bytes) table_bloat_pct,
                           bloat_bytes
                    FROM table_estimates_plus),
     pg_table_bloat as (SELECT databasename                as database,
                               schemaname                  as schema,
                               tablename                   as table,
                               table_bloat_pct,
                               pg_size_pretty(bloat_bytes) as bloat_size,
                               pg_size_pretty(table_bytes) as table_size
                        FROM bloat_data)
SELECT c.nspname || '.' || c.relname                              AS table,
       (SELECT count(*) FROM pg_index i WHERE i.indrelid = c.oid) AS n_idx,
       c.total_bytes                                              AS total_bytes,
       pg_size_pretty(c.total_bytes)                              AS total,
       pg_size_pretty(pg_relation_size(c.reltoastrelid))          AS toast,
       pg_size_pretty(pg_indexes_size(c.oid))                     AS indexes,
       pg_size_pretty(pg_relation_size(c.oid, 'main'))            AS main,
       pg_size_pretty(pg_relation_size(c.oid, 'fsm'))             AS fsm,
       pg_size_pretty(pg_relation_size(c.oid, 'vm'))              AS vm,
       (SELECT nullif(
               CASE WHEN COALESCE(n_tup_ins, 0) + COALESCE(n_tup_upd, 0) + COALESCE(n_tup_del, 0) > 0
                    THEN 'INS: ' || to_char(100.0 * COALESCE(n_tup_ins, 0) / (COALESCE(n_tup_ins, 0) + COALESCE(n_tup_upd, 0) + COALESCE(n_tup_del, 0)), 'FM990.0') || '%, '
                      || 'UPD: ' || to_char(100.0 * COALESCE(n_tup_upd, 0) / (COALESCE(n_tup_ins, 0) + COALESCE(n_tup_upd, 0) + COALESCE(n_tup_del, 0)), 'FM990.0') || '%, '
                      || 'DEL: ' || to_char(100.0 * COALESCE(n_tup_del, 0) / (COALESCE(n_tup_ins, 0) + COALESCE(n_tup_upd, 0) + COALESCE(n_tup_del, 0)), 'FM990.0') || '%'
                      || COALESCE(', HOT UPD: ' || to_char(100.0 * COALESCE(n_tup_hot_upd, 0) / nullif(COALESCE(n_tup_upd, 0), 0), 'FM990.0') || '%', '')
                    ELSE '' END
               || CASE WHEN COALESCE(seq_scan, 0) + COALESCE(idx_scan, 0) > 0
                    THEN CASE WHEN COALESCE(n_tup_ins, 0) + COALESCE(n_tup_upd, 0) + COALESCE(n_tup_del, 0) > 0 THEN ', ' ELSE '' END
                      || CASE WHEN COALESCE(idx_scan, 0) = 0 THEN 'SEQ_SCN: 100%'
                              WHEN COALESCE(seq_scan, 0) = 0 THEN 'IDX_SCN: 100%'
                              ELSE 'SEQ_SCN: ' || to_char(100.0 * COALESCE(seq_scan, 0) / (COALESCE(seq_scan, 0) + COALESCE(idx_scan, 0)), 'FM990.0') || '%'
                         END
                    ELSE '' END
               , '')
        FROM pg_stat_all_tables s
        WHERE s.relid = c.oid)                                    AS stat_info,
       bloat_size || '(' || ptb.table_bloat_pct || '%)'           as bloat,
       replace(translate(c.reloptions::text, '{}', ''), ',', ', ') as reloptions
FROM top_tables c
         LEFT JOIN pg_table_bloat ptb
                   ON ptb."database" = current_database() AND ptb."schema" = c.nspname AND ptb."table" = c.relname
ORDER BY c.total_bytes DESC;
