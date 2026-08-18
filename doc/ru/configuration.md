# Конфигурация

[English version](../en/configuration.md) · [← README](../../README.ru.md)

## Требования

- Go 1.26+
- Node.js 22+ и npm
- PostgreSQL 14+ (целевые базы данных)
- Docker и Docker Compose (для демо-лаборатории)

## Файл конфигурации

Создайте файл `dasha.yaml` (ищется в `.`, `$HOME/.dasha/`, `/etc/dasha/`):

```yaml
debug: false
# pg_stats_view: monitoring.pg_stats  # кастомная view, если у пользователя нет доступа к pg_catalog.pg_stats
# view должна содержать schemaname, tablename, attname, null_frac, n_distinct, avg_width;
# иначе Dasha пишет предупреждение и использует pg_catalog.pg_stats.
# inherited необязателен: без него рекомендации по индексам работают, но не смогут
# предпочесть наследуемую статистику партиционированной таблицы
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

Роли достаточно `pg_monitor` и `CONNECT` на наблюдаемые базы: базы, к которым подключиться нельзя,
просто не попадают в список. Шаблонные базы и базы с запрещёнными подключениями не перечисляются
никогда. Хосты перебираются по порядку, ответивший запоминается и используется первым в следующем
цикле, поэтому один недоступный хост ничего не стоит; пока не отвечает ни один — сохраняется
последний известный список.

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
