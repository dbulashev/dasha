# Architecture

[Русская версия](../ru/architecture.md) · [← README](../../README.md)

```mermaid
flowchart LR
    SPA["Vue 3 SPA (Vuetify)<br/>:3000"]
    LLM["AI assistant<br/>(MCP client)"]
    MCP["dasha-mcp<br/>MCP server :8765"]
    BE["Go backend (Echo)<br/>:8000"]
    AS["dasha autosnapshot<br/>daemon"]
    PG[("PostgreSQL clusters<br/>14 – 18")]
    ST[("Snapshot storage<br/>pgss, PAT, HS weights")]
    TS[("Prometheus /<br/>VictoriaMetrics")]
    YC["Yandex Cloud API<br/>MDB discovery · logs"]

    SPA -->|/api| BE
    LLM -->|stdio / HTTP| MCP
    MCP -->|REST + X-API-Key| BE
    BE --> PG
    BE --> ST
    BE -.->|metrics-backed<br/>health score| TS
    BE -.->|discovery,<br/>log search| YC
    AS --> PG
    AS --> ST
```

**API-first**: the OpenAPI 3.0 spec (`doc/swagger.yaml`) is the single source of truth. Backend stubs and frontend API client are generated from it.

| Layer | Stack |
|-------|-------|
| Frontend | Vue 3, Vuetify 3, Pinia, TanStack Vue Query, vue-i18n, Vite |
| Backend | Go 1.26, Echo v4, pgx v5, Casbin, gorilla/securecookie, coreos/go-oidc, Viper, Cobra, Zap, samber/do |
| Code generation | oapi-codegen (Go server), orval (TypeScript client) |
| Testing | Vitest, Playwright, testcontainers-go (PG 14-18 matrix) |

