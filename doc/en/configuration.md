# Configuration

[Русская версия](../ru/configuration.md) · [← README](../../README.md)

## Prerequisites

- Go 1.26+
- Node.js 22+ & npm
- PostgreSQL 14+ (target databases)
- Docker & Docker Compose (for demo lab)

## Configuration file

Create `dasha.yaml` (searched in `.`, `$HOME/.dasha/`, `/etc/dasha/`):

```yaml
debug: false
# pg_stats_view: monitoring.pg_stats  # custom view when user lacks pg_catalog.pg_stats access
# the view must expose schemaname, tablename, attname, inherited, null_frac, n_distinct, avg_width;
# otherwise Dasha logs a warning and falls back to pg_catalog.pg_stats
clusters:
  - name: production
    username: monitoring_user
    password: secret
    port: 5432
    databases:
      - myapp
    hosts:
      - pg-master.example.com
      - pg-replica-1.example.com

  - name: staging
    username: monitoring_user
    password: secret
    databases:
      - myapp
    hosts:
      - pg-staging.example.com
```

## Yandex MDB Service Discovery (optional)

```yaml
discovery:
  yandex_mdb:
    type: yandex-mdb
    config:
      authorized_key: /path/to/service-account-key.json
      folder_id: "b1g..."
      user: "monitoring_user"
      password: "secret"
      refresh_interval: 5  # minutes
      clusters:
        - name: "prod-.*"       # regex filter
          exclude_name: "test"
          exclude_db: "system_db"
```

## Database discovery inside a cluster (optional)

Instead of listing `databases` by hand, Dasha can ask the cluster itself and keep the list current —
a database created after startup shows up within `refresh_interval`, a dropped one disappears
together with its connections:

```yaml
discovery:
  onprem_prod:                    # entry name = cluster name (lower-case)
    type: postgres
    config:
      hosts: [pg-01.local, pg-02.local]   # primary and replicas
      port: 5432                  # default 5432
      user: dasha
      password: secret            # or password_from_env: DASHA_PG_PASSWORD
      bootstrap_db: postgres      # database the discovery query connects to
      refresh_interval: 5         # minutes, default 5
      db: ".*"                    # regex filter
      exclude_db: "(template.*)"
```

The role needs `pg_monitor` and `CONNECT` on the databases to be monitored: databases it may not
connect to are simply left out. Templates and databases with connections disabled are never listed.
Hosts are tried in order and the one that answered is preferred next time, so a single unreachable
host costs nothing; while no host answers, the previously discovered list is kept.

Dasha opens one connection pool per host and database, so on a cluster with dozens of databases
narrow the list with `db` / `exclude_db` and check `db_pool.max_conns`.

## Log Search (optional)

For clusters discovered via Yandex MDB, the `/logs` page works out of the box (it reuses the discovery service-account key). The global `log_search` block only tunes the limits:

```yaml
log_search:
  max_scan: 5000          # max records scanned per search
  max_page_size: 1000     # upper bound for page_size
  timeout_seconds: 30     # upstream read timeout
  rate_limit:             # per user (per IP when anonymous); rps <= 0 disables
    requests_per_second: 0.0333   # 1 request per 30s
    burst: 10
  admin_rate_limit:
    requests_per_second: 0.2      # 1 request per 5s
    burst: 20
```

## Schema Checks (optional)

The `/schema-lint` page works without configuration. The global `schema_lint` block silences checks and
schemas, and tunes the sequence thresholds:

```yaml
schema_lint:
  disabled_checks: [uuid_in_non_uuid_type]   # never run these
  enabled_checks: [relation_without_fk]      # opt-in for checks that are off by default
  ignore_schemas: ["_timescaledb*", "cron"]  # glob masks; system schemas are always excluded
  sequence_thresholds:                       # percent of values still free
    error: 5
    warning: 10
    notice: 20
  sequence_cache_ttl: 15m                    # TTL of the worst-sequence value the health score reads
```

Two more optional subsystems are configured in the same file but documented separately:

- authentication and personal access tokens — [auth.md](auth.md)
- snapshot storage and auto-snapshots — [autosnapshot.md](autosnapshot.md)
