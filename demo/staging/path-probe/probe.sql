-- Probe one query, passed as the psql variable q (the query text, without a
-- trailing semicolon):
--
--   psql -v q="$(cat query.sql)" -f trace-on.sql -f probe.sql
--
-- Prints the final plan and the pathprobe text report.  The JSON report goes
-- to a separate run with -v json=1, and EXPLAIN (FORMAT JSON) to one with
-- -v plan=1 (add -v analyze=1 to run the query for actual rows), so each
-- output stays parseable.

\pset footer off
\if :{?json}
\pset tuples_only on
\pset format unaligned
SELECT jsonb_pretty(pathprobe_json(:'q')::jsonb);
\elif :{?plan}
SET pathprobe.explain = off;
\pset tuples_only on
\pset format unaligned
\if :{?analyze}
EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) :q;
\else
EXPLAIN (FORMAT JSON) :q;
\endif
\else
-- The PathProbe section is EXPLAIN's; turn it off here, the report below
-- carries the same events.
SET pathprobe.explain = off;
EXPLAIN (COSTS ON) :q;
\pset tuples_only on
\pset format unaligned
SELECT pathprobe(:'q');
\endif
