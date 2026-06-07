# История изменений

## v1.2.0

### Новые возможности
- **Автоматические снимки pg_stat_statements**: отдельный демон `dasha autosnapshot` создаёт снимки pgss по настраиваемым триггерам
  - **Триггер всплеска активности** — скользящее среднее `count(state='active')` из `pg_stat_activity`; срабатывает, когда текущее значение превышает baseline на заданный процент (по умолчанию +50%) в течение заданного времени (по умолчанию 5 минут)
  - **Триггер смены роли** — определяет переход master↔replica через `pg_is_in_recovery()`, с настраиваемым направлением (`both` / `master_to_replica` / `replica_to_master`)
  - **Триггер спада активности** (`recovery_duration`) — после всплеска делает снимок, когда активность держится ниже порога достаточно долго; вместе с reset даёт два чистых pgss-окна на инцидент (разгон и само выполнение всплеска). `0s` — выключено
  - **Отложенный снимок** (`deferred_interval`) — после снимка всплеска ставит follow-up в персистентную очередь (`autosnapshot_pending`, переживает рестарт демона) — страховка для всплесков, которые **не отпускают**. **Автоматически отменяется при спаде** (снимок спада уже зафиксировал инцидент, иначе отложенный снял бы просто тихое окно после). `0s` — выключено. Оба настраиваются per-cluster
  - **Глобальные настройки** (хранятся в storage DB, правятся через UI): `poll_interval`, `max_snapshot_frequency` (debounce), `min_baseline_active` (пропуск при низкой нагрузке), `retention_bytes`, `retention_min_days`, `reset_query_stats` (reset `pg_stat_statements` после каждого авто-снимка — независимо от ручного флага UI)
  - **Кастомная функция reset pgss** (`pgss_reset_function` в `dasha.yaml`): вызывать обёртку с указанием схемы вместо `pg_stat_statements_reset()`, когда у роли мониторинга нет `EXECUTE` (по аналогии с `pg_stats_view`); применяется и к авто-, и к ручному сбросу
  - **Per-cluster оверрайды**: deep-merge поверх глобальных настроек; отдельный кластер может включать/выключать триггеры, настраивать пороги или полностью отключать auto-snapshots
  - **Leader election** (опционально, `storage.leader_election`, по умолчанию выключено): `pg_try_advisory_lock` на storage DB позволяет запускать демон в нескольких репликах для HA; по умолчанию выключено, т.к. session-level advisory lock требует отдельного соединения и несовместим с транзакционным пулингом (PgBouncer transaction mode)
  - **Ретеншен по общему размеру**: удаляет старые «тройки дней» (секции snapshots + query_texts + trigger_events), пока суммарный размер превышает `retention_bytes`; уважает минимальный порог `retention_min_days`
  - **Вкладка истории**: фильтры по кластеру / outcome / типу триггера, постраничный вывод; в историю пишутся только создания снимков и ошибки — временные пропуски (debounce, ниже baseline, неверное направление) логируются на уровне debug и не сохраняются, чтобы не зашумлять историю
  - **UI**: новый пункт меню «Авто-снимки» (`mdi-camera-timer`) с вкладками Настройки + История; редактирование только admin, viewer видит read-only; для non-admin пункт скрыт, если фича выключена
  - **API**: `GET/PUT /api/autosnapshot/config`, `GET/PUT /api/autosnapshot/clusters/{name}`, `GET /api/autosnapshot/status`, `GET /api/autosnapshot/trigger-events`
  - **CLI**: `dasha autosnapshot` (отдельная команда, не стартует вместе с `dasha serve`)
  - **Деплой**: в Helm чарт добавлен отдельный Deployment `autosnapshot` (по умолчанию отключен, включается флагом `autosnapshot.enabled: true`); в docker-compose добавлен сервис `autosnapshot` рядом с `backend` и `frontend`
- **Тюнинг пула соединений к БД** (`dasha.yaml`): пулы к мониторимым кластерам теперь настраиваются — `db_pool` (`max_conns`, `max_conn_idle_time`, `max_conn_lifetime`; 0 = дефолт pgx) для `dasha serve`, и `autosnapshot_db_pool` (override по полям) для демона `dasha autosnapshot`. Позволяет демону быстро освобождать соединения мониторинг-роли (например `max_conn_idle_time: 5s`) при жёстком лимите соединений. Пул к storage-БД по-прежнему тюнится через query-параметры `storage.dsn` (`pool_max_conns`, `pool_max_conn_idle_time`, ...)
- **Снимки блокировок по триггеру**: при срабатывании триггера всплеска активности снимок может дополнительно сохранять граф блокировок вместе с `pg_stat_statements`
  - **Гибридный тайминг**: во время всплеска в фоне идёт дешёвый подсчёт заблокированных сессий и фиксируется `background_peak`; в момент триггера серия из N детальных проб (по умолчанию 5 × 500 мс) снимает полный граф `pg_blocking_pids`
  - **Берётся самая жёсткая проба**: сохраняется проба с наибольшим числом различных заблокированных сессий (при равенстве — с максимальным временем ожидания); до 100 строк, отсортированных по убыванию ожидания
  - **Хранилище**: новая колонка `snapshots.locks_data jsonb`; `GET /api/queries/snapshot/{id}/locks` отдаёт её; в списке снимков появился флаг `has_locks`
  - **Настройки** (глобальные + per-cluster): `capture_locks` (по умолчанию вкл.), `lock_probe_count` (1–20), `lock_probe_interval` (100 мс–5 с)
  - **Ручной захват**: опция «со снимком блокировок» у кнопки ручного снимка; сохранённый граф блокировок виден из просмотра снимка в Статистике запросов

## v1.1.0

### Фичи
- **Health Score (новая верхнеуровневая страница `/health-score`):** композитная метрика 0–100 по восьми категориям — `connections`, `performance`, `storage`, `replication`, `maintenance`, `horizon`, `wal_checkpoint`, `locks` — с непрерывными штрафными функциями и параллельным движком правил.
  - 30 правил по восьми категориям.
  - **Drill down (детализация по базам):** `GET /api/health-score/databases` возвращает `DatabaseScore` для каждой БД с пересчётом правил в database-scope. Таблица «Базы данных» на `/health-score` становится фильтром — клик по строке закрепляет базу как контекст для списка рекомендаций. Instance-only категории (`connections / replication / horizon / wal_checkpoint / locks`) в DB-scope скрываются. Подсветка худшей БД через `WorstDatabase`.
  - **Авто-отбрасывание категорий без сигнала** с пропорциональным перераспределением веса на остальные, чтобы счёт не искажался: `replication` отбрасывается, если у инстанса нет реплик; `maintenance` отбрасывается на standby (`pg_is_in_recovery() = true`) — там не работают autovacuum / ANALYZE, и метрики отражали бы состояние мастера.
  - **Inline-таблицы данных для actionable-правил** — пять типизированных эндпойнтов под `/api/common/health-score/details/*` (по одному на правило, штатная 2-уровневая структура `internal/query/sql/common/health_score_*`): `high-dead-ratio-tables`, `low-hot-update-tables`, `tables-autovacuum-off`, `xid-wraparound-databases`, `horizon-blocking-sessions`. Карточки рекомендаций показывают сами проблемные строки вместо непрозрачного SQL-сниппета; правила, для которых соответствующая страница Dasha (`/locks`, `/tables` и т.д.) уже показывает данные, оставлены со ссылкой.
  - **Раскрывающийся блок «Как устроен health-score»** под `/health-score`: формула, веса категорий, точки перелома штрафных функций (с пояснением термина — это значение метрики, в котором меняется крутизна), все 30 правил с описанием и порогами LOW/MEDIUM/HIGH, drill down (детализация).
  - Карточки рекомендаций показывают долевые метрики в процентах (`HOT-ratio = 37.3%` вместо `0.3733230416811912`) через производное поле `metric_pct` в контексте `vue-i18n`.

### UX
- **Перекомпоновка главной страницы** (`/main`):
  - Карточка Health Score — основная сверху; в неё встроены чипы per-database сигналов из бывшего блока «Здоровье БД»: `Conflicts`, `ChecksumFailures`, `RollbackRatio`, `Stats since`. Отдельный компонент `DatabaseHealthSection` удалён.
  - `CacheHitRates` и `WaitEvents` объединены в одну `v-row` (50/50 на `md+`).
- **Учёт standby:**
  - Пункт меню `Maintenance` скрыт, когда текущий хост — реплика. Переход на `/maintenance/<cluster>` на реплике автоматически редиректит на `/main` через watcher на `pg_is_in_recovery()`.
  - `DescribeVacuumStatsSection` на `/table-describe` на standby пропускает запрос к API и показывает подсказку «смотрите на мастере» — `pg_stat_user_tables.last_*vacuum` на реплике локальный и не отражает реальную работу autovacuum.
  - Новый Pinia-store `instanceInfo` с ключом `cluster::host` (TTL 30s, дедупликация запросов) переиспользуется в меню, watcher'е редиректа и vacuum-stats секции вместо повторных вызовов `/api/common/instance-info`.
- **`InstanceInfo` теперь несёт `in_recovery`** сквозь весь стек (swagger → `Result.InRecovery` → ответ `GET /api/health-score`).
- **Пороги severity пересмотрены** у семи правил: `idle_in_transaction` (2/5/10 вместо 1/2/5), `low_cache_hit_ratio` (95/90/85 вместо 99/95/90), `high_max_dead_ratio` (10/20/30 вместо 10/20/50), `high_avg_dead_ratio` (5/15/25 вместо 5/10/25), `stale_vacuum` (7/21/60 дней вместо 2/7/14), `requested_checkpoint_ratio` (5/10/20% вместо 5/10/30%), `low_hot_update_ratio` (<0.80/<0.65/<0.50 вместо <0.80/<0.60/<0.30). Пороги lock-правил ужесточены: `active_lock_waiters` (1/3/10), `ungranted_locks` (2/5/15), `deadlocks_rate` сведён к LOW при total > 0 (без per-day нормализации MED/HIGH не имеют смысла, так как счётчик накопительный с момента `pg_stat_database_reset`).

### Производительность
- **Сжатие ответов nginx** (`gzip` и Brotli при наличии модуля в сборке) включено в `deploy/images/nginx.conf.template` и `demo/nginx.conf` для JS/CSS/JSON/SVG/text. Выигрыш и на статике, и на API-ответах; особенно помогает на cold-cache загрузке `/health-score`.

### Документация
- Новые верхнеуровневые `README-health-score.md` и `README-health-score.ru.md` описывают формулу, веса, точки перелома штрафов, все 31 правило с порогами и что меряет каждая метрика, семантику drop'а replication/maintenance и поведение drill down (детализации) по базам.

## v1.0.2

### Багфиксы
- **Устаревший SPA-bundle после апгрейда**: frontend nginx теперь отдаёт `index.html`/SPA-маршруты с `Cache-Control: no-cache`, а хэшированные `/assets/` — с `immutable` + `expires 1y`. Правки в `deploy/images/nginx.conf.template` и `demo/nginx.conf`. Сам переход v1.0.1 → v1.0.2 ещё требует разовой очистки кэша, последующие апгрейды будут чистыми.

## v1.0.1

### Багфиксы
- **Пороги срабатывания autovacuum / autoanalyze в `describe_vacuum_stats`**: колонки `vacuum_threshold`, `analyze_threshold` и `insert_vacuum_threshold` в эндпоинте описания таблицы считались относительно `pg_stat_user_tables.n_live_tup` вместо `pg_class.reltuples`. Сам PostgreSQL при принятии решения о запуске autovacuum/autoanalyze использует `reltuples`, поэтому отображаемые пороги расходились с реальным поведением сервера — особенно сразу после массовых INSERT/DELETE, когда два счётчика идут вразнобой вплоть до следующего ANALYZE. SQL теперь читает `GREATEST(c.reltuples, 0)::bigint` (специальное значение PG ≥14 «нет статистики» `-1` нормализуется к `0`) и подставляет это значение во все три формулы порога. Подсказки в `frontend/src/locales/{ru_RU,de_DE}.json` (`vacuumThreshold`, `analyzeThreshold`, `insertVacThreshold`) теперь корректно ссылаются на `pg_class.reltuples`.

### Зависимости (Dependabot bumps с момента v1.0.0)
- **Бэкенд (Go):** `oapi-codegen/runtime` `v1.4.0` → `v1.4.1`, `yandex-cloud/go-genproto` `v0.80.0` → `v0.82.0`.
- **Фронтенд (npm):** `vue-i18n` `^11.1.12` → `^11.4.4` (требование Node engine поднято upstream до `>= 22` — уже выполняется build-образом `node:26-alpine`), `eslint-plugin-playwright` `^2.10.2` → `^2.10.4` (dev), `vite-plugin-vue-devtools` `^8.0.3` → `^8.1.2` (dev, добавлен peer для Vite 8).

## v1.0.0

### Breaking changes
- **Helm chart:** `ingress.tls.certManager.reflectToNamespace` удалён. Интеграция с reflector (emberstack `kubernetes-reflector`) больше не рендерится — если нужна, добавляйте аннотации руками через `ingress.annotations`. (`ingress.tls.certNamespace` **сохранён** — нужен пользователям, у которых ingress controller живёт в отдельном namespace, например Istio.)
- **Helm chart:** маршрутизация ingress/gateway упрощена. При `frontend.enabled: true` (дефолт) рендерится одно правило `/` — frontend nginx сам проксирует `/api/` и `/auth/`. Прежнего отдельного правила `/api/` в Ingress больше нет. Headless-деплой (`frontend.enabled: false`) сохраняет прямые правила `/api/` и `/auth/` на backend.

### Безопасность
- **Бэкенд: проверка HTTPS:**
  - `auth.NewMiddlewares` пишет один zap-WARNING при инициализации, если `auth.mode != none && !require_https` — подсвечивает случай, когда credentials передаются открытым текстом. Unit-тесты добавлены в `backend/internal/auth/auth_test.go` (4 кейса).
  - Helm-шаблон `configmap.yaml` авто-инжектит `auth.require_https: true` в рендеримый `dasha.yaml` при `auth.mode != none && tls.enabled` (через новый helper `dasha.tlsEnabled` — OR `ingress.tls.enabled` и `gatewayAPI.tls.enabled`). Явное `auth.require_https: false` в values сохраняется как escape hatch.
  - Frontend nginx сохраняет оригинальный `X-Forwarded-Proto` через `map`-блок (`/etc/nginx/conf.d/00-proto-map.conf`: `$http_x_forwarded_proto → $forwarded_proto`, fallback на `$scheme` при отсутствии заголовка). Оба `proxy_pass` (`/api/`, `/auth/`) теперь используют `$forwarded_proto`. Раньше `$scheme` переписывал заголовок на внутрикластерный `http` и тихо ломал `require_https`.
- **Hardening контейнеров:**
  - `Dockerfile.backend` и `Dockerfile.frontend` работают от non-root (`USER dasha` / `USER nginx`). В nginx main-конфиг внесена правка: убрана директива `user nginx;` и pid перенесён в `/tmp/nginx.pid`, чтобы процесс стартовал без root.
  - Дефолтный container-level `securityContext` в Helm: `allowPrivilegeEscalation: false`, `readOnlyRootFilesystem: true`, `capabilities.drop: [ALL]`, `seccompProfile.type: RuntimeDefault`. Pod-level `runAsNonRoot: true` + `runAsUser/Group/fsGroup` (1000 для backend, 101 для frontend nginx). `emptyDir` mount'ы для `/tmp` (backend) и `/var/cache/nginx`, `/etc/nginx/conf.d`, `/tmp` (frontend) — чтобы ROFS работал.
- **Bump Go-зависимостей по находкам `trivy fs`:** `pgx/v5` `v5.7.6` → `v5.9.0` (CRITICAL memory-safety, CVE-2026-33816), `go-jose/v4` `v4.1.3` → `v4.1.4` (HIGH DoS через специально подготовленный JWE, CVE-2026-34986), `golang-jwt/v4` `v4.5.1` → `v4.5.2` (HIGH memory allocation в разборе header, CVE-2025-30204), `grpc` `v1.79.2` → `v1.79.3` (HIGH HTTP/2 path validation auth bypass, CVE-2026-33186). `CVE-2026-34040` в `docker/docker` (транзитивно через `testcontainers-go`, server-side баг, клиентом не задействован) — в `.trivyignore` с пояснением.

### Helm
- **Defense-in-depth редирект HTTP→HTTPS** на трёх уровнях при `tls.enabled`:
  - Ingress: аннотации `nginx.ingress.kubernetes.io/ssl-redirect` и `force-ssl-redirect` авто-добавляются.
  - Gateway API: отдельный `HTTPRoute` с filter `RequestRedirect` на HTTP-listener.
  - Frontend nginx: env `FORCE_HTTPS_REDIRECT=true` (авто-выставляется чартом) подставляет блок `if ($http_x_forwarded_proto = "http") { return 301 ... }`. Запросы без `X-Forwarded-Proto` (probes, port-forward) не редиректятся.
- **Поддержка Kubernetes Gateway API** (`gateway.networking.k8s.io/v1`): новый блок values `gatewayAPI.*`, новые шаблоны `gateway.yaml`, `httproute.yaml`, `httproute-redirect.yaml`, `gateway-certificate.yaml`. Портативно между Istio, NGINX Gateway Fabric, Envoy Gateway, Cilium. `ingress.enabled` и `gatewayAPI.enabled` взаимоисключаются — `helm template` падает через helper `dasha.validateTrafficMode`, если оба true. `dasha.validateGatewayAPI` дополнительно требует `allowedRoutes.namespaces.from != "Same"`, когда `gatewayNamespace` отличается от release namespace, иначе HTTPRoute не сможет attach.
- **Новые helpers в `_helpers.tpl`:** `dasha.tlsEnabled`, `dasha.validateTrafficMode`, `dasha.validateGatewayAPI`, `dasha.gatewayTLSSecretName`, `dasha.gatewayNamespace`.

### CI / Tooling
- **Trivy filesystem + config-скан** (job `trivy-scan`) — сканирует зависимости (`go.sum`, `package-lock.json`) и IaC-мисконфиги (Dockerfile, Helm chart) на каждый push/PR. Блокирует merge на `CRITICAL`/`HIGH` (`ignore-unfixed: true` — не шумим на advisories без патча). `skip-dirs: demo` исключает demo-lab.
- **Release: Trivy на образах теперь блокирующий** — `exit-code: 0` → `1` в `release.yaml`. Релизы падают на `CRITICAL`/`HIGH` в опубликованных образах (раньше только печатался отчёт).
- **Workflow CodeQL** (`.github/workflows/codeql.yaml`) — статический анализ Go + TypeScript с набором запросов `security-extended`. Запуск на push, PR и по расписанию (Пн 06:00 UTC). Находки появляются во вкладке Security.
- **Dependabot расширен** на экосистемы `gomod` (`/backend`), `npm` (`/frontend`) и Docker-образы в `/deploy/images`. Сгруппированные обновления для OpenTelemetry, gRPC/protobuf, Vuetify, Vue core, ESLint, Vite — меньше PR-шума.

### Зависимости (Dependabot bumps с момента v0.1.23)
- **Бэкенд (Go):** `pgx/v5` `v5.9.0` → `v5.9.2`, `getkin/kin-openapi` `v0.133.0` → `v0.138.0`, `labstack/echo/v4` `v4.15.1` → `v4.15.2`, `spf13/cobra` `v1.10.1` → `v1.10.2`, `coreos/go-oidc/v3` `v3.17.0` → `v3.18.0`, `go.uber.org/zap` `v1.27.0` → `v1.28.0`, `oapi-codegen/runtime` `v1.1.2` → `v1.4.0`, `yandex-cloud/go-genproto` (bump).
- **Фронтенд (npm):** группа `vuetify`, группа `vue-core` (6 пакетов), `vitest` `3.2.4` → `4.1.6`, `prettier` `3.6.2` → `3.8.3`, группа `eslint` (3 пакета), `@tsconfig/node22` `22.0.2` → `22.0.5`.
- **Контейнеры:** `alpine` `3.21` → `3.23`, `node` `22-alpine` → `26-alpine`.
- **GitHub Actions:** `github/codeql-action` `v3` → `v4`.

### Прочее
- Фикс в Dependabot config.

## v0.1.23

#### Безопасность
- **CVE в Go toolchain (10 уязвимостей)**: версия Go в CI поднята с `1.26` до плавающего `1.26.x` — автоматически подтянется `go1.26.3`. Закрывает:
  - `html/template`: XSS через обход экранирования meta-content URL (GO-2026-4982), обход escaper (GO-2026-4980), баги отслеживания контекста JsBraceDepth (GO-2026-4865)
  - `crypto/x509`: неожиданная работа при построении цепочки (GO-2026-4947), неэффективная валидация политик (GO-2026-4946), обход auth через case-sensitive excludedSubtrees (GO-2026-4866)
  - `crypto/tls`: DoS через неаутентифицированный TLS 1.3 KeyUpdate (GO-2026-4870)
  - `net`: паника на NUL-байт в Dial/LookupPort на Windows (GO-2026-4971)
  - `net/http`: бесконечный цикл HTTP/2 при некорректном SETTINGS_MAX_FRAME_SIZE (GO-2026-4918)
- **Обновление `golang.org/x/net`** `v0.50.0` → `v0.53.0` — устраняет панику HTTP/2-сервера на специально подготовленных фреймах (GO-2026-4559) и упомянутый HTTP/2 SETTINGS_MAX_FRAME_SIZE (GO-2026-4918)
- Директива `go` в `go.mod` оставлена на `1.26.0` (минимум) — плавает только toolchain в CI/Docker; образ `golang:1.26-alpine` сам подтягивает последний patch
- **CI: job `govulncheck`** добавлен в `.github/workflows/ci.yaml` и включён в `build-check.needs` — каждый push и PR сканируется на символьные уязвимости Go, любое обнаружение блокирует merge в `main`
- **CI: job `npm audit`** добавлен для фронтенда (`vulncheck-frontend`), также включён в `build-check.needs`. Падает на advisories уровня `high` или `critical` (`--audit-level=high`) по `frontend/package-lock.json`

## v0.1.22
- **Chart** ESO version

## v0.1.20

#### Новые возможности
- **Активные запросы — фильтр по тексту запроса**: текстовое поле с переключателем `LIKE` / `NOT LIKE`, регистронезависимое (`ILIKE` на бэкенде). Wildcards `%`, `_` пользователь вводит сам. Новые query params `GET /api/queries/running`: `query_filter`, `query_filter_mode`
- **Активные запросы — фильтр по пользователю**: autocomplete на основе `/api/common/database-users` (тот же источник, что в Query Report exclude-users). Новый query param `username`
- **Активные запросы — Play/Stop + интервал обновления**: кнопка Play/Stop + селектор интервала (1 / 5 / 10 сек, дефолт 5). Авто-рефреш стартует только по клику пользователя (как в Прогрессе операций) и ограничен 5 минутами; остаток времени отображается рядом с кнопкой. Смена интервала перезапускает таймер на лету. Смена кластера останавливает авто-рефреш
- **Активные запросы — текст запроса в expanded-строке**: SQL вынесен из колонки в отдельную раскрытую строку под основной (паттерн как в TOP таблиц по размеру). Подсветка синтаксиса + кнопка копирования в буфер, обрезка на 100 символах с кнопкой «Показать SQL» (диалог с полным текстом — как в карточке Отчёта по запросам). Колонка `state` убрана — для активных запросов она почти всегда `active`
- **Отчёт по запросам / Сравнение — stddev и пользователи**: новые поля `StddevExecTimeMs`, `StddevPlanTimeMs` (`max(stddev_*_time)` по агрегированным строкам `pg_stat_statements`) и `Usernames` (`array_agg(DISTINCT rolname)`). σ выводится рядом с avg в строке `min..max, avg`. Имена пользователей отображаются чипами: в карточке отчёта — в шапке у queryid; в карточке сравнения — отдельной строкой на всю ширину для каждой стороны A/B (с поддержкой plural-формы: «Пользователь / Пользователя / Пользователей»). Обновлены все три SQL-шаблона отчёта (base / 150000 / 170000)

#### Исправления
- **OIDC error-страницы**: все ошибки в callback (token exchange, missing id_token, invalid id_token, разбор claims, сессия) и сбой генерации state-cookie на login теперь рендерят стилизованную страницу с appology (`oidc_unavailable.html`) вместо сырого JSON. HTML теперь шаблон `html/template` с подстановкой `{{.Message}}` и `{{.ShowRetry}}`; для каждой ошибки — своё сообщение, при наличии смысла повторить попытку выводится ссылка «Try logging in again»

#### Улучшения
- **Активные запросы — состояние секции в Pinia**: стор `activeQueries` (per-cluster, localStorage) теперь хранит `minDuration`, `queryFilter`, `queryFilterMode`, `username`, `intervalSec`. Плавное переключение кластеров с восстановлением UI-состояния. Состояние `running` авто-рефреша намеренно **не** сохраняется — пользователь явно запускает таймер после смены кластера или перезагрузки
- **Composable `useAutoRefresh`**: `pollInterval` принимает геттер `() => number` для реактивных интервалов; новый метод `restart()` для применения нового интервала на лету
- **Дерево блокировок / Активные запросы — human-readable длительности**: длительности форматируются как «2 ч 30 мин» / «45 сек» / «120 мс» (`fmtMs(ms, t)` из `utils/format.ts`) вместо сырой PG-строки `00:01:23.456`. Бэкенд: `QueryBlocked` теперь возвращает `BlockedDurationMs` / `BlockingDurationMs` (`EXTRACT(EPOCH FROM age(...)) * 1000`); в Active Queries колонка таблицы привязана к `DurationMs` — корректная числовая сортировка
- **Активные запросы — пауза авто-обновления на копирование / показ SQL**: клик по «копировать SQL» или «Показать SQL» останавливает авто-рефреш, чтобы строка не исчезла во время чтения
- **`truncateSql` / `SQL_PREVIEW_MAX` в `utils/sql.ts`**: убрано дублирование локальных хелперов обрезки SQL в `RunningQueriesSection` и `ReportCard` (раньше пороги 100 и 120 символов отличались) — единый общий порог 100 символов

## v0.1.19

#### Исправления
- **Отчёт по запросам — Время CPU**: раньше могло быть отрицательным или превышать 100% для запросов с параллельным выполнением (`pg_stat_statements` суммирует время IO по всем воркерам, а `total_exec_time` — wall-clock leader process). Теперь: бэкенд возвращает `null` для `cpu_time`, если расчёт даёт отрицательный результат; фронт показывает иконку `?` с пояснительным тултипом. В IO time дополнительно учитывается `temp_blk_read_time + temp_blk_write_time` (PG15+). Создан новый шаблон `150000/`, чтобы PG14 (где нет временных метрик temp_blk) использовал прежнюю формулу
- **Использование индексов**: таблицы с `seq_scan > 0, idx_scan = 0` теперь показывают `0%` вместо заглушки «Недостаточно данных»; прочерк `—` отображается только при полном отсутствии активности сканирования
- **Описание таблицы — смена кластера**: смена кластера больше не приводит к 404; выбранная таблица очищается, устаревшие данные предыдущей таблицы сбрасываются. `useClusterSelector.pushToUrl(true)` дропает кластер-специфичные query params (`schema`, `table` и т.п.) при смене кластера; `isSyncing` удерживается через `nextTick`, чтобы watcher host/db не успел повторно добавить extras; `DescribeTableSelector` пересоздаётся через `:key="clusterName"`
- **Описание таблицы — карточка Bloat**: теперь сбрасывается при смене кластера / хоста / БД / схемы / таблицы (раньше сохраняла данные предыдущей таблицы при смене контекста)
- **Подменю в сайдбаре**: подменю Tables / Indexes раскрывается после перезагрузки страницы, когда текущий маршрут внутри подменю (причина: до монтирования приложения не дожидались готовности роутера). Навигационное раскрытие — через индивидуальные `:model-value` computed на каждой группе (Vuetify не всегда подхватывал изменения массива `:opened` родительского v-list)

#### Улучшения
- **Состояние подключений / Источники подключений**: пустые ячейки для служебных процессов (autovacuum launcher, walwriter, checkpointer и др.) теперь заполняются значением `backend_type` из `pg_stat_activity` через `COALESCE`
- **Бублик «Состояние подключений»**: детерминированный цвет на каждое состояние через HSL-хеш для неизвестных значений `backend_type` (раньше все служебные процессы получали один коричневый цвет fallback)
- **Bloat индексов**: колонки с размерами отображаются через `fmtBytes` (KB/MB/GB); избыточный суффикс «(байт)» убран из заголовков

## v0.1.18

#### Новые возможности
- **Снимки pg_stat_statements**: сохранение и просмотр снимков pgss в отдельной БД хранилища
  - Опциональная БД хранилища (`storage.dsn` в конфиге)
  - Секционированные по дням таблицы: `snapshots` (JSON-отчёт) и `query_texts` (дедупликация по SHA-256 хешу)
  - CLI-команда `dasha migrate` создаёт таблицы и секции
  - Фронтенд: кнопка создания снимка, селектор данных (текущие / сохранённый снимок), общие URL с `?snapshot=uuid`
  - Отчёт по снимку: скрывает фильтр «Исключить пользователей», уведомление если снимок из URL не найден
- **Сравнение запросов**: side-by-side сравнение двух снимков или одного снимка с live-данными (`GET /api/queries/compare`)
  - Сортировка по total_time / calls / WAL / rows / cpu_time / io_time / temp_blks
  - Фильтры «скрыть запросы, отсутствующие в A/B»; блок Left/Right метрик с разницей по каждому запросу
  - Фильтр «Исключить пользователей» для live-стороны
  - Пункт меню скрывается автоматически, если хранилище снимков не настроено (проверка через `GET /api/queries/snapshots/status`, кеш в Pinia 10 мин)
- **Описание таблицы — оценка размера строки**: новая секция со сведениями о tuple header, null bitmap, средней ширине данных, оценочном размере строки, fillfactor, доступном/полезном месте на странице, числе строк на страницу, резерве под HOT-обновления, предупреждение WILL_TOAST и список колонок-кандидатов на TOAST (`GET /api/tables/describe-row-estimate`)
- **Описание таблицы — статистика вакуума**: время последних (auto)vacuum/(auto)analyze, dead/live tuples, `n_mod_since_analyze`, `n_ins_since_vacuum`, расчётные пороги vacuum/analyze/insert-vacuum с учётом глобальных настроек и per-table reloptions (`GET /api/tables/describe-vacuum-stats`)
- **Санитизация SQL**: `sanitize.SQL()` маскирует `password=` и `PASSWORD 'x'` в текстах запросов
- **OIDC маппинг ролей**: `role_mapping` в конфиге OIDC сопоставляет корпоративные группы с ролями dasha (admin/viewer)
- **Сброс pg_stat_statements**: `POST /api/queries/reset-stats` (только admin), управляется параметром `enable_query_stats_reset`

#### Исправления
- **Бэкенд**: ответы 404 теперь возвращают правильный HTTP-статус (был 500 из-за oapi-codegen strict handler, игнорировавшего response object при ненулевом error)
- **Фронтенд**: глобальная обработка ошибок через provide/inject — код ошибки из API пробрасывается корректно (раньше всегда был 500)
- **Фронтенд**: ошибка «Нет доступных кластеров» больше не пропадает при навигации
- **Фронтенд**: невалидный кластер/хост в URL показывает 404 с подсказками похожих имён вместо молчаливого редиректа
- **Отчёт по запросам / Top10: потеря точности queryid**
- **Активные запросы: ошибка скана NULL**: `GetQueriesRunning` падал с ошибкой `cannot scan NULL into *string` для фоновых процессов (autovacuum, walsender, logical replication worker), у которых `usename` = NULL. `usename` и `backend_type` обёрнуты в `COALESCE(..., '')` во всех трёх SQL-шаблонах (базовый, 100000/, 90600/).


#### Улучшения
- Section-компоненты используют `useViewError()` напрямую вместо цепочки emit — убрана косвенность, сохраняются коды ошибок
- `useClusterInfo` возвращает null для неизвестного кластера/хоста — блокирует API-запросы с невалидными параметрами
- Карточка входа с кнопкой SSO, отображением версии, сохранением URL при OIDC-редиректе
- Класс `ApiError` с HTTP-статусом, извлечённым из тела ответа
- `IoCpuScatterSection`: оси автоматически масштабируются в ms / s / min / h по диапазону данных
- `DescribeTableSelector`: при смене кластера схема сбрасывается на `public` (если есть) и очищается выбранная таблица; watcher схем предпочитает `public`, а не первую из списка
- `DescribeBloatSection` теперь отображается только для обычных таблиц (раньше показывалась всегда)
- Frontend Docker-образ зашивает `BUILD_NUMBER` через env `VITE_APP_VERSION` — версия видна в карточке логина и меню пользователя
- Nginx: добавлены `X-Forwarded-Proto`, отдельный `location /auth/`, увеличены `proxy_buffer_size` / `proxy_buffers` для cookie-тяжёлых ответов OIDC
- Компонент `ErrorAlert` для полноэкранного fallback при критической ошибке
- **Отчёт по запросам: поиск по подстроке**: новое текстовое поле в шапке отчёта фильтрует карточки по подстроке в полном тексте запроса (включая часть, скрытую за многоточием в карточке) или по queryid. Дебаунс 200 мс; сброс по нажатию на крестик.


#### Демо
- Добавлен сервис БД хранилища для снимков
- `dasha migrate` запускается автоматически перед стартом приложения

## v0.1.13

#### Новые возможности
- **Аутентификация и авторизация**: три режима — `none` (по умолчанию), `token` (статические API-ключи), `oidc` (OpenID Connect BFF)
- **RBAC**: ролевой доступ через Casbin — `admin` (полный) и `viewer` (только чтение)
- **Per-identity rate limiting**: token bucket с настраиваемыми RPS и burst

#### Безопасность
- Constant-time сравнение токенов и OAuth2 state
- Генерация случайных строк через `crypto/rand`
- Отзыв refresh token при logout (RFC 7009)
- Поддержка reverse proxy для флага `Secure` на cookie

#### Демо
- Keycloak OIDC-провайдер с настроенным realm, пользователями и ролями
- Исправлен race condition логической репликации в standalone init

## v0.1.12

- Косметические правки фронтенда

## v0.1.11

#### Новые возможности
- **Страница репликации**: новая страница с 3 секциями — настройки, статус, слоты
  - `GET /api/replication/status` — pg_stat_replication с JOIN pg_replication_slots (слот для каждой реплики), чипы state/sync_state с тултипами, client_addr/PID/LSN в раскрывающихся строках
  - `GET /api/replication/slots` — slot_type, wal_status (с тултипами-пояснениями), safe_wal_size, backlog_bytes
  - `GET /api/replication/config` — synchronous_standby_names + synchronous_commit с тултипами для каждого режима (on, remote_apply, remote_write, local, off)
- **БД Health**: новый эндпоинт `GET /api/database/health` — дедлоки, конфликты, ошибки контрольных сумм, соотношение rollback/commit из pg_stat_database
- **Wait events**: новый эндпоинт `GET /api/connection/wait-events` — агрегированные события ожидания из pg_stat_activity (исключая штатное ожидание Client.ClientRead)

#### Фронтенд
- **ReplicationView**: ReplicationConfigSection (настройки с тултипами на чипах), ReplicationStatusSection (цветовая кодировка задержки, чипы state/sync с тултипами, раскрывающиеся строки), ReplicationSlotsSection (тултипы wal_status)
- **DatabaseHealthSection**: чипы индикаторов здоровья на главной странице с зелёными/жёлтыми/красными порогами
- **WaitEventsSection**: таблица wait events на главной странице с цветовой кодировкой по типу
- Навигация: добавлен пункт меню «Репликация» с иконкой `mdi-database-sync-outline`
- `fmtLag` и `fmtBytes` вынесены в общий `utils/format.ts`

#### Бэкенд
- Новые SQL-шаблоны: `replication/status` (с `LEFT JOIN pg_replication_slots`), `connections/wait_events`, `database/health`
- Расширен `replication/slots`: slot_type, wal_status, safe_wal_size, backlog_bytes
- Новый `replication/config` — `current_setting()` для synchronous_standby_names и synchronous_commit
- Защита `pg_is_in_recovery()` при вызове `pg_current_wal_lsn()` (безопасно на репликах)
- Новые DTO: ReplicationStatus, ReplicationConfig, ReplicationSlot, WaitEvent, DatabaseHealth

#### Демо
- Добавлен генератор дедлоков в скрипт нагрузки для демонстрации индикаторов здоровья БД

## v0.1.10

### Описание таблицы (`\d+`)

#### Новые возможности
- **Селектор схемы/таблицы**: автокомплит с поиском схем и таблиц, синхронизация с URL
- **Метаданные индексов**: размер, иконки первичного/уникального/невалидного индекса с тултипами
- **Статистика столбцов**: `null_frac`, `n_distinct`, `avg_width` из `pg_stats` в раскрывающихся строках
- **Оценка количества строк**: `reltuples` из `pg_class` в человекочитаемом формате (K/M/B)
- **Статистика активности**: INS/UPD/DEL/SEQ_SCN/IDX_SCN из `pg_stat_all_tables`
- **Партиции**: постраничный список дочерних партиций для партиционированных таблиц со ссылками на describe
- **Оценка bloat**: результаты `pgstattuple_approx()` по кнопке "Рассчитать bloat", отключена с чипом статуса если расширение недоступно
- **Ссылки на describe**: кликабельные имена таблиц в 12 секциях индексов/таблиц через общий composable `useDescribeLink`

#### Бэкенд
- Новые SQL-шаблоны: `describe_bloat`, `describe_partitions`, `describe_schemas`, `describe_search`, `pgstattuple_available`
- Расширен `describe_columns`: `LEFT JOIN pg_stats` (null_frac, n_distinct, avg_width)
- Расширен `describe_indexes`: `indisvalid`, `pg_relation_size`, `pg_size_pretty`
- Расширен `describe_metadata`: `reltuples`, подзапрос stat_info
- Новые API-эндпоинты: `GET /api/tables/describe-bloat`, `GET /api/tables/describe-partitions`, `GET /api/tables/pgstattuple-available`, `GET /api/tables/schemas`, `GET /api/tables/search`
- Таймаут запроса `pgstattuple_approx` увеличен до 1 минуты

#### Фронтенд
- **Рефакторинг** `TableDescribeView.vue` с ~580 строк в 8 компонентов:
  - `DescribeTableSelector` — автокомплит схемы/таблицы с синхронизацией URL
  - `DescribeHeaderSection` — метаданные таблицы и карточка размеров
  - `DescribeColumnsSection` — столбцы с раскрывающимися строками статистики
  - `DescribeIndexesSection` — индексы с иконками PK/unique/invalid
  - `DescribeConstraintsSection` — переиспользуемый для check- и FK-ограничений
  - `DescribeReferencedBySection` — ссылающиеся таблицы
  - `DescribePartitionsSection` — партиции с пагинацией через `usePaginatedApiLoader`
  - `DescribeBloatSection` — проверка pgstattuple и расчёт bloat
- Добавлен `fmtRowCount` в общий `utils/format.ts`
- Добавлены правила плюрализации для русского языка в vue-i18n (`pluralRules` в main.ts)
- Навигация: меню "Таблицы" разделено на "Обзор" и "Описание"

#### Демо
- Добавлено расширение `pgstattuple` в init-скрипты демо

