# Changelog

## v0.1.18

#### New Features
- **pg_stat_statements snapshots**: save and view pgss snapshots in a dedicated storage database
  - Optional PostgreSQL storage database (`storage.dsn` in config)
  - Daily-partitioned tables: `snapshots` (report JSON) and `query_texts` (deduplicated by SHA-256 hash)
  - `dasha migrate` CLI command creates tables
  - Frontend: snapshot create button, snapshot selector (live data / saved snapshot), shareable URLs with `?snapshot=uuid`
  - Snapshot-aware query report: hides "exclude users" filter, shows snackbar when snapshot from URL is not found
- **Query Compare**: side-by-side comparison of two snapshots or one snapshot vs live data (`GET /api/queries/compare`)
  - Sort by total_time / calls / WAL / rows / cpu_time / io_time / temp_blks
  - Filters "hide queries absent in A/B"; per-query Left/Right metrics block with deltas
  - Exclude-users filter for the live side
  - Menu item is hidden automatically when snapshot storage is not configured (probed via `GET /api/queries/snapshots/status`, cached in Pinia for 10 min)
- **Table Describe — Row Estimate Analysis**: new section showing tuple header, null bitmap, row data width, estimated row size, fillfactor, page-usable / available space, rows-per-page, HOT-update reserve, WILL_TOAST warning and TOAST-candidate columns (`GET /api/tables/describe-row-estimate`)
- **Table Describe — Vacuum Stats**: last (auto)vacuum / (auto)analyze timestamps, dead/live tuples, `n_mod_since_analyze`, `n_ins_since_vacuum`, computed vacuum / analyze / insert-vacuum thresholds from global + per-table reloptions (`GET /api/tables/describe-vacuum-stats`)
- **SQL sanitization**: `sanitize.SQL()` masks `password=` and `PASSWORD 'x'` in query text fields
- **OIDC role mapping**: `role_mapping` in OIDCConfig maps corporate groups to dasha roles (admin/viewer)
- **pg_stat_statements reset**: `POST /api/queries/reset-stats` (admin-only), controlled by `enable_query_stats_reset` config

#### Bug Fixes
- **Backend**: 404 responses now return correct HTTP status (was 500 due to oapi-codegen strict handler ignoring response object when error is non-nil)
- **Frontend**: global error handling via provide/inject — error code from API propagated correctly (was always 500)
- **Frontend**: "No clusters available" error no longer disappears on route change
- **Frontend**: invalid cluster/host in URL now shows 404 with similar name suggestions instead of silent redirect

#### Improvements
- Section components use `useViewError()` directly instead of emit chain — removes indirection, preserves error codes
- `useClusterInfo` returns null for unknown cluster/host — blocks API calls with invalid params
- Login card with SSO button, version display, return URL preservation across OIDC redirect
- `ApiError` class with HTTP status extracted from response body
- `IoCpuScatterSection`: axes auto-scale to ms / s / min / h based on data range
- `DescribeTableSelector`: on cluster change resets schema to `public` (when present) and clears selected table; schema watcher prefers `public` over first-in-list
- `DescribeBloatSection` now renders only for regular tables (was unconditional)
- Frontend Docker image embeds `BUILD_NUMBER` via `VITE_APP_VERSION` env — version shown in login card and user menu
- Nginx: added `X-Forwarded-Proto`, dedicated `/auth/` location block, larger `proxy_buffer_size` / `proxy_buffers` for OIDC cookie-heavy responses
- `ErrorAlert` component for full-page error fallback with illustration

#### Demo
- Added storage database service for snapshots
- `dasha migrate` runs automatically before app start

## v0.1.13

#### New Features
- **Authentication & Authorization**: three modes — `none` (default), `token` (static API keys), `oidc` (OpenID Connect BFF)
- **RBAC**: role-based access via Casbin — `admin` (full) and `viewer` (read-only)
- **Per-identity rate limiting**: token bucket with configurable RPS and burst

#### Security
- Constant-time token and OAuth2 state comparison
- Secure random generation via `crypto/rand`
- Refresh token revocation on logout (RFC 7009)
- Reverse proxy support for `Secure` cookie flag

#### Demo
- Keycloak OIDC provider with preconfigured realm, users and roles
- Fixed logical replication race condition in standalone init

## v0.1.12

- Visual improvements

## v0.1.11

#### New Features
- **Replication view**: new page with 3 sections — config, status, slots
  - `GET /api/replication/status` — pg_stat_replication with LEFT JOIN pg_replication_slots (slot per replica), state/sync_state chips with tooltips, client_addr/PID/LSN in expandable rows
  - `GET /api/replication/slots` — slot_type, wal_status (with tooltip explanations), safe_wal_size, backlog_bytes
  - `GET /api/replication/config` — synchronous_standby_names + synchronous_commit with tooltip descriptions for each mode (on, remote_apply, remote_write, local, off)
- **Database health**: new `GET /api/database/health` — deadlocks, conflicts, checksum failures, rollback ratio from pg_stat_database
- **Wait events**: new `GET /api/connection/wait-events` — aggregated wait events from pg_stat_activity (excluding idle Client.ClientRead)

#### Frontend
- **ReplicationView**: ReplicationConfigSection (settings with chip tooltips), ReplicationStatusSection (lag color coding, state/sync chips with tooltips, expandable rows), ReplicationSlotsSection (wal_status chip tooltips)
- **DatabaseHealthSection**: chip-based health indicators on Home page with green/yellow/red thresholds
- **WaitEventsSection**: wait events table on Home page with wait type color coding
- Navigation: added "Replication" menu item with `mdi-database-sync-outline` icon
- `fmtLag` and `fmtBytes` extracted to shared `utils/format.ts`

#### Backend
- New SQL templates: `replication/status` (with `LEFT JOIN pg_replication_slots`), `connections/wait_events`, `database/health`
- Enriched `replication/slots` with slot_type, wal_status, safe_wal_size, backlog_bytes
- New `replication/config` — `current_setting()` for synchronous_standby_names and synchronous_commit
- `pg_is_in_recovery()` guard on `pg_current_wal_lsn()` calls (replica-safe)
- New DTOs: ReplicationStatus, ReplicationConfig, ReplicationSlot, WaitEvent, DatabaseHealth

#### Demo
- Added deadlock generator to workload script for database health demonstration

## v0.1.10

### Table Describe (`\d+`) Enhancements

#### New Features
- **Table/schema selector**: autocomplete search for schemas and tables with URL sync
- **Index metadata**: size column, primary/unique/invalid icons with tooltips
- **Column statistics**: `null_frac`, `n_distinct`, `avg_width` from `pg_stats` in expandable rows
- **Estimated row count**: `reltuples` from `pg_class` in human-readable format (K/M/B)
- **Activity stats**: INS/UPD/DEL/SEQ_SCN/IDX_SCN from `pg_stat_all_tables`
- **Partitions display**: paginated list of child partitions for partitioned tables with describe links
- **Bloat estimation**: `pgstattuple_approx()` results via "Calculate bloat" button, disabled with status chip when extension is unavailable
- **Describe links**: clickable table names across 12 index/table section components via shared `useDescribeLink` composable

#### Backend
- New SQL templates: `describe_bloat`, `describe_partitions`, `describe_schemas`, `describe_search`, `pgstattuple_available`
- Extended `describe_columns` with `LEFT JOIN pg_stats` (null_frac, n_distinct, avg_width)
- Extended `describe_indexes` with `indisvalid`, `pg_relation_size`, `pg_size_pretty`
- Extended `describe_metadata` with `reltuples`, stat_info subquery
- New API endpoints: `GET /api/tables/describe-bloat`, `GET /api/tables/describe-partitions`, `GET /api/tables/pgstattuple-available`, `GET /api/tables/schemas`, `GET /api/tables/search`
- 1-minute query timeout for `pgstattuple_approx`

#### Frontend
- **Refactored** `TableDescribeView.vue` from ~580 lines into 8 focused components:
  - `DescribeTableSelector` — schema/table autocomplete with URL sync
  - `DescribeHeaderSection` — table metadata and size cards
  - `DescribeColumnsSection` — columns with expanded stats rows
  - `DescribeIndexesSection` — indexes with PK/unique/invalid icons
  - `DescribeConstraintsSection` — reusable for check and FK constraints
  - `DescribeReferencedBySection` — referenced-by with source table
  - `DescribePartitionsSection` — paginated partitions via `usePaginatedApiLoader`
  - `DescribeBloatSection` — pgstattuple availability check and bloat calculation
- Added `fmtRowCount` to shared `utils/format.ts`
- Added Russian plural rules for vue-i18n (`pluralRules` in main.ts)
- Navigation: "Tables" menu split into "Overview" and "Describe" sub-items

#### Demo
- Added `pgstattuple` extension to demo init scripts

## v0.1.9

See git history for previous changes.
