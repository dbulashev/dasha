# Reading pg_stat_io

How to read `io_summary` and `io_trend`. `pg_stat_io` splits physical I/O by
`backend_type` × `object` × `context`, which is the only place that answers
*whose* I/O it is. `wait_events` says backends are waiting on `DataFileRead`
but not who reads; `query_report` covers client backends in
`pg_stat_statements` only — autovacuum, the checkpointer, the WAL writer and
the background writer are invisible there.

`pg_stat_io` is instance-wide. There is no per-database breakdown, and asking
for one is a category error.

## The counters that mislead

### hits are not I/O
A `hits` count is a page found in shared buffers — no disk was touched. It is
not part of the `io_ops` ranking and never answers "whose I/O". A row whose
only non-zero counter is `hits` is dropped from `io_summary` entirely; the
figure survives in `totals`, and `totals` is reported even when every row was
dropped — a large `hits` total with no rows means a busy, perfectly cached
instance, not an idle one. (The Dasha UI's "show idle" counts `hits` as
activity, so its row list can be longer than the tool's — the numbers agree,
the row filter does not.)

`io_ops` is `reads + writes + extends + fsyncs`: the operations that touch a
file. Buffer management (`evictions`, `reuses`, `writebacks`) is not counted,
so a row whose only activity is eviction pressure is kept but ranks `io_ops: 0`
— read its `values`, not its rank.

### evictions under bulkread are normal
`bulkread` and `bulkwrite` run through a small ring buffer that recycles its
own pages by design, so evictions there are the mechanism working, not cache
pressure. Evictions in the `normal` context are the ones worth reading as
pressure on shared buffers.

### fsyncs on a client backend are an anomaly
Synchronising files is the checkpointer's job. `fsyncs` attributed to
`client backend` mean it could not keep up and backends are flushing for it —
look at `checkpoint_timeout`, `max_wal_size` and checkpoint frequency via
`settings_analyze`. They count towards `io_ops`, so such a row ranks on its own
rather than being cut off in the tail.

### zero time is not fast
With `track_io_timing` off, `read_time`, `write_time`, `extend_time`,
`writeback_time` and `fsync_time` are zero by construction. Check
`meta.track_io_timing` before drawing any latency conclusion;
`meta.track_io_timing_changed` means the setting was toggled inside the window,
so the times cover only part of it. The tools omit `avg_read_ms` /
`avg_write_ms` rather than report `0.00`, so an absent latency means
"not measured", never "instant".

## Contexts

- **normal** — ordinary buffered access through shared buffers. The baseline.
- **vacuum** — autovacuum and manual VACUUM/ANALYZE. A large share at night is
  expected; a large share during peak hours competes with the application.
- **bulkread** — sequential scans large enough to use a ring buffer instead of
  polluting the cache. A high share means big scans: either the working set no
  longer fits, or queries are missing indexes.
- **bulkwrite** — bulk writes (COPY, CREATE TABLE AS, some ALTER TABLE).
- **init** — relation forks being initialised. Normally negligible.

## Objects

- **relation** — tables and indexes. The bulk of the traffic.
- **temp relation** — spills to disk: sorts, hashes and CTEs that exceeded
  `work_mem`. This is the instance-wide spill volume, which
  `query_report`'s per-query temp counters cannot give you.
- **wal** — WAL I/O, reported from PostgreSQL 18 on.

## Other counters

- **extends** — file growth. A sustained extend rate is the honest measure of
  how fast the database is growing, unlike a size delta that vacuum can mask.
- **reuses** — ring-buffer pages recycled during bulk operations.
- **writebacks** — pages handed to the kernel to flush.

## Incomplete points

A statistics reset, a restart or a major upgrade breaks the counter epoch.
`io_trend` marks the affected bucket `complete: false` and adds a
`coverage_pct`: the counters it carries are real, but they measure only that
share of the bucket's own span. So the values are usable — and throwing them
away would lose 55 minutes of real I/O in a bucket broken at minute five — but
they are **not comparable** with a complete bucket, and a smaller number there
is not a drop in load. `incomplete_points` counts such buckets.

An incomplete bucket with no `values` at all measured nothing: that one is a
true gap in the record.

## The window that was actually read

`requested` is the window asked for, `window` the one the data covers; a
difference between them is snapshot coverage, not load. A request longer than
31 days is cut back to 31 and flagged `window_capped: true` — a conclusion of
the form "nothing changed in the last 90 days" cannot be drawn from a capped
window.

## Empty results

An empty result is not an answer until `empty_reason` says which one it is.
Exactly one of these values means the instance was idle; the rest mean the
question went unanswered:

- **unsupported_version** — the host runs PostgreSQL 15 or older and has no
  `pg_stat_io`. Nothing about its I/O can be read this way; fall back to
  `wait_events` and `query_report`.
- **no_snapshots** — the server supports `pg_stat_io`, but no snapshot has been
  captured yet. Nothing to do but wait for the collector.
- **support_unknown** — the probe that tells an unsupported server from an
  un-captured one did not answer; `empty_detail` carries its error (a 403 means
  the token may not read live I/O, a 404 a wrong cluster/instance name).
- **window_before_history** — the window ends before the stored history starts.
  Ask for a later window; `meta.earliest_at` says from when.
- **window_after_history** — the window starts after the last stored capture:
  the collector has not run since `meta.latest_at`. This says nothing about the
  instance's I/O — say the history stops there, and do not report an all-clear.
- **no_snapshots_in_window** — history exists on both sides, but no capture
  fell inside this window. Widen it.
- **no_comparable_snapshots** — captures exist in the window, but no two of
  them are comparable: the counter epoch broke between every pair. Widen the
  window past the resets, or read `meta.version_changed`.
- **no_io_matching_filter** — nothing matched `context` / `object` /
  `backend_type`. The filter is applied before the series are built, so this
  also covers a window that holds no captures at all. `backend_type` is not
  validated, and `'autovacuum'` (the real value is `'autovacuum worker'`)
  silently matches nothing. Re-run without the filter before concluding
  anything.
- **no_io** — snapshots cover the window, they are comparable, and there
  genuinely was no physical I/O. This is the only one that is a real answer,
  and even here `totals` may show heavy cache activity.

## Where to go next

- `vacuum` dominates → `vacuum_danger`, `top_tables` — which tables keep
  autovacuum busy.
- `bulkread` dominates → `top_queries` (by=time) and `list_indexes` (missing) —
  large scans that an index would remove.
- `extends` growing → `top_tables` by size; check retention and bloat.
- `fsyncs` on backends → `settings_analyze` for checkpoint settings.
- `temp relation` significant → `top_queries` and `work_mem`: the spills come
  from sorts and hashes that do not fit.
- a spike with an unclear owner → `io_summary` with `group_by=full` over the
  window `io_trend` pointed at.
