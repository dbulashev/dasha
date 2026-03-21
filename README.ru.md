<p align="center">
  <img src="assets/logo.png" width="650">
</p>

Дашборд производительности PostgreSQL для анализа состояния кластеров баз данных, выявления проблем и предоставления рекомендаций по оптимизации.

[![CI](https://github.com/dbulashev/dasha/actions/workflows/ci.yaml/badge.svg)](https://github.com/dbulashev/dasha/actions/workflows/ci.yaml)
[![Docker Backend](https://img.shields.io/docker/v/dbulashev/dasha-backend?label=backend&sort=semver)](https://hub.docker.com/r/dbulashev/dasha-backend)
[![Docker Frontend](https://img.shields.io/docker/v/dbulashev/dasha-frontend?label=frontend&sort=semver)](https://hub.docker.com/r/dbulashev/dasha-frontend)
![License](https://img.shields.io/badge/license-GPLv3-blue)
![Go](https://img.shields.io/badge/Go-1.26-00ADD8)
![Vue](https://img.shields.io/badge/Vue-3.5-4FC08D)
![PostgreSQL](https://img.shields.io/badge/PostgreSQL-14--18-336791)

## Возможности

**Анализ запросов**
- Топ-10 запросов по времени выполнения и объёму WAL
- Развёрнутый отчёт по запросам (строки, вызовы, время планирования/выполнения, cache hit ratio, WAL, временные буферы, вклад в %)
- Мониторинг активных и заблокированных запросов
- Статус `pg_stat_statements` и отслеживание времени сброса статистики

**Анализ индексов**
- Топ-K по размеру, оценка bloat, обнаружение дубликатов
- B-tree по столбцам-массивам (выявление потенциальных ошибок)
- Невалидные / не готовые индексы
- Три алгоритма поиска похожих индексов
- Неиспользуемые индексы (кросс-хостовый анализ), статистика использования, cache hit rate

**Анализ таблиц**
- Топ-K по размеру с разбивкой TOAST (main, FSM, VM)
- Соотношение последовательного и индексного сканирования
- Cache hit rate, информация о партиционированных таблицах
- Кастомные параметры хранения (fillfactor, переопределения автовакуума)

**Анализ внешних ключей**
- Невалидные ограничения
- Несовпадение типов в столбцах FK
- Nullable-атрибуты FK
- Обнаружение похожих FK

**Обслуживание и вакуум**
- Autovacuum freeze max age, опасность wraparound transaction ID
- Мониторинг прогресса вакуума (PG 9.6+, расширено в PG 17+)
- Статистика вакуума/анализа по таблицам с учётом кастомных параметров

**Соединения и блокировки**
- Разбивка по состояниям и источникам соединений
- Детали активных сессий (`pg_stat_activity`)
- Визуализация дерева блокировок

**Отслеживание прогресса**
- ANALYZE, VACUUM, CLUSTER / VACUUM FULL, CREATE INDEX, BASE BACKUP

**Анализ настроек**
- Обнаружение избыточного логирования
- Отклонения `from_collapse_limit` / `join_collapse_limit`
- Проверка `huge_pages`, алгоритмов сжатия TOAST/WAL
- Анализ соотношения чекпоинтов (`checkpoint_req` vs `checkpoint_timed`)
- Обзор конфигурации автовакуума и автоанализа

**Инфраструктура**
- Поддержка множества кластеров с выбором хоста/базы для каждого
- Сервис-дискавери Yandex Managed Service for PostgreSQL
- Отображение роли хоста (primary / replica)
- Интернационализация (русский, немецкий)

## Архитектура

```
┌─────────────┐     ┌──────────────┐     ┌──────────────────┐
│  Vue 3 SPA  │────>│  Go Backend  │────>│  PostgreSQL 14+  │
│  (Vuetify)  │<────│  (Echo)      │<────│  Кластеры        │
└─────────────┘     └──────────────┘     └──────────────────┘
     :3000               :8000            несколько кластеров
```

**API-first**: спецификация OpenAPI 3.0 (`doc/swagger.yaml`) — единственный источник истины. Серверные заглушки и клиент фронтенда генерируются из неё.

| Слой | Стек |
|------|------|
| Фронтенд | Vue 3, Vuetify 3, Pinia, TanStack Vue Query, vue-i18n, Vite |
| Бэкенд | Go 1.26, Echo v4, pgx v5, Viper, Cobra, Zap, samber/do |
| Кодогенерация | oapi-codegen (Go-сервер), orval (TypeScript-клиент) |
| Тестирование | Vitest, Playwright, testcontainers-go (матрица PG 14-18) |

## Быстрый старт

### Требования

- Go 1.26+
- Node.js 22+ и npm
- PostgreSQL 14+ (целевые базы данных)
- Docker и Docker Compose (для демо-лаборатории)

### Конфигурация

Создайте файл `dasha.yaml` (ищется в `.`, `$HOME/.dasha/`, `/etc/dasha/`):

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

#### Сервис-дискавери Yandex MDB (опционально)

```yaml
discovery:
  yandex_mdb:
    type: yandex-mdb
    config:
      authorized_key: /path/to/service-account-key.json
      folder_id: "b1g..."
      user: "monitoring_user"
      password: "secret"
      refresh_interval: 5  # минуты
      clusters:
        - name: "prod-.*"       # фильтр по regex
          exclude_name: "test"
          exclude_db: "system_db"
```

### Локальный запуск

```bash
# Бэкенд (API на :8000)
make run-backend

# Фронтенд (dev-сервер на :5173, проксирует /api на :8000)
make run-frontend
```

### Демо-лаборатория

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
- **Генератор нагрузки**: непрерывная фоновая нагрузка для реалистичных данных

## Разработка

### Структура проекта

```
├── doc/swagger.yaml              # Спецификация OpenAPI 3.0 (источник истины)
├── backend/
│   ├── cmd/main.go               # Точка входа (Cobra CLI + Echo-сервер)
│   ├── gen/serverhttp/           # Сгенерированные серверные заглушки (oapi-codegen)
│   ├── internal/
│   │   ├── config/               # Типы конфигурации
│   │   ├── deps/                 # DI-контейнер (samber/do)
│   │   ├── discovery/            # Сервис-дискавери (Yandex MDB)
│   │   ├── dto/                  # Структуры данных ответов
│   │   ├── enums/                # Перечисления запросов (автогенерация)
│   │   ├── http/                 # Обработчики (v1.go, strictserver.go)
│   │   ├── query/sql/            # SQL-шаблоны с версионными переопределениями
│   │   ├── repository/           # Слой доступа к данным (pgx-пулы)
│   │   └── testinfra/            # Инфраструктура тестов (testcontainers)
│   └── dasha.yaml                # Пример конфигурации
├── frontend/
│   ├── src/
│   │   ├── api/gen/              # Сгенерированный API-клиент (orval)
│   │   ├── api/models/           # Сгенерированные TypeScript-типы
│   │   ├── views/                # Компоненты страниц (10 представлений)
│   │   ├── components/           # Компоненты секций по доменам
│   │   ├── stores/               # Pinia-хранилища (clusters, hosts, theme)
│   │   ├── composables/          # Vue composables
│   │   └── locales/              # i18n (ru_RU, de_DE)
│   └── package.json
├── demo/                         # Docker Compose демо-окружение
└── mk/                           # Include-файлы для Makefile
```

### Команды

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

### Пайплайн кодогенерации

```
doc/swagger.yaml
       │
       ├──> oapi-codegen ──> backend/gen/serverhttp/api.gen.go
       │
       └──> orval ──> frontend/src/api/gen/    (Vue Query хуки)
                    └> frontend/src/api/models/ (TypeScript-типы)
```

### Версионирование SQL-шаблонов

SQL-запросы находятся в `backend/internal/query/sql/<домен>/<запрос>/`. Версионные переопределения используют нумерованные директории:

```
sql/queries/running/
├── running.tmpl.sql          # Базовый шаблон (последняя версия PG)
├── 100000/running.tmpl.sql   # Для PG < 10
└── 90600/running.tmpl.sql    # Для PG < 9.6
```

Движок запросов выбирает наиболее подходящий шаблон: наименьшую версионную директорию, превышающую версию подключённого сервера, с откатом на базовый шаблон.

### Добавление нового API-эндпоинта

1. Добавьте эндпоинт в `doc/swagger.yaml`
2. Запустите `make generate` (обновит Go-заглушки + TypeScript-клиент)
3. Реализуйте SQL-шаблон в `backend/internal/query/sql/`
4. Добавьте метод репозитория в `backend/internal/repository/`
5. Реализуйте обработчик в `backend/internal/http/v1.go`
6. Создайте компонент секции фронтенда в `frontend/src/components/`
7. Подключите его в соответствующее представление в `frontend/src/views/`

## Обзор API

Dasha предоставляет 46+ REST-эндпоинтов по пути `/api/`. Основные группы:

| Группа | Эндпоинтов | Описание |
|--------|-----------|----------|
| `/api/clusters` | 1 | Список настроенных кластеров |
| `/api/common/*` | 2 | Сводка, информация об инстансе |
| `/api/connection/*` | 3 | Состояния, источники, активность |
| `/api/queries/*` | 6 | Активные, заблокированные, топ-10, отчёт, статистика |
| `/api/indexes/*` | 12 | Размер, bloat, похожие, неиспользуемые, кэширование |
| `/api/tables/*` | 4 | Размер, кэширование, hit rate, партиции |
| `/api/fk/*` | 4 | Ограничения, несовпадения типов, nullable, похожие |
| `/api/progress/*` | 5 | Analyze, vacuum, cluster, index, base backup |
| `/api/maintenance/*` | 4 | Freeze age, txid danger, прогресс вакуума, информация |
| `/api/settings/*` | 3 | Анализ, настройки PG, автовакуум |
| `/api/database/*` | 3 | Размер, время сброса статистики |

Все эндпоинты данных принимают параметры `cluster_name`, `instance` (host:port) и `database`.

Полная спецификация: [`doc/swagger.yaml`](doc/swagger.yaml)


## Лицензия

[GNU General Public License v3.0](LICENSE)
