# Plans from the logs

How to read `plan_insights`, `query_plans` and `plan_regressions`. All three read
the plans `auto_explain` writes to the PostgreSQL log, through the log source
bound to the cluster. The answers ship codes and numbers, never prose.

## What the sample is

`auto_explain` logs a statement only when it ran longer than
`auto_explain.log_min_duration`. Every count, `sum_ms` and ranking describes that
slow tail, not the workload:

- a statement absent from the plans is not proven fast; it may never cross the
  threshold, or cross it in a part of the window that was not read;
- a group's `sum_ms` is the time of its logged runs, not of all its runs:
  `top_queries` and `query_report` measure the whole load from
  `pg_stat_statements`;
- a statement that became faster than the threshold vanishes from the current
  window instead of looking better.

A tree is the slowest run of its group, not a typical one.

Plans carry the literals of the user's statement: `auto_explain` prints the query
text always, and Dasha does not mask values inside plans. Quote them only as far
as the answer needs.

## Partial windows

A scan stops at its record, byte or time budget, or when the source gives up.
`window.partial` (and `partial_reasons`: `records`, `bytes`, `plans`, `timeout`,
`source`) says so, and `covered_from` / `covered_to` bound what was read.
VictoriaLogs returns the newest records first, OpenSearch the oldest, so the
unread part is at either end.

On a partial window every count is a lower bound. In `plan_regressions` a
partial side makes `p50_ratio` / `p95_ratio` compare two samples of unknown size:
never state them as "N times slower". `new_shape` and `lost_index` stand
regardless: a shape or an index seen is seen.

## An empty answer

`plans.empty_reason` names why no group came back:

- `no_records` — the window held no log record at all (wrong window, host or
  source);
- `no_plan_records` — records, but none from `auto_explain`: read
  `configuration`;
- `unsupported_plan_format` — `auto_explain.log_format` is `xml` or `yaml`;
  Dasha parses `text` and `json`;
- `not_parsed` — plan records came back and none parsed; `not_parsed` counts the
  reasons;
- `budget_exhausted` — the scan budget ran out before the first plan;
- `source_unavailable` — the log store did not answer.

`query_plans` adds `not_in_scan`: the stored scan holds no plan of that
`query_id`.

`configuration` is read from `pg_settings` of one host and names what is off:
`auto_explain` not loaded (nothing is logged), `log_min_duration_ms` -1 (nothing
is logged) or so high that little is, `log_analyze` false (plans carry
estimates only; the rules that need measured rows do not run),
`compute_query_id` off (plans cannot be tied to `pg_stat_statements`). Report
the setting instead of "there are no slow queries".

## Categories

`plan_insights` counts every record of the window into one category, by
SQLSTATE first, then by the record itself, then by its English message text:
`deadlock`, `lock_wait`, `canceled`, `connection_limit`, `authentication`,
`error`, `temp_file`, `checkpoint`, `autovacuum`, `connection`, `slow_query`
(a `log_min_duration_statement` record: text, no plan), `plan`, and `other`.
`other` is what matched nothing; its share is shown, never hidden. A category
with its records packed into a small part of the window is a burst; compare
`first_seen` and `last_seen` with the window.

## Groups and shapes

A group is one statement with one plan shape: `query_id`, the plan `hash` and
the normalized text. One `query_id` with several groups means the planner
chose different plans for it within the window; that alone is worth reporting.
Statements inside a function share the `query_id` of the outer call, so one
`query_id` may hold the function call and its inner statements as separate
groups. `without_query_id` counts plans whose record carries none
(`compute_query_id` off, or the log fields do not include it).

`ord` is the rank of a group by total time within its scan and names it in
`query_plans(scan_id, ord)`. A stored scan lives at most a day; a 404 on its
`scan_id` means it was cleared, and a new `plan_insights` gives a fresh one.

## Findings

A finding is a rule that fired on the group's slowest plan. `node` and
`relation` locate it; in a `query_plans` tree the node is marked `!!`. `params`
carries the numbers. A rule that could not run is listed in `dormant` with what
the plan lacked (`actual`, `timing`, `buffers`, `work_mem`): a dormant rule is
not a clean result.

### seq_scan_large

A Seq Scan with a filter over a table of 100 000 rows or more (`table_rows`
from the catalog, or `scanned_rows` measured). Without either, a scan costing
1 000 or more is reported at LOW with `estimate_only`. The first action is
`index_advisor` on the database: it proposes the index from the real workload
and may carry `evidence` for this very scan.

### cost_hotspot

On plans without measured numbers only: one node holds half or more of the
plan's cost (`cost_share`). A pointer to where to look, never a diagnosis.

### nested_loop_blowup

A Nested Loop estimated at 1 000 outer rows or more and a million row pairs or
more. The join method is chosen on estimates; check `row_misestimate` on its
inputs first.

### index_candidate_join

A Nested Loop whose inner side is a Seq Scan while there is a join condition or
filter: every outer row rescans the inner table. The inner relation is the
table an index would serve; hand it to `index_advisor`.

### sort_estimate_spill

A Sort or Hash whose estimated size exceeds `work_mem` (`estimated_kb` against
`work_mem_kb`). An estimate: `sort_spill_actual` is the measured one. Raising
`work_mem` globally multiplies across sessions; per role or per statement is
safer.

### cte_materialize

A CTE Scan or Materialize estimated at 100 000 rows or more. Since PostgreSQL 12
a CTE referenced once is inlined unless written `MATERIALIZED`.

### row_misestimate

Estimated and measured rows of one pass differ tenfold or more (`ratio`), on a
node where either side reaches 100 rows. Statistics come first: `ANALYZE`, a
higher statistics target on the column, extended statistics for correlated
columns. An index proposed over a misestimated table is built on the same
wrong numbers.

### sort_spill_actual

The sort ran as an external merge on disk (`sort_method`, `sort_space_kb`).

### filter_discards_rows

A filter threw away 10 000 rows or more and 90% or more of what the node read
(`rows_removed`, `removed_share`). The classic sign of a missing index or a
predicate an index cannot use (a function over the column, a type cast).

### heap_fetches_high

An Index Only Scan went to the heap for 1 000 rows or more and a fifth or more
of its rows: the visibility map is stale. The fix is `VACUUM`, not another
index.

### loops_blowup

The inner side of a Nested Loop ran ten times more often than the outer
estimate expected, and at least 1 000 times (`loops`, `expected_loops`). The
outer side was underestimated; look for `row_misestimate` below it.

### bitmap_lossy

A Bitmap Heap Scan went lossy on 10% or more of its blocks: `work_mem` was too
small for an exact bitmap, and every lossy block is rechecked row by row.

### workers_not_launched

Fewer parallel workers launched than planned: `max_parallel_workers` or
`max_worker_processes` ran out at that moment.

### jit_overhead

JIT took a quarter or more of the execution and at least 10 ms. Usually a sign
of `jit_above_cost` too low for short statements.

### trigger_time

Triggers took a quarter or more of the execution and at least 10 ms: the cost
is in the trigger, not in the plan above it.

## Regressions

`plan_regressions` lists a statement present in both windows when one of these
holds:

- `new_shape` — the current window holds a plan shape the baseline did not
  (`added_hashes`, `removed_hashes`);
- `lost_index` — an index the baseline read is no longer read
  (`lost_indexes`); usually dropped, invalid, or no longer chosen after a
  statistics change;
- `slower` — `p95` of the dominant shape grew at least twofold, with at least
  20 plans of that shape on each side. Below 20 plans a nearest-rank p95 is the
  slowest run, so the reason is left out however large the ratio.

`severity` is HIGH for a lost index or a `slower` p95 grown tenfold or more,
MEDIUM for any other `slower`, LOW for a new shape that cost no time.
`current.count` and `baseline.count` are the plans behind the ratios. A
statement present in one window only is not listed. Choose a baseline of the
same hours (`baseline_shift` 24h, or 7d for weekly load); a baseline beyond the
log store's retention comes back empty, which is no regression.
