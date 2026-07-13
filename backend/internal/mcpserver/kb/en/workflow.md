# Diagnostic workflows

Complaint → tool chain. Execute steps in order, one tool per step; stop early
when the cause is found. Get (cluster, instance) from `list_clusters` first —
per-database tools also need `database`.

## "The database is slow"
1. `get_health_score` — score ≥80: look at the app, not the DB (report and stop).
   Otherwise note the 2 worst categories by penalty.
2. `get_health_recommendations` — HIGH severity first; look up unfamiliar rule
   IDs in dasha://kb/health-rules. It names the rule, not the culprit: follow up
   with `health_details` (detail = that rule_id) for the actual tables, then
   `describe_table` on the worst one to confirm the mechanism before advising.
3. `top_queries` (by=time) — few calls × high mean_time = plan problem
   (suggest EXPLAIN, indexes); huge calls × low mean_time = frequency problem
   (suggest caching/batching).
4. `wait_events` — a dominant event tells the bottleneck class
   (dasha://kb/wait-events).
5. If storage/maintenance categories are bad: `vacuum_danger`.

## "Everything hangs / requests stuck"
1. `blocked_queries` — build the picture: who blocks whom.
2. `running_queries` — find the root blocker (often idle in transaction or a
   very old transaction).
3. Recommend terminating the BLOCKER (pg_terminate_backend), never the
   victims; suggest idle_in_transaction_session_timeout / lock_timeout.

## "Disk is filling up"
1. `get_health_recommendations` — check host_disk_space / bloat rules, then
   `health_details` (detail=high_dead_ratio_tables) to name the bloated tables;
   if vacuum cannot reclaim them, detail=horizon_blocking_sessions says who is
   holding the xmin horizon.
2. `get_replication` — inactive slots retain WAL (a classic silent eater).
3. `top_tables` — largest tables; `describe_table` the suspects (bloat section).
4. On Yandex MDB clusters: `search_logs` (service_type=postgresql,
   message=["checkpoint"]) only if WAL churn needs confirmation.

## "Replica is lagging"
1. `get_replication` — which standby, how far (time and bytes), slot state.
2. `get_instance_info` on the standby — confirm it is in recovery.
3. `running_queries` on the standby — long SELECTs conflict with replay.
4. `wait_events` on the primary — WAL-write pressure also inflates lag.

## "Application reports errors"
1. Yandex MDB clusters only (`supports_logs` in list_clusters):
   `search_logs` with severity=["ERROR","FATAL"], dedup on, a narrow window
   (since="1h"). One call with all filters — the endpoint is rate-limited.
2. Match error templates against `blocked_queries` (deadlocks, lock timeouts)
   and `get_health_recommendations`.

## Fleet triage
1. `fleet_health` — worst instances first (one call, do not loop clusters).
2. Run "The database is slow" flow for the worst 1-2 instances.

## Care rules (always)
- A recommendation is not yet a target. `get_health_recommendations` returns a
  rule_id plus a count/ratio; `health_details` turns it into objects — hand that
  rule_id straight back as `detail`. The per-table drill-downs
  (tables_autovacuum_off, low_hot_update_tables, high_dead_ratio_tables) also need
  `database`; the wraparound / xmin-horizon ones are instance-wide.
- Naming the table is not yet the cause. Confirm the mechanism with
  `describe_table` (fillfactor, the index list, the HOT share in StatInfo) and
  `top_queries` for the statement itself — e.g. 0% HOT usually means the UPDATE
  touches an *indexed* column, and only describe_table says which. Never name a
  table, column or index that is not in a tool's output.
- Bloat remediation: say what it costs. Plain `VACUUM` is safe (SHARE UPDATE
  EXCLUSIVE — blocks neither reads nor writes, no rewrite, no extra disk) but only
  makes the space REUSABLE; the file does not shrink. Shrinking needs `VACUUM FULL`
  (ACCESS EXCLUSIVE — blocks even SELECT) or `pg_repack` (online, brief locks, an
  extension that may be missing) — both need ~2x the table+index size in free disk.
  Never suggest either without quoting the table size from `describe_table` /
  `top_tables`. For a still-written table, plain VACUUM + a working autovacuum is
  usually the whole answer.
- Never advise a DROP INDEX from a scan counter, and never hedge it. Call
  `unused_index_report` (cluster-wide, no instance): `idx_scan` is not replicated
  and means nothing without its statistics window. Only `verdict=drop_candidate`
  justifies a DROP; otherwise repeat the reason. Exception: a structurally
  redundant index (exact duplicate, or invalid) — `describe_table` shows those and
  their safety does not depend on usage.
- `search_logs` is rate-limited per user (~1 request / 30s by default):
  combine all filters into ONE call, keep dedup on, never poll; after a 429
  wait ≥30s.
- One tool call per step; do not re-call a tool with the same arguments.
- If a result is refused as too large — narrow (one database, smaller limit,
  shorter window), do not retry as-is.
- health_trend needs metrics mode; a 404/error there is not an instance
  problem.
- Report format: 3-5 findings, each = fact (numbers from tools) + cause +
  one concrete action, worst first. Never invent metrics that are not in the
  tool output.
