# Changelog

## v1.5.0

### Features
- **Unused-index verdict (`GET /api/indexes/unused-report`, MCP tool `unused_index_report`):** answers *may I drop this index?* instead of dumping raw scan counters, because a counter alone cannot answer it. Two facts make the naive `idx_scan == 0` test dangerous, and both are now encoded: (1) `idx_scan` is per-instance and is **not replicated**, so an index idle on the primary may be serving the entire read workload on a replica — the report therefore takes **no instance** and consults every host of the cluster; (2) `idx_scan` is a counter *since the last statistics reset*, so zero scans five minutes after `pg_stat_reset()` prove nothing and five scans over two years is effectively unused — the report reads `pg_stat_database.stats_reset` alongside the counter and normalizes to **scans per day over a known window** (the same per-day normalization already used by the settings rules). Each index comes back with a verdict and the reasoning: `drop_candidate` (zero scans on every host over an adequate window), `used` (naming the host — including the *"idle on the primary but hot on a replica"* case), `stale_evidence` (scanned so rarely the scans may be historical — the counter cannot say *when*, so reset the statistics and re-observe a full business cycle), `insufficient_data` (window too short — a monthly report can be an index's only user), and `unknown`. A host that cannot be reached yields `unknown` rather than a false "unused": an incomplete picture must never justify a `DROP`. Unique/constraint-backing indexes are excluded outright — their `idx_scan` does not grow when the index enforces a constraint, so it can never prove them unused. **Partitioned indexes are judged as a whole:** `pg_stat_user_indexes` never lists the parent (relkind `I`), only its per-partition children, so a cold partition shows zero scans — that is partition pruning working, not a dead index. Worse, a child cannot be dropped at all (`ERROR: cannot drop index … because index … requires it`), and the hint PostgreSQL offers points at the *parent*, whose removal would strip the index off **every** partition including the hot ones. The report therefore walks `pg_inherits` to the top-level index (recursively — partitions can themselves be partitioned), sums the children's scans across all partitions and all hosts, and names the parent as the only droppable unit: one busy partition keeps the whole index. The existing `/api/indexes/unused` contract is unchanged. **UI:** a new *Indexes → Analysis* page, headed by the plain statement that this is **advice and can be wrong**; the reasoning and the per-host evidence sit under the row expander, with filters by verdict and by table/index name.
- **Personal access tokens (PAT):** a logged-in user can mint per-user API tokens (`POST`/`GET`/`DELETE /api/auth/tokens`, plus a *My tokens* tab in the Settings dialog) used as the `X-API-Key` header — bearer credentials for non-browser clients (scripts, the upcoming MCP server) that preserve the user's role (Casbin RBAC). Tokens are stored hashed in the snapshot DB (`api_tokens` table, created by `dasha migrate`), with least-privilege (requested role ≤ caller's, default `viewer`), anti-chaining (cannot mint from a PAT), a one-time secret reveal, immediate revoke, and optional expiry. Management is additionally gated by `auth.pat_min_role` (`admin` by default while the feature matures; set `viewer` to open it to every signed-in user). Token resolution is cached with a short TTL to keep the auth hot path off the database, and `last_used` is updated at most once per token per interval. **Two lifetime limits bound the blast radius of a token nobody remembers to revoke, because a PAT's role is frozen at mint time — resolution reads the role stored on the token, not the identity provider, so a token keeps working after its owner is demoted or leaves:** (1) an **admin token always expires within 30 days** — including when `expires_in_days` is omitted, since otherwise the cap would be bypassed by simply not asking for an expiry; the request is clamped rather than rejected, and the applied value comes back in `expires_at`; (2) **any token unused for 90 days is revoked automatically** (idleness measured from last use, falling back to creation for a token never used at all — a token minted and forgotten would otherwise live forever). The idle cutoff is enforced at resolve time *and* by a periodic sweep: the check in `ResolveAPIToken` makes it effective the moment it is crossed (between two sweeps an idle token would still authenticate), while the sweep writes `revoked_at` down so the state is visible in the token lists rather than a row that looks live but silently no longer works.
- **Token administration & user directory (admin-only):** an administrator can see and revoke **every** user's personal access tokens, not just their own (`GET /api/auth/admin/tokens`, `DELETE /api/auth/admin/tokens/{id}`), and browse who has access (`GET /api/auth/admin/users`) — surfaced as *All tokens* and *Users* tabs in the Settings dialog. A new `users` table (created by `dasha migrate`) records each principal on first SSO sign-in and stamps `last_login_at` on every login, so operators can answer *who has access and when did they last use it* without reading the IdP; the stored role is an audit trail refreshed from the identity provider on each login, never an authorization source. Both listings accept `include_revoked` (default `false`): a revoked token is kept as an audit row — it can be shown, but `ResolveAPIToken` still requires `revoked_at IS NULL`, so it can never authenticate again. Access is deliberately narrower than the `admin` role: the endpoints require an **interactive OIDC admin session**, so an admin-scoped PAT cannot enumerate or revoke the tokens that would replace it (anti-chaining, consistent with minting). Because a `viewer` holds a blanket `GET /api/*` grant, the Casbin model was extended from allow-only to **allow-and-deny** (`e = some(allow) && !some(deny)`) and `/api/auth/admin/*` is explicitly denied to viewers — a broad grant can now be carved out by a narrower deny, whatever the rule order.
- **MCP connector (`dasha-mcp`):** a separate, read-only [MCP](https://modelcontextprotocol.io) server over the Dasha API for AI assistants (Claude Desktop / Code, opencode, Cursor, …). 24 read-only tools (clusters, fleet health ranking, instance info, health score/trend/per-database & recommendations — plus `health_details`, a rule-level drill-down that turns a recommendation into an actionable target: pass its `rule_id` as `detail` and it names the offending tables, databases or sessions (autovacuum-disabled tables, low-HOT-update tables, high-dead-ratio tables, xid-wraparound databases, xmin-horizon-blocking sessions), since a recommendation alone only reports a rule and a count — top/running/blocked queries, query report & snapshot compare, indexes, top tables, table drill-down, replication, settings analysis, wait events, connections, vacuum-wraparound danger, Yandex Cloud log search — rate-limit-aware: dedup-by-default, local argument validation so a bad request never burns a rate-limit slot, and 429/501/504 mapped to actionable guidance for the model) and 5 prompts (`diagnose_cluster`, `explain_health_score`, `find_index_opportunities`, `investigate_slow_queries`, `fleet_overview`). Every tool carries the MCP read-only + closed-world annotations so clients can auto-approve it; the server sends usage instructions that prime the model, and HTTP mode shares one schema cache across per-token servers. Runs over **stdio** (single identity) or **HTTP/SSE** with per-user token passthrough (no shared server token — Casbin RBAC preserved). Hardened: per-tool result size cap (oversized results refused, never truncated to invalid JSON), optional `limit` args on `describe_table` (partitions) and `connections` (activity sample), hashed + bounded per-token server cache, tokens never logged; rate limiting applies upstream via Dasha's per-identity limiter. Ships a bilingual (en/ru, `--lang` / `DASHA_MCP_LANG`) embedded knowledge base as 3 MCP resources (`dasha://kb/health-rules` — every scoring rule with LOW/MED/HIGH thresholds and first actions, kept in lockstep with the rules engine by a sync test; `dasha://kb/wait-events` — wait event glossary; `dasha://kb/workflow` — complaint-to-tool-chain playbooks and API care rules), and the 5 prompts are linear playbooks (numbered steps, one tool per step, an interpretation criterion on each) — so LLMs without deep PostgreSQL expertise can drive the tools competently, while strong models are not slowed down. Ops parity with the backend: every MCP call is logged via zap to stderr (method, tool, duration — never arguments or tokens), HTTP mode drains in-flight requests on SIGTERM/SIGINT, and the advertised server version is stamped from the release build (same internal/version ldflags scheme as the backend). Typed Dasha API client generated from the OpenAPI spec; multi-arch image `deploy/images/Dockerfile.mcp`, published in the release workflow; optional gated Helm Deployment + Service (`mcp.enabled`); the chart deliberately offers no shared fallback token — HTTP mode is strict per-user passthrough.

- **Hot tables & indexes (`GET /api/tables/hot`, `/api/indexes/hot`, `/api/hot/object-history`, `/api/hot/percentile`; MCP tools `hot_tables` / `hot_indexes`):** answers *what is loading this database* from scheduled activity-counter delta snapshots, stored the way PostgreSQL stores column statistics: an **exact top-N per metric class** (reads / writes / physical I/O; indexes have no writes class — PostgreSQL keeps no per-index write counters) plus a **decile histogram of the tail**. The histogram yields a **coverage ratio** (how much of the total activity the stored top holds, so a top is never mistaken for the whole picture), an **activity percentile for any table** on the describe page (live delta against the stored anchor; the absolute rate is always shown first), and a storage footprint bounded by construction (append-only day partitions with their own age-based `hot_retention_days` retention, independent of the size-based pgss one). Counters are sampled **from every cluster host** and summed — they are not replicated — with a per-host breakdown and primary/replica badges; an unreachable host makes the snapshot partial instead of blocking it. Capture runs in the autosnapshot daemon on a **cron schedule** (`hot_schedule`, default `0 3 * * *` in **UTC**, `CRON_TZ=<zone>` prefix for local time), configurable together with top size and retention in the auto-snapshots settings UI; rates are normalized by the snapshot's actual window, so hourly schedules still read as honest per-day figures. Zero-activity objects never enter the top, and idle tails/tables collapse to a short note — a quiet database is not dressed up as a hot one. UI: *Hot tables* / *Hot indexes* sections (class selector, snapshot picker, trend arrows, per-host rows, tail histogram summary; large numbers as k/M/B) plus the *Activity* block on table describe; the auto-snapshots page keeps the two snapshot families apart (grouped settings, separate *last trigger event* / *last hot snapshot* status facts). Requires snapshot storage (`501` otherwise).
- **Autovacuum summary on the Maintenance page (`GET /api/maintenance/autovacuum-summary`):** a pie chart of tables currently past their autovacuum/autoanalyze trigger thresholds — split into *VACUUM only / ANALYZE only / both*, computed with PostgreSQL's own formula (`threshold + scale_factor × rows`, per-table reloptions honoured) — plus counters of maintenance processes currently running in the database (`pg_stat_progress_vacuum` / `pg_stat_progress_analyze`). A legend with counts and shares sits next to the chart (and is hidden entirely when nothing is due — a green "all within thresholds" state replaces the chart); slice colors are CVD-validated against light and dark surfaces separately.

### UX
- **Settings dialog (gear in the user menu):** one place for per-user preferences, replacing the standalone *Personal access tokens* item — tabs for *Interface*, *My tokens*, and (for admins) *All tokens* / *Users*. When auth is `none`/`token` there is no user menu, so the gear moves to the toolbar; preferences are otherwise unavailable in those modes.
- **UI language is now selectable (and English was added):** a third locale `en_US` ships alongside `ru_RU` / `de_DE`, the language is auto-detected from the browser when unset (`navigator.languages`, matched on the language subtag), and the choice persists in `localStorage`. `fallbackLocale` now points at the real `en_US` instead of the non-existent `en` it named before; Vuetify's own strings (data tables, pagination) follow the same setting. Untranslated keys fall back to English rather than rendering the raw key, so a locale no longer has to be complete to be usable.
- **Theme moved into Settings, with the system option restored:** the toolbar toggle only flipped light↔dark, which made *System* (the default) a one-way door — once you clicked it, you could never get back to following the OS. The selector offers all three.
- **Time zone for all rendered timestamps:** local (default), UTC, or any of the eleven Russian zones, listed by IANA id (`Europe/Moscow`) because that is what PostgreSQL's `timezone` GUC and the server logs use; offsets in the list are read from the runtime tz database rather than hardcoded, so a future zone change cannot leave a stale label. Fixed timestamps carry their zone (`… GMT+3`, `… UTC`) — an unlabelled time in an unexpected zone is worse than none. Timestamps also follow the **chosen UI language**: `fmtDateTime` called `toLocaleString()` with no arguments and silently used the *browser's* locale. Chart axes (log histogram, Health Score trend) were formatting dates independently and now share the same setting, so ticks and table rows cannot disagree about what "10:00" means. An unknown persisted zone falls back to local time instead of throwing `RangeError` and taking down every table with a date in it.
- **Configurable page size** (10/15/25/50/100, default 15 as before): this is the server-side `limit`, so changing it refetches from page 1 rather than only re-slicing the rendered rows. Compact-row tables (connection sources) scale off the same setting, keeping their previous 2:1 ratio instead of pinning a size of their own.
- **Token tables are paged and sortable**, and show a `Created` column; a *Show revoked* checkbox reveals revoked tokens (greyed out, with their revocation time and no revoke button).
- **Query report and snapshot compare can sort by mean execution time and its standard deviation** (`mean_exec_time` / `stddev_exec_time` from pg_stat_statements), alongside the existing total-time/calls/WAL/… keys — total time surfaces the heavy hitters, while mean and σ surface queries that are slow or unstable per call regardless of how often they run. When either key is selected, the Exec time block is highlighted on the cards; the >5% contribution marker does not apply (these are per-call averages, not shares of a cluster-wide total).

### Bug Fixes
- **Stale `reltuples` no longer hides tables from autovacuum eligibility** (Health Score `vacuum_backlog` / `stale_planner_stats` and the new autovacuum summary): trigger thresholds were computed from `pg_class.reltuples`, which goes stale exactly when ANALYZE never runs — the flagship case being a table with `autovacuum_enabled=false` (it disables autoanalyze too): it can reach 100% dead rows while the formula still counts rows long deleted, so it never looked due and the vacuum queue stayed at 0 on a visibly sick instance. The row estimate is now the **lower of `reltuples` and current live tuples**, so either estimate trips the trigger; the same convention is shared by the health score and the maintenance summary so the two counters cannot disagree.
- **VACUUM progress reported bytes under a "dead rows" label on PG 17+:** PG 17 replaced the `num_dead_tuples` / `max_dead_tuples` row counters with TID-store memory counters (`dead_tuple_bytes` / `max_dead_tuple_bytes`), and the query aliased the byte columns to the old names — so the UI showed memory figures as row counts. PG 17+ now reports the collected count from `num_dead_item_ids`, and the memory shows as its own "used / limit" metric (`DeadTupleBytes` / `MaxDeadTupleBytes`, nullable in the API; `MaxDeadTuples` became nullable in turn — PG 17+ caps dead tuples by memory, not by row count). Both metrics carry a header tooltip explaining that these are counters of the *current vacuum cycle* (reset after each index pass), not the table's total dead rows (`n_dead_tup`, shown on the Maintenance page).
- **Auto-snapshot settings reported "saved" when the server rejected the values:** the save handler never checked the response status — orval's fetch resolves on HTTP errors, so a 400 (invalid duration, out-of-range number, bad cron) still flashed the green "Settings saved" while nothing was stored, and the silently-reverted form only became apparent after a page reload. Non-204 responses now surface an error banner naming the likely culprit fields. The settings form also lost its field tooltips in favour of per-section description lines (the same style everywhere), and the spike-trigger fields are validated client-side too (cron structure, durations) so garbage never reaches the server unannounced.
- **Console noise from font preloads:** unplugin-fonts injects a `<link rel="preload">` for every font file in the bundle, including the eot/woff/ttf fallbacks of `@mdi/font` — browsers only ever download woff2, so the rest produced "preloaded but not used" warnings and `font/eot` an "unsupported type" error on every page load. A `linkFilter` now keeps only the woff2 preloads.
- **Health Score: `stale_planner_stats` no longer flags cold tables forever.** The rule counted tables past *half* their autoanalyze threshold and unanalyzed for >24h — but below the full threshold autoanalyze never fires by design, so a cold table that once received a batch of changes landing between 0.5× and 1× of its threshold could never leave the flagged state: autovacuum would not analyze it, nothing else reset the counter, and the recommendation ("run ANALYZE") asked the operator to fix something autovacuum did not consider broken. The rule now requires the **full** (reloption-aware) autoanalyze threshold — the flagged table is one autoanalyze *should have* processed and did not — and the analyze-age window shrinks from 24h to **3h**: past the threshold a healthy autovacuum picks a table up within minutes, so the window is only an anti-flap guard, and a full day of bad plans before the signal was too slow; 3h still absorbs workers stuck on huge tables and long ANALYZE runs. The recommendation now says to find out *why* autoanalyze never reached the table (per-table `autovacuum_enabled=false`, starved workers) instead of suggesting to lower `autovacuum_analyze_scale_factor` — past the full threshold, lowering it changes nothing. KB and UI texts follow; the UI severity caption was also corrected («≥3 / ≥10 / ≥30» → the actual «≥3 / ≥5 / ≥10»).

## v1.4.0

### Features
- **Yandex Cloud log search (new top-level page `/logs`):** for clusters discovered via Yandex MDB service discovery, search and view PostgreSQL server logs and connection pooler (Odyssey) logs through the Yandex MDB API.
  - New endpoint `GET /api/logs` (`getLogs`). The backend reads `StreamClusterLogs` as a bounded historical read (`from`/`to` set), so it can fetch past windows rather than only tailing live.
  - **Native server-side filters** for `severity` and `host` (the only fields the Yandex API filters on); `message` substring, `database` and `user` are filtered Dasha-side over the stream. The native filter expression is built only from an allowlist (severity enum + validated cluster hosts), so it is injection-safe. Severity casing follows the service: PostgreSQL `UPPER` (`error_severity`), pooler `lower` (`level`).
  - **Optional deduplication** groups near-identical messages by normalized text with `count` + `first_seen` / `last_seen` and a representative (most severe) severity.
  - **Cursor pagination** (`next_page_token`) for non-deduped results with a "load more" button. The token is emitted only when a further match actually exists (the stream is read ahead past a full page), so "load more" never returns an empty page; a `partial` banner is shown when the scan limit (`max_scan`) is reached. `page_token` cannot be combined with `dedup` (`400`), since a resume cursor would silently under-count dedup groups.
  - **Partial results on timeout:** when the upstream read exceeds `timeout_seconds`, entries (or dedup groups) collected so far are returned as a partial page with the `partial` flag instead of a bare `504`.
  - Sensitive text is masked through `sanitize.SQL()` per service type before leaving the backend; service-account keys never leave the backend (reused from discovery via an internal SDK registry).
  - Access is `viewer+` (covered by the existing `GET /api/*` policy). Clusters advertise the capability via a new `supports_logs` field on `Cluster` API objects (alongside `source`); the `Logs` menu item appears only when at least one such cluster is present, and `GET /api/logs` returns `501` for clusters without log search support.
  - New global config `log_search` (`max_scan` default 5000, `max_page_size` default 1000, `timeout_seconds` default 30).
  - **Log frequency histogram on `/logs`:** a stacked bar chart (time × severity) over the loaded results, computed client-side — no extra Yandex API calls. Buckets cover the time span the loaded records actually span (caption states the coverage); severity colors are CVD-validated for both light and dark themes. Chronological mode only (dedup groups carry no per-record timestamps).
  - **Search presets & deep links:** eight one-click presets (autovacuum, deadlocks, checkpoints, temp files, canceled statements, connection failures, slow queries, errors); the deadlocks health-score recommendation gains a "Search logs" button linking to `/logs?preset=deadlock` on clusters with log search.
  - **Filter UX:** multiple include terms (AND) and multiple `grep -v` excludes; an exclude containing the `<*>` placeholder matches masked templates, so one click excludes a whole dedup group shape; click-to-filter on severity chips and user/database/host values in the expanded row; filters sync to the URL (shareable links) and persist in localStorage (concrete dates are never restored); Enter submits; a red dot on the search button marks filters changed since the last search; rarely used host/database/user fields fold behind a spoiler that opens automatically when any of them is set.
  - **Time range interop & histogram zoom:** copy/paste of the period in Grafana's time-picker clipboard format (`{"from":"now-1h","to":"now"}` or absolute local timestamps); clicking a histogram bar or drag-selecting a range (chartjs-plugin-zoom) sets a custom period in the filters — the search itself stays a deliberate click.
  - **Dedup quality:** WAL LSNs (`2E/28E36B88`) are masked during normalization so checkpoint/restartpoint messages group correctly; dedup rows show the masked template (`<*>` placeholders) instead of one member's concrete numbers.
  - **Log entry detail:** `query`/`internal_query` rendered with SQL syntax highlighting; a copy menu (all fields / query only / message only / add to excludes); zero-valued fields (`query_id=0` and friends) hidden.
  - **429 countdown:** the rate-limit message shows a live 30-second retry countdown instead of static text.
  - **Health Score:** the `wal_level=logical` without-logical-slots warning is suppressed for Yandex MDB clusters (the platform fixes `wal_level`, it is not user-configurable); the host disk space recommendation text was rewritten for clarity.
  - **Per-user rate limiting for `GET /api/logs`:** separate from the global auth rate limit, configurable via `log_search.rate_limit` / `log_search.admin_rate_limit` (defaults: 1 req/30s with burst 10; admins 1 req/5s with burst 20; `requests_per_second: 0` disables). Exceeding the limit returns `429`, shown on the `/logs` page with a dedicated message. Every search is also logged at info level with the user name.

### Dependencies
Dependabot bumps since v1.3.0 — routine minor/patch freshness updates unless noted:
- **Backend (Go):** echo 4.15.2→4.15.4, go-oidc/v3 3.18.0→3.19.0, oapi-codegen/runtime 1.4.1→1.4.2, validator/v10 10.20.0→10.30.3, yandex-cloud/go-genproto 0.84.0→0.92.0 (current YC API definitions, used by the log search), testcontainers-go 0.42.0→0.43.0 (tests only).
- **Frontend:** vuetify 3.12.9, vue-i18n 11.4.6, @fontsource/roboto 5.2.10; dev tooling: vue-tsc 3.3.6, vitest 4.1.9, eslint 10.6.0, prettier 3.8.4, @playwright/test 1.61.0, @types/jsdom 28.0.3, jiti 2.7.0.
- **Infra:** alpine base image 3.23→3.24 — the security-relevant bump (current CVE fixes in OS packages, keeps the Trivy image gate green); actions/checkout 6→7.

## v1.3.0

### Features
- **Metrics-backed Health Score (optional):** when a Prometheus/VictoriaMetrics datasource is configured (`health_score.metrics` in `dasha.yaml`), the score, recommendations and a new trend are computed from time-series metrics (pgSCV + Yandex MDB + pgbouncer + host) instead of point-in-time SQL. The SQL snapshot stays the zero-config fallback; the `source` field on `GET /api/common/health-score` reports `"snapshot"` or `"metrics"`.
  - **Provider adapters + per-deployment label matching:** `pgscv` (PG internals, incl. YC MDB via remote scrape), `yc_native` (managed host/pooler), `pgbouncer`, `pgscv_system` (self-managed host). Selector templates are configurable; `GET /api/common/health-score/datasource/status` validates that each role matches exactly one series.
  - **Score trend** (`GET /api/common/health-score/history`): per-timestamp overall score, per-category scores and latency over a range, with a **seasonal (hour-of-week) baseline** and detected **dips**. New `HealthScoreTrend` chart on `/health-score` (24h / 7d / 30d).
  - **Richer signals** unavailable to the SQL snapshot: host CPU saturation (`load / vCPU`) and pooler saturation → `connections`; windowed query **latency** with **regression vs the seasonal baseline** → `performance`; data-page **checksum failures** and **sequence / ID-space exhaustion** → critical floor + rules.
  - **Sequential-scan regression** → `performance`: the rate of tuples read by seq scans, compared to its own seasonal (hour-of-week) baseline — a rise flags indexes going unused or stale planner stats (ANALYZE), without false-firing on normal analytical scans.
  - **Host disk space** → `storage`: used/total of the fullest host filesystem (from pgSCV `node_filesystem_*` and Yandex Cloud `disk_used_bytes`/`disk_total_bytes`), with LOW/MED/HIGH at ≥70/80/90% and a **role-agnostic critical floor at ≥90%** (a full data volume stops writes).
  - **Floor extensions:** checksum failures (role-agnostic) and near-overflow sequence exhaustion clamp the score into the red, alongside the existing wraparound / autovacuum-off floor.
  - **Catalog/GUC overlay keeps score↔rules parity in metrics mode:** facts a time-series datasource cannot express — per-table `autovacuum_enabled=false`, never-vacuumed tables, `relfrozenxid` age, planner-stat drift, `wal_level`, the `autovacuum`/`track_counts` GUCs, the MVCC horizon, lock-pool sizing and in-recovery — are overlaid from the SQL snapshot onto the metrics signals. So catalog-only rules (e.g. `tables_with_autovacuum_off`) keep firing **and** the score keeps penalising them even when a datasource is attached, instead of silently disappearing. Best-effort: a snapshot read failure leaves the metrics-only score intact. (The history **trend** stays time-series-only.)
  - **Auto-mapping of service-discovered clusters:** clusters from `discovery:` (e.g. Yandex MDB) are mapped to datasource labels from their discovery metadata — host FQDN → `{{.Host}}`, cloud resource id (MDB cluster id) → `{{.Service}}`, `folder_id` label → `{{.Env}}`, short host → `{{.Container}}`; providers from `providers_default` — so they need no hand-written `targets:` entry. A static `targets:` entry always overrides; `auto_map_discovered` (default on) and `discovery_env_label` knobs.
  - **Datasource auth from environment:** `datasource.auth` supports `token_from_env` (bearer) and `username` + `password_from_env` (basic), resolved like the other `*_from_env` secrets so credentials are injected from a Secret instead of stored inline; `auth.type` is validated (`none|bearer|basic`).
- **Vacuum maintenance reworked as an autovacuum-trigger queue (fewer false positives):** the `maintenance` category now derives its vacuum signals from PostgreSQL's own autovacuum trigger instead of a raw "time since last vacuum". A table counts as *due* when `n_dead_tup` exceeds `autovacuum_vacuum_threshold + autovacuum_vacuum_scale_factor·reltuples`, or `n_ins_since_vacuum` exceeds the insert threshold — honouring per-table `reloptions` overrides (reusing the `describe_vacuum_stats` formula). Large static / read-mostly tables and partitions that autovacuum correctly never touches no longer inflate the score.
  - New `vacuum_backlog` rule + `vacuum_backlog_tables` penalty — the queue depth (tables eligible for autovacuum right now, dead-tuple **or** insert trigger). Thresholds ≥6 / ≥15 / ≥30.
  - `max_vacuum_age_hours` → `max_overdue_vacuum_age_hours`: oldest vacuum **among the backlog tables only**, so static tables can no longer drive the "stale vacuum" signal.
  - `stale_planner_stats` (auto-analyze) now reads the same reloption-aware threshold, consistent with the vacuum queue.
  - The vacuum queue is **SQL-snapshot only** (`reltuples` / `n_ins_since_vacuum` / autovacuum GUCs are not faithfully expressible as time-series); in metrics mode it is overlaid from the snapshot, so both vacuum rules keep contributing to the score and recommendations.

### UX
- **Health Score is admin-only for now** while the scoring model is still being calibrated and validated across many clusters: the menu item and the Home-page gauge are hidden from regular viewers, and a direct visit to `/health-score` redirects non-admins to `/main` (router guard). No-RBAC modes (`none`/`token`) keep full access.
- **Paginated recommendation detail tables:** the five inline detail tables (dead-ratio tables, autovacuum-off tables, low-HOT tables, xid-wraparound databases, horizon-blocking sessions) are now paged via `limit`/`offset` with the shared `PaginationControls`, instead of a hard server-side cap, so long lists are fully browsable (sensible row-size thresholds kept).
- **Reduced numeric precision** in recommendation texts and category tooltips — metric values are rounded for display (e.g. `90.22492448754167%` → `90.22%`) via a shared `fmtNum` helper.

### Internal
- New `internal/metrics` package: datasource client, label matcher (with discovery-driven auto-mapping), MetricsQL query catalog, signal collector, seasonal baseline, dip detection, history service.
- Demo lab extended with a VictoriaMetrics + pgSCV + pgbouncer stack (`demo/docker-compose.metrics.yaml`).
- Helm chart `values.yaml`: documented `health_score.metrics` block — datasource (incl. auth via `*_from_env` / ExternalSecret), providers, selector templates, targets, discovery auto-mapping and tuning.

## v1.2.1

### New Features
- **Auto-snapshots of pg_stat_statements**: separate `dasha autosnapshot` daemon creates pgss snapshots automatically on configurable triggers
  - **Trigger: activity spike** — sliding-window moving average of `count(state='active')` from `pg_stat_activity`; fires when current value exceeds baseline by a configurable percent (default +50%) for a sustained duration (default 5 min)
  - **Trigger: role change** — detects master↔replica transitions via `pg_is_in_recovery()`, with configurable direction (`both` / `master_to_replica` / `replica_to_master`)
  - **Trigger: activity drop** (`recovery_duration`) — after a spike, snapshots the aftermath once activity stays below the threshold long enough; paired with reset this gives two clean pgss windows per incident (the buildup and the spike's actual execution). `0s` disables
  - **Deferred follow-up** (`deferred_interval`) — after a spike snapshot, schedules a follow-up snapshot in a persisted queue (`autosnapshot_pending`, survives daemon restarts) — a safety net/fixed-offset capture for spikes that never resolve. **Auto-cancelled when the spike resolves** (the drop snapshot already captured the incident, so the deferred would only snapshot the quiet aftermath). `0s` disables. Both are per-cluster overridable
  - **Global knobs** (stored in storage DB, editable via UI): `poll_interval`, `max_snapshot_frequency` (debounce), `min_baseline_active` (skip when load is low), `retention_bytes`, `retention_min_days`, `reset_query_stats` (reset `pg_stat_statements` after each auto-snapshot — independent of the manual UI reset flag)
  - **Custom pgss reset function** (`pgss_reset_function` in `dasha.yaml`): call a schema-qualified wrapper instead of `pg_stat_statements_reset()` when the monitoring role lacks `EXECUTE` (mirrors `pg_stats_view`); applies to both auto and manual resets
  - **Per-cluster overrides**: deep-merged on top of global defaults; clusters can toggle triggers, tune thresholds or disable auto-snapshots individually
  - **Leader election** (opt-in, `storage.leader_election`, off by default): `pg_try_advisory_lock` on the storage DB lets the daemon run in multiple replicas for HA; disabled by default because a session-level advisory lock needs a dedicated connection and is incompatible with transaction-pooling proxies (PgBouncer transaction mode)
  - **Retention by total size**: drops oldest day-triples (snapshots + query_texts + trigger_events partitions) once total exceeds `retention_bytes`; respects `retention_min_days` floor
  - **History tab**: filter by cluster / outcome / trigger_type, paginated; persists snapshot creations and errors only — transient skips (debounce, below-baseline, wrong-direction) are logged at debug level and not written to history to avoid noise
  - **UI**: new "Auto-snapshots" menu item (`mdi-camera-timer`) with Settings + History tabs; admin-only editing, viewers see read-only state; menu hidden for non-admin when feature is disabled
  - **API**: `GET/PUT /api/autosnapshot/config`, `GET/PUT /api/autosnapshot/clusters/{name}`, `GET /api/autosnapshot/status`, `GET /api/autosnapshot/trigger-events`
  - **CLI**: `dasha autosnapshot` (separate command, not started by `dasha serve`)
  - **Deploy**: Helm chart `autosnapshot` subchart (disabled by default, toggle `autosnapshot.enabled: true`); docker-compose adds `autosnapshot` service alongside `backend` and `frontend`
- **DB connection-pool tuning** (`dasha.yaml`): the pools to monitored clusters are now configurable — `db_pool` (`max_conns`, `max_conn_idle_time`, `max_conn_lifetime`; 0 = pgx default) for `dasha serve`, and `autosnapshot_db_pool` (per-field override) for the `dasha autosnapshot` daemon. Lets the daemon free monitoring-user connections quickly (e.g. `max_conn_idle_time: 5s`) under a tight connection budget. The storage pool stays tunable via `storage.dsn` query params (`pool_max_conns`, `pool_max_conn_idle_time`, ...)
- **Lock snapshots on triggers**: an activity-spike snapshot can additionally capture the lock-contention graph alongside `pg_stat_statements`
  - **Hybrid timing**: cheap blocked-session counting runs in the background during the spike and records a `background_peak`; at trigger time a short burst of N detailed probes (default 5 × 500 ms) captures the full `pg_blocking_pids` graph
  - **Harshest probe wins**: the probe with the most distinct blocked sessions is kept (tie-break by longest wait); up to 100 rows are stored, sorted by wait descending
  - **Storage**: new `snapshots.locks_data jsonb` column; `GET /api/queries/snapshot/{id}/locks` serves it; the snapshot list now carries `has_locks`
  - **Knobs** (global + per-cluster): `capture_locks` (default on), `lock_probe_count` (1–20), `lock_probe_interval` (100 ms–5 s)
  - **Manual capture**: a "with locks" option on the manual snapshot button; the captured lock graph is viewable from the Query Stats snapshot view

## v1.1.0

### Features
- **Health Score (new top-level page `/health-score`):** a composite 0–100 metric across eight categories — `connections`, `performance`, `storage`, `replication`, `maintenance`, `horizon`, `wal_checkpoint`, `locks` — with continuous penalty functions and a parallel rules engine.
  - 30 rules across the eight categories.
  - **Per-database drill-down (детализация):** `GET /api/health-score/databases` returns a `DatabaseScore` per DB with the rules engine re-run in database scope. The Databases table on `/health-score` becomes the filter — clicking a row pins it as the context for the recommendation list. Instance-only categories (`connections / replication / horizon / wal_checkpoint / locks`) are hidden in DB scope. Worst-DB hint highlights the lowest score.
  - **Auto-drop of categories without signal**, with proportional weight redistribution across the remaining categories so the score is not artificially inflated or deflated: `replication` is dropped when the instance has no replicas; `maintenance` is dropped on standbys (`pg_is_in_recovery() = true`) since autovacuum / ANALYZE can't run there and the metrics would reflect primary state.
  - **Inline data tables for actionable rules** — five typed endpoints under `/api/common/health-score/details/*` (one per rule, conventional 2-level `internal/query/sql/common/health_score_*` layout): `high-dead-ratio-tables`, `low-hot-update-tables`, `tables-autovacuum-off`, `xid-wraparound-databases`, `horizon-blocking-sessions`. Recommendation cards render the actual offending rows instead of an opaque SQL snippet; rules whose linked Dasha page already shows the data (`/locks`, `/tables`, etc.) keep just the link.
  - **About panel** under `/health-score`: collapsible explanation of the formula, category weights, penalty breakpoints (with an explicit definition of *breakpoint* — the metric value at which the slope changes), all 30 rules with descriptions and LOW/MEDIUM/HIGH thresholds, and the drill-down concept.


### UX
- **Home page restructure** (`/main`):
  - The Health Score card is the primary view at the top, including the chips for per-database health signals merged from the former "Здоровье БД" card: `Conflicts`, `ChecksumFailures`, `RollbackRatio`, `Stats since`. The standalone `DatabaseHealthSection` component is removed.
  - `CacheHitRates` and `WaitEvents` are placed in a single `v-row` underneath (50/50 on `md+`).
- **Standby awareness:**
  - The `Maintenance` menu item is hidden when the current host is a standby. Navigating to `/maintenance/<cluster>` on a replica auto-redirects to `/main` via a watcher on `pg_is_in_recovery()`.
  - `DescribeVacuumStatsSection` on `/table-describe` skips its API fetch on standbys and renders a "look at master" hint instead — `pg_stat_user_tables.last_*vacuum` on a replica is local and does not reflect autovacuum activity.
  - New Pinia store `instanceInfo` keyed by `cluster::host` (TTL 30s, request dedup) reuses `/api/common/instance-info` across the menu, the redirect watcher, and the vacuum-stats section.
- **`InstanceInfo` carries `in_recovery`** end-to-end (swagger → `Result.InRecovery` → `GET /api/health-score` response).
- **Severity thresholds recalibrated** across seven rules: `idle_in_transaction` (2/5/10 was 1/2/5), `low_cache_hit_ratio` (95/90/85 was 99/95/90), `high_max_dead_ratio` (10/20/30 was 10/20/50), `high_avg_dead_ratio` (5/15/25 was 5/10/25), `stale_vacuum` (7/21/60 days was 2/7/14), `requested_checkpoint_ratio` (5/10/20% was 5/10/30%), `low_hot_update_ratio` (<0.80/<0.65/<0.50 was <0.80/<0.60/<0.30). Lock thresholds tightened: `active_lock_waiters` (1/3/10), `ungranted_locks` (2/5/15), `deadlocks_rate` simplified to LOW-only when total > 0 (no MED/HIGH without per-day normalisation since the counter accumulates from `pg_stat_database_reset`).

### Performance
- **nginx response compression** (`gzip` and Brotli when the build provides the module) added to `deploy/images/nginx.conf.template` and `demo/nginx.conf` for JS/CSS/JSON/SVG/text. Static assets and API responses both benefit; smaller payloads especially help cold-cache loads of `/health-score`.

### Docs
- New top-level `README-health-score.md` and `README-health-score.ru.md` document the formula, weights, penalty breakpoints, all 31 rules with thresholds and what each metric measures, the drop semantics for replication/maintenance, and the per-database drill-down behaviour.

## v1.0.2

### Bug Fixes
- **Stale SPA bundle after upgrade**: frontend nginx now sets `Cache-Control: no-cache` for `index.html`/SPA routes and `immutable` + `expires 1y` for hashed `/assets/`. Affects `deploy/images/nginx.conf.template` and `demo/nginx.conf`. One manual cache clear is still needed for the v1.0.1 → v1.0.2 transition; future upgrades are clean.

## v1.0.1

### Bug Fixes
- **Autovacuum / autoanalyze thresholds in `describe_vacuum_stats`**: the `vacuum_threshold`, `analyze_threshold` and `insert_vacuum_threshold` columns of the table description endpoint were computed against `pg_stat_user_tables.n_live_tup` instead of `pg_class.reltuples`. PostgreSQL itself uses `reltuples` when deciding whether to trigger autovacuum/autoanalyze, so the displayed thresholds did not match real autovacuum behaviour — especially right after large bulk inserts/deletes where the two counters diverge until the next ANALYZE. The SQL now reads `GREATEST(c.reltuples, 0)::bigint` (clamping the PG ≥14 "no statistics" sentinel value `-1` to `0`) and joins it into all three threshold formulas. Tooltips in `frontend/src/locales/{ru_RU,de_DE}.json` (`vacuumThreshold`, `analyzeThreshold`, `insertVacThreshold`) updated to reference `pg_class.reltuples` accordingly.

### Dependencies (Dependabot bumps since v1.0.0)
- **Backend (Go):** `oapi-codegen/runtime` `v1.4.0` → `v1.4.1`, `yandex-cloud/go-genproto` `v0.80.0` → `v0.82.0`.
- **Frontend (npm):** `vue-i18n` `^11.1.12` → `^11.4.4` (Node engine requirement bumped to `>= 22` by upstream — already satisfied by the `node:26-alpine` build image), `eslint-plugin-playwright` `^2.10.2` → `^2.10.4` (dev), `vite-plugin-vue-devtools` `^8.0.3` → `^8.1.2` (dev, adds Vite 8 peer support).

## v1.0.0

### Breaking Changes
- **Helm chart:** `ingress.tls.certManager.reflectToNamespace` removed. Reflector integration (emberstack `kubernetes-reflector`) is no longer rendered — add the annotations manually via `ingress.annotations` if you still need it. (`ingress.tls.certNamespace` is **kept** for users whose ingress controller lives in a separate namespace, e.g. Istio.)
- **Helm chart:** ingress/gateway routing simplified. With `frontend.enabled: true` (default), only a single `/` rule is rendered — frontend nginx handles `/api/` and `/auth/` proxying. The previous separate `/api/` Ingress rule is gone. Headless deploys (`frontend.enabled: false`) keep direct `/api/` and `/auth/` rules to backend.

### Security
- **Backend HTTPS enforcement:**
  - `auth.NewMiddlewares` emits a single zap WARNING at startup when `auth.mode != none && !require_https` — surfaces the case where credentials may be transmitted in plaintext. Unit tests added in `backend/internal/auth/auth_test.go` (4 cases).
  - Helm `configmap.yaml` auto-injects `auth.require_https: true` into the rendered `dasha.yaml` when `auth.mode != none && tls.enabled` (via new `dasha.tlsEnabled` helper which ORs `ingress.tls.enabled` and `gatewayAPI.tls.enabled`). Explicit `auth.require_https: false` from values is preserved as escape hatch.
  - Frontend nginx preserves the original `X-Forwarded-Proto` via a `map`-block (`/etc/nginx/conf.d/00-proto-map.conf`: `$http_x_forwarded_proto → $forwarded_proto`, fallback to `$scheme` when the header is absent). Both `proxy_pass` blocks (`/api/`, `/auth/`) now use `$forwarded_proto`. Previously `$scheme` rewrote the header to in-cluster `http` and silently broke `require_https`.
- **Container hardening:**
  - `Dockerfile.backend` and `Dockerfile.frontend` run as non-root (`USER dasha` / `USER nginx`). Nginx main config patched: `user nginx;` directive removed and pid moved to `/tmp/nginx.pid` so the process starts without root.
  - Helm default container `securityContext`: `allowPrivilegeEscalation: false`, `readOnlyRootFilesystem: true`, `capabilities.drop: [ALL]`, `seccompProfile.type: RuntimeDefault`. Pod-level `runAsNonRoot: true` + `runAsUser/Group/fsGroup` (1000 backend, 101 frontend nginx). `emptyDir` mounts for `/tmp` (backend) and `/var/cache/nginx`, `/etc/nginx/conf.d`, `/tmp` (frontend) keep ROFS working.
- **Go dependency CVE patches** (from `trivy fs` sweep): `pgx/v5` `v5.7.6` → `v5.9.0` (CRITICAL memory-safety, CVE-2026-33816), `go-jose/v4` `v4.1.3` → `v4.1.4` (HIGH DoS via crafted JWE, CVE-2026-34986), `golang-jwt/v4` `v4.5.1` → `v4.5.2` (HIGH memory allocation in header parsing, CVE-2025-30204), `grpc` `v1.79.2` → `v1.79.3` (HIGH HTTP/2 path validation auth bypass, CVE-2026-33186). `CVE-2026-34040` in `docker/docker` (transitive via `testcontainers-go`, server-side bug not exercised by client) ignored via `.trivyignore` with rationale.

### Helm
- **Defense-in-depth HTTP→HTTPS redirect** at three layers when `tls.enabled`:
  - Ingress: `nginx.ingress.kubernetes.io/ssl-redirect` and `force-ssl-redirect` annotations auto-added.
  - Gateway API: separate `HTTPRoute` with `RequestRedirect` filter on the HTTP listener.
  - Frontend nginx: `FORCE_HTTPS_REDIRECT=true` env (auto-set by chart) injects an `if ($http_x_forwarded_proto = "http") { return 301 ... }` block. Requests without `X-Forwarded-Proto` (probes, port-forward) are not redirected.
- **Kubernetes Gateway API support** (`gateway.networking.k8s.io/v1`): new `gatewayAPI.*` values block, new templates `gateway.yaml`, `httproute.yaml`, `httproute-redirect.yaml`, `gateway-certificate.yaml`. Portable between Istio, NGINX Gateway Fabric, Envoy Gateway, Cilium. `ingress.enabled` and `gatewayAPI.enabled` are mutually exclusive — `helm template` fails via `dasha.validateTrafficMode` if both are true. `dasha.validateGatewayAPI` additionally requires `allowedRoutes.namespaces.from != "Same"` when `gatewayNamespace` differs from the release namespace, otherwise HTTPRoute cannot attach.
- **New helpers in `_helpers.tpl`:** `dasha.tlsEnabled`, `dasha.validateTrafficMode`, `dasha.validateGatewayAPI`, `dasha.gatewayTLSSecretName`, `dasha.gatewayNamespace`.

### CI / Tooling
- **Trivy filesystem + config scan** (`trivy-scan` job) — scans dependencies (`go.sum`, `package-lock.json`) and IaC misconfig (Dockerfile, Helm chart) on every push/PR. Blocks merge on `CRITICAL`/`HIGH` (`ignore-unfixed: true` to avoid noise on advisories without a patch). `skip-dirs: demo` excludes demo-lab artifacts.
- **Release: Trivy image scan now gating** — `exit-code: 0` → `1` in `release.yaml`. Releases fail on `CRITICAL`/`HIGH` in published images (previously: report only).
- **CodeQL workflow** (`.github/workflows/codeql.yaml`) — Go + TypeScript static analysis with `security-extended` query suite. Runs on push, PR, and weekly schedule (Mon 06:00 UTC). Findings appear in the Security tab.
- **Dependabot expanded** to `gomod` (`/backend`), `npm` (`/frontend`), and Docker base images in `/deploy/images`. Grouped updates for OpenTelemetry, gRPC/protobuf, Vuetify, Vue core, ESLint, Vite to reduce PR noise.

### Dependencies (Dependabot bumps since v0.1.23)
- **Backend (Go):** `pgx/v5` `v5.9.0` → `v5.9.2`, `getkin/kin-openapi` `v0.133.0` → `v0.138.0`, `labstack/echo/v4` `v4.15.1` → `v4.15.2`, `spf13/cobra` `v1.10.1` → `v1.10.2`, `coreos/go-oidc/v3` `v3.17.0` → `v3.18.0`, `go.uber.org/zap` `v1.27.0` → `v1.28.0`, `oapi-codegen/runtime` `v1.1.2` → `v1.4.0`, `yandex-cloud/go-genproto` (bump).
- **Frontend (npm):** `vuetify` group, `vue-core` group (6 packages), `vitest` `3.2.4` → `4.1.6`, `prettier` `3.6.2` → `3.8.3`, `eslint` group (3 packages), `@tsconfig/node22` `22.0.2` → `22.0.5`.
- **Containers:** `alpine` `3.21` → `3.23`, `node` `22-alpine` → `26-alpine`.
- **GitHub Actions:** `github/codeql-action` `v3` → `v4`.

### Misc
- Dependabot config bugfix.

## v0.1.23

#### Security
- **Go toolchain CVEs (10 vulnerabilities)**: bumped CI Go version pin from `1.26` to `1.26.x` (floating patch) to pick up `go1.26.3`. Fixes:
  - `html/template` XSS via meta-content URL escaping bypass (GO-2026-4982), escaper bypass (GO-2026-4980), JsBraceDepth context tracking (GO-2026-4865)
  - `crypto/x509` unexpected work in chain building (GO-2026-4947), inefficient policy validation (GO-2026-4946), case-sensitive excludedSubtrees auth bypass (GO-2026-4866)
  - `crypto/tls` unauthenticated TLS 1.3 KeyUpdate DoS (GO-2026-4870)
  - `net` panic on NUL byte in Dial/LookupPort on Windows (GO-2026-4971)
  - `net/http` HTTP/2 infinite loop on bad SETTINGS_MAX_FRAME_SIZE (GO-2026-4918)
- **`golang.org/x/net` bump** `v0.50.0` → `v0.53.0` — fixes HTTP/2 server panic on crafted frames (GO-2026-4559) and the above HTTP/2 SETTINGS_MAX_FRAME_SIZE issue (GO-2026-4918)
- `go.mod` `go` directive kept at `1.26.0` (minimum) — only the CI/Docker toolchain floats forward; `golang:1.26-alpine` image already auto-picks the latest patch
- **CI: `govulncheck` job** added to `.github/workflows/ci.yaml` and wired into `build-check.needs` — every push and PR is now scanned for symbol-level Go vulnerabilities, blocking merge to `main` on any finding
- **CI: `npm audit` job** added for the frontend (`vulncheck-frontend`), also wired into `build-check.needs`. Fails on `high` or `critical` advisories (`--audit-level=high`) against `frontend/package-lock.json`

## v0.1.22
- **Chart** ESO version

## v0.1.20

#### New Features
- **Active Queries — query text filter**: text field with `LIKE` / `NOT LIKE` toggle, case-insensitive (`ILIKE` on the backend). Wildcards (`%`, `_`) are explicit. New query params on `GET /api/queries/running`: `query_filter`, `query_filter_mode`
- **Active Queries — username filter**: autocomplete sourced from `/api/common/database-users` (same source as Query Report exclude-users). New query param `username`
- **Active Queries — Play/Stop + refresh interval**: Play/Stop button + interval selector (1 / 5 / 10 sec, default 5). Auto-refresh starts only on user click (same UX as Operation Progress) and is capped at 5 minutes; remaining time is shown next to the Play/Stop button. Interval changes restart the timer in flight. Cluster switch stops auto-refresh
- **Active Queries — query text in expanded row**: SQL is moved out of the column into a per-row expanded cell (same pattern as Top Tables by Size). Syntax highlighting + copy-to-clipboard button, query truncated at 100 chars with a "Show SQL" dialog for the full text (same UX as Query Report card). The `state` column is removed from the table — for non-idle queries it is almost always `active`
- **Query Report / Compare — stddev and usernames**: new fields `StddevExecTimeMs`, `StddevPlanTimeMs` (`max(stddev_*_time)` across aggregated `pg_stat_statements` rows) and `Usernames` (`array_agg(DISTINCT rolname)`). σ is shown next to avg on the `min..max, avg` line; usernames render as chips — in the report card next to queryid, in the comparison card as a single full-width row per A/B side (with i18n plural support: «Пользователь / Пользователя / Пользователей»). All three report SQL templates (base / 150000 / 170000) updated

#### Bug Fixes
- **OIDC error pages**: all auth callback failures (token exchange, missing id_token, invalid id_token, claims parse, session error) and login state-cookie generation now render the styled apology page (`oidc_unavailable.html`) instead of raw JSON. The HTML is now a `html/template` with `{{.Message}}` and `{{.ShowRetry}}` substitution; specific error contexts get tailored messages, and a "Try logging in again" link is shown when retry makes sense

#### Improvements
- **Active Queries — section state in Pinia**: `activeQueries` store (per-cluster, localStorage) now persists `minDuration`, `queryFilter`, `queryFilterMode`, `username`, `intervalSec`. Smooth cluster switching with restored UI state. Auto-refresh `running` state is intentionally **not** persisted — the timer must be re-armed by the user after a cluster switch or page reload
- **`useAutoRefresh` composable**: `pollInterval` accepts a getter `() => number` for reactive intervals; new `restart()` method to reapply interval mid-flight
- **Locks tree / Active Queries — human-readable durations**: durations now render as `2 h 30 min` / `45 sec` / `120 ms` via `fmtMs(ms, t)` instead of the raw PG interval string `00:01:23.456`. Backend: `QueryBlocked` now also returns `BlockedDurationMs` / `BlockingDurationMs` (`EXTRACT(EPOCH FROM age(...)) * 1000`); Active Queries table column is bound to `DurationMs` for correct numeric sorting
- **Active Queries — pause auto-refresh on SQL copy / show**: clicking the copy or "Show SQL" button stops auto-refresh so the row doesn't disappear while the user is reading
- **`truncateSql` / `SQL_PREVIEW_MAX` shared in `utils/sql.ts`**: deduplicated local truncation helpers in `RunningQueriesSection` and `ReportCard` (previously 100 vs 120 chars) — single shared 100-char preview threshold

## v0.1.19

#### Bug Fixes
- **Query Report — CPU time**: previously could be negative or exceed 100% when a query ran with parallel workers (because `pg_stat_statements` aggregates IO time across all workers, while `total_exec_time` is wall-clock leader). Now: backend returns `null` for `cpu_time` when math gives a negative result; frontend renders `?` icon with explanatory tooltip. IO time now also includes `temp_blk_read_time + temp_blk_write_time` (PG15+) for completeness. New `150000/` SQL template added so PG14 (which lacks temp_blk timing) keeps its existing formula
- **Index Usage**: tables with `seq_scan > 0, idx_scan = 0` now show `0%` instead of the "Insufficient data" placeholder; `—` is shown only when there is no scan activity at all
- **Table Describe — cluster switch**: switching cluster no longer triggers 404; selected table is cleared and stale table data is dropped. `useClusterSelector.pushToUrl(true)` drops cluster-specific query params (`schema`, `table`, etc.) on cluster change; `isSyncing` is held through `nextTick` to suppress the host/db watcher from re-adding extras; `DescribeTableSelector` is remounted via `:key="clusterName"`
- **Table Describe — Bloat card**: now resets on cluster / host / database / schema / table change (was retaining stale data when context changed)
- **Sidebar submenu state**: Tables / Indexes submenu expands correctly after page reload when the current route is inside the submenu (root cause: router readiness was not awaited before app mount). Navigation-based expansion via per-group `:model-value` computeds — Vuetify did not always pick up changes to the parent v-list `:opened` array

#### Improvements
- **Connection States / Connection Sources**: empty cells for service processes (autovacuum launcher, walwriter, checkpointer, etc.) are now filled with `backend_type` from `pg_stat_activity` via `COALESCE`
- **Connection States chart**: deterministic per-state color via HSL hash for unknown `backend_type` values (was using a single brown fallback for all service processes)
- **Index Bloat**: byte-size columns rendered via `fmtBytes` (KB/MB/GB); redundant "(bytes)" suffix removed from column headers

## v0.1.18

#### New Features
- **pg_stat_statements snapshots**: save and view pgss snapshots in a dedicated storage database
  - Optional PostgreSQL storage database (`storage.dsn` in config)
  - Daily-partitioned tables: `snapshots` (report JSON) and `query_texts` (deduplicated by SHA-256 hash)
  - `dasha migrate` CLI command creates tables
  - Frontend: snapshot create button, snapshot selector (live data / saved snapshot), shareable URLs with `?snapshot=uuid`
  - Snapshot-aware query report: hides "exclude users" filter, shows snackbar when snapshot from URL is not found
- **Query Compare**: side-by-side comparison of two snapshots or one snapshot vs live data (`GET /api/queries/compare`)
  - Sort by total_time / calls / WAL / rows / cpu_time / io_time / temp_blks
  - Filters "hide queries absent in A/B"; per-query Left/Right metrics block with deltas
  - Exclude-users filter for the live side
  - Menu item is hidden automatically when snapshot storage is not configured (probed via `GET /api/queries/snapshots/status`, cached in Pinia for 10 min)
- **Table Describe — Row Estimate Analysis**: new section showing tuple header, null bitmap, row data width, estimated row size, fillfactor, page-usable / available space, rows-per-page, HOT-update reserve, WILL_TOAST warning and TOAST-candidate columns (`GET /api/tables/describe-row-estimate`)
- **Table Describe — Vacuum Stats**: last (auto)vacuum / (auto)analyze timestamps, dead/live tuples, `n_mod_since_analyze`, `n_ins_since_vacuum`, computed vacuum / analyze / insert-vacuum thresholds from global + per-table reloptions (`GET /api/tables/describe-vacuum-stats`)
- **SQL sanitization**: `sanitize.SQL()` masks `password=` and `PASSWORD 'x'` in query text fields
- **OIDC role mapping**: `role_mapping` in OIDCConfig maps corporate groups to dasha roles (admin/viewer)
- **pg_stat_statements reset**: `POST /api/queries/reset-stats` (admin-only), controlled by `enable_query_stats_reset` config

#### Bug Fixes
- **Backend**: 404 responses now return correct HTTP status (was 500 due to oapi-codegen strict handler ignoring response object when error is non-nil)
- **Frontend**: global error handling via provide/inject — error code from API propagated correctly (was always 500)
- **Frontend**: "No clusters available" error no longer disappears on route change
- **Frontend**: invalid cluster/host in URL now shows 404 with similar name suggestions instead of silent redirect
- **Query Report / Top10: queryid precision loss**
- **Running queries: NULL scan error**: `GetQueriesRunning` crashed with `cannot scan NULL into *string` for background processes (autovacuum, walsender, logical replication worker) where `usename` is NULL. `usename` and `backend_type` are wrapped in `COALESCE(..., '')` across all three SQL templates (base, 100000/, 90600/).


#### Improvements
- Section components use `useViewError()` directly instead of emit chain — removes indirection, preserves error codes
- `useClusterInfo` returns null for unknown cluster/host — blocks API calls with invalid params
- Login card with SSO button, version display, return URL preservation across OIDC redirect
- `ApiError` class with HTTP status extracted from response body
- `IoCpuScatterSection`: axes auto-scale to ms / s / min / h based on data range
- `DescribeTableSelector`: on cluster change resets schema to `public` (when present) and clears selected table; schema watcher prefers `public` over first-in-list
- `DescribeBloatSection` now renders only for regular tables (was unconditional)
- Frontend Docker image embeds `BUILD_NUMBER` via `VITE_APP_VERSION` env — version shown in login card and user menu
- Nginx: added `X-Forwarded-Proto`, dedicated `/auth/` location block, larger `proxy_buffer_size` / `proxy_buffers` for OIDC cookie-heavy responses
- `ErrorAlert` component for full-page error fallback with illustration
- **Query Report: substring search**: new text field in the report header filters cards by substring match against the full query text (including the part hidden behind the ellipsis in the card) or queryid. 200 ms debounce; clearable via the "×" button.


#### Demo
- Added storage database service for snapshots
- `dasha migrate` runs automatically before app start

## v0.1.13

#### New Features
- **Authentication & Authorization**: three modes — `none` (default), `token` (static API keys), `oidc` (OpenID Connect BFF)
- **RBAC**: role-based access via Casbin — `admin` (full) and `viewer` (read-only)
- **Per-identity rate limiting**: token bucket with configurable RPS and burst

#### Security
- Constant-time token and OAuth2 state comparison
- Secure random generation via `crypto/rand`
- Refresh token revocation on logout (RFC 7009)
- Reverse proxy support for `Secure` cookie flag

#### Demo
- Keycloak OIDC provider with preconfigured realm, users and roles
- Fixed logical replication race condition in standalone init

## v0.1.12

- Visual improvements

## v0.1.11

#### New Features
- **Replication view**: new page with 3 sections — config, status, slots
  - `GET /api/replication/status` — pg_stat_replication with LEFT JOIN pg_replication_slots (slot per replica), state/sync_state chips with tooltips, client_addr/PID/LSN in expandable rows
  - `GET /api/replication/slots` — slot_type, wal_status (with tooltip explanations), safe_wal_size, backlog_bytes
  - `GET /api/replication/config` — synchronous_standby_names + synchronous_commit with tooltip descriptions for each mode (on, remote_apply, remote_write, local, off)
- **Database health**: new `GET /api/database/health` — deadlocks, conflicts, checksum failures, rollback ratio from pg_stat_database
- **Wait events**: new `GET /api/connection/wait-events` — aggregated wait events from pg_stat_activity (excluding idle Client.ClientRead)

#### Frontend
- **ReplicationView**: ReplicationConfigSection (settings with chip tooltips), ReplicationStatusSection (lag color coding, state/sync chips with tooltips, expandable rows), ReplicationSlotsSection (wal_status chip tooltips)
- **DatabaseHealthSection**: chip-based health indicators on Home page with green/yellow/red thresholds
- **WaitEventsSection**: wait events table on Home page with wait type color coding
- Navigation: added "Replication" menu item with `mdi-database-sync-outline` icon
- `fmtLag` and `fmtBytes` extracted to shared `utils/format.ts`

#### Backend
- New SQL templates: `replication/status` (with `LEFT JOIN pg_replication_slots`), `connections/wait_events`, `database/health`
- Enriched `replication/slots` with slot_type, wal_status, safe_wal_size, backlog_bytes
- New `replication/config` — `current_setting()` for synchronous_standby_names and synchronous_commit
- `pg_is_in_recovery()` guard on `pg_current_wal_lsn()` calls (replica-safe)
- New DTOs: ReplicationStatus, ReplicationConfig, ReplicationSlot, WaitEvent, DatabaseHealth

#### Demo
- Added deadlock generator to workload script for database health demonstration

## v0.1.10

### Table Describe (`\d+`) Enhancements

#### New Features
- **Table/schema selector**: autocomplete search for schemas and tables with URL sync
- **Index metadata**: size column, primary/unique/invalid icons with tooltips
- **Column statistics**: `null_frac`, `n_distinct`, `avg_width` from `pg_stats` in expandable rows
- **Estimated row count**: `reltuples` from `pg_class` in human-readable format (K/M/B)
- **Activity stats**: INS/UPD/DEL/SEQ_SCN/IDX_SCN from `pg_stat_all_tables`
- **Partitions display**: paginated list of child partitions for partitioned tables with describe links
- **Bloat estimation**: `pgstattuple_approx()` results via "Calculate bloat" button, disabled with status chip when extension is unavailable
- **Describe links**: clickable table names across 12 index/table section components via shared `useDescribeLink` composable

#### Backend
- New SQL templates: `describe_bloat`, `describe_partitions`, `describe_schemas`, `describe_search`, `pgstattuple_available`
- Extended `describe_columns` with `LEFT JOIN pg_stats` (null_frac, n_distinct, avg_width)
- Extended `describe_indexes` with `indisvalid`, `pg_relation_size`, `pg_size_pretty`
- Extended `describe_metadata` with `reltuples`, stat_info subquery
- New API endpoints: `GET /api/tables/describe-bloat`, `GET /api/tables/describe-partitions`, `GET /api/tables/pgstattuple-available`, `GET /api/tables/schemas`, `GET /api/tables/search`
- 1-minute query timeout for `pgstattuple_approx`

#### Frontend
- **Refactored** `TableDescribeView.vue` from ~580 lines into 8 focused components:
  - `DescribeTableSelector` — schema/table autocomplete with URL sync
  - `DescribeHeaderSection` — table metadata and size cards
  - `DescribeColumnsSection` — columns with expanded stats rows
  - `DescribeIndexesSection` — indexes with PK/unique/invalid icons
  - `DescribeConstraintsSection` — reusable for check and FK constraints
  - `DescribeReferencedBySection` — referenced-by with source table
  - `DescribePartitionsSection` — paginated partitions via `usePaginatedApiLoader`
  - `DescribeBloatSection` — pgstattuple availability check and bloat calculation
- Added `fmtRowCount` to shared `utils/format.ts`
- Added Russian plural rules for vue-i18n (`pluralRules` in main.ts)
- Navigation: "Tables" menu split into "Overview" and "Describe" sub-items

#### Demo
- Added `pgstattuple` extension to demo init scripts

