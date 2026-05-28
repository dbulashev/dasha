# Health Score

Композитная метрика (0–100), отражающая общее состояние инстанса PostgreSQL по восьми категориям. Чем выше — тем лучше.

## Формула

```
score = 100 − Σ (penalty_i × weight_i)
обрезается до [0..100]
```

Для каждой категории `i` Dasha вычисляет непрерывный **штраф** (0..100) по «сырым» метрикам и складывает их с **весами** категорий, в сумме дающими 1.0. Веса валидируются и нормализуются; некорректные значения откатываются к дефолтным.

Категории, у которых на текущем инстансе нет полезного сигнала, **отбрасываются**, а их вес пропорционально перераспределяется на остальные — это не даёт отсутствию сигнала искусственно искажать счёт:

- `replication` — отбрасывается, если у инстанса нет реплик.
- `maintenance` — отбрасывается, если `pg_is_in_recovery()` = true (инстанс является standby). На реплике не работает autovacuum/ANALYZE, поэтому давность вакуума, возраст XID и maintenance-GUC отражают состояние мастера, наблюдаемое с реплики — действовать надо на мастере. Соответствующие правила также скрываются из рекомендаций.

Параллельно **движок правил** оценивает те же метрики и формирует список рекомендаций с уровнями LOW / MEDIUM / HIGH и ссылками на нужную страницу Dasha. Правила и штрафы независимы: штрафы дают числовой score, правила — список действий.

## Категории и веса

| Категория       | Вес    | Что меряет                                                          |
|-----------------|--------|----------------------------------------------------------------------|
| `connections`   | 0.15   | Использование коннектов, idle in tx, длинные транзакции             |
| `performance`   | 0.15   | Cache hit ratio, `track_io_timing`                                   |
| `storage`       | 0.10   | Доля dead-кортежей, bloat, эффективность HOT-обновлений             |
| `replication`   | 0.15   | Лаг репликации (время и байты), отключённые реплики                 |
| `maintenance`   | 0.15   | Возраст XID, давность вакуума, GUC autovacuum/track_counts, ANALYZE |
| `horizon`       | 0.10   | Лаг горизонта MVCC (старейший снепшот, блокирующий VACUUM)          |
| `wal_checkpoint`| 0.10   | Соотношение requested/timed чекпоинтов, рассогласование `wal_level` |
| `locks`         | 0.10   | Lock-waiters, ungranted locks, deadlocks, насыщение lock pool       |


## Пороги штрафов (обзор)

Штраф растёт по метрике непрерывно. **Точки перелома** — это значения метрики, в которых меняется крутизна штрафной функции: до первой точки штраф нулевой, между точками растёт плавно, после последней — достигает максимума категории. Стрелки `→` в правой колонке читаются именно так: первая точка → вторая → третья.

| Категория      | Метрика                                | Точки перелома (без штрафа → максимум)   |
|----------------|-----------------------------------------|-------------------------------------------|
| connections    | `total / max_connections`              | 0.60 → 0.80 → 0.95+                        |
| connections    | `idle_in_transaction` (шт.)            | по 5 баллов за каждый, потолок 30          |
| connections    | `longest_transaction_seconds`          | >300 с, потолок 20 баллов                  |
| performance    | `cache_hit_ratio` (%)                  | ≥95 → ≥90 → ≥85 → ниже                     |
| storage        | `max_dead_ratio` (%)                   | ≤20 → 20–30 → >30                          |
| storage        | `avg_dead_ratio` (%)                   | >15 — до 30 баллов                         |
| storage        | `tables_high_bloat` (шт.)              | >5 — до 30 баллов                          |
| replication    | `max_replay_lag_seconds`               | >10 с — растёт до максимума                 |
| replication    | `max_lag_bytes`                        | >16 МиБ — растёт до максимума              |
| replication    | `disconnected_replicas`                | каждое отключение даёт 25 баллов           |
| maintenance    | `max_xid_age` (xid)                    | 500 М → 1 Б → 1.5 Б                        |
| maintenance    | `max_vacuum_age_hours`                 | >168 ч → >504 ч → >1440 ч (7/21/60 дней)   |
| maintenance    | `tables_never_vacuumed`                | по 5 баллов за таблицу, потолок 20         |
| horizon        | `horizon_lag_xids`                     | 1 М → 10 М → 100 М                          |
| wal_checkpoint | `requested / total_checkpoints`        | ≥5 % → ≥10 % → ≥20 %                       |
| locks          | взвешенная сумма факторов блокировок   | см. `penaltyLocks` (накопительный)         |

Пороги по XID и horizon откалиброваны относительно `autovacuum_freeze_max_age` (200 М) и `vacuum_freeze_table_age` (150 М) из устройства PostgreSQL.

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
- `high_newpage_update_ratio` — `n_tup_newpage_upd / NULLIF(n_tup_upd, 0)` (PG 16+). Доля UPDATE, разорвавших HOT-цепочку и положивших новый кортеж на свежую страницу. Пороги ≥0.05 / ≥0.15 / ≥0.25.

### Replication
- `replication_lag_time` — `EXTRACT(EPOCH FROM replay_lag)` для худшей строки `pg_stat_replication`. На сколько standby отстаёт по проигрыванию WAL. Пороги ≥10 / ≥60 / ≥300 секунд.
- `replication_lag_bytes` — `pg_current_wal_lsn() - replay_lsn` для худшего standby. Хвост WAL, который ещё надо применить. Пороги ≥16 МиБ / ≥256 МиБ / ≥1 ГиБ.
- `disconnected_replicas` — реплики, описанные в `dasha.yaml` (или найденные discovery), которых нет в `pg_stat_replication`. Пороги ≥1 / ≥2 / ≥3.

### Maintenance
- `xid_wraparound_risk` — `max(age(datfrozenxid))` по `pg_database`. Число транзакций до wraparound-аварии. Откалибровано по `autovacuum_freeze_max_age=200 М` (на этой границе должен включаться anti-wraparound autovacuum) и жёсткому пределу 2 Б. Пороги ≥150 М / ≥200 М / ≥1.6 Б.
- `stale_vacuum` — дней с последнего `last_vacuum`/`last_autovacuum` по user-таблицам. Сигнал застрявшего autovacuum. Пороги ≥7 / ≥21 / ≥60 дней.
- `tables_never_vacuumed` — таблицы, у которых одновременно `last_vacuum IS NULL` и `last_autovacuum IS NULL`. Пороги ≥1 / ≥2 / ≥5.
- `autovacuum_disabled` — глобальный GUC `autovacuum=off`. Bloat и возраст XID растут бесконтрольно. HIGH.
- `track_counts_disabled` — глобальный GUC `track_counts=off`. У autovacuum нет статистики, фактически он не работает. HIGH.
- `tables_with_autovacuum_off` — таблицы с `autovacuum_enabled=false` в `pg_class.reloptions`. Пороги ≥1 / ≥5 / ≥20.
- `relfrozenxid_age_outlier` — худший `age(relfrozenxid)` по таблицам из `pg_class`. Потабличная версия `xid_wraparound_risk`. Пороги ≥200 М / ≥500 М / ≥1 Б.
- `stale_planner_stats` — таблицы, у которых `n_mod_since_analyze` велик относительно `n_live_tup` (статистика планировщика устарела). Пороги ≥3 / ≥10 / ≥30 таблиц.
- `analyze_disabled_tables` — таблицы с `autovacuum_analyze_threshold=-1` в `reloptions` (ANALYZE отключён на таблице). Пороги ≥1 / ≥5 / ≥20.

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


