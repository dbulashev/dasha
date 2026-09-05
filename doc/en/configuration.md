# Configuration

[Русская версия](../ru/configuration.md) · [← README](../../README.md)

## Prerequisites

- Go 1.26+
- Node.js 22+ & npm
- PostgreSQL 14+ (target databases); Postgres Pro is supported — its `pgpro_stats` is used in place of `pg_stat_statements` automatically
- Docker & Docker Compose (for demo lab)

## Monitoring role

Dasha only reads the monitored databases and changes nothing in them; the one exception, resetting
query statistics, is off by default. The role it connects as has to be allowed to connect to every
monitored database; with `pg_monitor` it sees the statements of the other roles too:

```sql
CREATE ROLE monitoring_user LOGIN PASSWORD 'secret';
GRANT pg_monitor TO monitoring_user;
GRANT CONNECT ON DATABASE myapp TO monitoring_user;
```

`pg_stat_statements` (`pgpro_stats` on Postgres Pro) belongs in `shared_preload_libraries` and has
to be created in one database of the instance — its contents cover the whole instance, and Dasha
reads them through whichever database carries the extension, in whatever schema it sits:

```sql
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
```

`pg_monitor` carries `pg_read_all_stats`. A role without it reads `pg_stat_statements` with neither
the identifier nor the text of the statements of other users: the query report and the Top 10
panels then hold the statements of the monitoring role alone, and say so above the tables.

Beyond `pg_monitor`, the sequence-exhaustion check reads `last_value` and needs
`GRANT SELECT ON ALL SEQUENCES IN SCHEMA public TO monitoring_user`; the schema-lint page names
the grant on the rows it had to skip. Column statistics and the reset of query statistics have
their own recipes below.

### Column statistics without access to the tables

`pg_catalog.pg_stats` returns a row only for the columns the reading role may select, and
`pg_monitor` opens no user tables: without `SELECT` on them the table pages, index bloat and the
index recommendations come back without column statistics. Where the tables have to stay closed, a
superuser creates a view over `pg_statistic` and Dasha reads that instead:

```sql
CREATE SCHEMA IF NOT EXISTS monitoring;

CREATE VIEW monitoring.pg_stats AS
SELECT n.nspname     AS schemaname,
       c.relname     AS tablename,
       a.attname     AS attname,
       s.stainherit  AS inherited,
       s.stanullfrac AS null_frac,
       s.stawidth    AS avg_width,
       s.stadistinct AS n_distinct
FROM pg_catalog.pg_statistic s
    JOIN pg_catalog.pg_class c ON c.oid = s.starelid
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid AND a.attnum = s.staattnum
WHERE NOT a.attisdropped;

GRANT USAGE ON SCHEMA monitoring TO monitoring_user;
GRANT SELECT ON monitoring.pg_stats TO monitoring_user;
```

```yaml
pg_stats_view: monitoring.pg_stats
```

The view reads `pg_statistic` as its owner, which is what opens the statistics to a role that may
not read the tables — so a superuser has to create it, and on PostgreSQL 15 and newer it must not
carry `security_invoker = true`. A view over `pg_catalog.pg_stats` gives nothing: the filter inside
it asks about the privileges of the caller. `pg_statistic` is per database, so the view belongs in
every monitored database.

The columns `schemaname`, `tablename`, `attname`, `null_frac`, `n_distinct` and `avg_width` are
required; `inherited` is optional and lets the index recommendations prefer the inherited
statistics of a partitioned table. The name is taken as written and has to be an unquoted
`schema.name`. Dasha probes the view on first use and falls back to `pg_catalog.pg_stats` with a
warning in the log when it is unreadable or short of a column.

Data values stay out of it: the view carries the share of nulls, the width and the number of
distinct values of a column, not the most common values and histogram bounds `pg_stats` exposes.

### Resetting query statistics without a superuser

The reset button calls `pg_stat_statements_reset()` — `pgpro_stats_statements_reset()` on Postgres
Pro — in the database the statistics are read through, and it drops the statistics of the whole
instance. Where the monitoring role has no `EXECUTE` on that function, a superuser owns a wrapper
and Dasha calls the wrapper instead:

```sql
CREATE SCHEMA IF NOT EXISTS monitoring;
GRANT USAGE ON SCHEMA monitoring TO monitoring_user;

CREATE FUNCTION monitoring.reset_pgss() RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
BEGIN
    PERFORM public.pg_stat_statements_reset();
END;
$$;

REVOKE EXECUTE ON FUNCTION monitoring.reset_pgss() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION monitoring.reset_pgss() TO monitoring_user;
```

```yaml
enable_query_stats_reset: true
pgss_reset_function: monitoring.reset_pgss
```

The wrapper belongs in the database that holds the extension, and the call inside it carries the
schema `pg_stat_statements` was installed in — `public` above. `PUBLIC` must have no `CREATE` on
that schema: `REVOKE CREATE ON SCHEMA public FROM PUBLIC` (the default since PostgreSQL 15), or
install the extension in a schema only a superuser can create in. Dasha calls the function as
`SELECT monitoring.reset_pgss()`, without arguments and discarding the result, so the return type
is free. The name has to be an unquoted `schema.name`; an invalid one is ignored with a warning and
the function of the extension is called. Without `enable_query_stats_reset` the button is not shown
at all.

## Configuration file

Create `dasha.yaml` (searched in `.`, `$HOME/.dasha/`, `/etc/dasha/`):

```yaml
debug: false
# pg_stats_view: monitoring.pg_stats  # see "Column statistics without access to the tables"
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

Databases the role may not connect to are simply left out. Templates and databases with connections
disabled are never listed. Hosts are tried in order and the one that answered is preferred next
time, so a single unreachable host costs nothing; while no host answers, the previously discovered
list is kept.

Dasha opens one connection pool per host and database, so on a cluster with dozens of databases
narrow the list with `db` / `exclude_db` and check `db_pool.max_conns`.

## Log Search (optional)

The `/logs` page reads an existing log store; Dasha never collects or parses logs itself. For clusters
discovered via Yandex MDB it works out of the box (it reuses the discovery service-account key). Every
other cluster reads from a source declared in `log_search.sources` and referenced by name.

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

### OpenSearch sources

The server must write `log_destination = jsonlog` (PostgreSQL 15+) or `csvlog`, and the delivery
pipeline must keep the fields intact — an index holding the raw log line is not supported.

```yaml
log_search:
  default_source: main          # serves every cluster that names no source
  sources:
    main:
      type: opensearch
      addresses: ["https://os-1.example.net:9200"]
      auth:
        kind: basic             # none | basic | api_key
        user: dasha
        password_from_env: OS_PASSWORD
      tls:
        ca_file: /etc/dasha/os-ca.pem
        insecure_skip_verify: false
      batch_size: 1000          # records per upstream request
      max_boundary_ids: 10000   # cursor stops past this many records at one timestamp;
                                # never below batch_size, never above the index
                                # max_result_window
      rate_limit:               # overrides the global limits for this source
        requests_per_second: 1
        burst: 20
      streams:
        postgresql:
          index: "pg-logs-{{ .Cluster }}-*"
          selector:             # extra term filter when one index holds the whole fleet
            cluster: "{{ .Cluster }}"
          field_map:
            preset: jsonlog     # jsonlog | csvlog | odyssey | none
            timestamp: "@timestamp"
            host: host.name
            host_match: suffix  # exact (default) or suffix, when the index holds FQDNs
            keyword_fields:     # exact-match field of a field the store analyzes
              error_severity: error_severity.keyword
              host.name: host.name.keyword
        pooler:
          index: "pgbouncer-logs-*"
          field_map:
            preset: none
            timestamp: "@timestamp"
            severity: level
            text: msg
            host: host.name
            severities: [debug, info, warning, error, fatal]
            mask: [msg, query]  # extra fields to sanitize; text is always masked

clusters:
  - name: prod
    log_source: main
```

Binding order: the cluster's `log_source`, then the built-in Yandex MDB source for clusters
discovered there, then `default_source`. A cluster named in `log_source` must reference a source
declared in `sources`, and a source may only declare the `postgresql` and `pooler` streams; both are
checked at startup.

`{{ .Cluster }}` is the only substitution; it expands in the index pattern and in selector values.
The host is not substituted: a search without a host filter has none, so a host-dependent index would
resolve to nothing.

A preset fills in the field names of a known log format, and any field overrides it. `timestamp` and
`host` are never part of a preset — PostgreSQL writes neither, the delivery agent names them — so both
must be set. Severity and host are the only filters pushed down to the store; message, database and
user substrings are matched by Dasha, so a `text` field analyzed by the store still behaves the way
the search box promises. Severity, host and selector fields are matched exactly, so they must be
indexed as `keyword`; when the store analyzes one of them instead — the default dynamic mapping does —
name its exact-match counterpart in `keyword_fields`, otherwise the filter matches nothing. The check
endpoint below reports the type of every mapped field.

The `text` field always passes through the query sanitizer before it leaves the backend; `mask` adds
the other free-text fields. An entry naming a bare field also covers the same field nested beside the
text field, so a `text: pg.message` masks `pg.detail` as well as `detail`.

A stream a source does not declare is unavailable: the API answers 501 and the UI hides the switch.

`GET /api/logs/check?cluster_name=…&service_type=…` (admin only) probes a source: the resolved index,
how many records the last hour holds, which mapped fields exist, which are missing, and one masked
sample record.

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
