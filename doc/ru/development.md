# Разработка

[English version](../en/development.md) · [← README](../../README.ru.md)

## Локальный запуск

```bash
# Бэкенд (API на :8000)
make run-backend

# Фронтенд (dev-сервер на :5173, проксирует /api на :8000)
make run-frontend

# MCP-сервер (HTTP/SSE на :8765, к бэкенду на :8000)
make run-mcp
```

## Демо-лаборатория

Полноценное демо-окружение с несколькими кластерами PostgreSQL, потоковой репликацией и генератором нагрузки:

```bash
make demo-lab          # Собрать и запустить (http://localhost:3000)
make demo-lab-logs     # Просмотр логов
make demo-lab-restart  # Пересобрать и перезапустить
make demo-lab-down     # Остановить и очистить
```

Демо включает:
- **Кластер PG18**: мастер + потоковая реплика
- **Кластер PG17**: мастер + 2 реплики (с намеренно «плохими» настройками для анализа)
- **PG18 standalone**: подписчик логической репликации
- **Keycloak**: OIDC-провайдер с настроенным realm, пользователи `admin`/`admin` и `viewer`/`viewer`
- **БД хранилища**: хранилище снимков с автоматической миграцией при запуске
- **Генератор нагрузки**: непрерывная фоновая нагрузка для реалистичных данных

## Структура проекта

```
├── doc/swagger.yaml              # Спецификация OpenAPI 3.0 (источник истины)
├── doc/en/, doc/ru/              # Пользовательская документация (английская / русская)
├── backend/
│   ├── cmd/main.go               # Точка входа (Cobra CLI + Echo-сервер)
│   ├── cmd/dasha-mcp/            # Точка входа MCP-сервера (stdio / HTTP)
│   ├── gen/serverhttp/           # Сгенерированные серверные заглушки (oapi-codegen)
│   ├── gen/apiclient/            # Сгенерированный API-клиент (oapi-codegen, для dasha-mcp)
│   ├── internal/
│   │   ├── auth/                 # Аутентификация, RBAC (Casbin), rate limiting
│   │   ├── autosnapshot/         # Демон авто-снимков (триггеры, ретеншн, выбор лидера)
│   │   ├── config/               # Типы конфигурации
│   │   ├── deps/                 # DI-контейнер (samber/do)
│   │   ├── discovery/            # Сервис-дискавери (Yandex MDB, базы внутри кластера)
│   │   ├── dto/                  # Структуры данных ответов
│   │   ├── enums/                # Перечисления запросов (автогенерация)
│   │   ├── health/               # Движок Health Score (штрафы, правила)
│   │   ├── http/                 # Обработчики (v1_*.go, strictserver.go)
│   │   ├── logs/                 # Поиск по логам Yandex Cloud (фильтры, дедуп, пагинация)
│   │   ├── mcpserver/            # MCP-коннектор (tools, prompts, транспорты)
│   │   ├── metrics/              # Health Score на метриках (PromQL-источник)
│   │   ├── query/sql/            # SQL-шаблоны с версионными переопределениями
│   │   ├── repository/           # Слой доступа к данным (pgx-пулы)
│   │   ├── storage/              # Хранилище снимков (миграции, CRUD, PAT)
│   │   └── testinfra/            # Инфраструктура тестов (testcontainers)
│   └── dasha.yaml                # Пример конфигурации
├── frontend/
│   ├── src/
│   │   ├── api/gen/              # Сгенерированный API-клиент (orval)
│   │   ├── api/models/           # Сгенерированные TypeScript-типы
│   │   ├── views/                # Компоненты страниц (20 представлений)
│   │   ├── components/           # Компоненты секций по доменам
│   │   ├── stores/               # Pinia-хранилища (clusters, hosts, theme, auth)
│   │   ├── composables/          # Vue composables
│   │   └── locales/              # i18n (ru_RU, de_DE)
│   └── package.json
├── demo/                         # Docker Compose демо-окружение
└── mk/                           # Include-файлы для Makefile
```

## Команды

```bash
# Кодогенерация (после изменения swagger.yaml)
make generate

# Линтинг
make lint-go  # Go: revive + gosec
make lint-vue # Vue: eslint

# Тестирование
make test-unit                                     # Юнит-тесты
make test-integration                              # Интеграционные тесты (нужен Docker)
POSTGRES_VERSION=14 make test-integration          # Конкретная версия PG
cd frontend && npm run test:unit                   # Юнит-тесты фронтенда

# Зависимости
make deps-install      # Установить инструменты
make deps              # go mod tidy + download
```

## Пайплайн кодогенерации

```
doc/swagger.yaml
       │
       ├──> oapi-codegen ──> backend/gen/serverhttp/api.gen.go
       │
       └──> orval ──> frontend/src/api/gen/    (Vue Query хуки)
                    └> frontend/src/api/models/ (TypeScript-типы)
```

## Версионирование SQL-шаблонов

SQL-запросы находятся в `backend/internal/query/sql/<домен>/<запрос>/`. Версионные переопределения используют нумерованные директории:

```
sql/queries/running/
├── running.tmpl.sql          # Базовый шаблон (последняя версия PG)
├── 100000/running.tmpl.sql   # Для PG < 10
└── 90600/running.tmpl.sql    # Для PG < 9.6
```

Движок запросов выбирает наиболее подходящий шаблон: наименьшую версионную директорию, превышающую версию подключённого сервера, с откатом на базовый шаблон.


## CI/CD

- **CI** запускается при каждом push/PR в `main`: линтинг Go (revive + gosec), линтинг фронтенда (ESLint), юнит-тесты, интеграционные тесты (матрица PG 14–18), проверки уязвимостей `govulncheck` + `npm audit`, Trivy-скан зависимостей/IaC, линтинг Helm, проверка сборки
- **CodeQL** (Go + TypeScript, `security-extended`) на push, PR и еженедельно по расписанию
- **Релиз** запускается по тегу `v*`: проверяет прохождение CI, собирает мультиархитектурные Docker-образы (backend, frontend, MCP) с attestation provenance/SBOM, гейтит их Trivy-сканом, публикует Helm-чарт в GHCR
- **Dependabot** автоматически обновляет Go-модули, npm-пакеты, базовые Docker-образы и GitHub Actions

