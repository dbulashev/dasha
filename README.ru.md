<p align="center">
  <img src="assets/logo.png" width="650">
</p>

Дашборд производительности PostgreSQL для анализа состояния кластеров баз данных, выявления проблем и предоставления рекомендаций по оптимизации.

[English version](README.md)

[![CI](https://github.com/dbulashev/dasha/actions/workflows/ci.yaml/badge.svg)](https://github.com/dbulashev/dasha/actions/workflows/ci.yaml)
[![Docker Backend](https://img.shields.io/docker/v/dbulashev/dasha-backend?label=backend&sort=semver)](https://hub.docker.com/r/dbulashev/dasha-backend)
[![Docker Frontend](https://img.shields.io/docker/v/dbulashev/dasha-frontend?label=frontend&sort=semver)](https://hub.docker.com/r/dbulashev/dasha-frontend)
![License](https://img.shields.io/badge/license-GPLv3-blue)
![Go](https://img.shields.io/badge/Go-1.27-00ADD8)
![Vue](https://img.shields.io/badge/Vue-3.5-4FC08D)
![PostgreSQL](https://img.shields.io/badge/PostgreSQL-14--18-336791)

<p align="center">
  <img src="assets/dasha-demo.gif" alt="Dasha — Home, Health Score, Query Stats, Query Report, Locks" width="900">
</p>

| | |
|---|---|
| **Кластеры** | Задаются списком в `dasha.yaml` или подтягиваются автоматически: кластеры **Patroni** из **etcd v3** или **Consul** (с именами member'ов, ролями и связями standby-кластеров) и **Yandex Managed Service for PostgreSQL**. Список баз Dasha читает из самого кластера и обновляет без перезапуска — [Patroni](doc/ru/configuration.md#кластеры-patroni-из-etcd-или-consul-опционально) · [Yandex MDB](doc/ru/configuration.md#сервис-дискавери-yandex-mdb-опционально) · [базы](doc/ru/configuration.md#автообнаружение-баз-в-кластере-опционально) |
| **Поиск по логам** | Ищет по логам PostgreSQL и пулера в **VictoriaLogs**, **OpenSearch / Elasticsearch** или через API Yandex MDB. Есть фильтры, схлопывание одинаковых сообщений, гистограмма и готовые пресеты — [настройка](doc/ru/configuration.md#поиск-по-логам-опционально) · [VictoriaLogs](doc/ru/configuration.md#источники-victorialogs) · [OpenSearch](doc/ru/configuration.md#источники-opensearch) |
| **Планы из логов** | Собирает планы `auto_explain` по запросам, показывает запросы, у которых план стал хуже, чем в прошлом периоде, и проверяет рекомендации по индексам на реальных планах — [Сводка по логам](doc/ru/configuration.md#сводка-по-логам) |
| **Health Score** | Оценивает инстанс от 0 до 100 и подсказывает, что исправить в первую очередь. Работает сразу, без настройки; с **Prometheus / VictoriaMetrics** (pgSCV, Yandex MDB, pgbouncer, метрики хоста) добавляются история и сравнение с обычным для этого времени уровнем — [модель](README-health-score.ru.md) · [режим метрик](README-health-score.ru.md#metrics-backed-режим-история-baseline-богатые-сигналы) |
| **Подбор индексов** | Разбирает запросы из `pg_stat_statements` парсером PostgreSQL, находит недостающие btree-индексы и выдаёт готовый `CREATE INDEX` — [настройка](doc/ru/configuration.md#рекомендации-по-индексам-опционально) |
| **Снимки** | Сохраняет и сравнивает снимки `pg_stat_statements`, блокировок и самых нагруженных объектов. Демон автоснимков делает их сам при всплеске активности и при смене мастера — [Снимки](doc/ru/autosnapshot.md) |
| **Проверки схемы** | 17 проверок структуры, от исчерпания последовательностей до таблиц без первичного ключа; к каждой находке — чем она грозит и как исправить — [настройка](doc/ru/configuration.md#проверки-схемы-опционально) |
| **Доступ** | Без аутентификации, по статическим токенам или через **OIDC** (Keycloak, Google и др.); роли `admin` и `viewer`, персональные токены — [Аутентификация](doc/ru/auth.md) |
| **AI-ассистенты** | `dasha-mcp` — MCP-сервер только для чтения: 34 инструмента, чтобы диагностировать кластеры из AI-ассистента — [MCP-коннектор](doc/ru/mcp.md) |
| **Развёртывание** | Docker Compose, образы на Docker Hub, Helm chart — [Развёртывание](doc/ru/deployment.md) |

Поддерживаются PostgreSQL 14–18 и Postgres Pro: вместо `pg_stat_statements` Dasha сама использует `pgpro_stats`.

## Возможности

[Анализ запросов](doc/ru/features.md#анализ-запросов) ·
[Анализ индексов](doc/ru/features.md#анализ-индексов) ·
[Анализ таблиц](doc/ru/features.md#анализ-таблиц) ·
[Анализ внешних ключей](doc/ru/features.md#анализ-внешних-ключей) ·
[Обслуживание и вакуум](doc/ru/features.md#обслуживание-и-вакуум) ·
[Соединения и блокировки](doc/ru/features.md#соединения-и-блокировки) ·
[I/O](doc/ru/features.md#io-pg_stat_io-postgresql-16) ·
[Отслеживание прогресса](doc/ru/features.md#отслеживание-прогресса) ·
[Анализ настроек](doc/ru/features.md#анализ-настроек) ·
[Проверки схемы](doc/ru/features.md#проверки-схемы) ·
[Health Score](doc/ru/features.md#health-score) ·
[Поиск по логам](doc/ru/features.md#поиск-по-логам) ·
[Анализ логов](doc/ru/features.md#анализ-логов) ·
[Аутентификация и авторизация](doc/ru/features.md#аутентификация-и-авторизация) ·
[Инфраструктура](doc/ru/features.md#инфраструктура) ·
[Пользовательские настройки](doc/ru/features.md#пользовательские-настройки) ·
[Автоснимки](doc/ru/autosnapshot.md) ·
[MCP-коннектор](doc/ru/mcp.md)

Полный список с подробностями: [doc/ru/features.md](doc/ru/features.md).

## Быстрый старт

> [!TIP]
> **Демо-лаборатория: Dasha на живой нагрузке**
>
> ```bash
> make demo-lab        # http://localhost:3000
> ```
>
> Кластер PostgreSQL 18 с репликой, генератор нагрузки и MCP-сервер, без входа.
> `make demo-lab-full` добавляет ещё кластеры, OIDC-провайдер, метрики и поиск по логам.
> Подробнее: [Демо-лаборатория](doc/ru/development.md#demo-lab).

Для своих кластеров создайте файл `dasha.yaml` (ищется в `.`, `$HOME/.dasha/`, `/etc/dasha/`):

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

Роль, под которой подключается Dasha, должна иметь право подключения к наблюдаемым базам; с `pg_monitor` ей видны и чужие запросы:

```sql
CREATE ROLE monitoring_user LOGIN PASSWORD 'secret';
GRANT pg_monitor TO monitoring_user;
GRANT CONNECT ON DATABASE myapp TO monitoring_user;
```

Без `pg_monitor` в отчёте по запросам и в панелях Top 10 будут видны только запросы самой этой роли.

Запуск на готовых образах:

```bash
cd deploy/compose
# Отредактируйте dasha.yaml под ваши кластеры
docker compose up -d
# Откройте http://localhost:3000
```

Всё остальное — сервис-дискавери, поиск по логам, проверки схемы, аутентификация, хранилище
снимков — опционально и описано в [Конфигурации](doc/ru/configuration.md).

## Документация

| Документ | Содержание |
|---|---|
| [Возможности](doc/ru/features.md) | Полный список возможностей по разделам |
| [Архитектура](doc/ru/architecture.md) | Компоненты, потоки данных, технологический стек |
| [Конфигурация](doc/ru/configuration.md) | `dasha.yaml`, сервис-дискавери, поиск по логам, проверки схемы |
| [Аутентификация](doc/ru/auth.md) | Режимы `none` / `token` / OIDC, RBAC, персональные токены |
| [Снимки](doc/ru/autosnapshot.md) | Хранилище снимков и демон автоснимков |
| [Развёртывание](doc/ru/deployment.md) | Docker Compose, образы, Helm-чарт |
| [MCP-коннектор](doc/ru/mcp.md) | `dasha-mcp` — read-only диагностика для AI-ассистентов |
| [Разработка](doc/ru/development.md) | Локальный запуск, демо-лаборатория, структура проекта, кодогенерация, CI/CD |
| [Модель Health Score](README-health-score.ru.md) | Формула, веса, все правила и пороги |

Английская версия: [doc/en/](doc/en/) — кроме модели Health Score, у неё отдельный файл
в корне: [README-health-score.md](README-health-score.md).

## История изменений

См. [CHANGELOG.ru.md](CHANGELOG.ru.md).

## Authors
* [Dmitry Bulashev](https://dbulashev.github.io/)

## Contributors

* [Anton Glushakov](https://github.com/glushakov)
* [Mikhail Grigorev](https://github.com/cherts)
* [Ilya Lukyanov](mailto:lukyanov1985@gmail.com)
* [Roman Minebaev](https://github.com/minebaev)
* [Rustem Sagdeev](https://github.com/SagdeevRR)

## Сторонние компоненты

Многие проверки индексов, таблиц, обслуживания, соединений, репликации, а также выполняющихся и
заблокированных запросов основаны на SQL из [pghero](https://github.com/ankane/pghero)
(MIT, © 2014-2026 Andrew Kane, 2008-2014 Heroku). Текст лицензии:
[backend/internal/query/LICENSE-pghero](backend/internal/query/LICENSE-pghero); атрибуция по шаблонам:
[backend/internal/query/README.md](backend/internal/query/README.md).

SQL для раздела **Проверки схемы** заимствован из проекта [db_verifier](https://github.com/sdblist/db_verifier)
(MIT, © 2024 Nikonov — текст лицензии в файле `LICENSE` проекта) — набора проверок структуры БД для
PostgreSQL. Атрибуция по шаблонам: [backend/internal/query/README.md](backend/internal/query/README.md).

Запрос **дерева блокировок** взят из [postgres_dba](https://github.com/NikolayS/postgres_dba)
(BSD 3-Clause, © 2017 Nikolay Samokhvalov) — `sql/l2_lock_trees.sql`, адаптирован в один запрос
без psql-ветвлений по версии. Текст лицензии:
[backend/internal/query/LICENSE-postgres_dba](backend/internal/query/LICENSE-postgres_dba).

Оценка bloat индексов взята из pghero и восходит к [pgsql-bloat-estimation](https://github.com/ioguix/pgsql-bloat-estimation)
(BSD-подобная лицензия PostgreSQL, © 2015-2019 Jehan-Guillaume (ioguix) de Rorthais; текст лицензии —
в файле `LICENSE` проекта).

Оценка bloat таблиц взята из [pgx_scripts](https://github.com/pgexperts/pgx_scripts)
(BSD 3-Clause, © 2014 PostgreSQL Experts, Inc.), файл `bloat/table_bloat_check.sql`. Текст лицензии:
[backend/internal/query/LICENSE-pgx_scripts](backend/internal/query/LICENSE-pgx_scripts).

## Лицензия

[GNU General Public License v3.0](LICENSE)
