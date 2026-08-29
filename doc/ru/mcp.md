# MCP-коннектор (dasha-mcp)

[English version](../en/mcp.md) · [← README](../../README.ru.md)

`dasha-mcp` — отдельный **read-only** [MCP](https://modelcontextprotocol.io)-сервер поверх Dasha API. Позволяет AI-ассистентам запрашивать диагностику флота PostgreSQL как tools/prompts, прокидывая токен каждого вызывающего в Dasha (RBAC сохраняется). Подходит любой MCP-совместимый клиент — Claude Desktop, Claude Code, Cursor, Continue, **opencode** и т.д.

- **Tools (30):** `list_clusters`, `fleet_health`, `get_instance_info`, `get_health_score`, `get_health_recommendations`, `health_details` (превращает рекомендацию в цель: передайте её `rule_id` как `detail` — вернутся сами таблицы, базы или сессии; потабличным drill-down нужна ещё `database`, инстанс-уровневым — нет), `health_trend`, `health_databases`, `top_queries` (по времени/WAL), `query_report`, `list_snapshots`, `query_compare`, `running_queries`, `blocked_queries`, `list_indexes` (missing/unused/usage), `unused_index_report` (вердикт по всему кластеру: можно ли удалить индекс — счётчик сканов взвешивается по всем хостам и по окну статистики, т.к. `idx_scan` не реплицируется, а счётчик без окна ничего не значит), `top_tables`, `schema_lint` / `schema_lint_summary` (дефекты структуры схемы по системному каталогу: кончающиеся последовательности, таблицы без первичного ключа, unlogged-объекты, схемы, где может создавать объекты PUBLIC — со списком `skipped`, называющим невыполнившиеся проверки, чтобы пропуск не приняли за чистый результат), `hot_tables` / `hot_indexes` (топ горячих объектов по классам метрик — чтения/записи/io — из суточных дельта-снимков, просуммированных по всем хостам кластера, с coverage-долей репрезентативности топа; требует snapshot-хранилище), `describe_table`, `get_replication`, `settings_analyze`, `wait_events`, `connections`, `vacuum_danger`, `search_logs` (логи PostgreSQL/пулера из Yandex Cloud; только для кластеров из Yandex MDB discovery, с per-user rate limit), `io_summary` / `io_trend` (физический I/O по `pg_stat_io` в разрезе типа бэкенда, объекта и контекста и его динамика — единственные инструменты, которые видят I/O автовакуума, чекпойнтера и walwriter, а не только клиентских бэкендов; нужен PostgreSQL 16+ и snapshot-хранилище). Все помечены **read-only** и closed-world, чтобы совместимые клиенты показывали (и авто-аппрувили) их как безопасные. Сервер также отдаёт **инструкции** по использованию, которые подсказывают модели, какой tool/prompt выбрать.
- **Prompts (5):** `diagnose_cluster`, `explain_health_score`, `find_index_opportunities`, `investigate_slow_queries`, `fleet_overview` — линейные плейбуки: нумерованные шаги, один tool на шаг, с критерием трактовки на каждом (рассчитаны на модели без глубокой экспертизы PostgreSQL; сильные модели просто проходят их быстрее).
- **Resources (5):** встроенная база знаний, которую модель читает по запросу — `dasha://kb/health-rules` (каждое правило health score с порогами LOW/MED/HIGH и первыми действиями), `dasha://kb/schema-checks` (каждый код проверки схемы, его params и первое действие), `dasha://kb/wait-events` (глоссарий wait events), `dasha://kb/pg-stat-io` (как читать счётчики `pg_stat_io`), `dasha://kb/workflow` (сценарии «жалоба → цепочка инструментов» и правила бережности к API).
- **Язык:** `--lang en|ru` (или `DASHA_MCP_LANG`) выбирает язык базы знаний, плейбуков и инструкций; имена tools, схемы и результаты остаются английскими.

**Предусловие:** токен Dasha API — [персональный токен](auth.md#персональные-api-токены-опционально) (`dasha_pat_…`) или статический config-токен. Он определяет роль (`viewer` достаточно).

## Сборка

```bash
cd backend && go build -o dasha-mcp ./cmd/dasha-mcp
# либо образ:
docker build -f deploy/images/Dockerfile.mcp -t dasha-mcp .
```

## stdio (локально — Claude Desktop / Claude Code / opencode / Cursor)

Клиент сам запускает бинарь и общается через stdin/stdout; токен — переменная окружения `DASHA_MCP_TOKEN`.

**Claude Desktop** (`claude_desktop_config.json`) или **Cursor** (`.cursor/mcp.json`):

```json
{
  "mcpServers": {
    "dasha": {
      "command": "/path/to/dasha-mcp",
      "args": ["--dasha-url", "http://localhost:8000"],
      "env": { "DASHA_MCP_TOKEN": "dasha_pat_…" }
    }
  }
}
```

**Claude Code:**

```bash
claude mcp add dasha --env DASHA_MCP_TOKEN=dasha_pat_… -- /path/to/dasha-mcp --dasha-url http://localhost:8000
```

**opencode** (`opencode.json` или `~/.config/opencode/opencode.json`):

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "dasha": {
      "type": "local",
      "command": ["/path/to/dasha-mcp", "--dasha-url", "http://localhost:8000"],
      "enabled": true,
      "environment": { "DASHA_MCP_TOKEN": "dasha_pat_…" }
    }
  }
}
```

## HTTP/SSE (общий / мультипользовательский)

Запускается как сервис; **каждый запрос несёт свой токен** (общего серверного токена нет), поэтому RBAC сохраняется поперсонально:

```bash
dasha-mcp --http :8765 --dasha-url http://dasha-backend:8000
# контейнер:
docker run -p 8765:8765 dasha-mcp --http :8765 --dasha-url http://dasha-backend:8000
```

Удалённый MCP-клиент указывает `http://<host>:8765` и шлёт токен в `Authorization: Bearer dasha_pat_…` (или `X-API-Key`). Например, **opencode**:

```json
{
  "mcp": {
    "dasha": {
      "type": "remote",
      "url": "http://localhost:8765",
      "enabled": true,
      "headers": { "Authorization": "Bearer dasha_pat_…" }
    }
  }
}
```

Сервер read-only (мутирующих эндпоинтов нет) и работает под non-root. Хардненинг: размер ответа tool ограничен (слишком большой результат отклоняется с подсказкой сузить запрос, а не режется в невалидный JSON); кэш серверов по токену хэширован и ограничен; токены не логируются. В общем HTTP-развёртывании ставьте за TLS; rate-limit обеспечивает вышестоящий per-identity лимитер Dasha (каждый PAT — отдельная личность), поэтому он действует и через passthrough.

## Несколько экземпляров Dasha (окружения)

Каждое окружение (dev / stage / prod) — со своей Dasha и своим `dasha-mcp`. Регистрируйте их как отдельные MCP-серверы на клиенте — имя сервера играет роль неймспейса (tools, prompts и ресурсы `dasha://kb/*` клиент различает по серверу-источнику, URI не конфликтуют):

```json
"mcpServers": {
  "dasha-dev":  { "command": "dasha-mcp", "args": ["--dasha-url", "https://dasha.dev.example.com"],  "env": { "DASHA_MCP_TOKEN": "dasha_pat_…" } },
  "dasha-prod": { "command": "dasha-mcp", "args": ["--dasha-url", "https://dasha.prod.example.com"], "env": { "DASHA_MCP_TOKEN": "dasha_pat_…" } }
}
```

Персональные токены привязаны к экземпляру: PAT, выпущенный на dev, не действует на prod.

## Kubernetes (Helm)

В чарте есть опциональные, выключенные по умолчанию Deployment + Service для MCP (HTTP-режим). При включении сервер автоматически подключается к in-cluster бэкенду:

```yaml
# values.yaml
mcp:
  enabled: true
  port: 8765
  # dashaUrl: ""   # пусто = in-cluster Service {release}-backend
  # lang: ru       # язык базы знаний / плейбуков (по умолчанию en)
  # frontendProxy: true   # публиковать на <dasha-host>/mcp (по умолчанию); false = публикуете сами
```

HTTP-режим — строгий per-user passthrough: чарт намеренно не предлагает общий fallback-токен — каждый клиент присылает собственный credential в каждом запросе, RBAC и аудит остаются per-user.

Создаются `{release}-mcp` Deployment + `ClusterIP` Service на порту `8765`. По умолчанию nginx фронтенда проксирует `/mcp` на этот Service, поэтому коннектор доступен на том же хосте и TLS, что и дашборд — отдельный Ingress и сертификат не нужны:

```jsonc
{ "url": "https://dasha.example.com/mcp", "headers": { "Authorization": "Bearer dasha_pat_…" } }
```

SSE-поток проксируется без буферизации, токен клиента передаётся как есть — RBAC остаётся per-user. `mcp.frontendProxy: false` убирает проксирование, управляемое чартом, — публикуете эндпойнт сами: ставите перед Service `{release}-mcp` свой Ingress/Gateway (TLS терминируется там) либо открываете его напрямую через `mcp.service.type`. В headless-развёртывании (`frontend.enabled: false`) nginx нет, поэтому правило `/mcp` чарт добавляет прямо в Ingress/HTTPRoute.

