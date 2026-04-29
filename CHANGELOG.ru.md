# История изменений

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

## v0.1.9

Предыдущие изменения см. в git-истории.
