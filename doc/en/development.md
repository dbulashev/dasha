# Development

[Русская версия](../ru/development.md) · [← README](../../README.md)

## Run Locally

```bash
# Backend (serves API on :8000)
make run-backend

# Frontend (dev server on :5173, proxies /api to :8000)
make run-frontend

# MCP server (HTTP/SSE on :8765, against the backend on :8000)
make run-mcp
```

## Demo Lab

A full demo environment with multiple PostgreSQL clusters, streaming replication, and a workload generator:

```bash
make demo-lab          # Build and start (http://localhost:3000)
make demo-lab-logs     # Follow logs
make demo-lab-restart  # Rebuild and restart (runs `down -v` first — volumes are dropped)
make demo-lab-down     # Stop and remove volumes (snapshot storage and PG data are lost)
```

The demo includes:
- **PG18 cluster**: master + streaming replica
- **PG17 cluster**: master + 2 replicas (with intentionally "bad" settings for analysis)
- **PG18 standalone**: logical replication subscriber
- **Keycloak**: OIDC provider with preconfigured realm, users `admin`/`admin` and `viewer`/`viewer`
- **Storage DB**: snapshot storage with auto-migration on startup
- **Workload generator**: continuous background load for realistic data

## Project Structure

```text
├── doc/swagger.yaml              # OpenAPI 3.0 spec (source of truth)
├── doc/en/, doc/ru/              # User documentation (English / Russian)
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
│   │   ├── discovery/            # Service discovery (Yandex MDB, in-cluster databases)
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

## Commands

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

## Code Generation Pipeline

```text
doc/swagger.yaml
       │
       ├──> oapi-codegen ──> backend/gen/serverhttp/api.gen.go
       │    (.oapi-codegen.yaml)
       │
       ├──> oapi-codegen ──> backend/gen/apiclient/    (API client used by dasha-mcp)
       │    (.oapi-codegen.client.yaml)
       │
       └──> orval ──> frontend/src/api/gen/    (Vue Query hooks)
                    └> frontend/src/api/models/ (TypeScript types)
```

## SQL Template Versioning

SQL queries live in `backend/internal/query/sql/<domain>/<query>/`. Version-specific overrides use numbered directories:

```text
sql/queries/running/
├── running.tmpl.sql          # Base template (latest PG)
├── 100000/running.tmpl.sql   # For PG < 10
└── 90600/running.tmpl.sql    # For PG < 9.6
```

The query engine selects the best matching template: the smallest version directory that exceeds the connected server's version, falling back to the base template.


## CI/CD

- **CI** runs on every push/PR to `main`: Go lint (revive + gosec), frontend lint (ESLint), unit tests, integration tests (PG 14–18 matrix), `govulncheck` + `npm audit` vulnerability gates, Trivy filesystem/IaC scan, Helm lint, build check
- **CodeQL** (Go + TypeScript, `security-extended`) on push, PR and a weekly schedule
- **Release** is triggered by a `v*` tag: verifies CI passed, builds multi-arch Docker images (backend, frontend, MCP) with provenance/SBOM attestation, gates them on a Trivy image scan, pushes Helm chart to GHCR
- **Dependabot** keeps Go modules, npm packages, Docker base images and GitHub Actions up to date

