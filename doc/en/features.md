# Features

[Русская версия](../ru/features.md) · [← README](../../README.md)

## Query Analysis
- Top-10 queries by execution time and WAL volume
- Comprehensive query report (rows, calls, planning/execution time, cache hit ratio, WAL, temp buffers, contribution %)
- Running and blocked queries monitoring
- `pg_stat_statements` status and reset time tracking
- **pgss snapshots**: save point-in-time snapshots to a dedicated storage database, view and share via URL
- **Snapshot comparison**: side-by-side diff of two snapshots or one snapshot vs live data, sortable by any metric
- **Auto-snapshots**: separate `dasha autosnapshot` daemon creates snapshots automatically on activity spikes (sliding-window avg on `pg_stat_activity`) or master↔replica role changes; configurable per cluster via UI, retention by total size
- **Lock snapshots**: an activity-spike snapshot can also capture the `pg_blocking_pids` lock-contention graph — a background blocked-count sampler runs during the spike, then a short burst of probes keeps the harshest graph (most distinct blocked sessions); also available on demand via a "with locks" manual snapshot

## Index Analysis
- Top-K by size, bloat estimation, duplicate detection
- B-tree on array columns (potential misuse detection)
- Invalid / not ready indexes
- Three similarity detection algorithms
- Unused indexes (cross-host analysis), usage statistics, cache hit rate
- **Index recommendations**: parses the statements of `pg_stat_statements` across every cluster host with a real PostgreSQL parser and proposes the btree indexes that are missing — the columns, the key order and a ready `CREATE INDEX` (for a partitioned table, the `ON ONLY` + `CONCURRENTLY` + `ATTACH PARTITION` script), the share of load behind each candidate, the write cost of the table and the statements the candidate covers; a candidate an existing index already covers as a prefix is dropped, and statements that yielded nothing are listed with the reason. Dasha never executes DDL — the recommendation is a heuristic you check and run yourself
- **Hot indexes**: which indexes do the actual work — scheduled activity delta snapshots (reads / physical I/O) summed across every cluster host; the natural complement of the unused-index analysis

## Table Analysis
- Top-K by size with TOAST breakdown (main, FSM, VM layers)
- Sequential vs. index scan ratio
- Cache hit rate, partitioned table info
- Custom storage parameters (fillfactor, autovacuum overrides)
- Detailed table describe: columns, indexes, constraints, bloat, partitions, vacuum stats with computed thresholds, row-size / TOAST estimate
- **Hot tables**: what is loading the database — activity delta snapshots (reads / writes / physical I/O) captured on a cron schedule from every cluster host and summed; an exact top-N per metric class plus a tail histogram with a coverage ratio (how representative the top is), per-host breakdown with primary/replica badges, and a per-table activity percentile on the describe page. Requires snapshot storage.

## Foreign Key Analysis
- Invalid constraints
- Type mismatches between FK columns
- Nullable FK attributes
- Similar FK detection

## Maintenance & Vacuum
- Autovacuum freeze max age, transaction ID wraparound danger
- Vacuum progress monitoring (PG 9.6+, extended in PG 17+)
- Per-table vacuum/analyze statistics with custom parameter awareness
- **Autovacuum summary**: how many tables are currently past their autovacuum/autoanalyze trigger thresholds (pie chart, reloption-aware formula) plus the maintenance processes running right now

## Connections & Locks
- Connection states and sources breakdown
- Active session details (`pg_stat_activity`)
- Wait events grouped by type/event
- Lock tree visualization

## I/O (pg_stat_io, PostgreSQL 16+)
- Server I/O split by backend type, object and context (`normal` / `vacuum` / `bulkread` / `bulkwrite`)
- History from scheduled snapshots plus a live per-tick mode that needs no snapshot storage
- Buffer-cache efficiency, vacuum cost and bulk-operation cost as summary cards
- Count and Time metrics; the Time mode requires `track_io_timing = on`
- Broken series (statistics reset, restart, major upgrade) are drawn as gaps, never as zeros

## Progress Tracking
- ANALYZE, VACUUM, CLUSTER / VACUUM FULL, CREATE INDEX, BASE BACKUP

## Settings Analysis
- Excessive logging detection
- `from_collapse_limit` / `join_collapse_limit` deviations
- `huge_pages`, TOAST/WAL compression algorithm checks
- Checkpoint ratio analysis (`checkpoint_req` vs `checkpoint_timed`)
- Autovacuum and autoanalyze configuration review

## Schema Checks
- 17 structural checks: a sequence running out of values, a table without a primary key or without any unique key, an `UNLOGGED` relation or sequence, a schema `PUBLIC` may create objects in (CVE-2018-1058), a UUID kept as `varchar`, a relation with no columns left, a reserved keyword or a quoting-unsafe character in an object name, tables outside any foreign key
- Three severities (`error` / `warning` / `notice`); findings on the partitions of one table roll up to the root table
- A check that cannot run (no privilege, unsupported version, timeout) is listed as skipped with its reason
- Instance-wide overview: per-database counts for every configured database
- Heuristic and noisy checks (a UUID kept as `varchar`, tables outside any foreign key, similar keys and indexes) are suppressible by check and by schema mask; the noisiest are off until switched on
- Invalid constraints, foreign-key type mismatches, nullable keys, similar keys and indexes and B-tree on arrays appear both in the schema report and on their own pages
- Every finding carries what the defect leads to and how to fix it, with a copyable SQL statement where the fix is unambiguous
- Hidden on standbys

## Health Score
- Composite 0–100 instance score across eight categories (connections, performance, storage, replication, maintenance, horizon, WAL/checkpoint, locks) with continuous penalty functions — top-level `/health-score` page plus a Home-page gauge
- Parallel rules engine producing prioritized recommendations: severity, metric values, the database each finding belongs to, per-database drill-down
- Optional **metrics-backed mode**: with a Prometheus/VictoriaMetrics datasource configured (`health_score.metrics`), the score, recommendations and a trend with seasonal baseline and dip detection are computed from time series (pgSCV, Yandex MDB, pgbouncer, host metrics) instead of point-in-time SQL; the SQL snapshot stays the zero-config fallback
- Scoring model details: [README-health-score.md](../../README-health-score.md)

## Log Search (Yandex Cloud)
- Search PostgreSQL server and connection-pooler (Odyssey) logs of Yandex-MDB-discovered clusters through the MDB API — top-level `/logs` page, no agents or log shipping required
- Native severity/host filters plus Dasha-side message substrings (AND), `grep -v`-style excludes and database/user filters; cursor pagination and partial results on timeout
- Optional deduplication groups near-identical messages by masked template (`<*>` placeholders) with count and first/last seen
- Frequency histogram (time × severity) with click/drag zoom, one-click presets (deadlocks, autovacuum, checkpoints, …), shareable URL filters, Grafana time-range clipboard interop
- Per-user rate limiting protects the Yandex Cloud API quota (`log_search.rate_limit`, separate admin limit)

## Authentication & Authorization
- Three modes: `none` (open), `token` (static API keys), `oidc` (OpenID Connect)
- OIDC: BFF pattern with encrypted session cookies (Keycloak, Google, any OIDC provider)
- Role-based access control (RBAC) via Casbin: `admin` (full access) and `viewer` (read-only)
- Per-identity rate limiting (token bucket): by authenticated user, session cookie, or client IP
- API keys with constant-time comparison, configurable per-key roles
- Secure session management: HttpOnly/Secure/SameSite cookies, AES-256 encryption, HMAC-SHA256 signing
- CSRF protection via OAuth2 state parameter with constant-time validation
- Token revocation on logout (RFC 7009, when supported by provider)
- Personal access tokens (PAT): user-minted API tokens for scripts and the MCP connector — hashed at rest, least-privilege, one-time secret reveal, revoke and optional expiry
- Token administration (admin): view and revoke **every** user's tokens, and a user directory recording first/last sign-in — both requiring an interactive OIDC admin session, so an admin-scoped PAT cannot revoke the tokens that would replace it

## Infrastructure
- Multi-cluster support with per-cluster host/database selection
- Yandex Managed Service for PostgreSQL service discovery
- In-cluster database discovery — the database list comes from the cluster itself and refreshes without a restart
- Primary / replica role display
- Optional snapshot storage database (daily-partitioned tables, `dasha migrate` CLI)
- [MCP connector](mcp.md) (`dasha-mcp`): read-only MCP server exposing fleet diagnostics to AI assistants (28 tools, 5 prompts)

## User Preferences

Settings dialog — the gear in the user menu.

- Interface language: English, Russian, German — auto-detected from the browser when unset, switchable at runtime, persisted locally (untranslated keys fall back to English)
- Theme: system (follows the OS), light or dark
- Time zone for every displayed timestamp: local, UTC, or any of the eleven Russian zones (listed by IANA id, the same ids PostgreSQL's `timezone` GUC and the server logs use); fixed zones are labelled on the timestamp (`… GMT+3`), and chart axes follow the same setting as the tables
- Rows per page (10–100) — this is the server-side `limit`, so it also controls how much each table requests

