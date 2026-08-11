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

<p align="center">
  <img src="assets/dasha-demo.gif" alt="Dasha — Home, Health Score, Query Stats, Query Report, Locks" width="900">
</p>

## Features

[Query Analysis](doc/en/features.md#query-analysis) ·
[Index Analysis](doc/en/features.md#index-analysis) ·
[Table Analysis](doc/en/features.md#table-analysis) ·
[Foreign Key Analysis](doc/en/features.md#foreign-key-analysis) ·
[Maintenance & Vacuum](doc/en/features.md#maintenance--vacuum) ·
[Connections & Locks](doc/en/features.md#connections--locks) ·
[Progress Tracking](doc/en/features.md#progress-tracking) ·
[Settings Analysis](doc/en/features.md#settings-analysis) ·
[Schema Checks](doc/en/features.md#schema-checks) ·
[Health Score](doc/en/features.md#health-score) ·
[Log Search (Yandex Cloud)](doc/en/features.md#log-search-yandex-cloud) ·
[Authentication & Authorization](doc/en/features.md#authentication--authorization) ·
[Infrastructure](doc/en/features.md#infrastructure) ·
[User Preferences](doc/en/features.md#user-preferences) ·
[Auto-snapshots](doc/en/autosnapshot.md) ·
[MCP connector](doc/en/mcp.md)

The full list with details: [doc/en/features.md](doc/en/features.md).

## Quick Start

Create `dasha.yaml` (searched in `.`, `$HOME/.dasha/`, `/etc/dasha/`):

```yaml
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
```

Run with the pre-built images:

```bash
cd deploy/compose
# Edit dasha.yaml with your cluster settings
docker compose up -d
# Open http://localhost:3000
```

Everything else — service discovery, log search, schema checks, authentication, snapshot
storage — is optional and documented in [Configuration](doc/en/configuration.md).

To see Dasha against a live workload, `make demo-lab` starts several PostgreSQL clusters with
streaming replication, an OIDC provider and a load generator: [Demo Lab](doc/en/development.md#demo-lab).

## Documentation

| Document | Contents |
|---|---|
| [Features](doc/en/features.md) | Full capability list by domain |
| [Architecture](doc/en/architecture.md) | Components, data flows, technology stack |
| [Configuration](doc/en/configuration.md) | `dasha.yaml`, service discovery, log search, schema checks |
| [Authentication](doc/en/auth.md) | `none` / `token` / OIDC modes, RBAC, personal access tokens |
| [Snapshots](doc/en/autosnapshot.md) | Snapshot storage and the auto-snapshot daemon |
| [Deployment](doc/en/deployment.md) | Docker Compose, images, Helm chart |
| [MCP connector](doc/en/mcp.md) | `dasha-mcp` — read-only diagnostics for AI assistants |
| [Development](doc/en/development.md) | Local run, demo lab, project layout, code generation, CI/CD |
| [Health Score model](README-health-score.md) | Formula, weights, all rules and thresholds |

Russian translation: [doc/ru/](doc/ru/) — except the Health Score model, which keeps its own
root-level file, [README-health-score.ru.md](README-health-score.ru.md).

## Changelog

See [CHANGELOG.md](CHANGELOG.md) for release notes.

## Authors
* [Dmitry Bulashev](https://dbulashev.github.io/)

## Contributors

* [Mikhail Grigorev](https://github.com/cherts)
* [Ilya Lukyanov](mailto:lukyanov1985@gmail.com)
* [Roman Minebaev](https://github.com/minebaev)
* [Rustem Sagdeev](https://github.com/SagdeevRR)

## Third-party components

The SQL behind **Schema Checks** is derived from [db_verifier](https://github.com/sdblist/db_verifier)
(MIT, © 2024 Nikonov — licence text in the project's `LICENSE` file), a set of structural checks for
PostgreSQL. Per-template attribution: [backend/internal/query/README.md](backend/internal/query/README.md).

The **lock tree** query comes from [postgres_dba](https://github.com/NikolayS/postgres_dba)
(BSD 3-Clause, © 2017 Nikolay Samokhvalov) — `sql/l2_lock_trees.sql`, adapted to run as a single
statement without psql version branches. Licence text:
[backend/internal/query/LICENSE-postgres_dba](backend/internal/query/LICENSE-postgres_dba).

Index bloat estimation descends from [pgsql-bloat-estimation](https://github.com/ioguix/pgsql-bloat-estimation)
(BSD-style — the PostgreSQL licence, © 2015-2019 Jehan-Guillaume (ioguix) de Rorthais; licence text in the
project's `LICENSE` file).

## License

[GNU General Public License v3.0](LICENSE)
