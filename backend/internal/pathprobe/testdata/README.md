# Test traces

Format: pathprobe 9bcf24c (`r2d2/dmitry-attribution`), `docs/technical/json-format.md`.

- `demo-index.json`, `demo-join.json`: real `pathprobe_json()` output from the
  staging stand (`make demo-lab-staging`, image
  `dbulashev/postgres-pathprobe:20devel-425daf545d91-pp9bcf24c` built from
  pathprobe 9bcf24c), the two demo queries of running-postgresql
  `experiments/ch08-path-probe/hackers/demo`. Regenerate with
  `make demo-lab-staging-trace` and copy from `demo/staging/traces/`
  (`trace-observed.json`, `trace-observed-join.json`) after a fresh
  `make demo-lab-staging-down && make demo-lab-staging`. The demo tables have
  autovacuum off (`demo/staging/path-probe/01-setup.sql`): with a filled
  visibility map Index Only Scan gets cheaper and the join demo takes another
  search path. Costs still move slightly between runs (ANALYZE samples); compare
  the event list before replacing a file.
- `demo-index.plan.json`, `demo-join.plan.json`: `EXPLAIN (ANALYZE, BUFFERS,
  FORMAT JSON)` of the same queries from the same stand run
  (`trace-observed.plan.json`, `trace-observed-join.plan.json`). A plan is
  paired only with the trace of its own run.
- `demo-index.plan.txt`, `demo-join.plan.txt`: the text EXPLAIN (COSTS, no
  ANALYZE) at the top of `trace-observed.txt` and `trace-observed-join.txt` of
  the same run, up to the first empty line.
- `removed.json`, `bitmap-or.json`, `setop.json`: hand-written after the queries
  of pathprobe `sql/pathprobe_graph.sql` (section 8: a path removed by a path
  that is then rejected; section 7: `child_path_ids` of BitmapOr and SetOp, the
  subquery paths below SubqueryScan). The stand does not produce these shapes.
- `truncated.json`, `no-accepted.json`: hand-written, small (a truncated trace,
  a run with `pathprobe.log_accepted = off`).

Field names and shapes of the hand-written traces follow the real output; the
numbers are made up.
