# Index advisor

How to read `index_advisor`. The report is built on demand from the top of
`pg_stat_statements` on every host of the cluster, parsed against the catalog of
one database. It ships codes and numbers, never prose.

There is no `instance` parameter. `pg_stat_statements` is per-host and is not
replicated, so a single host's answer would rank candidates against a workload
it never saw; the index itself IS replicated, so one `CREATE INDEX` on the
primary serves the whole cluster.

## A candidate is not a recommendation

`planner_checked` is `false` on this report, always. Nothing here asked the
planner whether the index would be used: the candidate is a structural
conclusion from the statement text and the catalog — these columns are filtered,
joined, sorted or grouped on, and no existing index covers them.

Turning a candidate into advice takes three things the report does not do:

- read its `warnings`;
- check `unused_index_report` on the same database, so the new index is not laid
  on top of redundant ones nobody removed;
- have a human run the DDL. Dasha never executes DDL.

`EXPLAIN` on the real statement is the only thing that proves the planner would
pick the index. Say so rather than implying the report proved it.

## weight_pct is the size of the problem

`weight_pct` is the share of analyzed execution time held by the statements the
candidate would serve. It is not a predicted gain, and there is no field
anywhere in this report that is. "This index makes the query 31% faster" is
unsupported; "the statements this index would serve hold 31% of the analyzed
execution time" is what the number says.

The candidates' `weight_pct` do not sum to `summary.covered_time_pct`: a
statement covered by two candidates is counted once in `covered_time_pct` and in
full under each candidate.

`candidates_total` is the number of candidates the ranking saw, before the page
was cut. A larger number than `candidates_returned` means the tail was dropped,
not that it is empty — though below a percent or so the tail is heuristic noise.

## Warning codes

Read them before recommending, not after.

- `write_heavy` — the analyzed workload writes the table far more often than it
  runs the statements the index would serve (over ten times as often, with at
  least a thousand write calls). `params.write_calls` and `params.read_calls`
  are both from the `pg_stat_statements` window. The index may cost more than it
  saves; on a hot write path say that plainly.
- `similar_index` — an existing index already holds every column of the
  candidate, in a different order or behind other columns, so it does not serve
  these statements. `names` lists it. This is a fork, not a veto: rewriting that
  index may be the better answer than adding a second one over the same columns.
- `many_indexes` — the table already carries `params.indexes` indexes (ten or
  more). One more is rarely the best trade available; look at what is already
  there first.
- `matview` — the relation is a materialized view. A plain `REFRESH` rewrites it
  and rebuilds every index on it; `REFRESH CONCURRENTLY` needs a unique index
  over plain columns covering every row, which a partial or expression index
  does not provide.
- `partition_root` — the table is partitioned and `params.partitions` counts the
  partitions. See the partitioned-table section below.
- `stats_missing` — no `pg_stats` row for some columns, so the key order is the
  order the statement wrote them and an `IS NULL` filter may have been left out.
  `ANALYZE` the table and ask again.
- `wide_index` — the statements asked for more columns (`params.requested`) than
  the key holds (`params.columns`). The candidate covers part of the access.
- `low_weight` — the covered statements hold under one percent
  (`params.weight_pct`) of the analyzed time. Fine as a footnote, wrong as a
  headline recommendation.

## not_parsed codes

One entry per statement that produced no candidate, counted per reason. Three
groups, and only the third is a gap in the analysis:

**Healthy outcomes.** `already_indexed` — an existing index already serves the
statement, which is what a well-indexed database looks like from here.
`system_relation` — a system catalog or monitoring view, including Dasha's own
polling. `table_too_small` — below `index_advisor.min_table_rows` (10000 by
default), where a sequential scan is the right plan. `no_indexable_predicate` —
parsed and resolved, with nothing an index would help.

**Read to the end, no candidate.** `or_predicate` and `expression_predicate` —
an `OR` branch or a function/cast over the column, which a plain btree index on
that column would not serve. `unsupported_type` — every candidate column is of a
type with no btree operator class; this step proposes btree and nothing else, so
a workload filtering on such types leaves the list empty with the analysis
complete.

**Real gaps.** `truncated` — `track_activity_query_size` clipped the text, so
the tail was never read; raising that setting recovers those statements.
`too_long`, `parse_error`, `insufficient_privilege` (the text was hidden from
the monitoring role), `empty`. `unknown_relation` — the statement names a table
the catalog does not have. `ambiguous_name` — an unqualified name several
schemas answer to; `pg_stat_statements` records no `search_path`, so the
statement is skipped rather than resolved to the wrong table.
`ambiguous_column`, `unknown_column` — the same refusal at column level. The
workload behind all of these is invisible to the analysis, and their share
drives the `workload_partly_unparsed` gap.

## summary and gaps

`gaps` is the machine-readable answer to "is this list complete". Empty `gaps`
is the only condition under which an empty candidate list means there is nothing
to propose.

- `pgss_unavailable` — `summary.pgss_available` is false: `pg_stat_statements`
  could not be read on ANY host. Nothing was analyzed. Check
  `shared_preload_libraries` and whether the extension is created in this
  database; on Postgres Pro, `pgpro_stats` is read instead.
- `no_workload` — `summary.analyzed_queries` is 0 while `pg_stat_statements`
  reads fine: it holds nothing after a reset or a restart. Nothing was analyzed,
  so the empty candidate list says nothing about the indexes.
- `hosts_unreachable` — `unreachable_hosts` names hosts that did not answer.
  Their workload never entered the analysis, so the list is incomplete by
  exactly that much.
- `hosts_without_stats` — the host answered but carries no readable statistics.
  Different from unreachable: the instance is up and whatever it runs is
  invisible here, which on a replica serving the reads is the opposite of idle.
- `workload_partly_unparsed` — unresolved statements are a fifth or more of what
  they are counted against: `summary.analyzed_queries` for the codes from
  reading the workload, `summary.collapsed_groups` for the codes from analyzing
  it, since one group holds a row per host the statement ran on.
- `catalog_truncated` — the catalog was read only in part, so a candidate may
  duplicate an index that was never read.

`summary.covered_time_pct` is how much of the analyzed time the candidates touch
at all. A low number with an empty `gaps` means the load is elsewhere — indexing
is not this database's problem.

## Two different windows

`writes` on a candidate is `pg_stat_user_tables`: `inserted`, `updated`,
`deleted`, `seq_scans` and `idx_scans` accumulated since that table's statistics
were last reset. `write_heavy.params` is the `pg_stat_statements` window.

They are different windows over different populations. Do not divide one by the
other, do not sum them, and do not build a "cost of the index" number out of
them. `writes` shows the maintenance side of the trade; `idx_scans` shows that
the existing indexes are being used at all.

## Partitioned tables

`table` is always the root, whichever partition the statement named — PostgreSQL
propagates an index down. With `partition_root` the `ddl` is a short script, not
one statement:

1. `CREATE INDEX ON ONLY` the root — invalid, holds no data, takes no long lock;
2. `CREATE INDEX CONCURRENTLY` on every partition;
3. `ALTER INDEX … ATTACH PARTITION` for each, which turns the root index valid
   with the last one attached.

`CREATE INDEX CONCURRENTLY` cannot run inside a transaction block and cannot
build a partitioned root index directly, which is why the script exists. Hand
over every statement, in order, and state that each partition pays for the index
in space and in write cost.

## Column order and the predicate

`columns` is the proposed key order: equality predicates first, then at most one
range predicate, then ordering columns where a range predicate did not already
break the ordering. Order matters — a btree serves a prefix of its key, so the
same columns in another order are a different index. That is exactly what
`similar_index` reports.

`predicate` makes the candidate partial: the condition is ready to read, without
the `WHERE` keyword. An `IS NULL` filter lands here rather than in the key,
because its selectivity is `null_frac` — on a soft-delete column it excludes
almost nothing and belongs in neither, while on a rare flag it makes a small
index over exactly the rows the statements read.

Under `stats_missing` the order came from the statement text, not from
statistics. Recheck it after `ANALYZE`.

## Where to go next

- `unused_index_report` on the same database — mandatory before advising a
  `CREATE`. Only `verdict='drop_candidate'` justifies a `DROP`.
- `describe_table` on the candidate's table — how many indexes it already
  carries, its size, the HOT share a new index would reduce.
- `query_report` on a host from `query_id_by_host` — the statement's own numbers
  on the host that recognizes that queryid. A statement carries a different
  queryid on each host, so the keyed map is the only safe source for a
  drill-down.
- `index_advisor` with `include_queries=true` — the normalized statement text,
  clipped, when the fingerprint is not enough.
- `hot_tables` / `hot_indexes` — what the table's real activity looks like
  before adding write cost to it.
