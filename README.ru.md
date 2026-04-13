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

**Аутентификация и авторизация**
- Три режима: `none` (открытый), `token` (статические API-ключи), `oidc` (OpenID Connect)
- OIDC: BFF-паттерн с зашифрованными session cookies (Keycloak, Google, любой OIDC-провайдер)
- Ролевой доступ (RBAC) через Casbin: `admin` (полный доступ) и `viewer` (только чтение)
- Per-identity rate limiting (token bucket): по пользователю, сессионной cookie или IP клиента
- API-ключи с constant-time сравнением, настраиваемые роли для каждого ключа
- Безопасное управление сессиями: HttpOnly/Secure/SameSite cookies, шифрование AES-256, подпись HMAC-SHA256
- CSRF-защита через OAuth2 state с constant-time валидацией
- Отзыв refresh token при logout (RFC 7009, при поддержке провайдером)

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
| Бэкенд | Go 1.26, Echo v4, pgx v5, Casbin, gorilla/securecookie, coreos/go-oidc, Viper, Cobra, Zap, samber/do |
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
# pg_stats_view: monitoring.pg_stats  # кастомная view, если у пользователя нет доступа к pg_catalog.pg_stats
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

#### Аутентификация (опционально)

Dasha поддерживает три режима аутентификации, настраиваемых в `dasha.yaml`:

**Без аутентификации (по умолчанию)**
```yaml
auth:
  mode: none
```

**Статические API-ключи**
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

Клиенты передают ключ через заголовок `X-API-Key`.

**OpenID Connect (Keycloak, Google и др.)**
```yaml
auth:
  mode: oidc
  oidc:
    issuer_url: "https://keycloak.example.com/realms/dasha"
    client_id: "dasha-app"
    client_secret_from_env: DASHA_OIDC_SECRET
    redirect_url: "https://dasha.example.com/auth/callback"
    role_claim: "realm_access.roles"
  cookie_secret_from_env: DASHA_COOKIE_SECRET  # 32+ символов для AES-256
  cookie_max_age: 86400
  tokens:  # API-ключи работают параллельно с OIDC
    - name: monitoring
      token_from_env: DASHA_TOKEN_MONITORING
      role: viewer
  rate_limit:
    requests_per_second: 10
    burst: 20
```

Роли извлекаются из claims ID-токена OIDC по пути, указанному в `role_claim`. Поддерживаемые роли: `admin` (полный доступ) и `viewer` (только GET-запросы на чтение). Если известная роль не найдена, по умолчанию назначается `viewer`.

**Генерация секретов**

```bash
# Cookie secret (32+ символов для AES-256 шифрования сессии)
openssl rand -base64 32

# OIDC client secret (зарегистрируйте значение в вашем OIDC-провайдере)
openssl rand -base64 32
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
- **Keycloak**: OIDC-провайдер с настроенным realm, пользователи `admin`/`admin` и `viewer`/`viewer`
- **Генератор нагрузки**: непрерывная фоновая нагрузка для реалистичных данных

## Разработка

### Структура проекта

```
├── doc/swagger.yaml              # Спецификация OpenAPI 3.0 (источник истины)
├── backend/
│   ├── cmd/main.go               # Точка входа (Cobra CLI + Echo-сервер)
│   ├── gen/serverhttp/           # Сгенерированные серверные заглушки (oapi-codegen)
│   ├── internal/
│   │   ├── auth/                 # Аутентификация, RBAC (Casbin), rate limiting
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
│   │   ├── stores/               # Pinia-хранилища (clusters, hosts, theme, auth)
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


## Развёртывание

### Docker Compose

Самый простой способ запустить Dasha с готовыми образами:

```bash
cd deploy/compose
# Отредактируйте dasha.yaml под ваши кластеры
docker compose up -d
# Откройте http://localhost:3000
```

### Docker-образы

Мультиархитектурные образы (`linux/amd64`, `linux/arm64`) публикуются на Docker Hub при каждом релизе:

| Образ | Описание |
|-------|----------|
| `dbulashev/dasha-backend` | Go API-сервер |
| `dbulashev/dasha-frontend` | Nginx + Vue SPA, проксирует `/api/` на бэкенд |

Фронтенд принимает переменную окружения `BACKEND_URL` (по умолчанию: `backend:8000`).

### Helm Chart

Чарт публикуется как OCI-артефакт в GitHub Container Registry:

```bash
helm install dasha oci://ghcr.io/dbulashev/charts/dasha --version 0.1.5
```

#### Минимальная конфигурация (статические кластеры)

```yaml
config:
  clusters:
    - name: production
      username: monitoring_user
      password_from_env: PG_PASSWORD
      databases: [myapp]
      hosts: [pg-master.example.com]

secrets:
  existingSecret: my-pg-credentials  # должен содержать ключ PG_PASSWORD
```

#### С ESO (External Secrets Operator)

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

#### С сервис-дискавери Yandex MDB

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

#### Ingress с TLS (cert-manager)

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

cert-manager создаст ресурс `Certificate` в namespace приложения.

#### Ingress с TLS (cert-manager + reflector)

Когда ingress controller работает в другом namespace (например, Istio), используйте `reflectToNamespace` для копирования TLS-секрета через [reflector](https://github.com/emberstack/kubernetes-reflector):

```yaml
ingress:
  enabled: true
  className: istio
  domain: dasha.example.com
  tls:
    enabled: true
    certManager:
      enabled: true
      issuer: cluster-issuer
      reflectToNamespace: istio-ingress
```

В этом режиме отдельный ресурс `Certificate` не создаётся. Вместо этого на Ingress добавляются аннотации cert-manager, а сгенерированный TLS-секрет получает аннотации reflector для копирования в указанный namespace.

#### Режим только API (без фронтенда)

```yaml
frontend:
  enabled: false

ingress:
  enabled: true
  domain: dasha-api.example.com
```

#### Ключевые возможности чарта

- **Конфиг как ConfigMap** — `dasha.yaml` рендерится из values, пароли не хранятся в открытом виде
- **Пароли через env** — `password_from_env` + ESO или существующий Kubernetes Secret
- **Ключи сервисных аккаунтов** — отдельный `authorized_key.json` для каждого фолдера через ESO или существующий Secret
- **Фронтенд опционален** — можно развернуть только бэкенд для доступа через API
- **Ingress** — `/api/` маршрутизируется на бэкенд, `/` на фронтенд (когда включён), поддержка cert-manager + reflector
- **Безопасность** — `podSecurityContext`, `securityContext`, отдельные настройки для фронтенда и бэкенда

## CI/CD

- **CI** запускается при каждом push/PR в `main`: линтинг Go (revive + gosec), линтинг фронтенда (ESLint), юнит-тесты, интеграционные тесты (матрица PG 14–18), проверка сборки
- **Релиз** запускается по тегу `v*`: проверяет прохождение CI, собирает мультиархитектурные Docker-образы с attestation provenance/SBOM, сканирует Trivy, публикует Helm-чарт в GHCR
- **Dependabot** автоматически обновляет GitHub Actions

## История изменений

См. [CHANGELOG.ru.md](CHANGELOG.ru.md).

## Authors
* [Dmitry Bulashev](https://dbulashev.github.io/)

## Contributors

* [Mikhail Grigorev](https://github.com/cherts)
* [Ilya Lukyanov](mailto:lukyanov1985@gmail.com)
* [Roman Minebaev](https://github.com/minebaev)
* [Rustem Sagdeev](https://github.com/SagdeevRR)

## Лицензия

[GNU General Public License v3.0](LICENSE)
