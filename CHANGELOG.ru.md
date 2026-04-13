# История изменений

## v0.1.17

#### Новые возможности
- **Снимки pg_stat_statements**: сохранение и просмотр снимков pgss в отдельной БД хранилища
  - Опциональная БД хранилища (`storage.dsn` в конфиге)
  - Секционированные по дням таблицы: `snapshots` (JSON-отчёт) и `query_texts` (дедупликация по SHA-256 хешу)
  - CLI-команда `dasha migrate` создаёт таблицы и секции
  - 4 новых API-эндпоинта: `GET /api/queries/snapshots/status`, `GET/POST /api/queries/snapshots`, `GET /api/queries/snapshot/{id}`
  - Фронтенд: кнопка создания снимка, селектор данных (текущие / сохранённый снимок), общие URL с `?snapshot=uuid`
  - Отчёт по снимку: скрывает фильтр «Исключить пользователей», уведомление если снимок из URL не найден
- **Санитизация SQL**: `sanitize.SQL()` маскирует `password=` и `PASSWORD 'x'` в текстах запросов
- **OIDC маппинг ролей**: `role_mapping` в конфиге OIDC сопоставляет корпоративные группы с ролями dasha (admin/viewer)
- **Сброс pg_stat_statements**: `POST /api/queries/reset-stats` (только admin), управляется параметром `enable_query_stats_reset`

#### Исправления
- **Бэкенд**: ответы 404 теперь возвращают правильный HTTP-статус (был 500 из-за oapi-codegen strict handler, игнорировавшего response object при ненулевом error)
- **Фронтенд**: глобальная обработка ошибок через provide/inject — код ошибки из API пробрасывается корректно (раньше всегда был 500)
- **Фронтенд**: ошибка «Нет доступных кластеров» больше не пропадает при навигации
- **Фронтенд**: невалидный кластер/хост в URL показывает 404 с подсказками похожих имён вместо молчаливого редиректа

#### Улучшения
- Section-компоненты используют `useViewError()` напрямую вместо цепочки emit — убрана косвенность, сохраняются коды ошибок
- `useClusterInfo` возвращает null для неизвестного кластера/хоста — блокирует API-запросы с невалидными параметрами
- Карточка входа с кнопкой SSO, отображением версии, сохранением URL при OIDC-редиректе
- Класс `ApiError` с HTTP-статусом, извлечённым из тела ответа

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
