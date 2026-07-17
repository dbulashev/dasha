# Правила health score и пороги

Health score — 0–100 (больше = лучше): `100 − Σ(штраф_категории × вес)`.
Веса категорий по умолчанию: connections 0.15, performance 0.15,
replication 0.15, maintenance 0.15, storage 0.10, horizon 0.10,
wal_checkpoint 0.10, locks 0.10.
Зоны: ≥80 — здоров, 40–79 — деградация, <40 — критично.

**Критический потолок:** одно катастрофическое условие зажимает весь score
до ≤30 независимо от остальных категорий: возраст XID за failsafe-зоной,
любая ошибка контрольных сумм, sequence исчерпан на ≥95%, диск хоста ≥90%.

**Ключевые XID-константы:** 150M — начинается агрессивная заморозка
(vacuum_freeze_table_age); 200M — принудительный анти-wraparound autovacuum
(autovacuum_freeze_max_age); 1.6B — failsafe-режим VACUUM (без очистки
индексов); ~2.1B — сервер перестаёт выдавать XID (простой, single-user VACUUM).

Ниже — каждый rule ID из get_health_recommendations: когда срабатывает
(LOW / MED / HIGH) и первое действие.

## connections (вес 0.15)

### high_connection_ratio
Доля занятых max_connections. LOW ≥60%, MED ≥80%, HIGH ≥95%.
Первое: `connections` — кто держит (утечка пула приложения?); рассмотреть pgbouncer.

### idle_in_transaction
Сессии idle in transaction дольше 30с. LOW ≥2, MED ≥5, HIGH ≥10.
Держат блокировки и xmin-горизонт (VACUUM не чистит). Первое:
`running_queries` — найти старейшую, завершить (pg_terminate_backend),
включить idle_in_transaction_session_timeout.

### long_running_transaction
Возраст старейшей транзакции. LOW ≥300с, MED ≥600с, HIGH ≥1800с.
Первое: `running_queries` — решить, прерывать ли; длинные транзакции
также держат горизонт (см. horizon_lag_xids).

### host_cpu_saturation
15-мин load average / vCPU (только режим метрик). LOW ≥1, MED ≥2, HIGH ≥4.
Очередь превышает ядра — растёт латентность. Первое: `top_queries` (by=time).

### pooler_saturation
Серверные соединения пулера / ёмкость пула (только режим метрик).
LOW ≥50%, MED ≥60%, HIGH ≥80%. Клиенты встают в очередь. Первое: `connections`.

## performance (вес 0.15)

### low_cache_hit_ratio
Cache hit ratio инстанса. LOW <95%, MED <90%, HIGH <85% (пороги намеренно
ослаблены: на OLAP большие последовательные сканы делают низкие значения нормой).
Первое: `top_queries` — найти запросы с чтением с диска; shared_buffers ~25% RAM.

### track_io_timing_disabled
track_io_timing=off. Всегда LOW. Без него в EXPLAIN ANALYZE и
pg_stat_statements нет таймингов I/O. Оверхед минимален — включить.

### latency_regression
Латентность запросов против сезонной нормы (только режим метрик).
LOW >1.5×, MED >3×, HIGH >6×. Первое: `top_queries` (by=time), сравнить со
снимком через `query_compare`.

### seq_scan_regression
Строки, читаемые seq scan, против сезонной нормы (только режим метрик).
LOW >1.5×, MED >3×, HIGH >6×. Планировщик ушёл с индекса (протухшая
статистика) или индекса нет/невалиден. Первое: `list_indexes` (kind=usage,
затем missing); ANALYZE горячих таблиц.

## storage (вес 0.10)

### high_max_dead_ratio
Худший dead ratio таблицы. LOW ≥10%, MED ≥20%, HIGH ≥30%.
Первое: `top_tables` / `describe_table` — VACUUM ANALYZE худшей таблицы.

### high_avg_dead_ratio
Средний dead ratio по таблицам. LOW ≥5%, MED ≥15%, HIGH ≥25%.
Autovacuum не справляется: настроить autovacuum_vacuum_scale_factor / cost_limit.

### many_bloated_tables
Таблиц с dead ratio >20%. LOW ≥5, MED ≥10, HIGH ≥20. Первое: `vacuum_danger`
и VACUUM по списку.

### low_hot_update_ratio
Доля HOT-обновлений. LOW <80%, MED <65%, HIGH <50%. Не-HOT обновления пишут
во все индексы (bloat). Для HOT нужны ОБА условия — проверь оба через
describe_table, прежде чем что-либо советовать:
1. UPDATE не должен трогать индексированную колонку. Если трогает — HOT
   невозможен ни при каком fillfactor, и лечится это только удалением того
   индекса (сначала проверь его через unused_index_report), а не тюнингом
   хранения.
2. На странице должно быть свободное место. Чтобы оно появилось, fillfactor
   надо СНИЗИТЬ до 70–90 — снизить, а не повысить. Свободное место — это ровно
   то, что нужно HOT, поэтому повышение fillfactor к 100 HOT УБИВАЕТ. Горячая
   таблица с fillfactor 70 и высокой долей HOT настроена ПРАВИЛЬНО — не трогать.

### high_newpage_update_ratio
PG16+: обновления с переносом на новую страницу (разрыв HOT-цепочки).
LOW ≥5%, MED ≥10%, HIGH ≥20%. Снизить fillfactor проблемной таблицы.

### checksum_failures
Любая ошибка контрольной суммы страницы. Всегда HIGH, зажимает score ≤30:
повреждение данных. Немедленно проверить диски и бэкапы.

### sequence_exhaustion
Худшее использование sequence относительно предела типа (например int4 PK).
LOW ≥75%, MED ≥85%, HIGH ≥95% (при ≥95% score зажат ≤30). Планировать
миграцию на bigint до остановки записи.

### host_disk_space
Самая заполненная ФС хоста (только режим метрик). LOW ≥70%, MED ≥80%,
HIGH ≥90% (при ≥90% score зажат ≤30 — полный том данных останавливает запись
и может испортить данные). Освободить WAL/логи/temp, проверить bloat и
неактивные слоты репликации (`get_replication`).

## replication (вес 0.15)

### replication_lag_time
Макс. replay lag реплики. LOW ≥1с, MED ≥5с, HIGH ≥30с.
Первое: `get_replication` — нагрузка на реплику (тяжёлые SELECT), сеть.

### replication_lag_bytes
Макс. отставание реплики в байтах. LOW ≥10MB, MED ≥100MB, HIGH ≥1GB.
Растущий lag → скорость применения WAL на реплике, max_wal_size.

### disconnected_replicas
Реплики не в streaming. MED =1, HIGH ≥2. Проверить сеть от реплики к
primary и walreceiver; неактивный слот копит WAL (риск диска).

## maintenance (вес 0.15) — на репликах категория исключается (autovacuum там не работает)

### xid_wraparound_risk
Макс. возраст XID базы. LOW ≥150M, MED ≥200M, HIGH ≥1.6B (см. константы выше;
за 1.6B score зажат ≤30). Первое: `vacuum_danger`; VACUUM FREEZE худших баз,
завершить транзакции, держащие горизонт.

### relfrozenxid_age_outlier
Макс. возраст relfrozenxid таблицы; пороги как у xid_wraparound_risk.
Таблицы, пропущенные autovacuum freeze — найти через `vacuum_danger`, VACUUM FREEZE.

### stale_vacuum
Старейшая таблица, уже превысившая порог autovacuum, но давно не чищенная.
LOW ≥7 дней, MED ≥21, HIGH ≥60. Autovacuum голодает: ужесточить scale_factor
для крупных таблиц, проверить воркеры.

### vacuum_backlog
Таблиц в очереди autovacuum. LOW ≥6, MED ≥15, HIGH ≥30.
Поднять autovacuum_max_workers / cost_limit, снизить cost_delay.

### tables_never_vacuumed
Таблиц, ни разу не вакуумированных. LOW ≥1, MED ≥2, HIGH ≥5. VACUUM ANALYZE;
проверить per-table autovacuum_enabled.

### autovacuum_disabled
autovacuum=off глобально. Всегда HIGH. Мёртвые версии копятся без предела — включить.

### track_counts_disabled
track_counts=off. Всегда HIGH: без статистики autovacuum слеп — включить.

### tables_with_autovacuum_off
Таблицы с reloptions autovacuum_enabled=false. Всегда LOW. Обычно остатки
старых миграций — проверить намеренность. Имена таблиц даст health_details
(detail=tables_autovacuum_off). Лечение: `ALTER TABLE t RESET
(autovacuum_enabled)` (или `SET (autovacuum_enabled = true)`), затем разовый
обычный `VACUUM (ANALYZE) t`, чтобы разгрести накопленное. Тюнинг порогов
autovacuum_* при autovacuum_enabled=false НЕ ДЕЛАЕТ НИЧЕГО — демон вообще не
смотрит на таблицу. Fillfactor здесь не трогать: мёртвые кортежи — проблема
автовакуума, а не HOT, и таблица с fillfactor 70 и высокой долей HOT настроена
правильно.

### stale_planner_stats
Таблицы, превысившие свой порог автоанализа (с учётом per-table reloptions) и
не анализировавшиеся >3 часов — автоанализ должен был сработать, но не сработал.
LOW ≥3, MED ≥5, HIGH ≥10. Вероятны плохие планы: ANALYZE вручную и выяснить,
почему автоанализ не дошёл до таблицы (autovacuum_enabled=false, воркеры заняты
крупными таблицами). Холодные таблицы ниже порога не считаются: для них
автоанализ и не должен запускаться.

## horizon (вес 0.10)

### horizon_lag_xids
Насколько старейший снапшот/транзакция держит горизонт очистки, в транзакциях.
LOW ≥1M, MED ≥10M, HIGH ≥100M. VACUUM видит мёртвые версии как «ещё не
подлежащие удалению». Первое: `running_queries` — найти бэкенд с минимальным
xmin (длинная транзакция, idle-in-transaction, брошенный слот репликации)
и завершить/починить.

## wal_checkpoint (вес 0.10)

### requested_checkpoint_ratio
Доля внеплановых (requested) чекпоинтов; оценивается после ≥10 чекпоинтов.
LOW ≥5%, MED ≥10%, HIGH ≥20%. WAL заполняет max_wal_size раньше
checkpoint_timeout: увеличить max_wal_size. В здоровой системе почти все
чекпоинты — timed.

### wal_level_minimal_with_replicas
wal_level=minimal при подключённых репликах. Всегда HIGH: streaming-репликация
не работает — установить wal_level=replica (нужен рестарт).

### wal_level_logical_without_publications
wal_level=logical без активных логических слотов. Всегда LOW: чистый оверхед
WAL — переключить на replica, если логическая репликация не планируется.
Подавляется на managed-платформах (например Yandex MDB), где wal_level
зафиксирован провайдером.

## locks (вес 0.10)

### active_lock_waiters
Бэкенды, ждущие блокировку сейчас. LOW ≥1, MED ≥3, HIGH ≥10.
Первое: `blocked_queries` — пройти дерево блокировок, разбираться с
блокирующим (жертву не убивать).

### longest_lock_wait_seconds
Самое долгое текущее ожидание. LOW ≥10с, MED ≥30с, HIGH ≥60с. Типичные
причины: длинная транзакция, контеншн на горячей строке, блокировка таблицы
от DDL без CONCURRENTLY.

### ungranted_locks
Строки pg_locks с granted=false (длина очереди). LOW ≥2, MED ≥5, HIGH ≥15.

### deadlocks_rate
Дедлоки с момента pg_stat_database_reset. LOW при >0. Любой дедлок — баг
приложения (разный порядок захвата блокировок) — детали в логе сервера
(`search_logs` с пресетом на кластерах Yandex MDB).

### lock_pool_saturation
Заполнение пула тяжёлых блокировок (max_locks_per_transaction × max_connections).
LOW ≥50%, MED ≥60%, HIGH ≥80%. Переполнение даёт «out of shared memory» —
увеличить max_locks_per_transaction (рестарт) или снизить конкурентность.
