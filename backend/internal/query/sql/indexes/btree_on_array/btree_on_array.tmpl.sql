SELECT
    c.relname,
    ic.relname as index
FROM pg_catalog.pg_index AS i
         INNER JOIN pg_catalog.pg_class AS ic ON i.indexrelid = ic.oid
         INNER JOIN pg_catalog.pg_am AS a ON ic.relam = a.oid AND a.amname = 'btree'
         INNER JOIN pg_catalog.pg_class AS c ON i.indrelid = c.oid
WHERE
    EXISTS (SELECT * FROM pg_catalog.pg_attribute AS att
                              INNER JOIN pg_catalog.pg_type AS typ ON typ.oid = att.atttypid
            WHERE att.attrelid = i.indrelid
              AND att.attnum = ANY ((string_to_array(indkey::text, ' ')::int2[])[1:indnkeyatts])
              AND typ.typcategory = 'A')
  AND NOT indisunique;
