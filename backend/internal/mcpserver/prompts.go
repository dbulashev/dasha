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
- "What is loading this database?" -> hot_tables / hot_indexes (class = reads, writes or io). Read snapshot.coverage before concluding anything: it says what share of total activity the stored top holds. Needs snapshot storage — a 501 means the feature is off, not a broken instance.
- A recommendation names the rule, not the culprit: it reports a rule_id and a count/ratio, never a table. To turn it into an actionable target call health_details with detail = that rule_id — it returns the offending tables, databases or sessions.
- A named object is still not a cause. Before advising anything, confirm the mechanism with describe_table (fillfactor, the index list, the HOT share in StatInfo) and top_queries for the statement itself: 0% HOT means the UPDATE touches an indexed column, and only describe_table says WHICH one. Never name a table, column or index that is not in a tool's output, and never merely offer to check — every tool here is read-only and cheap, so just call it.
- NEVER invent the remedy for a health rule. Every rule in dasha://kb/health-rules carries its first action — read that rule's section before you advise anything, and follow it. Inventing one gets the direction backwards: the classic error is "fillfactor 70 is low, raise it to 90", which is exactly wrong — free page space is what HOT updates need, so RAISING fillfactor destroys HOT. Likewise, tuning autovacuum_* thresholds does nothing on a table with autovacuum_enabled=false; it must be turned back on first. If a rule's section does not answer your case, say so rather than guessing.
- Bloat remediation has a cost, and you must state it instead of hiding it. Plain VACUUM is safe (SHARE UPDATE EXCLUSIVE — it blocks neither reads nor writes, rewrites nothing, needs no extra disk) but it only makes the dead space REUSABLE: the file does not shrink. Returning space to the OS needs VACUUM FULL (ACCESS EXCLUSIVE — blocks even SELECT for the whole rewrite) or pg_repack (online, only brief locks, but an extension that may not be installed) — and BOTH need roughly twice the table+index size in free disk. Never recommend either without first quoting the table's current size from describe_table or top_tables, so the caller can weigh the cost; for a table that keeps being written, plain VACUUM plus a working autovacuum is usually the right answer and the file size stops mattering.
- Never advise dropping an index from a scan counter, and never hedge it ("drop it if nobody queries that column") — call unused_index_report and find out. It is cluster-wide (no instance) because idx_scan is not replicated, and it weighs the counter against the statistics window behind it; only verdict='drop_candidate' justifies a DROP, on any other verdict repeat its reason. The one exception is a structurally redundant index — an exact duplicate of another, or an invalid one — which describe_table already exposes: its safety does not depend on usage.
- "Which index should I create?" is answered by index_advisor, not by list_indexes(kind='missing'): it parses the statements of every cluster host and drops what the existing indexes already serve, and it returns ready DDL. A 404 from index_advisor does not name its cause — an unknown cluster/database and the feature being off look alike: call list_clusters, and only once it shows both the cluster and the database fall back to list_indexes(kind='missing') — say then that it reads no queries. weight_pct is the share of analyzed time the covered statements hold, never a predicted speed-up: planner_checked is false. A candidate list that is empty while gaps is non-empty means part of the workload was never analyzed, not that the database is well indexed. Check unused_index_report on the same database before advising a CREATE, and hand the DDL to a human — Dasha never executes it.
- health_trend needs metrics-backed mode (a configured datasource); it returns an error otherwise.
- query_compare needs snapshot IDs from list_snapshots.
- search_logs works only on clusters with supports_logs=true (see list_clusters) and is rate-limited per user because every call reaches an external log store: combine all filters into one call, keep dedup on, and after a 429 wait ~30 seconds instead of retrying immediately.
- schema_lint answers a different question from every other tool: what is wrong with the STRUCTURE, not what is happening now. Read its skipped list before concluding anything — a check that could not run says nothing about the schema, and reporting "clean" over a non-empty skipped list is a false all-clear. Two findings have a fix that is NOT the obvious one: sequence_exhaustion on an owned_column_type of 'integer' needs the column type changed (a table rewrite, needs a window), not just ALTER SEQUENCE; and no_primary_key on a table whose unique index is nullable cannot be answered with "you already have a unique index" — that index is no replica identity. Read dasha://kb/schema-checks before advising on a code you do not know.
- "Who is doing all this I/O?" -> io_summary, and io_trend for when it started. These are the only tools that see non-client I/O: autovacuum, the checkpointer, the WAL writer. They need PostgreSQL 16+ (older hosts answer empty with empty_reason='unsupported_version', which does NOT mean no I/O) and snapshot storage. Never read a zero time metric as "fast" without checking meta.track_io_timing, and never read an incomplete io_trend point as a lull — its counters cover only coverage_pct of the bucket. An empty answer is never proof of an idle instance: only empty_reason='no_io' says that, 'no_io_in_measured_part' answers for the measured part of the window alone, and every other value means the question went unanswered. Read dasha://kb/pg-stat-io before interpreting the counters.
- If unsure how to interpret a result or which tool to call next, read the resources first: dasha://kb/workflow (complaint-to-tool-chain playbooks), dasha://kb/health-rules (rule thresholds and first actions), dasha://kb/schema-checks (schema defect codes and their fixes), dasha://kb/index-advisor (how to read index candidates), dasha://kb/wait-events (wait event glossary), dasha://kb/pg-stat-io (how to read the I/O counters).

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
			"1. index_advisor (cluster-wide, takes no instance) — candidates with ready DDL, ranked by the share of " +
			"workload time the statements behind them hold. Read gaps and warnings before quoting any candidate. " +
			"A 404 is either an unknown cluster/database or the feature disabled in Dasha's configuration, and " +
			"the error text does not say which: call list_clusters first. Only once it shows both the cluster and " +
			"the database, fall back to list_indexes (kind=missing) and say plainly that it is a " +
			"pg_stat_user_tables heuristic which reads no queries.\n" +
			"2. unused_index_report on the SAME database, BEFORE advising any CREATE: an index added on top of " +
			"redundant ones nobody removed makes writes dearer instead of making the database faster. It is " +
			"cluster-wide and takes no instance. Recommend a DROP only for verdict='drop_candidate'; on any other " +
			"verdict repeat its reason instead. Do NOT judge from list_indexes (kind=unused): a raw scan counter sees " +
			"neither the replicas nor the statistics window.\n" +
			"3. top_queries (by=time) or query_report — tie every candidate to the statement it would serve, taking " +
			"the queryid from query_id_by_host rather than from a position in a list.\n" +
			"Recommend indexes to add and unused ones to drop, each tied to specific statements. State weight_pct as " +
			"the share of analyzed time the covered statements hold, never as a speed-up: no planner saw these " +
			"candidates, and the DDL goes to a human — Dasha never executes it. " +
			"If sequential scans dominate, read dasha://kb/health-rules (seq_scan_regression) before concluding.",

		slowQueries: "Investigate slow queries on cluster %q instance %q (database %q). Execute in order:\n" +
			"1. top_queries (by=time). Few calls with high mean time = plan problem; huge calls with low mean time = " +
			"frequency problem (caching/batching).\n" +
			"2. running_queries — statements running for minutes, and idle-in-transaction sessions.\n" +
			"3. blocked_queries — lock waits masquerade as slowness; if present, find the root blocker.\n" +
			"4. wait_events — the dominant event names the bottleneck class; interpret via the resource " +
			"dasha://kb/wait-events.\n" +
			"5. Only if step 4 was dominated by an I/O event (DataFileRead, DataFileWrite, WALSync): io_summary — " +
			"wait_events says backends wait on disk but never who reads, and pg_stat_statements covers only client " +
			"backends. This is what separates client load from autovacuum, the checkpointer and bulk scans. " +
			"PostgreSQL 16+ and snapshot storage only; an empty result is not proof of no I/O.\n" +
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
- «Что нагружает базу?» -> hot_tables / hot_indexes (class = reads, writes или io). Прежде чем делать выводы, посмотрите snapshot.coverage — это доля всей активности, попавшая в сохранённый топ. Требуется snapshot-хранилище: 501 означает выключенную фичу, а не сломанный инстанс.
- Рекомендация называет правило, а не виновника: она возвращает rule_id и счётчик/долю, но никогда — таблицу. Чтобы превратить её в конкретную цель, вызовите health_details с detail = этот rule_id — он вернёт сами таблицы, базы или сессии.
- Названный объект — ещё не причина. Прежде чем что-либо советовать, подтвердите механизм: describe_table (fillfactor, список индексов, доля HOT в StatInfo) и top_queries — сам запрос. 0% HOT означает, что UPDATE трогает проиндексированную колонку, и только describe_table скажет, КАКУЮ именно. Не называйте таблицу, колонку или индекс, которых нет в выводе инструментов, и не предлагайте «посмотреть, если нужно» — все инструменты здесь read-only и дешёвые, просто вызовите их.
- НИКОГДА не выдумывайте лечение для health-правила. У каждого правила в dasha://kb/health-rules прописано первое действие — прочитайте раздел этого правила, прежде чем что-либо советовать, и следуйте ему. Выдуманное лечение обычно оказывается перевёрнутым: классическая ошибка — «fillfactor 70 — это мало, поднимем до 90», хотя всё ровно наоборот: свободное место на странице — это то, что нужно HOT-обновлениям, поэтому ПОВЫШЕНИЕ fillfactor HOT убивает. Точно так же тюнинг порогов autovacuum_* ничего не даёт на таблице с autovacuum_enabled=false — сначала его надо включить обратно. Если раздел правила не покрывает ваш случай — так и скажите, а не догадывайтесь.
- У борьбы с раздуванием есть цена, и её надо называть, а не умалчивать. Обычный VACUUM безопасен (SHARE UPDATE EXCLUSIVE — не блокирует ни чтения, ни записи, ничего не перезаписывает, места на диске не требует), но он лишь возвращает мёртвое место В ПЕРЕИСПОЛЬЗОВАНИЕ: файл не сжимается. Чтобы отдать место операционной системе, нужен VACUUM FULL (ACCESS EXCLUSIVE — на всё время перезаписи блокирует даже SELECT) или pg_repack (онлайн, короткие блокировки, но это расширение, которого может не быть) — и ОБОИМ нужно примерно вдвое больше свободного места, чем занимают таблица и её индексы. Никогда не рекомендуйте их, не приведя текущий размер таблицы из describe_table или top_tables, чтобы человек мог взвесить цену; для таблицы, в которую продолжают писать, обычно правильный ответ — обычный VACUUM плюс работающий автовакуум, и тогда размер файла перестаёт быть проблемой.
- Никогда не советуйте удалять индекс по счётчику сканов и не хеджируйте («убрать, если поиск по колонке не критичен») — вызовите unused_index_report и выясните. Он работает по всему кластеру (instance не нужен), потому что idx_scan не реплицируется, и взвешивает счётчик по окну статистики за ним; DROP оправдан только при verdict='drop_candidate', при любом другом — повторите его reason. Единственное исключение — структурно избыточный индекс (точный дубликат другого или invalid), который и так виден в describe_table: его безопасность от сканов не зависит.
- На вопрос «какой индекс создать» отвечает index_advisor, а не list_indexes(kind='missing'): он разбирает запросы со всех хостов кластера, отбрасывает то, что уже обслуживают существующие индексы, и отдаёт готовый DDL. 404 от index_advisor причину не называет — неизвестный кластер или база и выключенная фича выглядят одинаково: вызовите list_clusters, и только если и кластер, и база в нём есть, откатывайтесь на list_indexes(kind='missing'); тогда так и скажите, что он запросов не читает. weight_pct — доля проанализированного времени, которую занимают покрытые запросы, а не предсказанное ускорение: planner_checked равен false. Пустой список кандидатов при непустом gaps означает, что часть нагрузки не проанализирована, а не что база хорошо проиндексирована. Перед советом «создать» сверьтесь с unused_index_report по той же базе, а DDL отдайте человеку — Dasha его не выполняет.
- health_trend требует режима метрик (настроенный datasource), иначе вернёт ошибку.
- query_compare требует ID снимков из list_snapshots.
- search_logs работает только на кластерах с supports_logs=true (см. list_clusters) и лимитирован per-user, т.к. каждый вызов уходит во внешнее хранилище логов: собирайте все фильтры в один вызов, держите dedup включённым, после 429 ждите ~30 секунд вместо немедленного повтора.
- schema_lint отвечает не на тот вопрос, что остальные инструменты: что не так со СТРУКТУРОЙ, а не что происходит сейчас. Прежде чем делать выводы, прочитайте его список skipped — проверка, которая не выполнилась, не говорит о схеме ничего, и «всё чисто» при непустом skipped — ложное «отбой». У двух находок правильное лечение НЕ очевидное: sequence_exhaustion с owned_column_type = 'integer' требует смены типа колонки (переписывание таблицы, нужно окно), а не только ALTER SEQUENCE; а no_primary_key на таблице с nullable уникальным индексом нельзя закрывать фразой «у вас же есть unique» — такой индекс не годится в replica identity. Перед советами по незнакомому коду читайте dasha://kb/schema-checks.
- «Кто делает весь этот I/O?» -> io_summary, а io_trend — когда он начался. Только эти инструменты видят неклиентский I/O: автовакуум, чекпойнтер, walwriter. Нужен PostgreSQL 16+ (на старых хостах ответ пустой с empty_reason='unsupported_version', и это НЕ значит «I/O нет») и хранилище снимков. Никогда не читайте нулевое время как «быстро», не проверив meta.track_io_timing, и никогда не читайте неполную точку io_trend как затишье — её счётчики покрывают лишь coverage_pct бакета. Пустой ответ не доказывает простой: это говорит только empty_reason='no_io', 'no_io_in_measured_part' отвечает лишь за измеренную часть окна, любое другое значение значит, что на вопрос не ответили. Перед трактовкой счётчиков читайте dasha://kb/pg-stat-io.
- Если непонятно, как трактовать результат или какой инструмент звать дальше — сначала прочитайте ресурсы: dasha://kb/workflow (сценарии «жалоба -> цепочка»), dasha://kb/health-rules (пороги правил и первые действия), dasha://kb/schema-checks (коды дефектов схемы и их лечение), dasha://kb/index-advisor (как читать кандидатов на индексы), dasha://kb/wait-events (глоссарий wait events), dasha://kb/pg-stat-io (как читать счётчики I/O).

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
			"1. index_advisor (по всему кластеру, instance не нужен) — кандидаты с готовым DDL, ранжированные по доле " +
			"времени нагрузки, которую занимают стоящие за ними запросы. Прежде чем называть кандидата, прочитай gaps " +
			"и warnings. 404 — это либо неизвестный кластер или база, либо выключенная в конфигурации Dasha " +
			"фича, и текст ошибки не говорит, что именно: сначала вызови list_clusters. Только если и кластер, " +
			"и база в нём есть, откатывайся на list_indexes (kind=missing) и прямо скажи, что это эвристика " +
			"по pg_stat_user_tables, которая запросов не читает.\n" +
			"2. unused_index_report по ТОЙ ЖЕ базе, ДО любого совета «создать»: индекс поверх избыточных, которые никто " +
			"не убрал, делает запись дороже, а не базу быстрее. Он работает по всему кластеру, instance не нужен. " +
			"Рекомендуй DROP только при verdict='drop_candidate'; при любом другом — повтори его reason. НЕ суди по " +
			"list_indexes (kind=unused): сырой счётчик сканов не видит ни реплик, ни окна статистики.\n" +
			"3. top_queries (by=time) или query_report — привяжи каждого кандидата к запросу, которому он послужит; " +
			"queryid бери из query_id_by_host, а не по позиции в списке.\n" +
			"Порекомендуй индексы к добавлению и неиспользуемые к удалению, каждый с привязкой к конкретным запросам. " +
			"weight_pct называй долей проанализированного времени, которую занимают покрытые запросы, а не ускорением: " +
			"планировщик этих кандидатов не видел, а DDL уходит человеку — Dasha его не выполняет. " +
			"Если доминируют последовательные сканы — сначала прочитай dasha://kb/health-rules (seq_scan_regression).",

		slowQueries: "Расследуй медленные запросы на кластере %q, инстанс %q (база %q). Выполняй по порядку:\n" +
			"1. top_queries (by=time). Мало вызовов с высоким средним временем = проблема плана; огромное число вызовов " +
			"с низким средним = проблема частоты (кэширование/батчинг).\n" +
			"2. running_queries — запросы, работающие минутами, и idle-in-transaction сессии.\n" +
			"3. blocked_queries — ожидания блокировок маскируются под медленность; если есть — найди корневого блокировщика.\n" +
			"4. wait_events — доминирующее событие называет класс узкого места; трактуй через ресурс dasha://kb/wait-events.\n" +
			"5. Только если на шаге 4 доминировало I/O-событие (DataFileRead, DataFileWrite, WALSync): io_summary — " +
			"wait_events говорит, что бэкенды ждут диск, но не говорит, кто читает, а pg_stat_statements видит только " +
			"клиентские бэкенды. Это отделяет клиентскую нагрузку от автовакуума, чекпойнтера и массовых сканов. " +
			"Нужен PostgreSQL 16+ и хранилище снимков; пустой ответ не доказывает отсутствие I/O.\n" +
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
