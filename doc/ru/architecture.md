# Архитектура

[English version](../en/architecture.md) · [← README](../../README.ru.md)

```mermaid
flowchart LR
    SPA["Vue 3 SPA (Vuetify)<br/>:3000"]
    LLM["AI-ассистент<br/>(MCP-клиент)"]
    MCP["dasha-mcp<br/>MCP-сервер<br/>(HTTP/SSE :8765)"]
    BE["Go-бэкенд (Echo)<br/>:8000"]
    AS["Демон<br/>dasha autosnapshot"]
    PG[("Кластеры PostgreSQL<br/>14 – 18")]
    ST[("Хранилище снимков<br/>pgss, PAT, веса HS")]
    TS[("Prometheus /<br/>VictoriaMetrics")]
    YC["Yandex Cloud API<br/>MDB discovery · логи"]

    SPA -->|/api| BE
    LLM -->|stdio / HTTP| MCP
    MCP -->|REST + X-API-Key| BE
    BE --> PG
    BE --> ST
    BE -.->|health score<br/>на метриках| TS
    BE -.->|discovery,<br/>поиск по логам| YC
    AS --> PG
    AS --> ST
```

**API-first**: спецификация OpenAPI 3.0 (`doc/swagger.yaml`) — единственный источник истины. Серверные заглушки и клиент фронтенда генерируются из неё.

| Слой | Стек |
|------|------|
| Фронтенд | Vue 3, Vuetify 3, Pinia, TanStack Vue Query, vue-i18n, Vite |
| Бэкенд | Go 1.26, Echo v4, pgx v5, Casbin, gorilla/securecookie, coreos/go-oidc, Viper, Cobra, Zap, samber/do |
| Кодогенерация | oapi-codegen (Go-сервер), orval (TypeScript-клиент) |
| Тестирование | Vitest, Playwright, testcontainers-go (матрица PG 14-18) |

