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
- Lock tree visualization

**Progress Tracking**
- ANALYZE, VACUUM, CLUSTER / VACUUM FULL, CREATE INDEX, BASE BACKUP

**Settings Analysis**
- Excessive logging detection
- `from_collapse_limit` / `join_collapse_limit` deviations
- `huge_pages`, TOAST/WAL compression algorithm checks
- Checkpoint ratio analysis (`checkpoint_req` vs `checkpoint_timed`)
- Autovacuum and autoanalyze configuration review

**Authentication & Authorization**
- Three modes: `none` (open), `token` (static API keys), `oidc` (OpenID Connect)
- OIDC: BFF pattern with encrypted session cookies (Keycloak, Google, any OIDC provider)
- Role-based access control (RBAC) via Casbin: `admin` (full access) and `viewer` (read-only)
- Per-identity rate limiting (token bucket): by authenticated user, session cookie, or client IP
- API keys with constant-time comparison, configurable per-key roles
- Secure session management: HttpOnly/Secure/SameSite cookies, AES-256 encryption, HMAC-SHA256 signing
- CSRF protection via OAuth2 state parameter with constant-time validation
- Token revocation on logout (RFC 7009, when supported by provider)

**Infrastructure**
- Multi-cluster support with per-cluster host/database selection
- Yandex Managed Service for PostgreSQL service discovery
- Primary / replica role display
- Optional snapshot storage database (daily-partitioned tables, `dasha migrate` CLI)
- Internationalization (Russian, German)

## Architecture

```
┌─────────────┐     ┌──────────────┐     ┌──────────────────┐
│  Vue 3 SPA  │────>│  Go Backend  │────>│  PostgreSQL 14+  │
│  (Vuetify)  │<────│  (Echo)      │<────│  Clusters        │
└─────────────┘     └──────────────┘     └──────────────────┘
     :3000               :8000            multiple clusters
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

#### Personal Access Tokens (optional)

A logged-in user can mint **personal access tokens (PATs)** — bearer secrets sent as the `X-API-Key` header — so non-browser clients (the `dasha-mcp` server, scripts) act with that user's identity and role (RBAC is preserved). Requires snapshot storage: tokens are stored hashed in `api_tokens`, so run `dasha migrate` first. Auth mode must be `token` or `oidc` (not `none`).

- **From the UI** (OIDC): user menu → *Personal access tokens* → create (name, role ≤ your own, optional expiry). The full secret is shown **once**.
- **From the API**: mint with an interactive identity (an OIDC session or a static config token — *not* from another PAT):

  ```bash
  curl -sX POST http://localhost:8000/api/auth/tokens \
    -H "X-API-Key: <static-config-token>" \
    -H "Content-Type: application/json" \
    -d '{"name":"mcp","role":"viewer"}'
  # → { "token": "dasha_pat_…", "prefix": "dasha_pat_xxxxxx", ... }   (token returned once)

  curl -H "X-API-Key: dasha_pat_…" http://localhost:8000/api/clusters   # use it
  ```

List your tokens with `GET /api/auth/tokens` (no secrets); revoke with `DELETE /api/auth/tokens/{id}` (effective immediately). The requested role cannot exceed yours (default `viewer`); `expires_in_days` is optional (0 / omitted = no expiry).

### Run Locally

```bash
# Backend (serves API on :8000)
make run-backend

# Frontend (dev server on :5173, proxies /api to :8000)
make run-frontend
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

- **Tools (21):** `list_clusters`, `fleet_health`, `get_instance_info`, `get_health_score`, `get_health_recommendations`, `health_trend`, `health_databases`, `top_queries` (by time/WAL), `query_report`, `list_snapshots`, `query_compare`, `running_queries`, `blocked_queries`, `list_indexes` (missing/unused/usage), `top_tables`, `describe_table`, `get_replication`, `settings_analyze`, `wait_events`, `connections`, `vacuum_danger`. All are annotated **read-only** and closed-world so compatible clients can surface (and auto-approve) them as safe. The server also ships usage **instructions** that prime the model on which tool/prompt to reach for.
- **Prompts (5):** `diagnose_cluster`, `explain_health_score`, `find_index_opportunities`, `investigate_slow_queries`, `fleet_overview`.

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

### Kubernetes (Helm)

The chart ships an optional, gated MCP Deployment + Service (HTTP mode). Enable it and the server is wired to the in-cluster backend automatically:

```yaml
# values.yaml
mcp:
  enabled: true
  port: 8765
  # dashaUrl: ""   # empty = in-cluster {release}-backend Service
  # token:         # optional shared fallback; omit for strict per-user passthrough
  #   existingSecret: dasha-mcp-token
  #   secretKey: token
```

This creates `{release}-mcp` Deployment + `ClusterIP` Service on port `8765`. To expose it outside the cluster, front the Service with your own Ingress/Gateway (terminate TLS there) and have clients send `Authorization: Bearer dasha_pat_…` per request.

## Development

### Project Structure

```
├── doc/swagger.yaml              # OpenAPI 3.0 spec (source of truth)
├── backend/
│   ├── cmd/main.go               # Entry point (Cobra CLI + Echo server)
│   ├── gen/serverhttp/           # Generated server stubs (oapi-codegen)
│   ├── internal/
│   │   ├── auth/                 # Authentication, RBAC (Casbin), rate limiting
│   │   ├── config/               # Configuration types
│   │   ├── deps/                 # DI container (samber/do)
│   │   ├── discovery/            # Service discovery (Yandex MDB)
│   │   ├── dto/                  # Response data structures
│   │   ├── enums/                # Query enum (auto-generated)
│   │   ├── http/                 # Handlers (v1.go, strictserver.go)
│   │   ├── query/sql/            # SQL templates with PG version overrides
│   │   ├── repository/           # Data access (pgx pools)
│   │   ├── storage/              # Snapshot storage (migrations, CRUD)
│   │   └── testinfra/            # Test containers setup
│   └── dasha.yaml                # Example config
├── frontend/
│   ├── src/
│   │   ├── api/gen/              # Generated API client (orval)
│   │   ├── api/models/           # Generated TypeScript types
│   │   ├── views/                # Page components (10 views)
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

- **CI** runs on every push/PR to `main`: Go lint (revive + gosec), frontend lint (ESLint), unit tests, integration tests (PG 14–18 matrix), build check
- **Release** is triggered by a `v*` tag: verifies CI passed, builds multi-arch Docker images with provenance/SBOM attestation, scans with Trivy, pushes Helm chart to GHCR
- **Dependabot** keeps GitHub Actions up to date

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
