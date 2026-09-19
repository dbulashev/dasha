# Конфигурация

[English version](../en/configuration.md) · [← README](../../README.ru.md)

## Требования

- Go 1.26+
- Node.js 22+ и npm
- PostgreSQL 14+ или Postgres Pro (вместо `pg_stat_statements` используется `pgpro_stats`)
- Docker и Docker Compose для демо-лаборатории

## Роль мониторинга

Dasha только читает базы. Сброс статистики запросов по умолчанию выключен.

Роли мониторинга нужны `pg_monitor` и `CONNECT` на наблюдаемые базы:

```sql
CREATE ROLE monitoring_user LOGIN PASSWORD 'secret';
GRANT pg_monitor TO monitoring_user;
GRANT CONNECT ON DATABASE myapp TO monitoring_user;
```

Без `pg_read_all_stats` (входит в `pg_monitor`) отчёт по запросам и Top 10 показывают только запросы
самой роли мониторинга.

`pg_stat_statements` (на Postgres Pro `pgpro_stats`) должен быть в `shared_preload_libraries` и
создан хотя бы в одной базе инстанса. Базу и схему расширения Dasha находит сама.

```sql
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
```

Проверка исчерпания последовательностей читает `last_value`, для неё нужно право `SELECT` на
последовательности:

```sql
GRANT SELECT ON ALL SEQUENCES IN SCHEMA public TO monitoring_user;
```

На странице проверок схемы у пропущенных строк указана недостающая привилегия.

### Статистика колонок без доступа к таблицам

`pg_stats` показывает статистику только тех колонок, которые роль может читать. Без `SELECT` на
таблицы раздел «Таблицы», оценка раздувания индексов и рекомендации по индексам работают без
статистики колонок.

Если выдать `SELECT` нельзя, суперпользователь создаёт в каждой наблюдаемой базе представление над
`pg_statistic`:

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

- На PostgreSQL 15+ представление создаётся без `security_invoker = true`.
- Представление над `pg_catalog.pg_stats` не сработает.
- Обязательные колонки: `schemaname`, `tablename`, `attname`, `null_frac`, `n_distinct`,
  `avg_width`. С необязательной `inherited` рекомендации по индексам учитывают все партиции
  партиционированной таблицы.
- `pg_stats_view` задаётся как `schema.name` без кавычек.

Если представление недоступно или в нём нет обязательной колонки, Dasha пишет предупреждение в лог и
читает `pg_catalog.pg_stats`.

Значений из таблиц (`most_common_vals`, `histogram_bounds`) в представлении нет.

### Сброс статистики запросов без суперпользователя

Кнопка сброса вызывает `pg_stat_statements_reset()` (на Postgres Pro
`pgpro_stats_statements_reset()`) и сбрасывает статистику всего инстанса. Если у роли мониторинга нет
`EXECUTE` на эту функцию, суперпользователь создаёт обёртку:

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

- Обёртку создают в базе с расширением, функцию расширения вызывают со схемой (в примере `public`).
- У `PUBLIC` не должно быть `CREATE` на схеме расширения. На PostgreSQL 14 это право отзывают
  командой `REVOKE CREATE ON SCHEMA public FROM PUBLIC` или ставят расширение в схему, где создавать
  объекты может только суперпользователь.
- Функция вызывается без аргументов, возвращаемое значение не используется.
- `pgss_reset_function` задаётся как `schema.name` без кавычек. При неверном имени Dasha пишет
  предупреждение и вызывает функцию расширения.

Без `enable_query_stats_reset: true` кнопки сброса нет.

## Файл конфигурации

Dasha ищет `dasha.yaml` в текущем каталоге, `$HOME/.dasha/` и `/etc/dasha/`:

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
        - name: "prod-.*"       # регулярное выражение
          exclude_name: "test"
          exclude_db: "system_db"
```

## Автообнаружение баз в кластере (опционально)

Вместо списка `databases` Dasha получает список баз из кластера и обновляет его раз в
`refresh_interval`:

```yaml
discovery:
  onprem_prod:                    # имя записи = имя кластера (в нижнем регистре)
    type: postgres
    config:
      hosts: [pg-01.local, pg-02.local]   # мастер и реплики
      port: 5432                  # по умолчанию 5432
      user: dasha
      password: secret            # или password_from_env: DASHA_PG_PASSWORD
      bootstrap_db: postgres      # база, из которой читается список баз
      refresh_interval: 5         # минуты, по умолчанию 5
      db: ".*"                    # регулярное выражение
      exclude_db: "(template.*)"
```

- В список не попадают шаблонные базы, базы с `datallowconn = false` и базы без `CONNECT` для роли.
- Соединения с удалённой базой закрываются.
- Хосты опрашиваются по порядку, первым идёт хост, ответивший в прошлый раз. Если не ответил ни один,
  остаётся прежний список.
- На каждую пару «хост + база» открывается свой пул соединений. Для кластера с десятками баз сузьте
  список через `db` и `exclude_db` и проверьте `db_pool.max_conns`.

## Поиск по логам (опционально)

Страница `/logs` ищет по существующему хранилищу логов. Dasha логи не собирает.

Для кластеров из сервис-дискавери Yandex MDB поиск работает без настройки, с ключом сервисного
аккаунта дискавери. Для остальных кластеров источник описывают в `log_search.sources` и указывают по
имени в `log_source` кластера.

```yaml
log_search:
  max_scan: 5000          # сколько записей просматривать за поиск
  max_page_size: 1000     # максимальный page_size
  timeout_seconds: 30     # таймаут чтения из хранилища
  rate_limit:             # на пользователя (для анонимных на IP); rps <= 0 отключает лимит
    requests_per_second: 0.0333   # 1 запрос в 30 с
    burst: 10
  admin_rate_limit:
    requests_per_second: 0.2      # 1 запрос в 5 с
    burst: 20
```

### Источники OpenSearch

Нужен `log_destination = jsonlog` (PostgreSQL 15+) или `csvlog`, агент доставки раскладывает запись
по полям. Индексы с сырой строкой лога не поддерживаются.

```yaml
log_search:
  default_source: main          # для кластеров без log_source
  sources:
    main:
      type: opensearch
      addresses: ["https://os-1.example.net:9200"]
      auth:
        kind: basic             # none | basic | api_key
        user: dasha
        password_from_env: OS_PASSWORD
      tls:
        ca_file: /etc/dasha/os-ca.pem
        insecure_skip_verify: false
      batch_size: 1000          # записей за запрос к хранилищу
      max_boundary_ids: 10000   # сколько записей с одной меткой времени помнит курсор, дальше поиск
                                # останавливается; от batch_size до max_result_window индекса
      rate_limit:               # вместо глобальных лимитов для этого источника
        requests_per_second: 1
        burst: 20
      streams:
        postgresql:
          index: "pg-logs-{{ .Cluster }}-*"
          selector:             # term-фильтр, если все кластеры пишут в один индекс
            cluster: "{{ .Cluster }}"
          field_map:
            preset: jsonlog     # jsonlog | csvlog | odyssey | pgbouncer | none
            timestamp: "@timestamp"
            host: host.name
            host_match: suffix  # exact (по умолчанию) или suffix, если в индексе FQDN
            keyword_fields:     # keyword-поле для анализируемого поля
              error_severity: error_severity.keyword
              host.name: host.name.keyword
        pooler:
          index: "pgbouncer-logs-*"
          field_map:
            preset: pgbouncer
            timestamp: "@timestamp"
            host: host.name
            severities: [NOISE, LOG, WARNING, ERROR, FATAL]  # вместо списка уровней пресета
            mask: [msg, query]  # дополнительные поля для маскирования

clusters:
  - name: prod
    log_source: main
```

CA в Helm-чарте подключается томом бэкенда:

```yaml
# values.yaml; kubectl create secret generic dasha-os-ca --from-file=ca.pem=os-ca.pem
backend:
  extraVolumes:
    - name: os-ca
      secret:
        secretName: dasha-os-ca
  extraVolumeMounts:
    - name: os-ca
      mountPath: /etc/ssl/opensearch   # ca_file: /etc/ssl/opensearch/ca.pem
      readOnly: true
```

`/etc/dasha` в чарте занят ConfigMap с `dasha.yaml`, сертификат монтируйте в другой каталог.

### Источники VictoriaLogs

Агент доставки раскладывает запись по полям. Строка лога целиком в `_msg` не поддерживается.

```yaml
log_search:
  sources:
    vlogs:
      type: victorialogs
      addresses: ["https://vlogs.example.net:9428"]
      auth:
        kind: bearer            # none | basic | api_key | bearer
        token_from_env: VL_TOKEN
      tenant:                   # по умолчанию 0/0
        account_id: 12
        project_id: 34
      batch_size: 1000          # записей за запрос
      max_boundary_ids: 1000    # сколько записей с одной меткой времени помнит курсор
      streams:
        postgresql:
          stream_selector:      # -> {cluster="prod"}
            cluster: "{{ .Cluster }}"
          selector:             # -> "app":="postgres"
            app: postgres
          query: '*'            # дополнительное выражение LogsQL
          field_map:
            preset: jsonlog
            timestamp: _time
            text: _msg
            host: host

clusters:
  - name: prod
    log_source: vlogs
```

### Общее для внешних источников

Источник кластера выбирается по порядку: `log_source` кластера, встроенный источник Yandex MDB для
кластеров из сервис-дискавери, `default_source`.

При запуске проверяется:

- источник из `log_source` описан в `sources`;
- в `sources` нет имени `yandex-mdb`, оно занято встроенным источником;
- в источнике только потоки `postgresql` и `pooler`;
- при `auth.kind`, отличном от `none`, все адреса начинаются с `https://`, `tls.insecure_skip_verify`
  выключен;
- в потоке OpenSearch нет `query` и `stream_selector`;
- в потоке VictoriaLogs нет `index` и задан хотя бы один из `query`, `selector`, `stream_selector`.

В `index`, `query` и значениях `selector` и `stream_selector` подставляется `{{ .Cluster }}`. Других
переменных, в том числе для хоста, нет.

Пресет задаёт имена полей формата лога, поля из `field_map` его переопределяют. `timestamp` и `host`
в пресеты не входят, их указывают всегда.

| Пресет      | `query_id` | `sql_state`      | `severities`                                             |
|-------------|------------|------------------|----------------------------------------------------------|
| `jsonlog`   | `query_id` | `state_code`     | `DEBUG, LOG, INFO, NOTICE, WARNING, ERROR, FATAL, PANIC` |
| `csvlog`    | `query_id` | `sql_state_code` | `DEBUG, LOG, INFO, NOTICE, WARNING, ERROR, FATAL, PANIC` |
| `odyssey`   | нет        | нет              | `debug, info, warning, error, fatal`                     |
| `pgbouncer` | нет        | нет              | `NOISE, DEBUG, LOG, WARNING, ERROR, FATAL`               |

`query_id` и `sql_state` необязательны. `query_id` PostgreSQL пишет при `compute_query_id = on`.
Сводка по логам определяет категорию события сначала по `sql_state`, потом по тексту сообщения.

`severities` перечисляет уровни в том написании, в котором они лежат в хранилище. Поиск принимает
только эти значения, фильтр уровней на странице логов показывает этот же список.

В хранилище фильтруются только уровень и хост. Сообщение, базу и пользователя Dasha фильтрует сама.
Поле `text` может быть анализируемым. В OpenSearch поля уровня, хоста и `selector` сравниваются
точно и должны быть `keyword`. Если поле анализируемое (так его создаёт динамический маппинг),
укажите его keyword-вариант в `keyword_fields`, иначе фильтр ничего не найдёт.

Из `text` всегда удаляются учётные данные, `mask` добавляет к нему другие поля. Если `text` вложенный
(`text: pg.message`), имя без точки в `mask` маскирует оба поля: `detail` и `pg.detail`.

Поток, которого нет в источнике, недоступен: API отвечает 501, переключатель в интерфейсе скрыт.

`GET /api/logs/check?cluster_name=…&service_type=…` (только администратор) проверяет источник и
возвращает:

- индекс или выражение LogsQL после подстановки `{{ .Cluster }}`;
- число записей за последний час;
- найденные и недостающие поля маппинга с их типами;
- одну маскированную запись.

### Сводка по логам

`GET /api/logs/insights` за выбранный интервал возвращает:

- число событий по категориям: блокировки, контрольные точки, автоочистка, ошибки и другие;
- планы `auto_explain`, сгруппированные по запросу и структуре плана.

Источник, `timeout_seconds` и `rate_limit` те же, что у поиска по логам. Лимиты на один запрос задаёт
секция `log_insights`:

```yaml
log_insights:
  enabled: true           # при false эндпоинт отвечает 404
  max_records: 50000      # сколько записей читать за запрос
  max_bytes: 67108864     # сколько байт читать за запрос
  max_plan_bytes: 2097152 # план крупнее учитывается, но не разбирается
  max_plans: 5000         # сколько планов разбирать за запрос
  compare_rate_limit:     # GET /api/logs/plans/compare, на пользователя
    requests_per_second: 0.016
    burst: 3
  index_advisor_evidence: false  # подтверждать кандидаты индексов планами из логов
```

Если лимит исчерпан до конца интервала, в ответе указан отрезок, за который собрана сводка: для
VictoriaLogs последние записи, для OpenSearch первые. При исчерпании `max_plans` отрезок у планов
короче, чем у категорий.

`GET /api/logs/plans` возвращает только планы — за весь интервал или по одному `query_id`. Хранилищу
логов передаётся сужающий фильтр: уровень, которым пишет `auto_explain`, затем идентификатор запроса,
затем слово `plan`. Уровень передаётся, только когда `auto_explain.log_level` прочитан у всех хостов,
которые читает скан. Поле `narrowed_by` в ответе перечисляет то, что хранилище выполнило само;
фильтр, который оно выполнить не может, не передаётся, и Dasha читает более широкое окно. Записи,
которые вернулось хранилище, в любом случае фильтруются на стороне Dasha. Если у потока логов нет
роли `query_id`, запрос по `query_id` отвечает 400: какие роли отсутствуют, показывает
`GET /api/logs/check`.

При `index_advisor_evidence: true` кандидат в отчёте «Рекомендации по индексам» приходит с числом
планов за последний час, где его таблицу читают последовательно, временем этих узлов и числом
отброшенных фильтром строк. Кандидат, у которого таких планов не нашлось, и кандидат, у которого
планы не смотрели, различаются в ответе: второе не довод против индекса. Если за окно auto_explain не
записал ни одного плана, это второй ответ, а не первый. Если план ошибся в оценке числа строк этой
таблицы, кандидат получает предупреждение `stale_statistics` — сначала `ANALYZE`, потом индекс.
Глубину окна задаёт `index_advisor.evidence_window` (по умолчанию `1h`). По умолчанию
подтверждение выключено: с ним время ответа отчёта зависит от хранилища логов.

`GET /api/logs/plans/compare` читает два интервала и перечисляет запросы, у которых план изменился к
худшему: появилась структура плана, которой не было в базовом интервале, пропал индекс, который
базовый интервал читал, либо p95 вырос вдвое и больше. Рост p95 учитывается, только когда в каждом
интервале набралось не меньше 20 планов самой затратной структуры. Запрос, который встретился только
в одном из интервалов, в список не попадает. Длительности каждой стороны взяты у той структуры плана, на которую
в этом интервале ушло больше всего времени; если хотя бы один интервал прочитан не целиком, ответ
помечен `partial`. Текущий интервал задают `from` и `to` либо `scan_id` уже сохранённого скана.
Базовый интервал читается, только когда в текущем нашёлся хотя бы один план; при `scan_id` он
читается по тому хосту, который покрывает сохранённый скан, а `host` с другим значением даёт 400.
Один запрос — это два чтения хранилища логов: он расходует и `rate_limit` своего источника, как
остальные эндпоинты логов, и сверх того `compare_rate_limit`. Оба чтения вместе укладываются в
`timeout_seconds`, отведённый одному.

Блок `configuration` в ответе показывает, что Dasha прочитала в `pg_settings` хоста, названного в
поле `instance`: загружен ли `auto_explain`, его `auto_explain.log_min_duration`,
`auto_explain.log_analyze`, `auto_explain.log_format`, `auto_explain.log_level` и `compute_query_id`.
Кластер, который не ответил, оставляет блок пустым, а скан выполняется.

Для планов нужны `auto_explain` в `shared_preload_libraries` и `auto_explain.log_format` `text` или
`json`. В сводку попадают запросы дольше `auto_explain.log_min_duration`. Проверки плана по
фактическим строкам и времени требуют `auto_explain.log_analyze = on`. Привязка плана к запросу
требует `compute_query_id = on` и роли `query_id` у потока логов. В планах маскируются только
учётные данные, литералы остаются.

Длинный план может не дойти до Dasha:

- VictoriaLogs отбрасывает строки длиннее `-insert.maxLineSizeBytes` (по умолчанию 256 КиБ,
  максимум 2 МБ);
- `tail` в Fluent Bit с `Skip_Long_Lines` пропускает строки длиннее `Buffer_Max_Size`.

Если агент доставки обрезает строки, текстовый план приходит неполным, а json-план не разбирается.

Когда настроено хранилище снимков (`storage.dsn`), каждый скан сохраняется снимком, а в ответе
появляется `scan_id`. `GET /api/logs/scans/{scan_id}` повторяет те же числа, `.../groups` отдаёт все
группы планов (в самой сводке — только верх списка), `.../groups/{ord}` — дерево одного плана. Логи
при этом не читаются заново, поэтому уточнение бесплатно и не расходится со сводкой. Без хранилища
эти три эндпоинта отвечают 501, а `scan_id` в сводке нет.

Демон автоснимков очищает снимки раз в сутки, первый раз — при старте. Значит, `scan_id` живёт не
дольше суток, после чего отвечает 404 и клиенту нужен новый скан. Без запущенного демона таблицы
`log_insights_scans` и `log_insights_groups` не очищаются. Литералы запросов в сохранённом плане не
маскируются и до очистки доступны любому пользователю с правами viewer.

## Рекомендации по индексам (опционально)

Отчёт «Рекомендации по индексам» работает без настройки. Глобальный блок `index_advisor` ограничивает
объём работы и вид кандидатов:

```yaml
index_advisor:
  enabled: true            # при false эндпоинт отвечает 404
  max_queries: 500         # сколько запросов читать на отчёт, по убыванию общего времени
  max_query_bytes: 102400  # запрос длиннее не разбирается
  max_candidates: 50       # сколько кандидатов попадает в отчёт
  max_index_columns: 3     # сколько колонок в ключе кандидата; предел — 4
  min_table_rows: 10000    # по таблице меньше кандидат не предлагается
  parse_cache_size: 1000   # сколько разобранных запросов хранить между отчётами
  timeout: 60s             # ограничивает построение одного отчёта
  evidence_window: 1h      # окно, за которое читаются планы из логов под кандидата
```

`evidence_window` читается только при включённом `log_insights.index_advisor_evidence`. MCP-сервер
запрашивает этот отчёт со своим `--slow-timeout` (по умолчанию `90s`): если `timeout` больше,
он не сработает — вызов MCP прекратится раньше.

## Проверки схемы (опционально)

Страница `/schema-lint` работает без настройки. Секция `schema_lint` необязательна:

```yaml
schema_lint:
  disabled_checks: [uuid_in_non_uuid_type]   # не запускать
  enabled_checks: [relation_without_fk]      # включить выключенные по умолчанию
  ignore_schemas: ["_timescaledb*", "cron"]  # маски; системные схемы исключены всегда
  sequence_thresholds:                       # процент оставшихся значений
    error: 5
    warning: 10
    notice: 20
  sequence_cache_ttl: 15m                    # кеш худшей последовательности для Health Score
```

Аутентификация и персональные токены описаны в [auth.md](auth.md), хранилище снимков и автоснимки
в [autosnapshot.md](autosnapshot.md).
