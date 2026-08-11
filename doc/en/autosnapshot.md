# Snapshot storage and auto-snapshots

[Русская версия](../ru/autosnapshot.md) · [← README](../../README.md)

## Snapshot Storage (optional)

To enable pgss snapshots, configure a dedicated PostgreSQL database:

```yaml
storage:
  dsn: "postgres://dasha:secret@localhost:5432/dasha_storage?sslmode=require"
  # dsn_from_env: DASHA_STORAGE_DSN  # alternative: read from env variable
```

Run `dasha migrate` to create partitioned tables before first use.

## Auto-snapshots (optional)

When snapshot storage is configured, you can run a separate daemon that creates snapshots automatically on configurable triggers:

```bash
dasha autosnapshot
```

The daemon uses the same `dasha.yaml` config. All knobs (triggers, thresholds, retention) are stored in the storage DB and edited from the UI (*Auto-snapshots* menu, admin-only). Run a single instance by default; to run multiple replicas for HA, enable advisory-lock leader election with `storage.leader_election: true` (off by default, since a session-level advisory lock needs a dedicated connection and is incompatible with transaction-pooling proxies like PgBouncer in transaction mode).

On an activity-spike trigger the daemon can additionally capture the lock-contention graph (`capture_locks`, on by default): a cheap blocked-session counter runs in the background during the spike, and at trigger time a short burst of probes (`lock_probe_count` × `lock_probe_interval`, default 5 × 500 ms) records the full `pg_blocking_pids` graph, keeping the probe with the most distinct blocked sessions. The result is stored in `snapshots.locks_data` and viewable from the snapshot view in *Query Stats*.

Triggers:
- **activity_spike** — fires when `count(state='active')` in `pg_stat_activity` exceeds the sliding-window baseline (`window_size`, default 30 min) by a configurable percent (default +50%) for a sustained duration (`spike_duration`, default 2 min). The window must be at least twice the duration, otherwise a sustained spike inflates its own baseline before it can fire. Single dips below the threshold shorter than half the duration are tolerated as long as 70% of the probes stay above it. Installations created before v1.5.0 keep their stored settings — check them against this rule
- **role_change** — fires on master↔replica transitions (direction: `both` / `master_to_replica` / `replica_to_master`)

Retention drops the oldest day-triples once total size exceeds `retention_bytes`, respecting the `retention_min_days` floor.

