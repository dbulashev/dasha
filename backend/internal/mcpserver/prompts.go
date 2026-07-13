package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// promptTexts holds every language-dependent string of the server: the
// instructions sent at initialization and the playbook text of each prompt.
// Two complete sets (en/ru), no machine translation at runtime; tool names,
// schemas and results stay English regardless. The playbooks are deliberately
// linear — numbered steps, one tool per step, an interpretation criterion on
// each — because weak models lose branching scenarios. Criteria duplicate the
// dasha://kb resources on purpose: a client without resource support still
// gets a self-sufficient playbook.
type promptTexts struct {
	instructions string

	diagnose    string // fmt args: cluster, instance
	explain     string // fmt args: cluster, instance, database suffix
	indexes     string // fmt args: database, cluster, instance
	slowQueries string // fmt args: cluster, instance, database
	fleet       string
}

// textsFor returns the language's text set, falling back to English.
func textsFor(lang string) promptTexts {
	if t, ok := texts[lang]; ok {
		return t
	}

	return texts[kbDefaultLang]
}

var texts = map[string]promptTexts{
	"en": {
		instructions: `Dasha exposes read-only PostgreSQL fleet diagnostics. All tools are safe to call.

Getting oriented:
- Call list_clusters first to get cluster and instance (host) names, or fleet_health for a worst-first overview of the whole fleet.
- Most tools require cluster + instance; query/index/table/lock tools also require database.

Investigating:
- For a guided workflow prefer the prompts: diagnose_cluster, explain_health_score, investigate_slow_queries, find_index_opportunities, fleet_overview.
- Typical chain: get_health_score -> get_health_recommendations -> (top_queries, blocked_queries, list_indexes, describe_table) to drill into the worst findings.
- A recommendation names the rule, not the culprit: it reports a rule_id and a count/ratio, never a table. To turn it into an actionable target call health_details with detail = that rule_id — it returns the offending tables, databases or sessions.
- A named object is still not a cause. Before advising anything, confirm the mechanism with describe_table (fillfactor, the index list, the HOT share in StatInfo) and top_queries for the statement itself: 0% HOT means the UPDATE touches an indexed column, and only describe_table says WHICH one. Never name a table, column or index that is not in a tool's output, and never merely offer to check — every tool here is read-only and cheap, so just call it.
- Bloat remediation has a cost, and you must state it instead of hiding it. Plain VACUUM is safe (SHARE UPDATE EXCLUSIVE — it blocks neither reads nor writes, rewrites nothing, needs no extra disk) but it only makes the dead space REUSABLE: the file does not shrink. Returning space to the OS needs VACUUM FULL (ACCESS EXCLUSIVE — blocks even SELECT for the whole rewrite) or pg_repack (online, only brief locks, but an extension that may not be installed) — and BOTH need roughly twice the table+index size in free disk. Never recommend either without first quoting the table's current size from describe_table or top_tables, so the caller can weigh the cost; for a table that keeps being written, plain VACUUM plus a working autovacuum is usually the right answer and the file size stops mattering.
- Never advise dropping an index from a scan counter, and never hedge it ("drop it if nobody queries that column") — call unused_index_report and find out. It is cluster-wide (no instance) because idx_scan is not replicated, and it weighs the counter against the statistics window behind it; only verdict='drop_candidate' justifies a DROP, on any other verdict repeat its reason. The one exception is a structurally redundant index — an exact duplicate of another, or an invalid one — which describe_table already exposes: its safety does not depend on usage.
- health_trend needs metrics-backed mode (a configured datasource); it returns an error otherwise.
- query_compare needs snapshot IDs from list_snapshots.
- search_logs works only on clusters with supports_logs=true (see list_clusters) and is rate-limited per user because every call reaches the Yandex Cloud API: combine all filters into one call, keep dedup on, and after a 429 wait ~30 seconds instead of retrying immediately.
- If unsure how to interpret a result or which tool to call next, read the resources first: dasha://kb/workflow (complaint-to-tool-chain playbooks), dasha://kb/health-rules (rule thresholds and first actions), dasha://kb/wait-events (wait event glossary).

If a result is refused as too large, narrow it (one database, a smaller range, or a more specific tool).`,

		diagnose: "Diagnose the health of cluster %q instance %q. Execute strictly in order, one tool per step:\n" +
			"1. get_health_score. score >= 80 — healthy: report briefly and stop; 40-79 — degraded; < 40 — critical. " +
			"Note the two worst categories by penalty.\n" +
			"2. get_health_recommendations. Match recommendations to the worst categories, HIGH severity first. " +
			"For unfamiliar rule IDs read the resource dasha://kb/health-rules.\n" +
			"3. Only if locks is among the worst: blocked_queries, then running_queries — find the root blocker " +
			"(often idle in transaction). Suggest pg_terminate_backend for the BLOCKER, never for the victims.\n" +
			"4. Only if performance is among the worst: top_queries (by=time). Few calls with high mean time = plan " +
			"problem (suggest EXPLAIN / indexes); huge calls with low mean time = frequency problem (caching/batching).\n" +
			"5. Only if maintenance or horizon is among the worst: vacuum_danger. XID age >= 200M = forced-autovacuum " +
			"zone; >= 1.6B = emergency (VACUUM FREEZE now).\n" +
			"Report 3-5 findings, worst first, each = fact (numbers from tool output) + cause + one concrete action. " +
			"Never invent metrics that are not in the tool output.",

		explain: "Explain the health score of cluster %q instance %q%s. Execute in order:\n" +
			"1. get_health_score — record the overall number and each category's penalty and weight.\n" +
			"2. get_health_recommendations — map every recommendation to its category.\n" +
			"3. Read the resource dasha://kb/health-rules for thresholds, category weights and the critical ceiling " +
			"(one catastrophic condition clamps the score to <= 30).\n" +
			"Then explain: the number and its band (>= 80 healthy, 40-79 degraded, < 40 critical), which categories " +
			"drag it down and by how much, whether a critical ceiling applies, and each recommendation's meaning " +
			"with its first action.",

		indexes: "Find indexing opportunities in database %q of cluster %q instance %q. Execute in order:\n" +
			"1. list_indexes (kind=missing) — candidate new indexes.\n" +
			"2. unused_index_report (cluster-wide, takes no instance) — drop candidates WITH a verdict. Recommend a " +
			"DROP only for verdict='drop_candidate'; on any other verdict repeat its reason instead. Do NOT judge " +
			"from list_indexes (kind=unused): a raw scan counter sees neither the replicas nor the statistics window.\n" +
			"3. top_queries (by=time) — tie every candidate to a heavy query it would help " +
			"(or a write-heavy table an unused index hurts).\n" +
			"Recommend indexes to add and unused ones to drop, each tied to specific queries. " +
			"If sequential scans dominate, read dasha://kb/health-rules (seq_scan_regression) before concluding.",

		slowQueries: "Investigate slow queries on cluster %q instance %q (database %q). Execute in order:\n" +
			"1. top_queries (by=time). Few calls with high mean time = plan problem; huge calls with low mean time = " +
			"frequency problem (caching/batching).\n" +
			"2. running_queries — statements running for minutes, and idle-in-transaction sessions.\n" +
			"3. blocked_queries — lock waits masquerade as slowness; if present, find the root blocker.\n" +
			"4. wait_events — the dominant event names the bottleneck class; interpret via the resource " +
			"dasha://kb/wait-events.\n" +
			"Report the heaviest statements, anything stuck or blocked, and next steps: EXPLAIN for plan problems, " +
			"caching/batching for frequency problems, terminating the blocker for lock problems.",

		fleet: "Give a fleet health overview. Execute in order:\n" +
			"1. fleet_health — one call returns the worst instances; do NOT loop list_clusters + get_health_score.\n" +
			"2. For the one or two worst instances: get_health_recommendations to name their main issues.\n" +
			"Report: how many instances, the score spread, the worst instances with their top issues, and which " +
			"single instance to fix first. For deeper drill-down chains read the resource dasha://kb/workflow.",
	},

	"ru": {
		instructions: `Dasha предоставляет read-only диагностику флота PostgreSQL. Все инструменты безопасны.

С чего начать:
- Сначала list_clusters — имена кластеров и инстансов (хостов), либо fleet_health — обзор всего флота от худших.
- Большинству инструментов нужны cluster + instance; инструментам по запросам/индексам/таблицам/блокировкам — ещё database.

Расследование:
- Для направляемого сценария используйте prompts: diagnose_cluster, explain_health_score, investigate_slow_queries, find_index_opportunities, fleet_overview.
- Типовая цепочка: get_health_score -> get_health_recommendations -> (top_queries, blocked_queries, list_indexes, describe_table) по худшим находкам.
- Рекомендация называет правило, а не виновника: она возвращает rule_id и счётчик/долю, но никогда — таблицу. Чтобы превратить её в конкретную цель, вызовите health_details с detail = этот rule_id — он вернёт сами таблицы, базы или сессии.
- Названный объект — ещё не причина. Прежде чем что-либо советовать, подтвердите механизм: describe_table (fillfactor, список индексов, доля HOT в StatInfo) и top_queries — сам запрос. 0% HOT означает, что UPDATE трогает проиндексированную колонку, и только describe_table скажет, КАКУЮ именно. Не называйте таблицу, колонку или индекс, которых нет в выводе инструментов, и не предлагайте «посмотреть, если нужно» — все инструменты здесь read-only и дешёвые, просто вызовите их.
- У борьбы с раздуванием есть цена, и её надо называть, а не умалчивать. Обычный VACUUM безопасен (SHARE UPDATE EXCLUSIVE — не блокирует ни чтения, ни записи, ничего не перезаписывает, места на диске не требует), но он лишь возвращает мёртвое место В ПЕРЕИСПОЛЬЗОВАНИЕ: файл не сжимается. Чтобы отдать место операционной системе, нужен VACUUM FULL (ACCESS EXCLUSIVE — на всё время перезаписи блокирует даже SELECT) или pg_repack (онлайн, короткие блокировки, но это расширение, которого может не быть) — и ОБОИМ нужно примерно вдвое больше свободного места, чем занимают таблица и её индексы. Никогда не рекомендуйте их, не приведя текущий размер таблицы из describe_table или top_tables, чтобы человек мог взвесить цену; для таблицы, в которую продолжают писать, обычно правильный ответ — обычный VACUUM плюс работающий автовакуум, и тогда размер файла перестаёт быть проблемой.
- Никогда не советуйте удалять индекс по счётчику сканов и не хеджируйте («убрать, если поиск по колонке не критичен») — вызовите unused_index_report и выясните. Он работает по всему кластеру (instance не нужен), потому что idx_scan не реплицируется, и взвешивает счётчик по окну статистики за ним; DROP оправдан только при verdict='drop_candidate', при любом другом — повторите его reason. Единственное исключение — структурно избыточный индекс (точный дубликат другого или invalid), который и так виден в describe_table: его безопасность от сканов не зависит.
- health_trend требует режима метрик (настроенный datasource), иначе вернёт ошибку.
- query_compare требует ID снимков из list_snapshots.
- search_logs работает только на кластерах с supports_logs=true (см. list_clusters) и лимитирован per-user, т.к. каждый вызов уходит в Yandex Cloud API: собирайте все фильтры в один вызов, держите dedup включённым, после 429 ждите ~30 секунд вместо немедленного повтора.
- Если непонятно, как трактовать результат или какой инструмент звать дальше — сначала прочитайте ресурсы: dasha://kb/workflow (сценарии «жалоба -> цепочка»), dasha://kb/health-rules (пороги правил и первые действия), dasha://kb/wait-events (глоссарий wait events).

Если результат отклонён как слишком большой — сузьте запрос (одна база, меньший диапазон или более специфичный инструмент).`,

		diagnose: "Продиагностируй здоровье кластера %q, инстанс %q. Выполняй строго по порядку, один инструмент на шаг:\n" +
			"1. get_health_score. score >= 80 — здоров: кратко доложи и остановись; 40-79 — деградация; < 40 — критично. " +
			"Зафиксируй две худшие категории по штрафу.\n" +
			"2. get_health_recommendations. Сопоставь рекомендации с худшими категориями, сначала HIGH. " +
			"Незнакомые rule ID смотри в ресурсе dasha://kb/health-rules.\n" +
			"3. Только если среди худших locks: blocked_queries, затем running_queries — найди корневого блокировщика " +
			"(часто idle in transaction). Предложи pg_terminate_backend для БЛОКИРОВЩИКА; жертв не завершай — блокировку это не снимет.\n" +
			"4. Только если среди худших performance: top_queries (by=time). Мало вызовов с высоким средним временем = " +
			"проблема плана (предложи EXPLAIN / индексы); огромное число вызовов с низким средним = проблема частоты " +
			"(кэширование/батчинг).\n" +
			"5. Только если среди худших maintenance или horizon: vacuum_danger. Возраст XID >= 200M = зона " +
			"принудительного autovacuum; >= 1.6B = авария (VACUUM FREEZE немедленно).\n" +
			"Отчёт: 3-5 находок, худшее первым, каждая = факт (числа из вывода инструментов) + причина + одно конкретное " +
			"действие. Не выдумывай метрики, которых нет в выводе инструментов.",

		explain: "Объясни health score кластера %q, инстанс %q%s. Выполняй по порядку:\n" +
			"1. get_health_score — зафиксируй общее число, штраф и вес каждой категории.\n" +
			"2. get_health_recommendations — привяжи каждую рекомендацию к её категории.\n" +
			"3. Прочитай ресурс dasha://kb/health-rules: пороги, веса категорий и критический потолок " +
			"(одно катастрофическое условие зажимает score до <= 30).\n" +
			"Затем объясни: число и его зону (>= 80 здоров, 40-79 деградация, < 40 критично), какие категории и " +
			"насколько его тянут вниз, действует ли критический потолок, и смысл каждой рекомендации с первым действием.",

		indexes: "Найди возможности для индексов в базе %q кластера %q, инстанс %q. Выполняй по порядку:\n" +
			"1. list_indexes (kind=missing) — кандидаты на новые индексы.\n" +
			"2. unused_index_report (по всему кластеру, instance не нужен) — кандидаты на удаление С вердиктом. " +
			"Рекомендуй DROP только при verdict='drop_candidate'; при любом другом — повтори его reason. НЕ суди по " +
			"list_indexes (kind=unused): сырой счётчик сканов не видит ни реплик, ни окна статистики.\n" +
			"3. top_queries (by=time) — привяжи каждого кандидата к тяжёлому запросу, которому он поможет " +
			"(или к write-нагруженной таблице, которой вредит неиспользуемый индекс).\n" +
			"Порекомендуй индексы к добавлению и неиспользуемые к удалению, каждый с привязкой к конкретным запросам. " +
			"Если доминируют последовательные сканы — сначала прочитай dasha://kb/health-rules (seq_scan_regression).",

		slowQueries: "Расследуй медленные запросы на кластере %q, инстанс %q (база %q). Выполняй по порядку:\n" +
			"1. top_queries (by=time). Мало вызовов с высоким средним временем = проблема плана; огромное число вызовов " +
			"с низким средним = проблема частоты (кэширование/батчинг).\n" +
			"2. running_queries — запросы, работающие минутами, и idle-in-transaction сессии.\n" +
			"3. blocked_queries — ожидания блокировок маскируются под медленность; если есть — найди корневого блокировщика.\n" +
			"4. wait_events — доминирующее событие называет класс узкого места; трактуй через ресурс dasha://kb/wait-events.\n" +
			"Доложи самые тяжёлые запросы, всё застрявшее или заблокированное, и следующие шаги: EXPLAIN для проблем " +
			"плана, кэширование/батчинг для проблем частоты, завершение блокировщика для проблем блокировок.",

		fleet: "Дай обзор здоровья флота. Выполняй по порядку:\n" +
			"1. fleet_health — один вызов возвращает худшие инстансы; НЕ перебирай list_clusters + get_health_score циклом.\n" +
			"2. Для одного-двух худших инстансов: get_health_recommendations — назови их главные проблемы.\n" +
			"Отчёт: сколько инстансов, разброс score, худшие инстансы с их главными проблемами и какой один инстанс " +
			"чинить первым. Для более глубоких цепочек прочитай ресурс dasha://kb/workflow.",
	},
}

// arg reads one prompt argument (empty when absent).
func arg(req *mcp.GetPromptRequest, key string) string {
	if req.Params == nil {
		return ""
	}

	return req.Params.Arguments[key]
}

func target(req *mcp.GetPromptRequest) (cluster, instance string) {
	return arg(req, "cluster"), arg(req, "instance")
}

func dbSuffix(db string) string {
	if db != "" {
		return " (database " + db + ")"
	}

	return ""
}

// userPrompt wraps an instruction as a single user message — a conversation seed
// that tells the model which tools to call, in what order, and how to interpret
// each step's result.
func userPrompt(desc, text string) (*mcp.GetPromptResult, error) {
	return &mcp.GetPromptResult{
		Description: desc,
		Messages: []*mcp.PromptMessage{
			{Role: "user", Content: &mcp.TextContent{Text: text}},
		},
	}, nil
}

func clusterInstanceArgs() []*mcp.PromptArgument {
	return []*mcp.PromptArgument{
		{Name: "cluster", Description: "Dasha cluster name", Required: true},
		{Name: "instance", Description: "Dasha instance / host name", Required: true},
	}
}

// registerPrompts registers the five playbook prompts using the given text set.
// Prompt names and descriptions stay English (metadata, like tool schemas);
// only the playbook message text is localized.
func registerPrompts(s *mcp.Server, t promptTexts) {
	s.AddPrompt(&mcp.Prompt{
		Name:        "diagnose_cluster",
		Description: "Diagnose why a PostgreSQL instance is unhealthy and propose fixes.",
		Arguments:   clusterInstanceArgs(),
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		cluster, instance := target(req)

		return userPrompt("Cluster diagnosis", fmt.Sprintf(t.diagnose, cluster, instance))
	})

	s.AddPrompt(&mcp.Prompt{
		Name:        "explain_health_score",
		Description: "Explain an instance's health score and its recommendations.",
		Arguments: append(clusterInstanceArgs(),
			&mcp.PromptArgument{Name: "database", Description: "Optional: per-database scope"}),
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		cluster, instance := target(req)

		return userPrompt("Health score explanation",
			fmt.Sprintf(t.explain, cluster, instance, dbSuffix(arg(req, "database"))))
	})

	s.AddPrompt(&mcp.Prompt{
		Name:        "find_index_opportunities",
		Description: "Find missing/unused indexes in a database and tie them to slow queries.",
		Arguments: append(clusterInstanceArgs(),
			&mcp.PromptArgument{Name: "database", Description: "Database to inspect", Required: true}),
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		cluster, instance := target(req)

		return userPrompt("Index opportunities",
			fmt.Sprintf(t.indexes, arg(req, "database"), cluster, instance))
	})

	s.AddPrompt(&mcp.Prompt{
		Name:        "investigate_slow_queries",
		Description: "Investigate slow / stuck / blocked queries on an instance.",
		Arguments: append(clusterInstanceArgs(),
			&mcp.PromptArgument{Name: "database", Description: "Database for running/blocked queries", Required: true}),
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		cluster, instance := target(req)

		return userPrompt("Slow query investigation",
			fmt.Sprintf(t.slowQueries, cluster, instance, arg(req, "database")))
	})

	s.AddPrompt(&mcp.Prompt{
		Name:        "fleet_overview",
		Description: "Summarise health across the whole fleet and surface the worst instances.",
	}, func(_ context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return userPrompt("Fleet overview", t.fleet)
	})
}
