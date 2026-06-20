# Health Score

Композитная метрика (0–100), отражающая общее состояние инстанса PostgreSQL по восьми категориям. Чем выше — тем лучше.

## Формула

```text
score = 100 − Σ (penalty_i × weight_i)
обрезается до [0..100]
если есть критическое условие: score = min(score, 30)
```

Для каждой категории `i` Dasha вычисляет непрерывный **штраф** (0..100) по «сырым» метрикам и складывает их с **весами** категорий, в сумме дающими 1.0. Веса валидируются и нормализуются; некорректные значения откатываются к дефолтным.

Категории, у которых на текущем инстансе нет полезного сигнала, **отбрасываются**, а их вес пропорционально перераспределяется на остальные — это не даёт отсутствию сигнала искусственно искажать счёт:

- `replication` — отбрасывается, если у инстанса нет реплик.
- `maintenance` — отбрасывается, если `pg_is_in_recovery()` = true (инстанс является standby). На реплике не работает autovacuum/ANALYZE, поэтому давность вакуума, возраст XID и maintenance-GUC отражают состояние мастера, наблюдаемое с реплики — действовать надо на мастере. Соответствующие правила также скрываются из рекомендаций.

### Критические условия (нижний порог score)

Обычное взвешенное среднее размывает катастрофы: близкий wraparound XID двигает категорию `maintenance` максимум на её вес (~15 баллов), поэтому база в шаге от аварийной остановки иначе показывала бы ~85/100 рядом с HIGH-рекомендацией о wraparound. Чтобы число не лгало, любое из условий ниже зажимает score в красную зону (`min(score, 30)`):

- **wraparound XID на failsafe** — `max(age(datfrozenxid), age(relfrozenxid)) ≥ 1.6 Б` (`vacuum_failsafe_age`), где PostgreSQL сам уходит в аварийный VACUUM и пропускает чистку индексов, чтобы успеть до стены ~2.1 Б;
- **autovacuum выключен глобально** (`autovacuum=off`) — dead-кортежи и возраст XID растут бесконтрольно;
- **track_counts выключен** (`track_counts=off`) — autovacuum «слеп» и фактически не запускается.

Порог применяется только на мастере (`pg_is_in_recovery() = false`): standby не запускает autovacuum и наследует горизонт заморозки от мастера, поэтому действие — и красный score — относятся к мастеру. Эти же условия выдаются как HIGH-рекомендации, так что число и список действий синхронны.

Параллельно **движок правил** оценивает те же метрики и формирует список рекомендаций с уровнями LOW / MEDIUM / HIGH и ссылками на нужную страницу Dasha. Правила и штрафы независимы: штрафы дают числовой score, правила — список действий. У каждого правила есть соответствующий вклад в score (штрафное слагаемое или нижний порог), поэтому условие не может попасть в рекомендации, не сдвинув число.

## Категории и веса

| Категория       | Вес    | Что меряет                                                          |
|-----------------|--------|----------------------------------------------------------------------|
| `connections`   | 0.15   | Использование коннектов, idle in tx, длинные транзакции             |
| `performance`   | 0.15   | Cache hit ratio, `track_io_timing`                                   |
| `storage`       | 0.10   | Доля dead-кортежей, bloat, эффективность HOT-обновлений             |
| `replication`   | 0.15   | Лаг репликации (время и байты), отключённые реплики                 |
| `maintenance`   | 0.15   | Возраст XID, очередь и давность вакуума, GUC autovacuum/track_counts, ANALYZE |
| `horizon`       | 0.10   | Лаг горизонта MVCC (старейший снепшот, блокирующий VACUUM)          |
| `wal_checkpoint`| 0.10   | Соотношение requested/timed чекпоинтов, рассогласование `wal_level` |
| `locks`         | 0.10   | Lock-waiters, ungranted locks, deadlocks, насыщение lock pool       |


## Пороги штрафов (обзор)

Штраф растёт по метрике непрерывно. **Точки перелома** — это значения метрики, в которых меняется крутизна штрафной функции: до первой точки штраф нулевой, между точками растёт плавно, после последней — достигает максимума категории. Стрелки `→` в правой колонке читаются именно так: первая точка → вторая → третья.

| Категория      | Метрика                                | Точки перелома (без штрафа → максимум)        |
|----------------|-----------------------------------------|-----------------------------------------------|
| connections    | `total / max_connections`              | 0.60 → 0.80 → 0.95+                            |
| connections    | `idle_in_transaction` (шт.)            | по 5 баллов за каждый, потолок 30             |
| connections    | `longest_transaction_seconds`          | >300 с, потолок 20 баллов                     |
| performance    | `cache_hit_ratio` (%)                  | ≥95 → ≥90 → ≥85 → ниже                        |
| performance    | `track_io_timing` выключен             | фиксированные 5 баллов (LOW)                  |
| storage        | `max_dead_ratio` (%)                   | ≤20 → 20–30 → >30                             |
| storage        | `avg_dead_ratio` (%)                   | >15 — до 30 баллов                            |
| storage        | `tables_high_bloat` (шт.)              | >5 — до 30 баллов                             |
| storage        | `hot_update_ratio`                     | <0.80 → <0.65 → <0.50 (5 / 15 / 30 баллов)    |
| storage        | `newpage_update_ratio` (PG 16+)        | >0.05 → >0.10 → >0.20 (5 / 10 / 20 баллов)    |
| replication    | `max_replay_lag_seconds`               | >10 с — растёт до максимума                    |
| replication    | `max_lag_bytes`                        | >16 МиБ — растёт до максимума                 |
| replication    | `disconnected_replicas`                | каждое отключение даёт 25 баллов              |
| maintenance    | `max(xid_age, relfrozenxid_age)`       | 200 М → 1.6 Б → 2.1 Б (нарастает до 100)      |
| maintenance    | `vacuum_backlog_tables`                | >5 таблиц → +1.5 балла за шт., потолок 15     |
| maintenance    | `max_overdue_vacuum_age_hours`         | >168 ч → >504 ч → >1440 ч (7/21/60 дней)      |
| maintenance    | `tables_never_vacuumed`                | по 5 баллов за таблицу, потолок 20            |
| maintenance    | `tables_with_autovacuum_off`           | по 3 балла за таблицу, потолок 15             |
| maintenance    | `stale_planner_stats_tables`           | по 2 балла за таблицу, потолок 15             |
| maintenance    | `autovacuum` / `track_counts` выключен | насыщает категорию (и зажимает score)         |
| horizon        | `horizon_lag_xids`                     | 1 М → 10 М → 100 М                             |
| wal_checkpoint | `requested / total_checkpoints`        | ≥5 % → ≥10 % → ≥20 %                          |
| wal_checkpoint | рассогласование `wal_level`            | minimal+реплики 80 баллов; logical+нет слота 5 |
| locks          | взвешенная сумма факторов блокировок   | см. `penaltyLocks` (накопительный)            |

Штраф по возрасту XID откалиброван по механике заморозки PostgreSQL: начинается на `autovacuum_freeze_max_age` (200 М, аварийный autovacuum), достигает 80 на `vacuum_failsafe_age` (1.6 Б) и 100 на стене останова ~2.1 Б — то есть продолжает расти в опасной зоне, а не упирается в полку. Правило `relfrozenxid_age_outlier` использует ту же кривую через `max(datfrozenxid, relfrozenxid)`. Каждое правило ниже соответствует одному из этих штрафных слагаемых или критическому порогу, поэтому score и рекомендации покрывают одни и те же условия.

## Правила и severity (рекомендации)

Правило срабатывает, когда метрика пересекает дискретный порог LOW / MEDIUM / HIGH. Фильтрация по области видимости:

- instance-only категории (`connections`, `replication`, `horizon`, `wal_checkpoint`, `locks`) скрываются при drill down (детализации) на конкретную базу;
- вся категория `maintenance` скрывается на standby (`pg_is_in_recovery() = true`) — синхронно с тем, как отбрасывается её вес в score.

В каждой строке: что меряется / как считается, затем пороги LOW / MEDIUM / HIGH.

### Connections
- `high_connection_ratio` — `count(*) из pg_stat_activity / max_connections`. Запас до отказа в новых соединениях. Пороги ≥0.70 / ≥0.85 / ≥0.95.
- `idle_in_transaction` — сессии в `pg_stat_activity` со `state='idle in transaction'`. Каждая держит блокировки и пинит горизонт MVCC, блокируя VACUUM. Пороги ≥2 / ≥5 / ≥10.
- `long_running_transaction` — `now() - xact_start` самой долгой транзакции. Длинные транзакции усиливают bloat и не дают замораживать строки. Пороги ≥300 / ≥600 / ≥1800 секунд.

### Performance
- `low_cache_hit_ratio` — `heap_blks_hit / (heap_blks_hit + heap_blks_read)` по `pg_statio_user_tables`, в %. Доля чтений страниц из `shared_buffers`, а не с диска / ОС-кеша. Пороги <95 / <90 / <85.
- `track_io_timing_disabled` — GUC `track_io_timing` выключен, поэтому `pg_stat_statements.*_blk_*_time` всегда нули и I/O медленных запросов нельзя проанализировать. LOW.

### Storage
- `high_max_dead_ratio` — худшая по таблицам `n_dead_tup / NULLIF(n_live_tup + n_dead_tup, 0)` из `pg_stat_user_tables`, в %. Маркер таблицы, с которой не справляется autovacuum. Пороги ≥10 / ≥20 / ≥30.
- `high_avg_dead_ratio` — то же отношение, усреднённое по таблицам с > 1000 живых строк. Фоновый уровень bloat. Пороги ≥5 / ≥15 / ≥25.
- `many_bloated_tables` — число таблиц, у которых доля dead-кортежей выше триггера autovacuum (`autovacuum_vacuum_scale_factor`). Пороги ≥5 / ≥10 / ≥20.
- `low_hot_update_ratio` — `n_tup_hot_upd / NULLIF(n_tup_upd, 0)` по всем user-таблицам. Чем ниже, тем чаще UPDATE кладёт новый кортеж в другое место и переписывает все индексы — index bloat. Пороги <0.80 / <0.65 / <0.50.
- `high_newpage_update_ratio` — `n_tup_newpage_upd / NULLIF(n_tup_upd, 0)` (PG 16+). Доля UPDATE, разорвавших HOT-цепочку и положивших новый кортеж на свежую страницу. Пороги ≥0.05 / ≥0.10 / ≥0.20.

### Replication
- `replication_lag_time` — `EXTRACT(EPOCH FROM replay_lag)` для худшей строки `pg_stat_replication`. На сколько standby отстаёт по проигрыванию WAL. Пороги ≥10 / ≥60 / ≥300 секунд.
- `replication_lag_bytes` — `pg_current_wal_lsn() - replay_lsn` для худшего standby. Хвост WAL, который ещё надо применить. Пороги ≥16 МиБ / ≥256 МиБ / ≥1 ГиБ.
- `disconnected_replicas` — реплики, описанные в `dasha.yaml` (или найденные discovery), которых нет в `pg_stat_replication`. Пороги ≥1 / ≥2 / ≥3.

### Maintenance
- `xid_wraparound_risk` — `max(age(datfrozenxid))` по `pg_database`. Число транзакций до wraparound-аварии. Откалибровано по `autovacuum_freeze_max_age=200 М` (на этой границе должен включаться anti-wraparound autovacuum) и жёсткому пределу 2 Б. Пороги ≥150 М / ≥200 М / ≥1.6 Б.
- `vacuum_backlog` — таблицы, уже превысившие порог срабатывания autovacuum: `n_dead_tup` выше `autovacuum_vacuum_threshold + autovacuum_vacuum_scale_factor·reltuples`, либо `n_ins_since_vacuum` выше insert-порога. Потабличные `reloptions` переопределяют глобальные GUC (формула самого PostgreSQL). Длина очереди на vacuum — глубокая очередь означает, что autovacuum не успевает. Пороги ≥6 / ≥15 / ≥30 таблиц.
- `stale_vacuum` — возраст самого старого `last_vacuum`/`last_autovacuum`, в днях, **среди таблиц из очереди** (превысивших свой порог autovacuum). Статичные / преимущественно читаемые таблицы в очередь не попадают и больше не дают ложных срабатываний. Сигнал застрявшего autovacuum. Пороги ≥7 / ≥21 / ≥60 дней.
- `tables_never_vacuumed` — таблицы, у которых одновременно `last_vacuum IS NULL` и `last_autovacuum IS NULL`. Пороги ≥1 / ≥2 / ≥5.
- `autovacuum_disabled` — глобальный GUC `autovacuum=off`. Bloat и возраст XID растут бесконтрольно. HIGH.
- `track_counts_disabled` — глобальный GUC `track_counts=off`. У autovacuum нет статистики, фактически он не работает. HIGH.
- `tables_with_autovacuum_off` — таблицы с `autovacuum_enabled=false` в `pg_class.reloptions`. Пороги ≥1 / ≥5 / ≥20.
- `relfrozenxid_age_outlier` — худший `age(relfrozenxid)` по таблицам из `pg_class`. Потабличная версия `xid_wraparound_risk`. Пороги ≥200 М / ≥500 М / ≥1 Б.
- `stale_planner_stats` — таблицы, у которых `n_mod_since_analyze` превышает половину их (с учётом reloptions) порога auto-analyze и которые не анализировались более 24 ч (статистика планировщика устарела). Пороги ≥3 / ≥10 / ≥30 таблиц.

### Horizon
- `horizon_lag_xids` — `txid_current() - min(backend_xmin)` по `pg_stat_activity`. Сколько транзакций VACUUM не может убрать, потому что их ещё видит какая-то сессия (длинная транзакция, заброшенный replication-слот, prepared tx). Пороги ≥1 М / ≥10 М / ≥100 М.

### WAL / checkpoints
- `requested_checkpoint_ratio` — `checkpoints_req / (checkpoints_req + checkpoints_timed)` из `pg_stat_bgwriter` (PG <17) / `pg_stat_checkpointer` (PG 17+). Высокая доля — `max_wal_size` мал или сейчас всплеск записи. Нужно ≥10 семплов. Пороги ≥5 % / ≥10 % / ≥20 %.
- `wal_level_minimal_with_replicas` — GUC `wal_level=minimal` не даёт физическую репликацию; любой standby молча сломан. HIGH.
- `wal_level_logical_without_publications` — GUC `wal_level=logical` стоит, но `pg_publication` пуст; дополнительный объём WAL пишется впустую. LOW.

### Locks
- `active_lock_waiters` — сессии в `pg_stat_activity` со `wait_event_type='Lock'`. Они заблокированы прямо сейчас. Пороги ≥1 / ≥3 / ≥10.
- `longest_lock_wait_seconds` — `EXTRACT(EPOCH FROM now() - state_change)` самого долгого текущего Lock-wait. Пороги ≥10 / ≥30 / ≥60 секунд.
- `ungranted_locks` — строки в `pg_locks` с `granted=false`. Очередь запросов блокировок, скопившаяся за держателем. Пороги ≥2 / ≥5 / ≥15.
- `deadlocks_rate` — счётчик `deadlocks` из `pg_stat_database` (накопительный с `pg_stat_database_reset`). Без per-day нормализации применим только факт «больше нуля». LOW при total > 0.
- `lock_pool_saturation` — `count(*) из pg_locks` делёное на `max_connections × max_locks_per_transaction` (размер общего пула heavyweight-блокировок). Пороги ≥0.4 / ≥0.6 / ≥0.8.

## Drill down (детализация по базам)

В таблице «Базы данных» по каждой БД собраны те же метрики, что и для инстанса: cache hit ratio, dead tuples, давность вакуума. Движок правил пересчитывается в database-scope, скрывая instance-only категории. В UI таблица сортируется по размеру или score, выбранную базу можно закрепить как контекст рекомендаций.

## Metrics-backed режим (история, baseline, богатые сигналы)

По умолчанию score — это **точечный SQL-snapshot**. Если настроен Prometheus/VictoriaMetrics-совместимый datasource (`health_score.metrics` в `dasha.yaml`), Dasha считает **score**, **рекомендации** и **тренд** из time-series метрик. Откат на snapshot распространяется **только на score и рекомендации**: если datasource недоступен или цель не сопоставлена, они возвращаются к точечному SQL-snapshot, а поле `source` в `GET /api/common/health-score` показывает, каким путём получено число. У эндпоинта **тренда/истории** (`GET /api/common/health-score/history`) **отката на snapshot нет** — он отдаётся только из time-series и возвращает **404**, когда metrics-режим выключен, недоступен или цель не сопоставлена.

Каталожные и GUC-факты, которые time-series выразить не может — потабличный `autovacuum_enabled=false`, ни разу не вакуумленные таблицы, возраст `relfrozenxid`, дрейф статистики планировщика, `wal_level`, GUC `autovacuum`/`track_counts`, горизонт MVCC и размер lock-pool — **накладываются из SQL-снимка** на метрик-сигналы. Поэтому каждое правило продолжает влиять и на score, и на рекомендации даже в metrics-режиме (инвариант score↔rules сохраняется), а каталожные находки вроде потабличной рекомендации `tables_with_autovacuum_off` не исчезают молча при подключении datasource. Оверлей обязателен для паритета: если SQL-снимок прочитать не удалось, Dasha откатывается на чистый snapshot-score, а не отдаёт metrics-only число с обнулёнными каталожными фактами (которые иначе читались бы как, например, «autovacuum off»). (Исторический **тренд** остаётся чисто time-series — каталожные факты это значения «сейчас», поэтому gauge может быть чуть ниже последней точки тренда на величину каталожного штрафа.)

### Провайдеры и матчинг лейблов

Score потребляет **нормализованный набор сигналов**; адаптеры провайдеров переводят метрики и лейблы каждого источника:

| Роль | Self-managed | Managed (Yandex MDB) |
|------|--------------|----------------------|
| Внутренности PG (`core`) | pgSCV | pgSCV (удалённый скрейп) |
| Пуллер | pgbouncer (через pgSCV) | YC pooler |
| Хост | pgSCV system collector | YC host-метрики |

Схемы лейблов различаются по провайдеру/окружению, поэтому **шаблоны селекторов конфигурируемы** per-target (`selectors:` + `targets:`). `GET /api/common/health-score/datasource/status?cluster_name=…&instance=…` показывает по каждой роли провайдера, отрендеренный селектор и число сматченных рядов (ровно один = ок) — для валидации матчинга.

**Дискаверенные кластеры** (из `discovery:`, напр. Yandex MDB) авто-маппятся из метаданных discovery — их не нужно перечислять в `targets:`: FQDN хоста → `{{.Host}}`, cloud resource id (id кластера MDB) → `{{.Service}}`, лейбл `folder_id` → `{{.Env}}`, короткий хост → `{{.Container}}`; провайдеры берутся из `providers_default` (напр. `core: pgscv`, `pooler/host: yc_native`). Кастомизируются только шаблоны селекторов. Статический `targets:` всегда перекрывает выведенный маппинг; `auto_map_discovered: false` отключает авто-маппинг, `discovery_env_label` меняет, из какого лейбла брать `{{.Env}}`.

### Тренд, сезонная норма (baseline) и просадки

`GET /api/common/health-score/history?cluster_name=…&instance=…&from=…&to=…&step_seconds=…` отдаёт по времени общий балл, баллы категорий и латентность на `[from, to]`. График `HealthScoreTrend` на `/health-score` рисует score + сезонную норму + латентность с отметками просадок.

#### Что такое «сезонная норма»

Нагрузка на БД почти всегда **циклична**: будни ≠ выходные, день ≠ ночь, понедельник 09:00 ≠ воскресенье 03:00. Плоское среднее или фиксированный порог это игнорируют — либо шумят на ночном batch'е, либо пропускают реальное замедление в пик. Сезонная норма — это **ожидаемое значение метрики для конкретного момента недельного цикла**, а не общее среднее. Строится так:

1. **Бакетирование по «часу недели».** Каждая точка истории попадает в один из **168 бакетов** (7 дней × 24 часа): `hour_of_week = weekday*24 + hour` (UTC). Понедельник 09:00 → бакет 33, воскресенье 00:00 → бакет 0.
2. **Медиана на бакет** за длинное окно (дефолт 28 дней). Берём именно медиану, а не среднее — она устойчива к выбросам: разовый ночной `VACUUM` или деплой не сдвигают норму.

Получается «профиль недели»: нормальный балл (и латентность) для каждого часа каждого дня недели.

#### Как используется

Текущее значение сравнивается с **его собственной нормой для этого часа недели**, а не с глобальным средним:

- **Просадки (dips):** «сейчас вторник 14:00, score 70, а обычные вторники 14:00 = 92 → просадка 22 пункта» → отметка на тренде. Регулярный ночной batch, роняющий score, просадкой *не* считается (его норма тоже низкая) — ложной тревоги нет.
- **Регрессия латентности** → `performance`: `текущая латентность / сезонная норма` отвечает на вопрос «медленнее ли запрос, чем обычно *в это время недели*». Работает на любом воркладе, потому что сравнивает с собой, а не с абсолютным порогом `50/200/1000 мс`.

Пример: 50 мс в понедельник 14:00 (норма 45 мс) — чуть выше обычного; те же 50 мс в понедельник 03:00 (норма 12 мс) — ~4× нормы, реальная аномалия. Одно значение — два вердикта.

Сезонная норма и просадки появляются по мере накопления истории; пока её мало, график деградирует мягко (нет линии нормы, нет отметок просадок). Источник: `BuildBaseline` / `Baseline.Value` в `backend/internal/metrics/baseline.go`.

### Богатые сигналы (vs SQL-snapshot)

- **Сатурация CPU хоста** (`load_avg_15 / vCPU`) и **сатурация пуллера** (`server_conns / pool_size`) → `connections` — точнее, чем `total / max_connections` на пулящихся сетапах.
- **Регрессия латентности** → `performance`: оконная средняя латентность из `pg_stat_statements` относительно своего сезонного baseline (×1.5 / ×3 / ×6), чтобы `performance` двигался по реальной латентности, а не только по cache-hit. Латентность собирается всегда; штраф требует baseline.
- **Checksum-ошибки** (повреждение страниц) и **исчерпание sequence/ID-пространства** у предела → критический floor + HIGH-правила.
- **Регрессия sequential scans** → `performance`: темп чтения строк seq-scan'ами относительно своей сезонной нормы (×1.5 / ×3 / ×6) — рост сигналит, что индексы перестали использоваться или протухла статистика (`ANALYZE` / ревизия индексов), без ложных срабатываний на штатных аналитических сканах. Собирается всегда; штраф требует baseline.
- **Свободное место на диске хоста** → `storage`: занято/всего на самой заполненной ФС (pgSCV `node_filesystem_*`, Yandex Cloud `disk_used_bytes`/`disk_total_bytes`). Пороги LOW/MED/HIGH на ≥70/80/90% и **роль-агностичный критический floor на ≥90%** — полный data-том останавливает запись, поэтому зажимает score в красную зону и на мастере, и на реплике.

### Конфигурация

```yaml
health_score:
  metrics:
    enabled: true
    datasource:
      url: "http://victoria-metrics:8428"
      # auth (как секрет): type none|bearer|basic, креды через env
      auth: { type: bearer, token_from_env: DASHA_METRICS_DATASOURCE_TOKEN }
    providers_default: { core: pgscv, pooler: pgbouncer, host: pgscv_system }
    selectors: { … }   # шаблоны лейблов на провайдера (дефолты в комплекте)
    targets:           # проекция каждой Dasha-цели (cluster, instance) на лейблы datasource
      - { cluster: …, instance: …, env: …, service: …, host: …, container: … }
```

Auth datasource поддерживает `token_from_env` (bearer) и `username` + `password_from_env` (basic) — резолвятся из окружения как остальные `*_from_env`-секреты, поэтому креды инжектятся из Secret, а не лежат инлайн. `type: none` (по умолчанию) кредов не требует.



