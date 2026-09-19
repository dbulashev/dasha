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
            preset: jsonlog     # jsonlog | csvlog | odyssey | pgbouncer | none
            timestamp: "@timestamp"
            host: host.name
            host_match: suffix  # exact (default) or suffix, when the index holds FQDNs
            keyword_fields:     # exact-match field of a field the store analyzes
              error_severity: error_severity.keyword
              host.name: host.name.keyword
        pooler:
          index: "pgbouncer-logs-*"
          field_map:
            preset: pgbouncer
            timestamp: "@timestamp"
            host: host.name
            severities: [NOISE, LOG, WARNING, ERROR, FATAL]  # narrows the preset vocabulary
            mask: [msg, query]  # extra fields to sanitize; text is always masked

clusters:
  - name: prod
    log_source: main
```

In the Helm chart the CA is attached as a backend volume, and the mount path goes into `ca_file`:

```yaml
# values.yaml; kubectl create secret generic dasha-os-ca --from-file=ca.pem=os-ca.pem
backend:
  extraVolumes:
    - name: os-ca
      secret:
        secretName: dasha-os-ca
  extraVolumeMounts:
    - name: os-ca
      mountPath: /etc/ssl/opensearch   # ca_file: /etc/ssl/opensearch/ca.pem
      readOnly: true
```

In the chart `/etc/dasha` is taken by the ConfigMap holding `dasha.yaml`: the certificate volume
mounts elsewhere.

Binding order: the cluster's `log_source`, then the built-in Yandex MDB source for clusters
discovered there, then `default_source`. A source named in `log_source` must be declared in
`sources`, and a source may only declare the `postgresql` and `pooler` streams; both are checked at
startup. The name `yandex-mdb` belongs to the built-in source. With any `auth.kind` but `none` every
address must start with `https://`, and `tls.insecure_skip_verify` is rejected.

`{{ .Cluster }}` is the only substitution; it expands in the index pattern, in `query` and in the
values of `selector` and `stream_selector`.
The host is not substituted: a search without a host filter has none, so a host-dependent index would
resolve to nothing.

### VictoriaLogs sources

A stream is addressed by a LogsQL expression. The delivery agent must split
the record into fields; a whole log line in `_msg` is not supported.

```yaml
log_search:
  sources:
    vlogs:
      type: victorialogs
      addresses: ["https://vlogs.example.net:9428"]
      auth:
        kind: bearer            # none | basic | api_key | bearer
        token_from_env: VL_TOKEN
      tenant:                   # 0/0 by default
        account_id: 12
        project_id: 34
      batch_size: 1000          # records per request
      max_boundary_ids: 1000    # records of one timestamp the cursor remembers
      streams:
        postgresql:
          stream_selector:      # -> {cluster="prod"}
            cluster: "{{ .Cluster }}"
          selector:             # -> "app":="postgres"
            app: postgres
          query: '*'            # extra LogsQL expression
          field_map:
            preset: jsonlog
            timestamp: _time
            text: _msg
            host: host

clusters:
  - name: prod
    log_source: vlogs
```

One of `query`, `selector` or `stream_selector` must be set: a source without a filter serves the
logs of the whole fleet under the name of one cluster. `index` is rejected in a VictoriaLogs stream,
and so are `query` and `stream_selector` in an OpenSearch one. Both are checked at startup.


### Common to external sources

A preset fills in the field names of a known log format, and any field overrides it. `timestamp` and
`host` are never part of a preset — PostgreSQL writes neither, the delivery agent names them — so both
must be set. Severity and host are the only filters pushed down to the store; message, database and
user substrings are matched by Dasha, so a `text` field analyzed by the store still behaves the way
the search box promises. In OpenSearch severity, host and selector fields are matched exactly, so
they must be indexed as `keyword`; when the store analyzes one of them instead — the default dynamic mapping does —
name its exact-match counterpart in `keyword_fields`, otherwise the filter matches nothing. The check
endpoint below reports the type of every mapped field.

`query_id` is optional. The `jsonlog` and `csvlog` presets bind it to the field of that name, which
PostgreSQL fills when `compute_query_id` is on; `odyssey` and `pgbouncer` leave it unset. A stream
whose records lack the field still searches, and the check endpoint lists the role among the missing
ones.

`sql_state` is optional too: `csvlog` binds it to `sql_state_code`, `jsonlog` to `state_code`. The log
insights summary assigns an event its category by the SQLSTATE code first and by the message text only
after that.

`severities` lists the levels one stream accepts, in the casing the store holds them: the search
rejects any other value and the log page offers exactly this list in its level filter. Every preset
brings its own — upper-case PostgreSQL levels for `jsonlog` and `csvlog`, lower-case for `odyssey`,
`NOISE, DEBUG, LOG, WARNING, ERROR, FATAL` for `pgbouncer`.

The `text` field is always masked: its value passes through the query sanitizer before it leaves the
backend. `mask` adds the other free-text fields. When `text` sits in a nested object
(`text: pg.message`), a mask entry without a dot covers both names: `detail` masks `detail` and
`pg.detail`.

A stream a source does not declare is unavailable: the API answers 501 and the UI hides the switch.

`GET /api/logs/check?cluster_name=…&service_type=…` (admin only) probes a source: the resolved target
— an index name or a LogsQL expression — how many records the last hour holds, which mapped fields
exist, which are missing, and one masked sample record.

### Log insights

`GET /api/logs/insights` reads a cluster's logs for the selected interval and returns two summaries:
event counts by category (locks, checkpoints, autovacuum, errors and more) and the `auto_explain`
plans, grouped by query and plan shape. The logs come from the same source as the log search, with
the same `timeout_seconds` and `rate_limit`. The global `log_insights` block limits a single request:

```yaml
log_insights:
  enabled: true           # false answers 404
  max_records: 50000      # records read per request
  max_bytes: 67108864     # bytes read per request
  max_plan_bytes: 2097152 # a larger plan is counted but not parsed
  max_plans: 5000         # plans parsed per request
  compare_rate_limit:     # GET /api/logs/plans/compare, per user
    requests_per_second: 0.016
    burst: 3
  index_advisor_evidence: false  # back index candidates with plans from the log
```

If a limit is reached before the end of the interval, the response says which part of the interval
the summary covers: the latest records for VictoriaLogs, the earliest for OpenSearch. If `max_plans`
runs out first, the plans cover a shorter part than the categories.

`GET /api/logs/plans` returns the plans alone, over the whole interval or for one `query_id`. The log
store is handed a narrowing filter: the level `auto_explain` writes with, then the statement id, then
the word `plan`. The level is sent only when `auto_explain.log_level` was read from every host the
scan covers. The `narrowed_by` field of the response lists what the store executed itself; a filter
it cannot run is not sent, and Dasha reads a wider window instead. The records the store
returns are filtered on the Dasha side regardless. A log stream without a `query_id` role answers 400
to a request for one; `GET /api/logs/check` shows which roles are missing.

With `index_advisor_evidence: true`, a candidate in the index recommendation report comes with the
number of plans over the last hour that read its table sequentially, the time those nodes spent and
the rows their filters discarded. A candidate no such plan was found for and a candidate whose plans
were never read are two different answers in the response: the second is no argument against the
index. A window auto_explain wrote no plan to is the second answer, not the first. A plan that
misestimated the rows of that table raises the `stale_statistics` warning — `ANALYZE` first, the
index after. The depth of the window is `index_advisor.evidence_window` (`1h` by default). Off by default: with it on, the latency of the report depends on the log store.

`GET /api/logs/plans/compare` reads two intervals and lists the statements whose plans changed for
the worse: a plan shape the baseline interval did not hold, an index it read and the current one does
not, a p95 at least twice as high. A statement only one of the intervals holds is left out. The
durations of each side are those of the shape that took the most time in that interval, and the ratio
of two truncated intervals is marked `partial`. The current interval comes from `from` and `to`, or
from a stored scan named in `scan_id`; the baseline interval is read only when the current one found
a plan, and with `scan_id` it is read for the host the stored scan covers — a `host` naming another
one answers 400. One request costs two reads of the log store: it counts against the `rate_limit` of
its source like every other log endpoint, and against `compare_rate_limit` besides. Both reads
together stay within the `timeout_seconds` of a single one.

The `configuration` block of the response carries what Dasha read from `pg_settings` of one host,
named in `instance`: whether `auto_explain` is loaded, its `auto_explain.log_min_duration`,
`auto_explain.log_analyze`, `auto_explain.log_format`, `auto_explain.log_level`, and
`compute_query_id`. A cluster that did not answer leaves the block out, and the scan runs anyway.

Plans reach the log when `auto_explain` is loaded through `shared_preload_libraries` and
`auto_explain.log_format` is `text` or `json`. Only queries slower than
`auto_explain.log_min_duration` make it into the summary. Plan checks based on actual row counts and
timings need `auto_explain.log_analyze = on`. Tying a plan to a statement needs
`compute_query_id = on` and a `query_id` role on the log stream. Plans are masked for credentials
only, as in the log search; query literals stay as they are.

A large plan may never reach Dasha. VictoriaLogs drops lines longer than `-insert.maxLineSizeBytes`
(256 KiB by default, 2 MB at most), and the Fluent Bit `tail` input with `Skip_Long_Lines` on skips
lines longer than `Buffer_Max_Size`. If the delivery agent truncates long lines, a text plan arrives
without its end and a json plan fails to parse.

With snapshot storage configured (`storage.dsn`), every scan is stored as a snapshot and the
response carries `scan_id`. `GET /api/logs/scans/{scan_id}` repeats the same numbers,
`.../groups` lists every plan group — the summary itself carries the top ones only — and
`.../groups/{ord}` hands out one plan tree. None of them reads the log source again, so refining a
summary costs nothing and cannot disagree with it. Without storage the three endpoints answer 501
and the summary carries no `scan_id`.

The auto-snapshot daemon empties the snapshots once a day, the first time on start: a `scan_id`
lives a day at most, after which it answers 404 and the client scans again. Without a running
daemon the tables `log_insights_scans` and `log_insights_groups` are never cleared. Query literals
are not masked in a stored plan and are readable by any viewer until the cleanup.

## Index recommendations (optional)

The index recommendation report needs no configuration. The global `index_advisor` block bounds the
work and the shape of the candidates:

```yaml
index_advisor:
  enabled: true            # false answers 404
  max_queries: 500         # statements read per report, by total time descending
  max_query_bytes: 102400  # a longer statement is not parsed
  max_candidates: 50       # candidates in the report
  max_index_columns: 3     # columns in a candidate key; 4 is the ceiling
  min_table_rows: 10000    # a smaller table yields no candidate
  parse_cache_size: 1000   # parsed statements kept between reports
  timeout: 60s             # bounds one report
  evidence_window: 1h      # window the logged plans behind a candidate are read over
```

`evidence_window` is read only with `log_insights.index_advisor_evidence` on. The MCP server asks for
this report with its own `--slow-timeout` (`90s` by default): raised above it, `timeout` never applies,
because the MCP call gives up first.

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
