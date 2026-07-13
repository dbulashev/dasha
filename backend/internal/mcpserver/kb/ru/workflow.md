# Сценарии диагностики

Жалоба → цепочка инструментов. Выполняйте шаги по порядку, один tool на шаг;
останавливайтесь раньше, если причина найдена. Сначала возьмите
(cluster, instance) из `list_clusters` — per-database инструментам нужна ещё
`database`.

## «База тормозит»
1. `get_health_score` — score ≥80: смотреть в приложение, не в БД (доложить
   и остановиться). Иначе зафиксировать 2 худшие категории по штрафу.
2. `get_health_recommendations` — сначала HIGH; незнакомые rule ID смотреть
   в dasha://kb/health-rules. Он называет правило, а не виновника: следом
   вызвать `health_details` (detail = этот rule_id) и получить сами таблицы.
3. `top_queries` (by=time) — мало calls × высокий mean_time = проблема плана
   (предложить EXPLAIN, индексы); огромные calls × низкий mean_time =
   проблема частоты (кэширование/батчинг).
4. `wait_events` — доминирующее событие указывает класс узкого места
   (dasha://kb/wait-events).
5. Если плохи storage/maintenance: `vacuum_danger`.

## «Всё висит / запросы застряли»
1. `blocked_queries` — картина: кто кого блокирует.
2. `running_queries` — найти корневого блокировщика (часто idle in
   transaction или очень старая транзакция).
3. Рекомендовать завершить БЛОКИРОВЩИКА (pg_terminate_backend); жертв не
   завершать — блокировку это не снимет. Предложить
   idle_in_transaction_session_timeout / lock_timeout.

## «Кончается место на диске»
1. `get_health_recommendations` — правила host_disk_space / bloat, затем
   `health_details` (detail=high_dead_ratio_tables) — назвать распухшие таблицы;
   если vacuum не может их очистить, detail=horizon_blocking_sessions покажет,
   кто держит горизонт xmin.
2. `get_replication` — неактивные слоты копят WAL (классический тихий пожиратель).
3. `top_tables` — крупнейшие таблицы; подозрительные — `describe_table`
   (секция bloat).
4. На кластерах Yandex MDB: `search_logs` (service_type=postgresql,
   message=["checkpoint"]) — только если нужно подтвердить WAL-чехарду.

## «Реплика отстаёт»
1. `get_replication` — какая реплика, насколько (время и байты), состояние слота.
2. `get_instance_info` по реплике — подтвердить recovery.
3. `running_queries` на реплике — долгие SELECT конфликтуют с применением WAL.
4. `wait_events` на мастере — давление на запись WAL тоже раздувает lag.

## «Приложение сыплет ошибками»
1. Только кластеры Yandex MDB (`supports_logs` в list_clusters):
   `search_logs` с severity=["ERROR","FATAL"], dedup включён, узкое окно
   (since="1h"). Один вызов со всеми фильтрами — эндпоинт rate-limited.
2. Сопоставить шаблоны ошибок с `blocked_queries` (дедлоки, lock timeout)
   и `get_health_recommendations`.

## Триаж флота
1. `fleet_health` — худшие инстансы первыми (один вызов, не перебирать
   кластеры циклом).
2. Для худших 1-2 инстансов — сценарий «База тормозит».

## Правила бережности (всегда)
- Рекомендация — ещё не цель. `get_health_recommendations` даёт rule_id и
  счётчик/долю; `health_details` превращает это в объекты — верните этот rule_id
  прямо в `detail`. Потабличным drill-down (tables_autovacuum_off,
  low_hot_update_tables, high_dead_ratio_tables) нужна ещё `database`; wraparound
  и горизонт xmin — по инстансу. Не угадывать имя таблицы — спросить.
- `search_logs` лимитирован per-user (~1 запрос / 30с по умолчанию):
  собрать все фильтры в ОДИН вызов, держать dedup, не поллить; после 429
  ждать ≥30с.
- Один вызов инструмента на шаг; не повторять вызов с теми же аргументами.
- Если результат отклонён как слишком большой — сузить (одна база, меньший
  limit, короче окно), не повторять как есть.
- health_trend требует режима метрик; 404/ошибка там — не проблема инстанса.
- Формат отчёта: 3-5 находок, каждая = факт (числа из инструментов) +
  причина + одно конкретное действие, худшее первым. Не выдумывать метрики,
  которых нет в выводе инструментов.
