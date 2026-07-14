<p align="center">
  <img src="assets/logo.png" width="650">
</p>

PostgreSQL performance dashboard for analyzing database cluster health, identifying problems, and providing optimization recommendations.

[Russian / Русская версия](README.ru.md)

[![CI](https://github.com/dbulashev/dasha/actions/workflows/ci.yaml/badge.svg)](https://github.com/dbulashev/dasha/actions/workflows/ci.yaml)
[![Docker Backend](https://img.shields.io/docker/v/dbulashev/dasha-backend?label=backend&sort=semver)](https://hub.docker.com/r/dbulashev/dasha-backend)
[![Docker Frontend](https://img.shields.io/docker/v/dbulashev/dasha-frontend?label=frontend&sort=semver)](https://hub.docker.com/r/dbulashev/dasha-frontend)
![License](https://img.shields.io/badge/license-GPLv3-blue)
![Go](https://img.shields.io/badge/Go-1.26-00ADD8)
![Vue](https://img.shields.io/badge/Vue-3.5-4FC08D)
![PostgreSQL](https://img.shields.io/badge/PostgreSQL-14--18-336791)

## Features

**Query Analysis**
- Top-10 queries by execution time and WAL volume
- Comprehensive query report (rows, calls, planning/execution time, cache hit ratio, WAL, temp buffers, contribution %)
- Running and blocked queries monitoring
- `pg_stat_statements` status and reset time tracking
- **pgss snapshots**: save point-in-time snapshots to a dedicated storage database, view and share via URL
- **Snapshot comparison**: side-by-side diff of two snapshots or one snapshot vs live data, sortable by any metric
- **Auto-snapshots**: separate `dasha autosnapshot` daemon creates snapshots automatically on activity spikes (sliding-window avg on `pg_stat_activity`) or master↔replica role changes; configurable per cluster via UI, retention by total size
- **Lock snapshots**: an activity-spike snapshot can also capture the `pg_blocking_pids` lock-contention graph — a background blocked-count sampler runs during the spike, then a short burst of probes keeps the harshest graph (most distinct blocked sessions); also available on demand via a "with locks" manual snapshot

**Index Analysis**
- Top-K by size, bloat estimation, duplicate detection
- B-tree on array columns (potential misuse detection)
- Invalid / not ready indexes
- Three similarity detection algorithms
- Unused indexes (cross-host analysis), usage statistics, cache hit rate

**Table Analysis**
- Top-K by size with TOAST breakdown (main, FSM, VM layers)
- Sequential vs. index scan ratio
- Cache hit rate, partitioned table info
- Custom storage parameters (fillfactor, autovacuum overrides)
- Detailed table describe: columns, indexes, constraints, bloat, partitions, vacuum stats with computed thresholds, row-size / TOAST estimate

**Foreign Key Analysis**
- Invalid constraints
- Type mismatches between FK columns
- Nullable FK attributes
- Similar FK detection

**Maintenance & Vacuum**
- Autovacuum freeze max age, transaction ID wraparound danger
- Vacuum progress monitoring (PG 9.6+, extended in PG 17+)
- Per-table vacuum/analyze statistics with custom parameter awareness

**Connections & Locks**
- Connection states and sources breakdown
- Active session details (`pg_stat_activity`)
- Wait events grouped by type/event
- Lock tree visualization

**Progress Tracking**
- ANALYZE, VACUUM, CLUSTER / VACUUM FULL, CREATE INDEX, BASE BACKUP

**Settings Analysis**
- Excessive logging detection
- `from_collapse_limit` / `join_collapse_limit` deviations
- `huge_pages`, TOAST/WAL compression algorithm checks
- Checkpoint ratio analysis (`checkpoint_req` vs `checkpoint_timed`)
- Autovacuum and autoanalyze configuration review

**Health Score**
- Composite 0–100 instance score across eight categories (connections, performance, storage, replication, maintenance, horizon, WAL/checkpoint, locks) with continuous penalty functions — top-level `/health-score` page plus a Home-page gauge
- Parallel rules engine producing prioritized recommendations (severity, metric values, per-database drill-down) that stay in lockstep with the score
- Optional **metrics-backed mode**: with a Prometheus/VictoriaMetrics datasource configured (`health_score.metrics`), the score, recommendations and a trend with seasonal baseline and dip detection are computed from time series (pgSCV, Yandex MDB, pgbouncer, host metrics) instead of point-in-time SQL; the SQL snapshot stays the zero-config fallback
- Scoring model details: [README-health-score.md](README-health-score.md)

**Log Search (Yandex Cloud)**
- Search PostgreSQL server and connection-pooler (Odyssey) logs of Yandex-MDB-discovered clusters through the MDB API — top-level `/logs` page, no agents or log shipping required
- Native severity/host filters plus Dasha-side message substrings (AND), `grep -v`-style excludes and database/user filters; cursor pagination and partial results on timeout
- Optional deduplication groups near-identical messages by masked template (`<*>` placeholders) with count and first/last seen
- Frequency histogram (time × severity) with click/drag zoom, one-click presets (deadlocks, autovacuum, checkpoints, …), shareable URL filters, Grafana time-range clipboard interop
- Per-user rate limiting protects the Yandex Cloud API quota (`log_search.rate_limit`, separate admin limit)

**Authentication & Authorization**
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

**Infrastructure**
- Multi-cluster support with per-cluster host/database selection
- Yandex Managed Service for PostgreSQL service discovery
- Primary / replica role display
- Optional snapshot storage database (daily-partitioned tables, `dasha migrate` CLI)
- [MCP connector](#mcp-connector-dasha-mcp) (`dasha-mcp`): read-only MCP server exposing fleet diagnostics to AI assistants (22 tools, 5 prompts)

**User Preferences** (Settings dialog — gear in the user menu)
- Interface language: English, Russian, German — auto-detected from the browser when unset, switchable at runtime, persisted locally (untranslated keys fall back to English)
- Theme: system (follows the OS), light or dark
- Time zone for every displayed timestamp: local, UTC, or any of the eleven Russian zones (listed by IANA id, the same ids PostgreSQL's `timezone` GUC and the server logs use); fixed zones are labelled on the timestamp (`… GMT+3`), and chart axes follow the same setting as the tables
- Rows per page (10–100) — this is the server-side `limit`, so it also controls how much each table requests

## Architecture

```mermaid
flowchart LR
    SPA["Vue 3 SPA (Vuetify)<br/>:3000"]
    LLM["AI assistant<br/>(MCP client)"]
    MCP["dasha-mcp<br/>MCP server :8765"]
    BE["Go backend (Echo)<br/>:8000"]
    AS["dasha autosnapshot<br/>daemon"]
    PG[("PostgreSQL clusters<br/>14 – 18")]
    ST[("Snapshot storage<br/>pgss, PAT, HS weights")]
    TS[("Prometheus /<br/>VictoriaMetrics")]
    YC["Yandex Cloud API<br/>MDB discovery · logs"]

    SPA -->|/api| BE
    LLM -->|stdio / HTTP| MCP
    MCP -->|REST + X-API-Key| BE
    BE --> PG
    BE --> ST
    BE -.->|metrics-backed<br/>health score| TS
    BE -.->|discovery,<br/>log search| YC
    AS --> PG
    AS --> ST
```

**API-first**: the OpenAPI 3.0 spec (`doc/swagger.yaml`) is the single source of truth. Backend stubs and frontend API client are generated from it.

| Layer | Stack |
|-------|-------|
| Frontend | Vue 3, Vuetify 3, Pinia, TanStack Vue Query, vue-i18n, Vite |
| Backend | Go 1.26, Echo v4, pgx v5, Casbin, gorilla/securecookie, coreos/go-oidc, Viper, Cobra, Zap, samber/do |
| Code generation | oapi-codegen (Go server), orval (TypeScript client) |
| Testing | Vitest, Playwright, testcontainers-go (PG 14-18 matrix) |

## Quick Start

### Prerequisites

- Go 1.26+
- Node.js 22+ & npm
- PostgreSQL 14+ (target databases)
- Docker & Docker Compose (for demo lab)

### Configuration

Create `dasha.yaml` (searched in `.`, `$HOME/.dasha/`, `/etc/dasha/`):

```yaml
debug: false
# pg_stats_view: monitoring.pg_stats  # custom view when user lacks pg_catalog.pg_stats access
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

#### Yandex MDB Service Discovery (optional)

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

#### Log Search (optional)

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

#### Authentication (optional)

Dasha supports three authentication modes configured in `dasha.yaml`:

**No authentication (default)**
```yaml
auth:
  mode: none
```

**Static API keys**
```yaml
auth:
  mode: token
  tokens:
    - name: monitoring
      token_from_env: DASHA_TOKEN_MONITORING
      role: viewer
    - name: admin-cli
      token_from_env: DASHA_TOKEN_ADMIN
      role: admin
```

Clients send the key via `X-API-Key` header.

**OpenID Connect (Keycloak, Google, etc.)**
```yaml
auth:
  mode: oidc
  oidc:
    issuer_url: "https://keycloak.example.com/realms/dasha"
    client_id: "dasha-app"
    client_secret_from_env: DASHA_OIDC_SECRET
    redirect_url: "https://dasha.example.com/auth/callback"
    role_claim: "realm_access.roles"
  cookie_secret_from_env: DASHA_COOKIE_SECRET  # 32+ chars for AES-256
  cookie_max_age: 86400
  tokens:  # API keys also work alongside OIDC
    - name: monitoring
      token_from_env: DASHA_TOKEN_MONITORING
      role: viewer
  rate_limit:
    requests_per_second: 10
    burst: 20
```

Roles are extracted from the OIDC ID token claims at the path specified by `role_claim`. Supported roles: `admin` (full access) and `viewer` (read-only GET requests). If no known role is found, `viewer` is assigned by default.

**Generating secrets**

```bash
# Cookie secret (32+ characters for AES-256 session encryption)
openssl rand -base64 32

# OIDC client secret (register this value in your OIDC provider)
openssl rand -base64 32
```

#### Snapshot Storage (optional)

To enable pgss snapshots, configure a dedicated PostgreSQL database:

```yaml
storage:
  dsn: "postgres://dasha:secret@localhost:5432/dasha_storage?sslmode=require"
  # dsn_from_env: DASHA_STORAGE_DSN  # alternative: read from env variable
```

Run `dasha migrate` to create partitioned tables before first use.

#### Auto-snapshots (optional)

When snapshot storage is configured, you can run a separate daemon that creates snapshots automatically on configurable triggers:

```bash
dasha autosnapshot
```

The daemon uses the same `dasha.yaml` config. All knobs (triggers, thresholds, retention) are stored in the storage DB and edited from the UI (*Auto-snapshots* menu, admin-only). Run a single instance by default; to run multiple replicas for HA, enable advisory-lock leader election with `storage.leader_election: true` (off by default, since a session-level advisory lock needs a dedicated connection and is incompatible with transaction-pooling proxies like PgBouncer in transaction mode).

On an activity-spike trigger the daemon can additionally capture the lock-contention graph (`capture_locks`, on by default): a cheap blocked-session counter runs in the background during the spike, and at trigger time a short burst of probes (`lock_probe_count` × `lock_probe_interval`, default 5 × 500 ms) records the full `pg_blocking_pids` graph, keeping the probe with the most distinct blocked sessions. The result is stored in `snapshots.locks_data` and viewable from the snapshot view in *Query Stats*.

Triggers:
- **activity_spike** — fires when `count(state='active')` in `pg_stat_activity` exceeds the sliding-window baseline by a configurable percent (default +50%) for a sustained duration (default 5 min)
- **role_change** — fires on master↔replica transitions (direction: `both` / `master_to_replica` / `replica_to_master`)

Retention drops the oldest day-triples once total size exceeds `retention_bytes`, respecting the `retention_min_days` floor.

#### Personal Access Tokens (optional)

A logged-in user can mint **personal access tokens (PATs)** — bearer secrets sent as the `X-API-Key` header — so non-browser clients (the `dasha-mcp` server, scripts) act with that user's identity and role (RBAC is preserved). Requires snapshot storage: tokens are stored hashed in `api_tokens`, so run `dasha migrate` first.

**Auth mode must be `oidc`.** Minting requires an individually-identifiable principal, so it is refused for a static config token (shared, carries no per-user identity — a leaked one could otherwise mint tokens that outlive its removal from the config) and for another PAT (anti-chaining). Who may mint is further gated by `auth.pat_min_role`: `admin` (default while the feature matures) or `viewer` (any signed-in user).

- **Mint from the UI**: user menu → gear (*Settings*) → *My tokens* → create (name, role ≤ your own, optional expiry). The full secret is shown **once**.
- **Use it from any client:**

  ```bash
  curl -H "X-API-Key: dasha_pat_…" http://localhost:8000/api/clusters
  ```

List your tokens with `GET /api/auth/tokens` (no secrets); revoke with `DELETE /api/auth/tokens/{id}` (effective immediately). The requested role cannot exceed yours (default `viewer`); `expires_in_days` is optional (0 / omitted = no expiry). Both listings accept `?include_revoked=true` — a revoked token is kept as an audit row but can never authenticate again.

**Administration (admin only).** An administrator sees and revokes every user's tokens, and browses who has access — *Settings* → *All tokens* / *Users*:

```bash
curl -H "Cookie: <oidc-session>" http://localhost:8000/api/auth/admin/tokens        # all owners' tokens
curl -H "Cookie: <oidc-session>" -X DELETE .../api/auth/admin/tokens/{id}           # revoke any of them
curl -H "Cookie: <oidc-session>" http://localhost:8000/api/auth/admin/users          # who signed in, and when
```

The user directory is populated by SSO sign-ins (`users` table, also created by `dasha migrate`): each principal gets a row on first login, with `last_login_at` refreshed on every login. Roles shown there come from the identity provider and are an audit trail, not an authorization source. Like minting, these endpoints require an **interactive OIDC admin session** — an admin-scoped PAT is refused, so a leaked token cannot enumerate or revoke the tokens that would replace it.

### Run Locally

```bash
# Backend (serves API on :8000)
make run-backend

# Frontend (dev server on :5173, proxies /api to :8000)
make run-frontend

# MCP server (HTTP/SSE on :8765, against the backend on :8000)
make run-mcp
```

### Demo Lab

A full demo environment with multiple PostgreSQL clusters, streaming replication, and a workload generator:

```bash
make demo-lab          # Build and start (http://localhost:3000)
make demo-lab-logs     # Follow logs
make demo-lab-restart  # Rebuild and restart
make demo-lab-down     # Stop and clean up
```

The demo includes:
- **PG18 cluster**: master + streaming replica
- **PG17 cluster**: master + 2 replicas (with intentionally "bad" settings for analysis)
- **PG18 standalone**: logical replication subscriber
- **Keycloak**: OIDC provider with preconfigured realm, users `admin`/`admin` and `viewer`/`viewer`
- **Storage DB**: snapshot storage with auto-migration on startup
- **Workload generator**: continuous background load for realistic data

## MCP Connector (dasha-mcp)

`dasha-mcp` is a separate, **read-only** [MCP](https://modelcontextprotocol.io) server over the Dasha API. It lets AI assistants query the fleet's PostgreSQL diagnostics as tools/prompts, forwarding each caller's token to Dasha so its RBAC is preserved. Any MCP-compatible client works — Claude Desktop, Claude Code, Cursor, Continue, **opencode**, etc.

- **Tools (24):** `list_clusters`, `fleet_health`, `get_instance_info`, `get_health_score`, `get_health_recommendations`, `health_details` (turns a recommendation into a target: pass its `rule_id` as `detail` to get the offending tables, databases or sessions — the per-table drill-downs also take a `database`, the instance-wide ones do not), `health_trend`, `health_databases`, `top_queries` (by time/WAL), `query_report`, `list_snapshots`, `query_compare`, `running_queries`, `blocked_queries`, `list_indexes` (missing/unused/usage), `unused_index_report` (cluster-wide verdict on whether an index is safe to DROP: weighs the scan counter against every host of the cluster and against the statistics window behind it, because `idx_scan` is not replicated and a counter without its window means nothing), `top_tables`, `describe_table`, `get_replication`, `settings_analyze`, `wait_events`, `connections`, `vacuum_danger`, `search_logs` (Yandex Cloud PostgreSQL/pooler logs; Yandex-MDB-discovered clusters only, rate-limited per user). All are annotated **read-only** and closed-world so compatible clients can surface (and auto-approve) them as safe. The server also ships usage **instructions** that prime the model on which tool/prompt to reach for.
- **Prompts (5):** `diagnose_cluster`, `explain_health_score`, `find_index_opportunities`, `investigate_slow_queries`, `fleet_overview` — linear playbooks: numbered steps, one tool per step, with an interpretation criterion on each (built for models without deep PostgreSQL expertise; strong models simply move faster through them).
- **Resources (3):** an embedded knowledge base the model can read on demand — `dasha://kb/health-rules` (every health rule with LOW/MED/HIGH thresholds and first actions), `dasha://kb/wait-events` (wait event glossary), `dasha://kb/workflow` (complaint-to-tool-chain playbooks and API care rules).
- **Language:** `--lang en|ru` (or `DASHA_MCP_LANG`) selects the language of the knowledge base, playbooks and instructions; tool names, schemas and results stay English.

**Prerequisite:** a Dasha API token — a [personal access token](#personal-access-tokens-optional) (`dasha_pat_…`) or a static config token. It determines the role (`viewer` is enough).

### Build

```bash
cd backend && go build -o dasha-mcp ./cmd/dasha-mcp
# or a container image:
docker build -f deploy/images/Dockerfile.mcp -t dasha-mcp .
```

### stdio (local — Claude Desktop / Claude Code / opencode / Cursor)

The client launches the binary and talks over stdin/stdout; the token is the `DASHA_MCP_TOKEN` env var.

**Claude Desktop** (`claude_desktop_config.json`) or **Cursor** (`.cursor/mcp.json`):

```json
{
  "mcpServers": {
    "dasha": {
      "command": "/path/to/dasha-mcp",
      "args": ["--dasha-url", "http://localhost:8000"],
      "env": { "DASHA_MCP_TOKEN": "dasha_pat_…" }
    }
  }
}
```

**Claude Code:**

```bash
claude mcp add dasha --env DASHA_MCP_TOKEN=dasha_pat_… -- /path/to/dasha-mcp --dasha-url http://localhost:8000
```

**opencode** (`opencode.json` or `~/.config/opencode/opencode.json`):

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "dasha": {
      "type": "local",
      "command": ["/path/to/dasha-mcp", "--dasha-url", "http://localhost:8000"],
      "enabled": true,
      "environment": { "DASHA_MCP_TOKEN": "dasha_pat_…" }
    }
  }
}
```

### HTTP/SSE (shared / multi-user)

Run it as a service; **each request carries its own token** (no shared server token), so per-user RBAC is preserved:

```bash
dasha-mcp --http :8765 --dasha-url http://dasha-backend:8000
# container:
docker run -p 8765:8765 dasha-mcp --http :8765 --dasha-url http://dasha-backend:8000
```

Point a remote-MCP client at `http://<host>:8765` and send the token as `Authorization: Bearer dasha_pat_…` (or `X-API-Key`). For example, **opencode**:

```json
{
  "mcp": {
    "dasha": {
      "type": "remote",
      "url": "http://localhost:8765",
      "enabled": true,
      "headers": { "Authorization": "Bearer dasha_pat_…" }
    }
  }
}
```

The server is read-only (no mutating endpoints are exposed) and runs as a non-root user. Hardening: tool results are size-capped (oversized results are refused with a hint to narrow the request, never truncated into invalid JSON); the per-token server cache is hashed and bounded; tokens are never logged. Put the HTTP transport behind TLS in shared deployments; rate limiting is enforced upstream by Dasha's per-identity limiter (each PAT is a distinct identity), so it applies through the passthrough.

### Multiple Dasha instances (environments)

Each environment (dev / stage / prod) runs its own Dasha and its own `dasha-mcp`. Register them as separate MCP servers on the client — the server name namespaces everything (tools, prompts and `dasha://kb/*` resources are tracked per server, so URIs never clash):

```json
"mcpServers": {
  "dasha-dev":  { "command": "dasha-mcp", "args": ["--dasha-url", "https://dasha.dev.example.com"],  "env": { "DASHA_MCP_TOKEN": "dasha_pat_…" } },
  "dasha-prod": { "command": "dasha-mcp", "args": ["--dasha-url", "https://dasha.prod.example.com"], "env": { "DASHA_MCP_TOKEN": "dasha_pat_…" } }
}
```

Personal access tokens are per-instance: a PAT minted on dev is not valid on prod.

### Kubernetes (Helm)

The chart ships an optional, gated MCP Deployment + Service (HTTP mode). Enable it and the server is wired to the in-cluster backend automatically:

```yaml
# values.yaml
mcp:
  enabled: true
  port: 8765
  # dashaUrl: ""   # empty = in-cluster {release}-backend Service
  # lang: ru       # knowledge-base / playbook language (default en)
```

HTTP mode is strict per-user passthrough: the chart deliberately offers no shared fallback token — every client must send its own credential per request, keeping RBAC and audit per-user.

This creates `{release}-mcp` Deployment + `ClusterIP` Service on port `8765`. To expose it outside the cluster, front the Service with your own Ingress/Gateway (terminate TLS there) and have clients send `Authorization: Bearer dasha_pat_…` per request.

## Development

### Project Structure

```
├── doc/swagger.yaml              # OpenAPI 3.0 spec (source of truth)
├── backend/
│   ├── cmd/main.go               # Entry point (Cobra CLI + Echo server)
│   ├── cmd/dasha-mcp/            # MCP server entry point (stdio / HTTP)
│   ├── gen/serverhttp/           # Generated server stubs (oapi-codegen)
│   ├── gen/apiclient/            # Generated API client (oapi-codegen, used by dasha-mcp)
│   ├── internal/
│   │   ├── auth/                 # Authentication, RBAC (Casbin), rate limiting
│   │   ├── autosnapshot/         # Auto-snapshot daemon (triggers, retention, leader election)
│   │   ├── config/               # Configuration types
│   │   ├── deps/                 # DI container (samber/do)
│   │   ├── discovery/            # Service discovery (Yandex MDB)
│   │   ├── dto/                  # Response data structures
│   │   ├── enums/                # Query enum (auto-generated)
│   │   ├── health/               # Health Score engine (penalties, rules)
│   │   ├── http/                 # Handlers (v1_*.go, strictserver.go)
│   │   ├── logs/                 # Yandex Cloud log search (filters, dedup, pagination)
│   │   ├── mcpserver/            # MCP connector (tools, prompts, transports)
│   │   ├── metrics/              # Metrics-backed Health Score (PromQL datasource)
│   │   ├── query/sql/            # SQL templates with PG version overrides
│   │   ├── repository/           # Data access (pgx pools)
│   │   ├── storage/              # Snapshot storage (migrations, CRUD, PAT)
│   │   └── testinfra/            # Test containers setup
│   └── dasha.yaml                # Example config
├── frontend/
│   ├── src/
│   │   ├── api/gen/              # Generated API client (orval)
│   │   ├── api/models/           # Generated TypeScript types
│   │   ├── views/                # Page components (20 views)
│   │   ├── components/           # Section components by domain
│   │   ├── stores/               # Pinia stores (clusters, hosts, theme, auth)
│   │   ├── composables/          # Vue composables
│   │   └── locales/              # i18n (ru_RU, de_DE)
│   └── package.json
├── demo/                         # Docker Compose demo environment
└── mk/                           # Makefile includes
```

### Commands

```bash
# Code generation (after changing swagger.yaml)
make generate

# Linting
make lint-go  # Go: revive + gosec
make lint-vue # Vue: eslint

# Testing
make test-unit                                     # Unit tests
make test-integration                              # Integration tests (Docker required)
POSTGRES_VERSION=14 make test-integration          # Specific PG version
cd frontend && npm run test:unit                   # Frontend unit tests

# Dependencies
make deps-install      # Install toolchain
make deps              # go mod tidy + download
```

### Code Generation Pipeline

```
doc/swagger.yaml
       │
       ├──> oapi-codegen ──> backend/gen/serverhttp/api.gen.go
       │
       └──> orval ──> frontend/src/api/gen/    (Vue Query hooks)
                    └> frontend/src/api/models/ (TypeScript types)
```

### SQL Template Versioning

SQL queries live in `backend/internal/query/sql/<domain>/<query>/`. Version-specific overrides use numbered directories:

```
sql/queries/running/
├── running.tmpl.sql          # Base template (latest PG)
├── 100000/running.tmpl.sql   # For PG < 10
└── 90600/running.tmpl.sql    # For PG < 9.6
```

The query engine selects the best matching template: the smallest version directory that exceeds the connected server's version, falling back to the base template.


## Deployment

### Docker Compose

The simplest way to run Dasha with pre-built images:

```bash
cd deploy/compose
# Edit dasha.yaml with your cluster settings
docker compose up -d
# Open http://localhost:3000
```

### Docker Images

Multi-architecture images (`linux/amd64`, `linux/arm64`) are published to Docker Hub on every release:

| Image | Description |
|-------|-------------|
| `dbulashev/dasha-backend` | Go API server |
| `dbulashev/dasha-frontend` | Nginx + Vue SPA, proxies `/api/` to backend |
| `dbulashev/dasha-mcp` | MCP connector for AI assistants (stdio / HTTP) |

The frontend accepts `BACKEND_URL` environment variable (default: `backend:8000`).

### Helm Chart

The chart is published as an OCI artifact to GitHub Container Registry:

```bash
helm install dasha oci://ghcr.io/dbulashev/charts/dasha --version 0.1.5
```

#### Minimal values (static clusters)

```yaml
config:
  clusters:
    - name: production
      username: monitoring_user
      password_from_env: PG_PASSWORD
      databases: [myapp]
      hosts: [pg-master.example.com]

secrets:
  existingSecret: my-pg-credentials  # must contain PG_PASSWORD key
```

#### With ESO (External Secrets Operator)

```yaml
config:
  clusters:
    - name: production
      username: monitoring_user
      password_from_env: PG_PASSWORD
      databases: [myapp]
      hosts: [pg-master.example.com]

secrets:
  externalSecret:
    enabled: true
    refreshInterval: "1m"
    secretStoreRef:
      name: vault-backend
      kind: ClusterSecretStore
    data:
      - secretKey: PG_PASSWORD
        remoteRef:
          key: dasha/production
          property: password
```

#### With Yandex MDB service discovery

```yaml
config:
  discovery:
    yandex_mdb_prod:
      type: yandex-mdb
      config:
        authorized_key: /secrets/prod/authorized_key.json
        folder_id: "b1g..."
        user: monitoring_user
        password_from_env: DISCOVERY_PROD_PASSWORD
        refresh_interval: 5
        clusters:
          - name: ".*"

secrets:
  externalSecret:
    enabled: true
    refreshInterval: "1m"
    secretStoreRef:
      name: vault-backend
      kind: ClusterSecretStore
    data:
      - secretKey: DISCOVERY_PROD_PASSWORD
        remoteRef:
          key: dasha/discovery
          property: password

cloudSAKeys:
  - name: prod
    mountPath: /secrets/prod
    externalSecret:
      enabled: true
      refreshInterval: "1m"
      secretStoreRef:
        name: vault-backend
        kind: ClusterSecretStore
      remoteRef:
        key: dasha/discovery
        property: sa_cloud_auth_key
```

#### Ingress with TLS (cert-manager)

```yaml
ingress:
  enabled: true
  className: nginx
  domain: dasha.example.com
  tls:
    enabled: true
    certManager:
      enabled: true
      issuer: cluster-issuer
```

cert-manager will create a `Certificate` resource in the application namespace.

#### Gateway API with TLS (cert-manager)

Portable alternative to Ingress — works with any Gateway API implementation (Istio, NGINX Gateway Fabric, Envoy Gateway, Cilium):

```yaml
gatewayAPI:
  enabled: true
  gatewayClassName: istio
  hostname: dasha.example.com
  # When the Gateway lives in a controller-specific namespace (e.g. istio-system),
  # set gatewayNamespace accordingly — Certificate is created in the same namespace.
  # gatewayNamespace: istio-system
  tls:
    enabled: true
    certManager:
      enabled: true
      issuer: cluster-issuer
```

The cert-manager `Certificate` is created in the Gateway's namespace (`gatewayNamespace`, defaults to the release namespace). Cross-namespace secret refs would require a `ReferenceGrant`, which the chart does not render — keeping Certificate and Gateway colocated avoids that.

Rendered resources (all conditional on `gatewayAPI.enabled: true`):
- `Gateway` — HTTP listener always; HTTPS listener only when `gatewayAPI.tls.enabled: true`.
- `HTTPRoute` (main) — attached to the HTTPS listener when `tls.enabled`, otherwise to the HTTP listener.
- `HTTPRoute` (HTTP→HTTPS redirect, `RequestRedirect` filter) — only when `gatewayAPI.tls.enabled && gatewayAPI.tls.redirect`.
- `Certificate` (cert-manager) — only when `gatewayAPI.tls.certManager.enabled`.

`ingress.enabled` and `gatewayAPI.enabled` are mutually exclusive — `helm template` fails if both are true.

#### API-only mode (without frontend)

```yaml
frontend:
  enabled: false

ingress:
  enabled: true
  domain: dasha-api.example.com
```

#### Key chart features

- **Config as ConfigMap** — `dasha.yaml` rendered from values, no passwords inline
- **Passwords via env** — `password_from_env` + ESO or existing Kubernetes Secret
- **Cloud SA keys** — per-folder `authorized_key.json` via ESO or existing Secret
- **Frontend optional** — deploy backend only for API access
- **Ingress / Gateway API** — single `/` rule routes to frontend (which proxies `/api/` and `/auth/` to backend); auto HTTP→HTTPS redirect when TLS is enabled; cert-manager support; mutually exclusive `gatewayAPI.enabled` for K8s Gateway API (`gateway.networking.k8s.io/v1`)
- **Security** — `podSecurityContext`, `securityContext`, separate settings for frontend/backend

## CI/CD

- **CI** runs on every push/PR to `main`: Go lint (revive + gosec), frontend lint (ESLint), unit tests, integration tests (PG 14–18 matrix), `govulncheck` + `npm audit` vulnerability gates, Trivy filesystem/IaC scan, Helm lint, build check
- **CodeQL** (Go + TypeScript, `security-extended`) on push, PR and a weekly schedule
- **Release** is triggered by a `v*` tag: verifies CI passed, builds multi-arch Docker images (backend, frontend, MCP) with provenance/SBOM attestation, gates them on a Trivy image scan, pushes Helm chart to GHCR
- **Dependabot** keeps Go modules, npm packages, Docker base images and GitHub Actions up to date

## Changelog

See [CHANGELOG.md](CHANGELOG.md) for release notes.

## Authors
* [Dmitry Bulashev](https://dbulashev.github.io/)

## Contributors

* [Mikhail Grigorev](https://github.com/cherts)
* [Ilya Lukyanov](mailto:lukyanov1985@gmail.com)
* [Roman Minebaev](https://github.com/minebaev)
* [Rustem Sagdeev](https://github.com/SagdeevRR)

## License

[GNU General Public License v3.0](LICENSE)
