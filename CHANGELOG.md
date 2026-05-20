# Changelog

## v1.0.0

### Breaking Changes
- **Helm chart:** `ingress.tls.certManager.reflectToNamespace` removed. Reflector integration (emberstack `kubernetes-reflector`) is no longer rendered — add the annotations manually via `ingress.annotations` if you still need it.
- **Helm chart:** `ingress.tls.certNamespace` removed; cert-manager `Certificate` is always created in the release namespace.
- **Helm chart:** ingress/gateway routing simplified. With `frontend.enabled: true` (default), only a single `/` rule is rendered — frontend nginx handles `/api/` and `/auth/` proxying. The previous separate `/api/` Ingress rule is gone. Headless deploys (`frontend.enabled: false`) keep direct `/api/` and `/auth/` rules to backend.

### Security
- **Backend HTTPS enforcement:**
  - `auth.NewMiddlewares` emits a single zap WARNING at startup when `auth.mode != none && !require_https` — surfaces the case where credentials may be transmitted in plaintext. Unit tests added in `backend/internal/auth/auth_test.go` (4 cases).
  - Helm `configmap.yaml` auto-injects `auth.require_https: true` into the rendered `dasha.yaml` when `auth.mode != none && tls.enabled` (via new `dasha.tlsEnabled` helper which ORs `ingress.tls.enabled` and `gatewayAPI.tls.enabled`). Explicit `auth.require_https: false` from values is preserved as escape hatch.
  - Frontend nginx preserves the original `X-Forwarded-Proto` via a `map`-block (`/etc/nginx/conf.d/00-proto-map.conf`: `$http_x_forwarded_proto → $forwarded_proto`, fallback to `$scheme` when the header is absent). Both `proxy_pass` blocks (`/api/`, `/auth/`) now use `$forwarded_proto`. Previously `$scheme` rewrote the header to in-cluster `http` and silently broke `require_https`.
- **Container hardening:**
  - `Dockerfile.backend` and `Dockerfile.frontend` run as non-root (`USER dasha` / `USER nginx`). Nginx main config patched: `user nginx;` directive removed and pid moved to `/tmp/nginx.pid` so the process starts without root.
  - Helm default container `securityContext`: `allowPrivilegeEscalation: false`, `readOnlyRootFilesystem: true`, `capabilities.drop: [ALL]`, `seccompProfile.type: RuntimeDefault`. Pod-level `runAsNonRoot: true` + `runAsUser/Group/fsGroup` (1000 backend, 101 frontend nginx). `emptyDir` mounts for `/tmp` (backend) and `/var/cache/nginx`, `/etc/nginx/conf.d`, `/tmp` (frontend) keep ROFS working.
- **Go dependency CVE patches** (from `trivy fs` sweep): `pgx/v5` `v5.7.6` → `v5.9.0` (CRITICAL memory-safety, CVE-2026-33816), `go-jose/v4` `v4.1.3` → `v4.1.4` (HIGH DoS via crafted JWE, CVE-2026-34986), `golang-jwt/v4` `v4.5.1` → `v4.5.2` (HIGH memory allocation in header parsing, CVE-2025-30204), `grpc` `v1.79.2` → `v1.79.3` (HIGH HTTP/2 path validation auth bypass, CVE-2026-33186). `CVE-2026-34040` in `docker/docker` (transitive via `testcontainers-go`, server-side bug not exercised by client) ignored via `.trivyignore` with rationale.

### Helm
- **Defense-in-depth HTTP→HTTPS redirect** at three layers when `tls.enabled`:
  - Ingress: `nginx.ingress.kubernetes.io/ssl-redirect` and `force-ssl-redirect` annotations auto-added.
  - Gateway API: separate `HTTPRoute` with `RequestRedirect` filter on the HTTP listener.
  - Frontend nginx: `FORCE_HTTPS_REDIRECT=true` env (auto-set by chart) injects an `if ($http_x_forwarded_proto = "http") { return 301 ... }` block. Requests without `X-Forwarded-Proto` (probes, port-forward) are not redirected.
- **Kubernetes Gateway API support** (`gateway.networking.k8s.io/v1`): new `gatewayAPI.*` values block, new templates `gateway.yaml`, `httproute.yaml`, `httproute-redirect.yaml`, `gateway-certificate.yaml`. Portable between Istio, NGINX Gateway Fabric, Envoy Gateway, Cilium. `ingress.enabled` and `gatewayAPI.enabled` are mutually exclusive — `helm template` fails via `dasha.validateTrafficMode` if both are true. `dasha.validateGatewayAPI` additionally requires `allowedRoutes.namespaces.from != "Same"` when `gatewayNamespace` differs from the release namespace, otherwise HTTPRoute cannot attach.
- **New helpers in `_helpers.tpl`:** `dasha.tlsEnabled`, `dasha.validateTrafficMode`, `dasha.validateGatewayAPI`, `dasha.gatewayTLSSecretName`, `dasha.gatewayNamespace`.

### CI / Tooling
- **Trivy filesystem + config scan** (`trivy-scan` job) — scans dependencies (`go.sum`, `package-lock.json`) and IaC misconfig (Dockerfile, Helm chart) on every push/PR. Blocks merge on `CRITICAL`/`HIGH` (`ignore-unfixed: true` to avoid noise on advisories without a patch). `skip-dirs: demo` excludes demo-lab artifacts.
- **Release: Trivy image scan now gating** — `exit-code: 0` → `1` in `release.yaml`. Releases fail on `CRITICAL`/`HIGH` in published images (previously: report only).
- **CodeQL workflow** (`.github/workflows/codeql.yaml`) — Go + TypeScript static analysis with `security-extended` query suite. Runs on push, PR, and weekly schedule (Mon 06:00 UTC). Findings appear in the Security tab.
- **Dependabot expanded** to `gomod` (`/backend`), `npm` (`/frontend`), and Docker base images in `/deploy/images`. Grouped updates for OpenTelemetry, gRPC/protobuf, Vuetify, Vue core, ESLint, Vite to reduce PR noise.

### Dependencies (Dependabot bumps since v0.1.23)
- **Backend (Go):** `pgx/v5` `v5.9.0` → `v5.9.2`, `getkin/kin-openapi` `v0.133.0` → `v0.138.0`, `labstack/echo/v4` `v4.15.1` → `v4.15.2`, `spf13/cobra` `v1.10.1` → `v1.10.2`, `coreos/go-oidc/v3` `v3.17.0` → `v3.18.0`, `go.uber.org/zap` `v1.27.0` → `v1.28.0`, `oapi-codegen/runtime` `v1.1.2` → `v1.4.0`, `yandex-cloud/go-genproto` (bump).
- **Frontend (npm):** `vuetify` group, `vue-core` group (6 packages), `vitest` `3.2.4` → `4.1.6`, `prettier` `3.6.2` → `3.8.3`, `eslint` group (3 packages), `@tsconfig/node22` `22.0.2` → `22.0.5`.
- **Containers:** `alpine` `3.21` → `3.23`, `node` `22-alpine` → `26-alpine`.
- **GitHub Actions:** `github/codeql-action` `v3` → `v4`.

### Misc
- Dependabot config bugfix.

## v0.1.23

#### Security
- **Go toolchain CVEs (10 vulnerabilities)**: bumped CI Go version pin from `1.26` to `1.26.x` (floating patch) to pick up `go1.26.3`. Fixes:
  - `html/template` XSS via meta-content URL escaping bypass (GO-2026-4982), escaper bypass (GO-2026-4980), JsBraceDepth context tracking (GO-2026-4865)
  - `crypto/x509` unexpected work in chain building (GO-2026-4947), inefficient policy validation (GO-2026-4946), case-sensitive excludedSubtrees auth bypass (GO-2026-4866)
  - `crypto/tls` unauthenticated TLS 1.3 KeyUpdate DoS (GO-2026-4870)
  - `net` panic on NUL byte in Dial/LookupPort on Windows (GO-2026-4971)
  - `net/http` HTTP/2 infinite loop on bad SETTINGS_MAX_FRAME_SIZE (GO-2026-4918)
- **`golang.org/x/net` bump** `v0.50.0` → `v0.53.0` — fixes HTTP/2 server panic on crafted frames (GO-2026-4559) and the above HTTP/2 SETTINGS_MAX_FRAME_SIZE issue (GO-2026-4918)
- `go.mod` `go` directive kept at `1.26.0` (minimum) — only the CI/Docker toolchain floats forward; `golang:1.26-alpine` image already auto-picks the latest patch
- **CI: `govulncheck` job** added to `.github/workflows/ci.yaml` and wired into `build-check.needs` — every push and PR is now scanned for symbol-level Go vulnerabilities, blocking merge to `main` on any finding
- **CI: `npm audit` job** added for the frontend (`vulncheck-frontend`), also wired into `build-check.needs`. Fails on `high` or `critical` advisories (`--audit-level=high`) against `frontend/package-lock.json`

## v0.1.22
- **Chart** ESO version

## v0.1.20

#### New Features
- **Active Queries — query text filter**: text field with `LIKE` / `NOT LIKE` toggle, case-insensitive (`ILIKE` on the backend). Wildcards (`%`, `_`) are explicit. New query params on `GET /api/queries/running`: `query_filter`, `query_filter_mode`
- **Active Queries — username filter**: autocomplete sourced from `/api/common/database-users` (same source as Query Report exclude-users). New query param `username`
- **Active Queries — Play/Stop + refresh interval**: Play/Stop button + interval selector (1 / 5 / 10 sec, default 5). Auto-refresh starts only on user click (same UX as Operation Progress) and is capped at 5 minutes; remaining time is shown next to the Play/Stop button. Interval changes restart the timer in flight. Cluster switch stops auto-refresh
- **Active Queries — query text in expanded row**: SQL is moved out of the column into a per-row expanded cell (same pattern as Top Tables by Size). Syntax highlighting + copy-to-clipboard button, query truncated at 100 chars with a "Show SQL" dialog for the full text (same UX as Query Report card). The `state` column is removed from the table — for non-idle queries it is almost always `active`
- **Query Report / Compare — stddev and usernames**: new fields `StddevExecTimeMs`, `StddevPlanTimeMs` (`max(stddev_*_time)` across aggregated `pg_stat_statements` rows) and `Usernames` (`array_agg(DISTINCT rolname)`). σ is shown next to avg on the `min..max, avg` line; usernames render as chips — in the report card next to queryid, in the comparison card as a single full-width row per A/B side (with i18n plural support: «Пользователь / Пользователя / Пользователей»). All three report SQL templates (base / 150000 / 170000) updated

#### Bug Fixes
- **OIDC error pages**: all auth callback failures (token exchange, missing id_token, invalid id_token, claims parse, session error) and login state-cookie generation now render the styled apology page (`oidc_unavailable.html`) instead of raw JSON. The HTML is now a `html/template` with `{{.Message}}` and `{{.ShowRetry}}` substitution; specific error contexts get tailored messages, and a "Try logging in again" link is shown when retry makes sense

#### Improvements
- **Active Queries — section state in Pinia**: `activeQueries` store (per-cluster, localStorage) now persists `minDuration`, `queryFilter`, `queryFilterMode`, `username`, `intervalSec`. Smooth cluster switching with restored UI state. Auto-refresh `running` state is intentionally **not** persisted — the timer must be re-armed by the user after a cluster switch or page reload
- **`useAutoRefresh` composable**: `pollInterval` accepts a getter `() => number` for reactive intervals; new `restart()` method to reapply interval mid-flight
- **Locks tree / Active Queries — human-readable durations**: durations now render as `2 h 30 min` / `45 sec` / `120 ms` via `fmtMs(ms, t)` instead of the raw PG interval string `00:01:23.456`. Backend: `QueryBlocked` now also returns `BlockedDurationMs` / `BlockingDurationMs` (`EXTRACT(EPOCH FROM age(...)) * 1000`); Active Queries table column is bound to `DurationMs` for correct numeric sorting
- **Active Queries — pause auto-refresh on SQL copy / show**: clicking the copy or "Show SQL" button stops auto-refresh so the row doesn't disappear while the user is reading
- **`truncateSql` / `SQL_PREVIEW_MAX` shared in `utils/sql.ts`**: deduplicated local truncation helpers in `RunningQueriesSection` and `ReportCard` (previously 100 vs 120 chars) — single shared 100-char preview threshold

## v0.1.19

#### Bug Fixes
- **Query Report — CPU time**: previously could be negative or exceed 100% when a query ran with parallel workers (because `pg_stat_statements` aggregates IO time across all workers, while `total_exec_time` is wall-clock leader). Now: backend returns `null` for `cpu_time` when math gives a negative result; frontend renders `?` icon with explanatory tooltip. IO time now also includes `temp_blk_read_time + temp_blk_write_time` (PG15+) for completeness. New `150000/` SQL template added so PG14 (which lacks temp_blk timing) keeps its existing formula
- **Index Usage**: tables with `seq_scan > 0, idx_scan = 0` now show `0%` instead of the "Insufficient data" placeholder; `—` is shown only when there is no scan activity at all
- **Table Describe — cluster switch**: switching cluster no longer triggers 404; selected table is cleared and stale table data is dropped. `useClusterSelector.pushToUrl(true)` drops cluster-specific query params (`schema`, `table`, etc.) on cluster change; `isSyncing` is held through `nextTick` to suppress the host/db watcher from re-adding extras; `DescribeTableSelector` is remounted via `:key="clusterName"`
- **Table Describe — Bloat card**: now resets on cluster / host / database / schema / table change (was retaining stale data when context changed)
- **Sidebar submenu state**: Tables / Indexes submenu expands correctly after page reload when the current route is inside the submenu (root cause: router readiness was not awaited before app mount). Navigation-based expansion via per-group `:model-value` computeds — Vuetify did not always pick up changes to the parent v-list `:opened` array

#### Improvements
- **Connection States / Connection Sources**: empty cells for service processes (autovacuum launcher, walwriter, checkpointer, etc.) are now filled with `backend_type` from `pg_stat_activity` via `COALESCE`
- **Connection States chart**: deterministic per-state color via HSL hash for unknown `backend_type` values (was using a single brown fallback for all service processes)
- **Index Bloat**: byte-size columns rendered via `fmtBytes` (KB/MB/GB); redundant "(bytes)" suffix removed from column headers

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
- **Query Report / Top10: queryid precision loss**
- **Running queries: NULL scan error**: `GetQueriesRunning` crashed with `cannot scan NULL into *string` for background processes (autovacuum, walsender, logical replication worker) where `usename` is NULL. `usename` and `backend_type` are wrapped in `COALESCE(..., '')` across all three SQL templates (base, 100000/, 90600/).


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
- **Query Report: substring search**: new text field in the report header filters cards by substring match against the full query text (including the part hidden behind the ellipsis in the card) or queryid. 200 ms debounce; clearable via the "×" button.


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
