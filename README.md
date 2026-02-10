# Dasha

PostgreSQL performance dashboard for analyzing database cluster health, identifying problems, and providing optimization recommendations.

[Russian / Русская версия](README.ru.md)

![License](https://img.shields.io/badge/license-GPLv3-blue)
![Go](https://img.shields.io/badge/Go-1.25-00ADD8)
![Vue](https://img.shields.io/badge/Vue-3.5-4FC08D)
![PostgreSQL](https://img.shields.io/badge/PostgreSQL-14--18-336791)

## Features

**Query Analysis**
- Top-10 queries by execution time and WAL volume
- Comprehensive query report (rows, calls, planning/execution time, cache hit ratio, WAL, temp buffers, contribution %)
- Running and blocked queries monitoring
- `pg_stat_statements` status and reset time tracking

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

**Infrastructure**
- Multi-cluster support with per-cluster host/database selection
- Yandex Managed Service for PostgreSQL service discovery
- Primary / replica role display
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
| Backend | Go 1.25, Echo v4, pgx v5, Viper, Cobra, Zap, samber/do |
| Code generation | oapi-codegen (Go server), orval (TypeScript client) |
| Testing | Vitest, Playwright, testcontainers-go (PG 14-18 matrix) |

## Quick Start

### Prerequisites

- Go 1.25+
- Node.js 22+ & npm
- PostgreSQL 14+ (target databases)
- Docker & Docker Compose (for demo lab)

### Configuration

Create `dasha.yaml` (searched in `.`, `$HOME/.dasha/`, `/etc/dasha/`):

```yaml
debug: false
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
- **Workload generator**: continuous background load for realistic data

## Development

### Project Structure

```
├── doc/swagger.yaml              # OpenAPI 3.0 spec (source of truth)
├── backend/
│   ├── cmd/main.go               # Entry point (Cobra CLI + Echo server)
│   ├── gen/serverhttp/           # Generated server stubs (oapi-codegen)
│   ├── internal/
│   │   ├── config/               # Configuration types
│   │   ├── deps/                 # DI container (samber/do)
│   │   ├── discovery/            # Service discovery (Yandex MDB)
│   │   ├── dto/                  # Response data structures
│   │   ├── enums/                # Query enum (auto-generated)
│   │   ├── http/                 # Handlers (v1.go, strictserver.go)
│   │   ├── query/sql/            # SQL templates with PG version overrides
│   │   ├── repository/           # Data access (pgx pools)
│   │   └── testinfra/            # Test containers setup
│   └── dasha.yaml                # Example config
├── frontend/
│   ├── src/
│   │   ├── api/gen/              # Generated API client (orval)
│   │   ├── api/models/           # Generated TypeScript types
│   │   ├── views/                # Page components (10 views)
│   │   ├── components/           # Section components by domain
│   │   ├── stores/               # Pinia stores (clusters, hosts, theme)
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

### Adding a New API Endpoint

1. Add the endpoint to `doc/swagger.yaml`
2. Run `make generate` (updates Go stubs + TypeScript client)
3. Implement the SQL template in `backend/internal/query/sql/`
4. Add the repository method in `backend/internal/repository/`
5. Implement the handler in `backend/internal/http/v1.go`
6. Create the frontend section component in `frontend/src/components/`
7. Wire it into the appropriate view in `frontend/src/views/`

## API Overview

Dasha exposes 46+ REST endpoints under `/api/`. Key endpoint groups:

| Group | Endpoints | Description |
|-------|-----------|-------------|
| `/api/clusters` | 1 | List configured clusters |
| `/api/common/*` | 2 | Summary, instance info |
| `/api/connection/*` | 3 | States, sources, activity |
| `/api/queries/*` | 6 | Running, blocked, top-10, report, stats |
| `/api/indexes/*` | 12 | Size, bloat, similar, unused, caching |
| `/api/tables/*` | 4 | Size, caching, hit rate, partitions |
| `/api/fk/*` | 4 | Constraints, type mismatches, nulls, similar |
| `/api/progress/*` | 5 | Analyze, vacuum, cluster, index, base backup |
| `/api/maintenance/*` | 4 | Freeze age, txid danger, vacuum progress, info |
| `/api/settings/*` | 3 | Analysis, PG settings, autovacuum |
| `/api/database/*` | 3 | Size, stats reset time, pgss reset time |

All data endpoints accept `cluster_name`, `instance` (host:port), and `database` query parameters.

Full specification: [`doc/swagger.yaml`](doc/swagger.yaml)


## License

[GNU General Public License v3.0](LICENSE)
