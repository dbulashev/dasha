WITH locked_rels AS MATERIALIZED (
    SELECT relation FROM pg_locks WHERE mode = 'AccessExclusiveLock' AND granted
),
constants AS (SELECT current_setting('block_size')::numeric bs, 23 hdr, 8 ma),
     monitor_pg_columns as (SELECT table_schema, table_name
                            FROM information_schema.columns),
     monitor_pg_stats as (SELECT schemaname, tablename, attname, null_frac, avg_width, n_distinct
                          FROM {{ .PgStatsView }}),
     no_stats AS (SELECT table_schema,
                         table_name,
                         n_live_tup::numeric           est_rows,
                         pg_table_size(relid)::numeric table_size
                  FROM information_schema.columns
                           JOIN pg_stat_user_tables psut ON table_schema = psut.schemaname AND table_name = psut.relname
                           LEFT JOIN monitor_pg_stats s ON table_schema = s.schemaname AND table_name = s.tablename AND
                                                           column_name = attname
                  WHERE attname IS NULL
                    AND table_schema NOT IN ('pg_catalog', 'information_schema')
                    AND psut.relid NOT IN (SELECT relation FROM locked_rels)
                  GROUP BY table_schema, table_name, relid, n_live_tup),
     null_headers AS (SELECT hdr + 1 + sum(CASE WHEN null_frac <> 0 THEN 1 ELSE 0 END) / 8 nullhdr,
                             sum((1 - null_frac) * avg_width)                              datawidth,
                             max(null_frac)                                                maxfracsum,
                             schemaname,
                             tablename,
                             hdr,
                             ma,
                             bs
                      FROM monitor_pg_stats
                               CROSS JOIN constants
                               LEFT JOIN no_stats
                                         ON schemaname = no_stats.table_schema AND tablename = no_stats.table_name
                      WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
                        AND no_stats.table_name IS NULL
                        AND EXISTS(SELECT 1
                                   FROM monitor_pg_columns c
                                   WHERE schemaname = c.table_schema AND tablename = c.table_name)
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
     table_estimates AS (SELECT schemaname,
                                tablename,
                                bs,
                                relpages * bs        table_bytes,
                                ceil(reltuples * (datahdr + nullhdr2 + 4 + ma -
                                                  CASE WHEN datahdr % ma = 0 THEN ma ELSE datahdr % ma END) /
                                     (bs - 20)) * bs expected_bytes,
                                reltoastrelid
                         FROM data_headers
                                  JOIN pg_class ON tablename = relname
                                  JOIN pg_namespace ON relnamespace = pg_namespace.oid AND schemaname = nspname
                         WHERE pg_class.relkind = 'r'),
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
                        FROM bloat_data),
     safe_tables AS MATERIALIZED (
         SELECT c.oid, c.relname, c.reltoastrelid, c.reloptions, ns.nspname
         FROM pg_class c
                  JOIN pg_catalog.pg_namespace ns ON ns.oid = c.relnamespace
         WHERE c.relkind IN ('p', 'r', 'm')
           AND c.oid NOT IN (SELECT relation FROM locked_rels)
     )
SELECT c.nspname || '.' || c.relname                              AS table,
       (SELECT count(*) FROM pg_index i WHERE i.indrelid = c.oid) AS n_idx,
       pg_total_relation_size(c.oid)                              AS total_bytes,
       pg_size_pretty(pg_total_relation_size(c.oid))              AS total,
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
FROM safe_tables c
         LEFT JOIN pg_table_bloat ptb
                   ON ptb."database" = current_database() AND ptb."schema" = c.nspname AND ptb."table" = c.relname
WHERE pg_total_relation_size(c.oid) > 500 * 2 ^ 10
ORDER BY pg_total_relation_size(c.oid) DESC
limit $1;
