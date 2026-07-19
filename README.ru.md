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
- **Снимки pgss**: сохранение снимков в отдельную БД хранилища, просмотр и расшаривание по URL
- **Сравнение снимков**: side-by-side сравнение двух снимков или снимка с live-данными, сортировка по любой метрике
- **Авто-снимки**: отдельный демон `dasha autosnapshot` автоматически создаёт снимки при всплесках активности (скользящее среднее по `pg_stat_activity`) или смене роли master↔replica; настройки per-cluster через UI, ретеншен по общему размеру
- **Снимки блокировок**: при срабатывании триггера всплеска снимок может дополнительно сохранять граф блокировок `pg_blocking_pids` — во время всплеска в фоне идёт подсчёт заблокированных сессий, затем серия проб оставляет самый жёсткий граф (с наибольшим числом различных заблокированных сессий); также доступно по запросу через ручной снимок «со блокировками»

**Анализ индексов**
- Топ-K по размеру, оценка bloat, обнаружение дубликатов
- B-tree по столбцам-массивам (выявление потенциальных ошибок)
- Невалидные / не готовые индексы
- Три алгоритма поиска похожих индексов
- Неиспользуемые индексы (кросс-хостовый анализ), статистика использования, cache hit rate
- **Горячие индексы**: какие индексы реально работают — дельта-снимки активности по расписанию (чтения / физический I/O), просуммированные по всем хостам кластера; естественное дополнение анализа неиспользуемых индексов

**Анализ таблиц**
- Топ-K по размеру с разбивкой TOAST (main, FSM, VM)
- Соотношение последовательного и индексного сканирования
- Cache hit rate, информация о партиционированных таблицах
- Кастомные параметры хранения (fillfactor, переопределения автовакуума)
- Подробное описание таблицы: колонки, индексы, ограничения, bloat, партиции, статистика вакуума с расчётными порогами, оценка размера строки и кандидатов в TOAST
- **Горячие таблицы**: что нагружает базу — дельта-снимки активности (чтения / записи / физический I/O) по cron-расписанию со всех хостов кластера с суммированием; точный top-N на класс метрик плюс гистограмма хвоста с coverage-долей (насколько топ репрезентативен), разбивка по хостам с бейджами мастер/реплика и перцентиль активности таблицы в describe-карточке. Требуется snapshot-хранилище.

**Анализ внешних ключей**
- Невалидные ограничения
- Несовпадение типов в столбцах FK
- Nullable-атрибуты FK
- Обнаружение похожих FK

**Обслуживание и вакуум**
- Autovacuum freeze max age, опасность wraparound transaction ID
- Мониторинг прогресса вакуума (PG 9.6+, расширено в PG 17+)
- Статистика вакуума/анализа по таблицам с учётом кастомных параметров
- **Сводка автовакуума**: сколько таблиц прямо сейчас превысило пороги срабатывания автовакуума/автоанализа (pie-chart, формула с учётом reloptions) плюс работающие процессы обслуживания

**Соединения и блокировки**
- Разбивка по состояниям и источникам соединений
- Детали активных сессий (`pg_stat_activity`)
- Wait events с группировкой по типу/событию
- Визуализация дерева блокировок

**Отслеживание прогресса**
- ANALYZE, VACUUM, CLUSTER / VACUUM FULL, CREATE INDEX, BASE BACKUP

**Анализ настроек**
- Обнаружение избыточного логирования
- Отклонения `from_collapse_limit` / `join_collapse_limit`
- Проверка `huge_pages`, алгоритмов сжатия TOAST/WAL
- Анализ соотношения чекпоинтов (`checkpoint_req` vs `checkpoint_timed`)
- Обзор конфигурации автовакуума и автоанализа

**Health Score**
- Композитная оценка инстанса 0–100 по восьми категориям (соединения, производительность, хранилище, репликация, обслуживание, горизонт, WAL/чекпоинты, блокировки) с непрерывными штрафными функциями — отдельная страница `/health-score` плюс индикатор на главной
- Параллельный движок правил с приоритизированными рекомендациями (severity, значения метрик, drill-down по базам), согласованными с оценкой
- Опциональный **режим на метриках**: при настроенном источнике Prometheus/VictoriaMetrics (`health_score.metrics`) оценка, рекомендации и тренд с сезонным базлайном и детекцией провалов считаются по временным рядам (pgSCV, Yandex MDB, pgbouncer, метрики хоста) вместо точечного SQL; SQL-снимок остаётся fallback'ом без настройки
- Детали модели скоринга: [README-health-score.ru.md](README-health-score.ru.md)

**Поиск по логам (Yandex Cloud)**
- Поиск по логам PostgreSQL и пулера соединений (Odyssey) кластеров из Yandex MDB discovery через MDB API — отдельная страница `/logs`, без агентов и пересылки логов
- Нативные фильтры severity/host плюс фильтрация на стороне Dasha: подстроки сообщения (AND), исключения в стиле `grep -v`, фильтры по базе/пользователю; курсорная пагинация и частичные результаты при таймауте
- Опциональная дедупликация группирует почти одинаковые сообщения по маскированному шаблону (плейсхолдеры `<*>`) с количеством и first/last seen
- Гистограмма частоты (время × severity) с зумом по клику/выделению, пресеты в один клик (дедлоки, автовакуум, чекпоинты, …), фильтры в URL для шаринга, обмен периодом с Grafana через буфер обмена
- Per-user rate limiting защищает квоту Yandex Cloud API (`log_search.rate_limit`, отдельный лимит для админов)

**Аутентификация и авторизация**
- Три режима: `none` (открытый), `token` (статические API-ключи), `oidc` (OpenID Connect)
- OIDC: BFF-паттерн с зашифрованными session cookies (Keycloak, Google, любой OIDC-провайдер)
- Ролевой доступ (RBAC) через Casbin: `admin` (полный доступ) и `viewer` (только чтение)
- Per-identity rate limiting (token bucket): по пользователю, сессионной cookie или IP клиента
- API-ключи с constant-time сравнением, настраиваемые роли для каждого ключа
- Безопасное управление сессиями: HttpOnly/Secure/SameSite cookies, шифрование AES-256, подпись HMAC-SHA256
- CSRF-защита через OAuth2 state с constant-time валидацией
- Отзыв refresh token при logout (RFC 7009, при поддержке провайдером)
- Персональные токены (PAT): API-токены, выпускаемые самим пользователем для скриптов и MCP-коннектора — хранятся хэшированными, least-privilege, одноразовый показ секрета, отзыв и опциональный срок действия
- Администрирование токенов (админ): просмотр и отзыв токенов **всех** пользователей, а также справочник пользователей с первым и последним входом — и то и другое требует интерактивной OIDC-сессии администратора, поэтому админский PAT не может отозвать токены, которые придут ему на смену

**Инфраструктура**
- Поддержка множества кластеров с выбором хоста/базы для каждого
- Сервис-дискавери Yandex Managed Service for PostgreSQL
- Отображение роли хоста (primary / replica)
- Опциональная БД хранилища снимков (секционированные таблицы, CLI `dasha migrate`)
- [MCP-коннектор](#mcp-коннектор-dasha-mcp) (`dasha-mcp`): read-only MCP-сервер с диагностикой флота для AI-ассистентов (22 tools, 5 prompts)

**Пользовательские настройки** (диалог настроек — шестерёнка в меню пользователя)
- Язык интерфейса: английский, русский, немецкий — при незаданном определяется по браузеру, меняется на лету, сохраняется локально (непереведённые ключи откатываются на английский)
- Тема: системная (следует за ОС), светлая или тёмная
- Часовой пояс для всех отображаемых времён: локальный, UTC или любой из одиннадцати российских (списком по IANA-идентификаторам — тем же, которыми оперируют GUC `timezone` и логи сервера); фиксированные пояса помечаются прямо во времени (`… GMT+3`), а оси графиков следуют той же настройке, что и таблицы
- Строк на странице (10–100) — это серверный `limit`, поэтому настройка влияет и на объём запроса каждой таблицы

## Архитектура

```mermaid
flowchart LR
    SPA["Vue 3 SPA (Vuetify)<br/>:3000"]
    LLM["AI-ассистент<br/>(MCP-клиент)"]
    MCP["dasha-mcp<br/>MCP-сервер :8765"]
    BE["Go-бэкенд (Echo)<br/>:8000"]
    AS["Демон<br/>dasha autosnapshot"]
    PG[("Кластеры PostgreSQL<br/>14 – 18")]
    ST[("Хранилище снимков<br/>pgss, PAT, веса HS")]
    TS[("Prometheus /<br/>VictoriaMetrics")]
    YC["Yandex Cloud API<br/>MDB discovery · логи"]

    SPA -->|/api| BE
    LLM -->|stdio / HTTP| MCP
    MCP -->|REST + X-API-Key| BE
    BE --> PG
    BE --> ST
    BE -.->|health score<br/>на метриках| TS
    BE -.->|discovery,<br/>поиск по логам| YC
    AS --> PG
    AS --> ST
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

#### Поиск по логам (опционально)

Для кластеров из Yandex MDB discovery страница `/logs` работает из коробки (переиспользуется ключ сервисного аккаунта discovery). Глобальный блок `log_search` только настраивает лимиты:

```yaml
log_search:
  max_scan: 5000          # максимум просканированных записей за поиск
  max_page_size: 1000     # верхняя граница page_size
  timeout_seconds: 30     # таймаут чтения из Yandex API
  rate_limit:             # на пользователя (на IP для анонимных); rps <= 0 отключает
    requests_per_second: 0.0333   # 1 запрос в 30с
    burst: 10
  admin_rate_limit:
    requests_per_second: 0.2      # 1 запрос в 5с
    burst: 20
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

#### Хранилище снимков (опционально)

Для включения снимков pgss настройте отдельную базу данных PostgreSQL:

```yaml
storage:
  dsn: "postgres://dasha:secret@localhost:5432/dasha_storage?sslmode=require"
  # dsn_from_env: DASHA_STORAGE_DSN  # альтернатива: читать из переменной окружения
```

Выполните `dasha migrate` для создания секционированных таблиц перед первым использованием.

#### Автоматические снимки (опционально)

Когда настроено хранилище снимков, можно запустить отдельный демон, который создаёт снимки автоматически по настраиваемым триггерам:

```bash
dasha autosnapshot
```

Демон использует тот же `dasha.yaml`. Все настройки (триггеры, пороги, ретеншен) хранятся в storage DB и редактируются из UI (меню *Авто-снимки*, только admin). По умолчанию запускается один экземпляр; для HA в нескольких репликах включите leader election на advisory-локе через `storage.leader_election: true` (по умолчанию выключено, т.к. session-level advisory lock требует отдельного долгоживущего соединения и несовместим с транзакционным пулингом, например PgBouncer в transaction mode).

По триггеру всплеска активности демон может дополнительно сохранять граф блокировок (`capture_locks`, включено по умолчанию): во время всплеска в фоне работает дешёвый счётчик заблокированных сессий, а в момент триггера серия проб (`lock_probe_count` × `lock_probe_interval`, по умолчанию 5 × 500 мс) снимает полный граф `pg_blocking_pids` и оставляет пробу с наибольшим числом различных заблокированных сессий. Результат сохраняется в `snapshots.locks_data` и доступен из просмотра снимка в *Статистике запросов*.

Триггеры:
- **activity_spike** — срабатывает, когда `count(state='active')` в `pg_stat_activity` превышает baseline (скользящее среднее) на заданный процент (по умолчанию +50%) в течение заданного времени (по умолчанию 5 минут)
- **role_change** — срабатывает при смене роли master↔replica (направление: `both` / `master_to_replica` / `replica_to_master`)

Ретеншен удаляет старые «тройки дней», пока общий размер превышает `retention_bytes`, уважая минимальный порог `retention_min_days`.

#### Персональные API-токены (опционально)

Залогиненный пользователь может выпускать **персональные API-токены (PAT)** — bearer-секреты, передаваемые в заголовке `X-API-Key`, — чтобы не-браузерные клиенты (`dasha-mcp`, скрипты) работали с его личностью и ролью (RBAC сохраняется). Требуется хранилище снимков: токены хранятся в виде хеша в `api_tokens`, поэтому сначала выполните `dasha migrate`.

**Режим auth должен быть `oidc`.** Выпуск требует индивидуально идентифицируемой личности, поэтому он запрещён из-под статического config-токена (он общий и не несёт персональной идентичности — иначе утёкший токен мог бы выпустить токены, которые переживут его удаление из конфига) и из-под другого PAT (anti-chaining). Кто может выпускать, дополнительно задаёт `auth.pat_min_role`: `admin` (по умолчанию, пока фича в обкатке) или `viewer` (любой залогиненный пользователь).

- **Выпуск через UI**: меню пользователя → шестерёнка (*Настройки*) → *Мои токены* → создать (имя, роль ≤ своей, опциональный срок). Полный секрет показывается **один раз**.
- **Использование из любого клиента:**

  ```bash
  curl -H "X-API-Key: dasha_pat_…" http://localhost:8000/api/clusters
  ```

Список своих токенов — `GET /api/auth/tokens` (без секретов); отзыв — `DELETE /api/auth/tokens/{id}` (немедленно). Запрашиваемая роль не выше своей (по умолчанию `viewer`); `expires_in_days` опционально (0 / нет = бессрочно). Оба списка принимают `?include_revoked=true` — отозванный токен остаётся аудит-записью, но аутентифицироваться им уже нельзя.

**Ограничения срока жизни.** Роль токена «застывает» в момент выпуска: при аутентификации читается роль, записанная в самом токене, а не текущая роль в IdP, — поэтому токен продолжает работать после понижения или увольнения владельца. Два ограничения сужают это окно, не полагаясь на то, что кто-то вспомнит про отзыв:

- **Admin-токены истекают в пределах 30 дней** — в том числе когда `expires_in_days` не передан (иначе лимит обходился бы простым отсутствием срока). Слишком длинный запрос зажимается, а не отклоняется; фактическое значение — в `expires_at` ответа.
- **Любой токен, не использовавшийся 90 дней, отзывается автоматически.** Простой считается от последнего использования, а для ни разу не использованного токена — от создания, поэтому выпущенный и забытый токен не живёт вечно. Порог срабатывает ровно в момент пересечения (проверяется при аутентификации), а фоновая зачистка проставляет `revoked_at`, чтобы токен отображался отозванным, а не выглядел живым, молча перестав работать.

Ни то, ни другое не заменяет отзыв: при увольнении токены нужно отозвать (*Настройки* → *Все токены*) — порог простоя гасит лишь то, чем никто не пользуется.

**Администрирование (только админ).** Администратор видит и отзывает токены всех пользователей и смотрит, у кого есть доступ — *Настройки* → *Все токены* / *Пользователи*:

```bash
curl -H "Cookie: <oidc-сессия>" http://localhost:8000/api/auth/admin/tokens        # токены всех владельцев
curl -H "Cookie: <oidc-сессия>" -X DELETE .../api/auth/admin/tokens/{id}           # отозвать любой из них
curl -H "Cookie: <oidc-сессия>" http://localhost:8000/api/auth/admin/users          # кто и когда входил
```

Справочник пользователей наполняется входами через SSO (таблица `users`, тоже создаётся `dasha migrate`): запись заводится при первом входе, `last_login_at` обновляется при каждом. Показанные там роли приходят от провайдера и являются аудит-следом, а не источником авторизации. Как и выпуск, эти эндпоинты требуют **интерактивной OIDC-сессии администратора** — админский PAT отклоняется, поэтому утёкший токен не сможет перечислить или отозвать токены, которые придут ему на смену.

### Локальный запуск

```bash
# Бэкенд (API на :8000)
make run-backend

# Фронтенд (dev-сервер на :5173, проксирует /api на :8000)
make run-frontend

# MCP-сервер (HTTP/SSE на :8765, к бэкенду на :8000)
make run-mcp
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
- **БД хранилища**: хранилище снимков с автоматической миграцией при запуске
- **Генератор нагрузки**: непрерывная фоновая нагрузка для реалистичных данных

## MCP-коннектор (dasha-mcp)

`dasha-mcp` — отдельный **read-only** [MCP](https://modelcontextprotocol.io)-сервер поверх Dasha API. Позволяет AI-ассистентам запрашивать диагностику флота PostgreSQL как tools/prompts, прокидывая токен каждого вызывающего в Dasha (RBAC сохраняется). Подходит любой MCP-совместимый клиент — Claude Desktop, Claude Code, Cursor, Continue, **opencode** и т.д.

- **Tools (26):** `list_clusters`, `fleet_health`, `get_instance_info`, `get_health_score`, `get_health_recommendations`, `health_details` (превращает рекомендацию в цель: передайте её `rule_id` как `detail` — вернутся сами таблицы, базы или сессии; потабличным drill-down нужна ещё `database`, инстанс-уровневым — нет), `health_trend`, `health_databases`, `top_queries` (по времени/WAL), `query_report`, `list_snapshots`, `query_compare`, `running_queries`, `blocked_queries`, `list_indexes` (missing/unused/usage), `unused_index_report` (вердикт по всему кластеру: можно ли удалить индекс — счётчик сканов взвешивается по всем хостам и по окну статистики, т.к. `idx_scan` не реплицируется, а счётчик без окна ничего не значит), `top_tables`, `hot_tables` / `hot_indexes` (топ горячих объектов по классам метрик — чтения/записи/io — из суточных дельта-снимков, просуммированных по всем хостам кластера, с coverage-долей репрезентативности топа; требует snapshot-хранилище), `describe_table`, `get_replication`, `settings_analyze`, `wait_events`, `connections`, `vacuum_danger`, `search_logs` (логи PostgreSQL/пулера из Yandex Cloud; только для кластеров из Yandex MDB discovery, с per-user rate limit). Все помечены **read-only** и closed-world, чтобы совместимые клиенты показывали (и авто-аппрувили) их как безопасные. Сервер также отдаёт **инструкции** по использованию, которые подсказывают модели, какой tool/prompt выбрать.
- **Prompts (5):** `diagnose_cluster`, `explain_health_score`, `find_index_opportunities`, `investigate_slow_queries`, `fleet_overview` — линейные плейбуки: нумерованные шаги, один tool на шаг, с критерием трактовки на каждом (рассчитаны на модели без глубокой экспертизы PostgreSQL; сильные модели просто проходят их быстрее).
- **Resources (3):** встроенная база знаний, которую модель читает по запросу — `dasha://kb/health-rules` (каждое правило health score с порогами LOW/MED/HIGH и первыми действиями), `dasha://kb/wait-events` (глоссарий wait events), `dasha://kb/workflow` (сценарии «жалоба → цепочка инструментов» и правила бережности к API).
- **Язык:** `--lang en|ru` (или `DASHA_MCP_LANG`) выбирает язык базы знаний, плейбуков и инструкций; имена tools, схемы и результаты остаются английскими.

**Предусловие:** токен Dasha API — [персональный токен](#персональные-api-токены-опционально) (`dasha_pat_…`) или статический config-токен. Он определяет роль (`viewer` достаточно).

### Сборка

```bash
cd backend && go build -o dasha-mcp ./cmd/dasha-mcp
# либо образ:
docker build -f deploy/images/Dockerfile.mcp -t dasha-mcp .
```

### stdio (локально — Claude Desktop / Claude Code / opencode / Cursor)

Клиент сам запускает бинарь и общается через stdin/stdout; токен — переменная окружения `DASHA_MCP_TOKEN`.

**Claude Desktop** (`claude_desktop_config.json`) или **Cursor** (`.cursor/mcp.json`):

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

**opencode** (`opencode.json` или `~/.config/opencode/opencode.json`):

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

### HTTP/SSE (общий / мультипользовательский)

Запускается как сервис; **каждый запрос несёт свой токен** (общего серверного токена нет), поэтому RBAC сохраняется поперсонально:

```bash
dasha-mcp --http :8765 --dasha-url http://dasha-backend:8000
# контейнер:
docker run -p 8765:8765 dasha-mcp --http :8765 --dasha-url http://dasha-backend:8000
```

Удалённый MCP-клиент указывает `http://<host>:8765` и шлёт токен в `Authorization: Bearer dasha_pat_…` (или `X-API-Key`). Например, **opencode**:

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

Сервер read-only (мутирующих эндпоинтов нет) и работает под non-root. Хардненинг: размер ответа tool ограничен (слишком большой результат отклоняется с подсказкой сузить запрос, а не режется в невалидный JSON); кэш серверов по токену хэширован и ограничен; токены не логируются. В общем HTTP-развёртывании ставьте за TLS; rate-limit обеспечивает вышестоящий per-identity лимитер Dasha (каждый PAT — отдельная личность), поэтому он действует и через passthrough.

### Несколько экземпляров Dasha (окружения)

Каждое окружение (dev / stage / prod) — со своей Dasha и своим `dasha-mcp`. Регистрируйте их как отдельные MCP-серверы на клиенте — имя сервера играет роль неймспейса (tools, prompts и ресурсы `dasha://kb/*` клиент различает по серверу-источнику, URI не конфликтуют):

```json
"mcpServers": {
  "dasha-dev":  { "command": "dasha-mcp", "args": ["--dasha-url", "https://dasha.dev.example.com"],  "env": { "DASHA_MCP_TOKEN": "dasha_pat_…" } },
  "dasha-prod": { "command": "dasha-mcp", "args": ["--dasha-url", "https://dasha.prod.example.com"], "env": { "DASHA_MCP_TOKEN": "dasha_pat_…" } }
}
```

Персональные токены привязаны к экземпляру: PAT, выпущенный на dev, не действует на prod.

### Kubernetes (Helm)

В чарте есть опциональные, выключенные по умолчанию Deployment + Service для MCP (HTTP-режим). При включении сервер автоматически подключается к in-cluster бэкенду:

```yaml
# values.yaml
mcp:
  enabled: true
  port: 8765
  # dashaUrl: ""   # пусто = in-cluster Service {release}-backend
  # lang: ru       # язык базы знаний / плейбуков (по умолчанию en)
```

HTTP-режим — строгий per-user passthrough: чарт намеренно не предлагает общий fallback-токен — каждый клиент присылает собственный credential в каждом запросе, RBAC и аудит остаются per-user.

Создаются `{release}-mcp` Deployment + `ClusterIP` Service на порту `8765`. Чтобы открыть наружу — поставьте перед Service свой Ingress/Gateway (TLS терминируется там), а клиенты шлют `Authorization: Bearer dasha_pat_…` в каждом запросе.

## Разработка

### Структура проекта

```
├── doc/swagger.yaml              # Спецификация OpenAPI 3.0 (источник истины)
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
│   │   ├── discovery/            # Сервис-дискавери (Yandex MDB)
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
| `dbulashev/dasha-mcp` | MCP-коннектор для AI-ассистентов (stdio / HTTP) |

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

#### Gateway API с TLS (cert-manager)

Портативная альтернатива Ingress — работает с любой реализацией Gateway API (Istio, NGINX Gateway Fabric, Envoy Gateway, Cilium):

```yaml
gatewayAPI:
  enabled: true
  gatewayClassName: istio
  hostname: dasha.example.com
  # Если Gateway живёт в namespace контроллера (например, istio-system),
  # задайте gatewayNamespace — Certificate создаётся в том же namespace.
  # gatewayNamespace: istio-system
  tls:
    enabled: true
    certManager:
      enabled: true
      issuer: cluster-issuer
```

`Certificate` от cert-manager создаётся в namespace Gateway (`gatewayNamespace`, по умолчанию — release namespace). Cross-namespace ссылки на secret потребовали бы `ReferenceGrant`, который чарт не рендерит — поэтому Certificate и Gateway держим в одном namespace.

Рендеримые ресурсы (все условны от `gatewayAPI.enabled: true`):
- `Gateway` — HTTP-listener всегда; HTTPS-listener только при `gatewayAPI.tls.enabled: true`.
- `HTTPRoute` (основной) — привязан к HTTPS-listener при `tls.enabled`, иначе к HTTP-listener.
- `HTTPRoute` (редирект HTTP→HTTPS, filter `RequestRedirect`) — только при `gatewayAPI.tls.enabled && gatewayAPI.tls.redirect`.
- `Certificate` (cert-manager) — только при `gatewayAPI.tls.certManager.enabled`.

`ingress.enabled` и `gatewayAPI.enabled` взаимоисключаются — `helm template` падает, если оба true.

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
- **Ingress / Gateway API** — одно правило `/` на фронтенд (который проксирует `/api/` и `/auth/` на бэкенд); авто-редирект HTTP→HTTPS при включённом TLS; поддержка cert-manager; взаимоисключающий `gatewayAPI.enabled` для K8s Gateway API (`gateway.networking.k8s.io/v1`)
- **Безопасность** — `podSecurityContext`, `securityContext`, отдельные настройки для фронтенда и бэкенда

## CI/CD

- **CI** запускается при каждом push/PR в `main`: линтинг Go (revive + gosec), линтинг фронтенда (ESLint), юнит-тесты, интеграционные тесты (матрица PG 14–18), проверки уязвимостей `govulncheck` + `npm audit`, Trivy-скан зависимостей/IaC, линтинг Helm, проверка сборки
- **CodeQL** (Go + TypeScript, `security-extended`) на push, PR и еженедельно по расписанию
- **Релиз** запускается по тегу `v*`: проверяет прохождение CI, собирает мультиархитектурные Docker-образы (backend, frontend, MCP) с attestation provenance/SBOM, гейтит их Trivy-сканом, публикует Helm-чарт в GHCR
- **Dependabot** автоматически обновляет Go-модули, npm-пакеты, базовые Docker-образы и GitHub Actions

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
