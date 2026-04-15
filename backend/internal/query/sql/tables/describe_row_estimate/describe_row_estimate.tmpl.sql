WITH tbl AS (
    SELECT c.oid, c.reloptions
    FROM pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = $1 AND c.relname = $2
),
cols AS (
    SELECT
        a.attname AS column_name,
        CASE a.attstorage
            WHEN 'p' THEN 'plain'
            WHEN 'e' THEN 'external'
            WHEN 'x' THEN 'extended'
            WHEN 'm' THEN 'main'
        END AS storage,
        s.avg_width
    FROM pg_attribute a
    LEFT JOIN {{ .PgStatsView }} s ON s.schemaname = $1 AND s.tablename = $2 AND s.attname = a.attname
    WHERE a.attrelid = (SELECT oid FROM tbl)
      AND a.attnum > 0
      AND NOT a.attisdropped
),
agg AS (
    SELECT
        count(*)::int AS columns_total,
        count(avg_width)::int AS columns_with_stats,
        COALESCE(sum(avg_width), 0)::int AS sum_avg_width
    FROM cols
),
fillfactor AS (
    SELECT COALESCE(
        (SELECT option_value::int FROM pg_options_to_table((SELECT reloptions FROM tbl)) WHERE option_name = 'fillfactor'),
        100
    ) AS val
),
toast_candidates AS (
    SELECT column_name, COALESCE(avg_width, 0) AS avg_width, storage
    FROM cols
    WHERE storage IN ('extended', 'external', 'main')
      AND avg_width IS NOT NULL
    ORDER BY avg_width DESC
)
SELECT
    current_setting('block_size')::int AS block_size,
    f.val AS fillfactor,
    a.columns_total,
    a.columns_with_stats,
    a.sum_avg_width,
    (SELECT COALESCE(json_agg(json_build_object(
        'column_name', tc.column_name,
        'avg_width', tc.avg_width,
        'storage', tc.storage
    )), '[]'::json) FROM toast_candidates tc) AS toast_candidates
FROM agg a
CROSS JOIN fillfactor f
