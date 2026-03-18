WITH
    idx AS (
        SELECT
            c.relname as table_name,
            ic.relname as index_name,
            ic.oid,
            i.indisunique,
            i.indrelid,
            pg_get_indexdef(ic.oid) AS object_definition,
            replace(
                    regexp_replace(
                            regexp_replace(
                                    regexp_replace(
                                            regexp_replace(
                                                    regexp_replace(
                                                            regexp_replace(
                                                                    regexp_replace(
                                                                            regexp_replace(
                                                                                    regexp_replace(
                                                                                            pg_get_indexdef(ic.oid), ' INDEX .* ON ', ' INDEX ON '),
                                                                                    ' NULLS FIRST\)', ')'),
                                                                            ' NULLS FIRST,', ','),
                                                                    ' NULLS LAST\)', ')'),
                                                            ' NULLS LAST,', ','),
                                                    ' DESC\)', ')'),
                                            ' DESC,', ','),
                                    ' WHERE .*', ''),
                            ' INCLUDE .*', ''),
                    ' UNIQUE ', ' ')
                AS simplified_object_definition,
            (SELECT string_agg(format('%I', c.conname), ',') FROM pg_catalog.pg_constraint AS c WHERE c.conindid = ic.oid)
                AS used_in_constraint
        FROM pg_catalog.pg_index AS i
                 INNER JOIN pg_catalog.pg_class AS ic ON i.indexrelid = ic.oid
                 INNER JOIN pg_catalog.pg_class AS c ON i.indrelid = c.oid
    )
SELECT
    i1.table_name,
    i1.index_name as i1_index_name,
    i2.index_name as i2_index_name,
    i1.simplified_object_definition as simplified_index_definition,
    i1.object_definition as i1_index_definition,
    i2.object_definition as i2_index_definition,
    i1.used_in_constraint as i1_used_in_constraint,
    i2.used_in_constraint as i2_used_in_constraint
FROM idx as i1
         INNER JOIN idx AS i2 ON i1.oid < i2.oid AND i1.indrelid = i2.indrelid
    AND i1.simplified_object_definition = i2.simplified_object_definition
ORDER BY 1, 2;