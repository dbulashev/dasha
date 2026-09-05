# Конфигурация

[English version](../en/configuration.md) · [← README](../../README.ru.md)

## Требования

- Go 1.26+
- Node.js 22+ и npm
- PostgreSQL 14+ (целевые базы данных); поддерживается Postgres Pro — вместо `pg_stat_statements` автоматически используется `pgpro_stats`
- Docker и Docker Compose (для демо-лаборатории)

## Роль мониторинга

Dasha только читает базы и ничего в них не меняет; единственное исключение — сброс статистики
запросов, он выключен по умолчанию. Роль, под которой Dasha подключается, должна иметь право
подключения к каждой наблюдаемой базе; с `pg_monitor` ей видны и чужие запросы:

```sql
CREATE ROLE monitoring_user LOGIN PASSWORD 'secret';
GRANT pg_monitor TO monitoring_user;
GRANT CONNECT ON DATABASE myapp TO monitoring_user;
```

Расширение `pg_stat_statements` (на Postgres Pro — `pgpro_stats`) должно быть перечислено в
`shared_preload_libraries` и создано хотя бы в одной базе инстанса — статистика в нём общая для
всего инстанса, а нужную базу и схему Dasha находит сама:

```sql
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
```

`pg_monitor` включает в себя `pg_read_all_stats`. Без него `pg_stat_statements` не покажет роли ни
идентификатор, ни текст чужих запросов: в отчёте по запросам и в панелях Top 10 останутся только
запросы самой роли мониторинга, о чём страница предупредит над таблицами.

Сверх `pg_monitor` права нужны проверке исчерпания последовательностей — она читает `last_value`,
поэтому выдайте `GRANT SELECT ON ALL SEQUENCES IN SCHEMA public TO monitoring_user`. Какого гранта
не хватило, страница проверок схемы пишет прямо в пропущенных строках. Ещё два случая разобраны
ниже.

### Статистика колонок без доступа к таблицам

`pg_catalog.pg_stats` показывает только те колонки, которые роль имеет право читать, а доступ к
пользовательским таблицам `pg_monitor` не даёт. Без `SELECT` на таблицы страницы таблиц, оценка
раздувания индексов и рекомендации по индексам останутся без статистики колонок. Если открывать
таблицы роли нельзя, суперпользователь создаёт представление над `pg_statistic`, и Dasha берёт
статистику оттуда:

```sql
CREATE SCHEMA IF NOT EXISTS monitoring;

CREATE VIEW monitoring.pg_stats AS
SELECT n.nspname     AS schemaname,
       c.relname     AS tablename,
       a.attname     AS attname,
       s.stainherit  AS inherited,
       s.stanullfrac AS null_frac,
       s.stawidth    AS avg_width,
       s.stadistinct AS n_distinct
FROM pg_catalog.pg_statistic s
    JOIN pg_catalog.pg_class c ON c.oid = s.starelid
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid AND a.attnum = s.staattnum
WHERE NOT a.attisdropped;

GRANT USAGE ON SCHEMA monitoring TO monitoring_user;
GRANT SELECT ON monitoring.pg_stats TO monitoring_user;
```

```yaml
pg_stats_view: monitoring.pg_stats
```

Представление читает `pg_statistic` с правами владельца — за счёт этого статистика видна роли,
которой сами таблицы закрыты. Поэтому создавать его должен суперпользователь, и на PostgreSQL 15 и
выше у него не должно быть `security_invoker = true`. Обёртка над `pg_catalog.pg_stats` не поможет:
фильтр внутри `pg_stats` проверяет права вызывающего, а не владельца. Каталог `pg_statistic` в
каждой базе свой, поэтому представление создаётся в каждой наблюдаемой базе.

Колонки `schemaname`, `tablename`, `attname`, `null_frac`, `n_distinct` и `avg_width` обязательны.
Необязательная `inherited` нужна рекомендациям по индексам: с ней для партиционированной таблицы
берётся статистика по всем партициям сразу. Имя из `pg_stats_view` подставляется в запросы как есть,
поэтому оно должно быть вида `schema.name` и без кавычек. При первом обращении Dasha проверяет
представление и, если оно недоступно или в нём не хватает колонки, пишет предупреждение в лог и
возвращается к `pg_catalog.pg_stats`.

Сами данные через такое представление не видны: в нём есть доля NULL, средняя ширина и число
различных значений колонки, но нет ни самых частых значений, ни границ гистограммы, которые
показывает `pg_stats`.

### Сброс статистики запросов без суперпользователя

Кнопка сброса вызывает `pg_stat_statements_reset()` (на Postgres Pro —
`pgpro_stats_statements_reset()`) и обнуляет статистику всего инстанса. Если роли мониторинга не
выдан `EXECUTE` на эту функцию, суперпользователь заводит обёртку, а Dasha вызывает её:

```sql
CREATE SCHEMA IF NOT EXISTS monitoring;
GRANT USAGE ON SCHEMA monitoring TO monitoring_user;

CREATE FUNCTION monitoring.reset_pgss() RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
BEGIN
    PERFORM public.pg_stat_statements_reset();
END;
$$;

REVOKE EXECUTE ON FUNCTION monitoring.reset_pgss() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION monitoring.reset_pgss() TO monitoring_user;
```

```yaml
enable_query_stats_reset: true
pgss_reset_function: monitoring.reset_pgss
```

Обёртку создают в той базе, где стоит расширение, а вызов внутри неё пишут со схемой этого
расширения — в примере это `public`. У `PUBLIC` не должно быть права `CREATE` на этой схеме:
либо `REVOKE CREATE ON SCHEMA public FROM PUBLIC` (в PostgreSQL 15 и новее так по умолчанию),
либо расширение ставят в схему, создавать в которой может только суперпользователь. Dasha
выполняет `SELECT monitoring.reset_pgss()` без аргументов и результат не читает, так что тип
возврата не важен. Имя функции тоже подставляется как есть и должно быть вида `schema.name` без
кавычек; при неверном имени Dasha пишет предупреждение и вызывает функцию самого расширения. Пока
`enable_query_stats_reset` выключен, кнопки сброса на странице нет.

## Файл конфигурации

Создайте файл `dasha.yaml` (ищется в `.`, `$HOME/.dasha/`, `/etc/dasha/`):

```yaml
debug: false
# pg_stats_view: monitoring.pg_stats  # см. «Статистика колонок без доступа к таблицам»
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

## Сервис-дискавери Yandex MDB (опционально)

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

## Дискаверинг баз данных в кластере (опционально)

Вместо ручного перечисления `databases` Dasha может спросить сам кластер и поддерживать список
актуальным: созданная база появляется в течение `refresh_interval`, удалённая — исчезает вместе
со своими соединениями:

```yaml
discovery:
  onprem_prod:                    # имя записи = имя кластера (в нижнем регистре)
    type: postgres
    config:
      hosts: [pg-01.local, pg-02.local]   # мастер и реплики
      port: 5432                  # по умолчанию 5432
      user: dasha
      password: secret            # или password_from_env: DASHA_PG_PASSWORD
      bootstrap_db: postgres      # база, к которой подключается запрос дискаверинга
      refresh_interval: 5         # минуты, по умолчанию 5
      db: ".*"                    # фильтр по regex
      exclude_db: "(template.*)"
```

Базы, к которым роль не может подключиться, просто не попадают в список. Шаблонные базы и базы с
запрещёнными подключениями не перечисляются никогда. Хосты перебираются по порядку, ответивший
запоминается и используется первым в следующем цикле, поэтому один недоступный хост ничего не
стоит; пока не отвечает ни один — сохраняется последний известный список.

Dasha открывает по пулу соединений на каждую пару «хост + база», поэтому на кластере с десятками
баз сузьте список через `db` / `exclude_db` и проверьте `db_pool.max_conns`.

## Поиск по логам (опционально)

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

## Проверки схемы (опционально)

Страница `/schema-lint` работает без настройки. Глобальная секция `schema_lint` гасит проверки и схемы
и подстраивает пороги последовательностей:

```yaml
schema_lint:
  disabled_checks: [uuid_in_non_uuid_type]   # не запускать эти проверки
  enabled_checks: [relation_without_fk]      # включить те, что выключены по умолчанию
  ignore_schemas: ["_timescaledb*", "cron"]  # маски; системные схемы отсекаются всегда
  sequence_thresholds:                       # процент оставшихся значений
    error: 5
    warning: 10
    notice: 20
  sequence_cache_ttl: 15m                    # TTL значения худшей последовательности для health score
```

Ещё две опциональные подсистемы настраиваются в том же файле, но описаны отдельно:

- аутентификация и персональные токены — [auth.md](auth.md)
- хранилище снимков и автоснимки — [autosnapshot.md](autosnapshot.md)
