# MCP Connector (dasha-mcp)

[Русская версия](../ru/mcp.md) · [← README](../../README.md)

`dasha-mcp` is a separate, **read-only** [MCP](https://modelcontextprotocol.io) server over the Dasha API. It lets AI assistants query the fleet's PostgreSQL diagnostics as tools/prompts, forwarding each caller's token to Dasha so its RBAC is preserved. Any MCP-compatible client works — Claude Desktop, Claude Code, Cursor, Continue, **opencode**, etc.

- **Tools (31):** `list_clusters`, `fleet_health`, `get_instance_info`, `get_health_score`, `get_health_recommendations`, `health_details` (turns a recommendation into a target: pass its `rule_id` as `detail` to get the offending tables, databases or sessions — the per-table drill-downs also take a `database`, the instance-wide ones do not), `health_trend`, `health_databases`, `top_queries` (by time/WAL), `query_report`, `list_snapshots`, `query_compare`, `running_queries`, `blocked_queries`, `list_indexes` (missing/unused/usage), `unused_index_report` (cluster-wide verdict on whether an index is safe to DROP: weighs the scan counter against every host of the cluster and against the statistics window behind it, because `idx_scan` is not replicated and a counter without its window means nothing), `index_advisor` (the other side of the same question — which index to create: the statements of every cluster host are parsed and btree candidates no existing index already covers come back with ready DDL, the statements behind each one and the share of workload time they hold, plus the gaps that say when an empty list is not a clean bill of health; built on demand, so it takes a longer timeout), `top_tables`, `schema_lint` / `schema_lint_summary` (structural defects of a schema from the system catalog: sequences running out of values, tables without a primary key, unlogged relations, schemas PUBLIC may create in — with a `skipped` list naming the checks that could not run, so a missing check is never mistaken for a clean result), `hot_tables` / `hot_indexes` (top hot objects per metric class — reads/writes/io — from the daily delta snapshots, summed over every cluster host, with a coverage ratio that says how representative the top is; needs snapshot storage), `describe_table`, `get_replication`, `settings_analyze`, `wait_events`, `connections`, `vacuum_danger`, `search_logs` (Yandex Cloud PostgreSQL/pooler logs; Yandex-MDB-discovered clusters only, rate-limited per user), `io_summary` / `io_trend` (physical I/O from `pg_stat_io`, broken down by backend type, object and context, and its shape over time — the only tools that see I/O done by autovacuum, the checkpointer and the WAL writer rather than by client backends; PostgreSQL 16+ and snapshot storage). All are annotated **read-only** and closed-world so compatible clients can surface (and auto-approve) them as safe. The server also ships usage **instructions** that prime the model on which tool/prompt to reach for.
- **Prompts (5):** `diagnose_cluster`, `explain_health_score`, `find_index_opportunities`, `investigate_slow_queries`, `fleet_overview` — linear playbooks: numbered steps, one tool per step, with an interpretation criterion on each (built for models without deep PostgreSQL expertise; strong models simply move faster through them).
- **Resources (6):** an embedded knowledge base the model can read on demand — `dasha://kb/health-rules` (every health rule with LOW/MED/HIGH thresholds and first actions), `dasha://kb/schema-checks` (every schema-check code, the params it fills and its first action), `dasha://kb/index-advisor` (how to read index candidates, warning codes and gaps), `dasha://kb/wait-events` (wait event glossary), `dasha://kb/pg-stat-io` (how to read the `pg_stat_io` counters), `dasha://kb/workflow` (complaint-to-tool-chain playbooks and API care rules).
- **Language:** `--lang en|ru` (or `DASHA_MCP_LANG`) selects the language of the knowledge base, playbooks and instructions; tool names, schemas and results stay English.
- **Timeouts:** `--timeout` (default 15s) bounds every API call; `index_advisor` uses `--slow-timeout` (default 90s) because its report is built on demand and never cached. Raise `--slow-timeout` above the backend's `index_advisor.timeout` if that setting is raised.

**Prerequisite:** a Dasha API token — a [personal access token](auth.md#personal-access-tokens-optional) (`dasha_pat_…`) or a static config token. It determines the role (`viewer` is enough).

## Build

```bash
cd backend && go build -o dasha-mcp ./cmd/dasha-mcp
# or a container image:
docker build -f deploy/images/Dockerfile.mcp -t dasha-mcp .
```

## stdio (local — Claude Desktop / Claude Code / opencode / Cursor)

The client launches the binary and talks over stdin/stdout; the token is the `DASHA_MCP_TOKEN` env var.

**Claude Desktop** (`claude_desktop_config.json`) or **Cursor** (`.cursor/mcp.json`):

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

**opencode** (`opencode.json` or `~/.config/opencode/opencode.json`):

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

## HTTP/SSE (shared / multi-user)

Run it as a service; **each request carries its own token** (no shared server token), so per-user RBAC is preserved:

```bash
dasha-mcp --http :8765 --dasha-url http://dasha-backend:8000
# container:
docker run -p 8765:8765 dasha-mcp --http :8765 --dasha-url http://dasha-backend:8000
```

Point a remote-MCP client at `http://<host>:8765` and send the token as `Authorization: Bearer dasha_pat_…` (or `X-API-Key`). For example, **opencode**:

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

The server is read-only (no mutating endpoints are exposed) and runs as a non-root user. Hardening: tool results are size-capped (oversized results are refused with a hint to narrow the request, never truncated into invalid JSON); the per-token server cache is hashed and bounded; tokens are never logged. Put the HTTP transport behind TLS in shared deployments; rate limiting is enforced upstream by Dasha's per-identity limiter (each PAT is a distinct identity), so it applies through the passthrough.

## Multiple Dasha instances (environments)

Each environment (dev / stage / prod) runs its own Dasha and its own `dasha-mcp`. Register them as separate MCP servers on the client — the server name namespaces everything (tools, prompts and `dasha://kb/*` resources are tracked per server, so URIs never clash):

```json
"mcpServers": {
  "dasha-dev":  { "command": "dasha-mcp", "args": ["--dasha-url", "https://dasha.dev.example.com"],  "env": { "DASHA_MCP_TOKEN": "dasha_pat_…" } },
  "dasha-prod": { "command": "dasha-mcp", "args": ["--dasha-url", "https://dasha.prod.example.com"], "env": { "DASHA_MCP_TOKEN": "dasha_pat_…" } }
}
```

Personal access tokens are per-instance: a PAT minted on dev is not valid on prod.

## Kubernetes (Helm)

The chart ships an optional, gated MCP Deployment + Service (HTTP mode). Enable it and the server is wired to the in-cluster backend automatically:

```yaml
# values.yaml
mcp:
  enabled: true
  port: 8765
  # dashaUrl: ""   # empty = in-cluster {release}-backend Service
  # lang: ru       # knowledge-base / playbook language (default en)
  # frontendProxy: true   # publish at <dasha-host>/mcp (default); false = expose the Service yourself
```

HTTP mode is strict per-user passthrough: the chart deliberately offers no shared fallback token — every client must send its own credential per request, keeping RBAC and audit per-user.

This creates `{release}-mcp` Deployment + `ClusterIP` Service on port `8765`. By default the frontend nginx proxies `/mcp` to that Service, so the connector is reachable on the dashboard's own host and TLS — no second Ingress or certificate:

```jsonc
{ "url": "https://dasha.example.com/mcp", "headers": { "Authorization": "Bearer dasha_pat_…" } }
```

The SSE stream is passed through unbuffered; the client's token is forwarded untouched, so RBAC stays per-user. Set `mcp.frontendProxy: false` to drop the chart-managed proxy and expose the endpoint yourself instead — front the `{release}-mcp` Service with your own Ingress/Gateway (terminate TLS there), or publish it directly via `mcp.service.type`. In a headless deploy (`frontend.enabled: false`) there is no nginx, so the chart adds the `/mcp` rule to the Ingress/HTTPRoute directly.

