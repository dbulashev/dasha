WITH
    fk_with_attributes AS (
        SELECT
            c.conname as fk_name,
            c.conrelid,
            c.confrelid,
            c.confmatchtype,
            fk_conkey.conkey_order AS att_order,
            fk_conkey.conkey_number,
            fk_confkey.confkey_number,
            rel_att.attname AS rel_att_name,
            rel_att.atttypid AS rel_att_type_id,
            rel_att.atttypmod AS rel_att_type_mod,
            rel_att.attnotnull AS rel_att_notnull,
            frel_att.attname AS frel_att_name,
            frel_att.atttypid AS frel_att_type_id,
            frel_att.atttypmod AS frel_att_type_mod,
            frel_att.attnotnull AS frel_att_notnull
        FROM pg_catalog.pg_constraint AS c
                 CROSS JOIN LATERAL UNNEST(c.conkey) WITH ORDINALITY AS fk_conkey(conkey_number, conkey_order)
                 LEFT JOIN LATERAL UNNEST(c.confkey) WITH ORDINALITY AS fk_confkey(confkey_number, confkey_order)
                           ON fk_conkey.conkey_order = fk_confkey.confkey_order
                 LEFT JOIN pg_catalog.pg_attribute AS rel_att
                           ON rel_att.attrelid = c.conrelid AND rel_att.attnum = fk_conkey.conkey_number
                 LEFT JOIN pg_catalog.pg_attribute AS frel_att
                           ON frel_att.attrelid = c.confrelid AND frel_att.attnum = fk_confkey.confkey_number
        WHERE c.contype IN ('f')
    )
SELECT
    c.fk_name,
    r_from.relname,
    c.rel_att_names
FROM (
         SELECT
             fk_name,
             conrelid,
             confrelid,
             array_agg (rel_att_name order by att_order ) as rel_att_names
         FROM fk_with_attributes
         WHERE
             (rel_att_notnull IS NOT TRUE)
           AND confmatchtype NOT IN ('f')
         GROUP BY 1, 2, 3
     ) AS c
         INNER JOIN pg_catalog.pg_class AS r_from
                    ON r_from.oid = c.conrelid;
