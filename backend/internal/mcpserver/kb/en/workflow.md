# Diagnostic workflows

Complaint → tool chain. Execute steps in order, one tool per step; stop early
when the cause is found. Get (cluster, instance) from `list_clusters` first —
per-database tools also need `database`.

## "The database is slow"
1. `get_health_score` — score ≥80: look at the app, not the DB (report and stop).
   Otherwise note the 2 worst categories by penalty.
2. `get_health_recommendations` — HIGH severity first; look up unfamiliar rule
   IDs in dasha://kb/health-rules.
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
1. `get_health_recommendations` — check host_disk_space / bloat rules.
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
