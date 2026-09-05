package mcpserver

import (
	"cmp"
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/dbulashev/dasha/gen/apiclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// noArgs is the empty argument set for tools that take no parameters.
type noArgs struct{}

type instanceArgs struct {
	Cluster  string `json:"cluster" jsonschema:"Dasha cluster name (from list_clusters)"`
	Instance string `json:"instance" jsonschema:"Dasha instance / host name"`
}

type recommendationsArgs struct {
	Cluster  string `json:"cluster" jsonschema:"Dasha cluster name"`
	Instance string `json:"instance" jsonschema:"Dasha instance / host name"`
	Database string `json:"database,omitempty" jsonschema:"Optional: restrict to one database (per-database drill-down)"`
}

// dbArgs targets a specific database on an instance.
type dbArgs struct {
	Cluster  string `json:"cluster" jsonschema:"Dasha cluster name"`
	Instance string `json:"instance" jsonschema:"Dasha instance / host name"`
	Database string `json:"database" jsonschema:"Database name to inspect"`
}

// unusedIndexReportArgs takes no instance on purpose: the verdict is cluster-wide.
type unusedIndexReportArgs struct {
	Cluster  string `json:"cluster" jsonschema:"Dasha cluster name"`
	Database string `json:"database" jsonschema:"Database to inspect"`
	Limit    int    `json:"limit,omitempty" jsonschema:"Max indexes to return, largest first (default 30)"`
}

// indexAdvisorArgs takes no instance on purpose: pg_stat_statements is per-host
// and is not replicated, while the index the report proposes is.
type indexAdvisorArgs struct {
	Cluster        string   `json:"cluster" jsonschema:"Dasha cluster name"`
	Database       string   `json:"database" jsonschema:"Database to analyse"`
	ExcludeUsers   []string `json:"exclude_users,omitempty" jsonschema:"Usernames whose statements are left out of the analysis — service roles whose load says nothing about the application's indexing needs"`
	Limit          int      `json:"limit,omitempty" jsonschema:"Max candidates to return, heaviest first (default 10, capped at 30). candidates_total says how many the ranking saw"`
	IncludeQueries bool     `json:"include_queries,omitempty" jsonschema:"Include the normalized text of the covered statements (clipped). Off by default: fingerprints and per-host queryids are enough to identify them"`
}

// hotArgs takes no instance on purpose: the stored snapshot already sums every
// host of the cluster (activity counters are not replicated).
type hotArgs struct {
	Cluster  string `json:"cluster" jsonschema:"Dasha cluster name"`
	Database string `json:"database" jsonschema:"Database to inspect"`
	Class    string `json:"class,omitempty" jsonschema:"Metric class: 'reads' (default), 'writes' (tables only) or 'io'"`
	Limit    int    `json:"limit,omitempty" jsonschema:"Max objects to return, hottest first (default 30)"`
}

type schemaLintArgs struct {
	Cluster  string `json:"cluster" jsonschema:"Dasha cluster name"`
	Instance string `json:"instance" jsonschema:"Dasha instance / host name (must be the primary)"`
	Database string `json:"database" jsonschema:"Database whose schema to check"`
	Level    string `json:"level,omitempty" jsonschema:"Keep only findings of this level: 'error', 'warning' or 'notice'. Omit for all"`
	Limit    int    `json:"limit,omitempty" jsonschema:"Max findings to return, worst level first (default 100)"`
}

type healthDetailsArgs struct {
	Cluster  string `json:"cluster" jsonschema:"Dasha cluster name"`
	Instance string `json:"instance" jsonschema:"Dasha instance / host name"`
	Detail   string `json:"detail" jsonschema:"Which evidence to fetch. Pass the rule_id straight from get_health_recommendations (e.g. 'tables_with_autovacuum_off', 'low_hot_update_ratio', 'high_avg_dead_ratio') — it is accepted as-is. The canonical names also work: 'tables_autovacuum_off', 'low_hot_update_tables', 'high_dead_ratio_tables', 'xid_wraparound_databases', 'horizon_blocking_sessions'"`
	Database string `json:"database,omitempty" jsonschema:"Database to inspect. Required for the per-table details (tables_autovacuum_off, low_hot_update_tables, high_dead_ratio_tables); the instance-wide ones ignore it"`
	Limit    int    `json:"limit,omitempty" jsonschema:"Max rows to return (default 15)"`
}

// healthDetail is one health-score drill-down: the evidence behind one or more
// rules. rules lists the rule_ids it explains and they are accepted as aliases for
// name, so a caller can hand the rule_id from a recommendation straight back —
// the canonical names deliberately differ (one drill-down can explain two rules).
type healthDetail struct {
	name    string
	rules   []string
	needsDB bool
	fetch   func(ctx context.Context, c *DashaClient, a healthDetailsArgs) (any, error)
}

// healthDetailList is the single source of truth for the health_details tool: the
// accepted values, which of them need a database, which rules each one explains and
// how to fetch it. Adding a drill-down is one entry — validation, dispatch and the
// error message all derive from here, so they cannot drift apart.
//
// Two nearby rules are deliberately NOT mapped: autovacuum_disabled is the
// instance-wide setting (not a table list) and relfrozenxid_age_outlier is
// per-relation, while the wraparound drill-down reports databases.
var healthDetailList = []healthDetail{
	{
		name:    "tables_autovacuum_off",
		rules:   []string{"tables_with_autovacuum_off"},
		needsDB: true,
		fetch: func(ctx context.Context, c *DashaClient, a healthDetailsArgs) (any, error) {
			return c.HealthTablesAutovacuumOff(ctx, a.Cluster, a.Instance, a.Database, a.Limit)
		},
	},
	{
		name:    "low_hot_update_tables",
		rules:   []string{"low_hot_update_ratio", "high_newpage_update_ratio"},
		needsDB: true,
		fetch: func(ctx context.Context, c *DashaClient, a healthDetailsArgs) (any, error) {
			return c.HealthLowHotUpdateTables(ctx, a.Cluster, a.Instance, a.Database, a.Limit)
		},
	},
	{
		name:    "high_dead_ratio_tables",
		rules:   []string{"high_avg_dead_ratio", "high_max_dead_ratio"},
		needsDB: true,
		fetch: func(ctx context.Context, c *DashaClient, a healthDetailsArgs) (any, error) {
			return c.HealthHighDeadRatioTables(ctx, a.Cluster, a.Instance, a.Database, a.Limit)
		},
	},
	{
		name:    "xid_wraparound_databases",
		rules:   []string{"xid_wraparound_risk"},
		needsDB: false,
		fetch: func(ctx context.Context, c *DashaClient, a healthDetailsArgs) (any, error) {
			return c.HealthXidWraparoundDatabases(ctx, a.Cluster, a.Instance, a.Limit)
		},
	},
	{
		name:    "horizon_blocking_sessions",
		rules:   []string{"horizon_lag_xids"},
		needsDB: false,
		fetch: func(ctx context.Context, c *DashaClient, a healthDetailsArgs) (any, error) {
			return c.HealthHorizonBlockingSessions(ctx, a.Cluster, a.Instance, a.Limit)
		},
	},
}

// healthDetailByKey indexes healthDetailList by canonical name and by every rule_id
// alias, so a lookup accepts either form.
var healthDetailByKey = func() map[string]*healthDetail {
	m := make(map[string]*healthDetail, len(healthDetailList)*2)

	for i := range healthDetailList {
		d := &healthDetailList[i]
		m[d.name] = d

		for _, rule := range d.rules {
			m[rule] = d
		}
	}

	return m
}()

// healthDetailNames lists the canonical values for the error message, so what the
// tool accepts and what it advertises on a miss stay in lockstep.
func healthDetailNames() string {
	names := make([]string, 0, len(healthDetailList))
	for i := range healthDetailList {
		names = append(names, healthDetailList[i].name)
	}

	return strings.Join(names, ", ")
}

type topQueriesArgs struct {
	Cluster  string `json:"cluster" jsonschema:"Dasha cluster name"`
	Instance string `json:"instance" jsonschema:"Dasha instance / host name"`
	Database string `json:"database,omitempty" jsonschema:"Optional: database name; omit to rank across the whole instance"`
	By       string `json:"by,omitempty" jsonschema:"Ranking metric: 'time' (total execution time, default) or 'wal' (WAL volume)"`
}

type blockedQueriesArgs struct {
	Cluster  string `json:"cluster" jsonschema:"Dasha cluster name"`
	Instance string `json:"instance" jsonschema:"Dasha instance / host name"`
	Database string `json:"database" jsonschema:"Database name to inspect"`
	Scope    string `json:"scope,omitempty" jsonschema:"Optional: 'database' (default) lists that database only; 'instance' lists every database of the host"`
}

type listSnapshotsArgs struct {
	Cluster  string `json:"cluster" jsonschema:"Dasha cluster name"`
	Instance string `json:"instance" jsonschema:"Dasha instance / host name"`
}

type listIndexesArgs struct {
	Cluster  string `json:"cluster" jsonschema:"Dasha cluster name"`
	Instance string `json:"instance" jsonschema:"Dasha instance / host name"`
	Database string `json:"database" jsonschema:"Database to inspect"`
	Kind     string `json:"kind,omitempty" jsonschema:"Which set: 'missing' (suggested new indexes, default), 'unused' (never scanned), or 'usage' (scan statistics)"`
}

type healthTrendArgs struct {
	Cluster  string `json:"cluster" jsonschema:"Dasha cluster name"`
	Instance string `json:"instance" jsonschema:"Dasha instance / host name"`
	Range    string `json:"range,omitempty" jsonschema:"Time window: '24h' (default), '7d' or '30d'"`
}

type queryReportArgs struct {
	Cluster      string   `json:"cluster" jsonschema:"Dasha cluster name"`
	Instance     string   `json:"instance" jsonschema:"Dasha instance / host name"`
	Database     string   `json:"database,omitempty" jsonschema:"Optional: database name; omit for the whole instance"`
	Queryid      string   `json:"queryid,omitempty" jsonschema:"Optional: pg_stat_statements queryid to include whatever it ranks; the report otherwise holds only the top of each metric"`
	ExcludeUsers []string `json:"exclude_users,omitempty" jsonschema:"Optional: usernames to exclude (e.g. monitoring/replication roles)"`
}

type queryCompareArgs struct {
	Cluster      string   `json:"cluster" jsonschema:"Dasha cluster name"`
	Instance     string   `json:"instance" jsonschema:"Dasha instance / host name"`
	Database     string   `json:"database" jsonschema:"Database name"`
	Scope        string   `json:"scope,omitempty" jsonschema:"Optional: 'database' (default) compares that database only; 'instance' compares every database of the host"`
	SnapshotA    string   `json:"snapshot_a" jsonschema:"Baseline snapshot ID (UUID, from list_snapshots)"`
	SnapshotB    string   `json:"snapshot_b,omitempty" jsonschema:"Optional: second snapshot ID; omit to compare snapshot_a vs. live stats"`
	ExcludeUsers []string `json:"exclude_users,omitempty" jsonschema:"Optional: usernames to exclude"`
}

type describeTableArgs struct {
	Cluster  string `json:"cluster" jsonschema:"Dasha cluster name"`
	Instance string `json:"instance" jsonschema:"Dasha instance / host name"`
	Database string `json:"database" jsonschema:"Database name"`
	Schema   string `json:"schema,omitempty" jsonschema:"Schema name (default 'public')"`
	Table    string `json:"table" jsonschema:"Table name"`
	Limit    int    `json:"limit,omitempty" jsonschema:"Max partitions to list for a partitioned table (default 50)"`
}

type connectionsArgs struct {
	Cluster  string `json:"cluster" jsonschema:"Dasha cluster name"`
	Instance string `json:"instance" jsonschema:"Dasha instance / host name"`
	Limit    int    `json:"limit,omitempty" jsonschema:"Max pg_stat_activity rows to sample (default 100)"`
}

type fleetHealthArgs struct {
	Limit int `json:"limit,omitempty" jsonschema:"How many worst-scoring instances to return (default 5)"`
}

type searchLogsArgs struct {
	Cluster     string   `json:"cluster" jsonschema:"Dasha cluster name; must have supports_logs=true in list_clusters"`
	ServiceType string   `json:"service_type,omitempty" jsonschema:"Log source: 'postgresql' (default) or 'pooler' (Odyssey connection pooler)"`
	Since       string   `json:"since,omitempty" jsonschema:"Look-back window ending now, e.g. '15m', '1h', '24h' (default '1h'); ignored when from/to are set"`
	From        string   `json:"from,omitempty" jsonschema:"Window start, RFC3339 (e.g. 2026-07-10T12:00:00Z); set together with to"`
	To          string   `json:"to,omitempty" jsonschema:"Window end, RFC3339; set together with from"`
	Severity    []string `json:"severity,omitempty" jsonschema:"Severities to include: PostgreSQL uses upper-case (ERROR, FATAL, PANIC, WARNING, LOG), the pooler lower-case (error, warn)"`
	Host        string   `json:"host,omitempty" jsonschema:"Optional: restrict to one cluster host"`
	Message     []string `json:"message,omitempty" jsonschema:"Substrings that must all be present in the message (AND, case-insensitive)"`
	Exclude     []string `json:"exclude,omitempty" jsonschema:"Drop records whose message contains any of these substrings (grep -v)"`
	Database    string   `json:"database,omitempty" jsonschema:"Optional: restrict to one database"`
	User        string   `json:"user,omitempty" jsonschema:"Optional: restrict to one user"`
	Dedup       *bool    `json:"dedup,omitempty" jsonschema:"Group near-identical messages with count/first_seen/last_seen (default true — much smaller results); set false for raw records with pagination"`
	PageSize    int      `json:"page_size,omitempty" jsonschema:"Max raw records per page when dedup=false (default 100)"`
	PageToken   string   `json:"page_token,omitempty" jsonschema:"Cursor from a previous dedup=false result to fetch the next page"`
}

// pg_stat_io is instance-wide, so neither I/O tool takes a database: there is
// nothing to narrow it to.
type ioSummaryArgs struct {
	Cluster     string `json:"cluster" jsonschema:"Dasha cluster name"`
	Instance    string `json:"instance" jsonschema:"Dasha instance / host name"`
	Since       string `json:"since,omitempty" jsonschema:"Look-back window ending now, e.g. '15m', '1h', '24h', '7d' (default '1h'); ignored when from/to are set"`
	From        string `json:"from,omitempty" jsonschema:"Window start, RFC3339; set together with to"`
	To          string `json:"to,omitempty" jsonschema:"Window end, RFC3339; set together with from"`
	GroupBy     string `json:"group_by,omitempty" jsonschema:"Dimensions to keep: 'context' (default, cheapest answer to whose I/O), 'backend_type' or 'full' (backend_type x object x context)"`
	Object      string `json:"object,omitempty" jsonschema:"Keep only this object: 'relation', 'temp relation' (work_mem spills) or 'wal'"`
	BackendType string `json:"backend_type,omitempty" jsonschema:"Keep only this backend type, e.g. 'client backend', 'autovacuum worker', 'checkpointer'"`
	Context     string `json:"context,omitempty" jsonschema:"Keep only this context: 'normal', 'vacuum', 'bulkread', 'bulkwrite' or 'init'"`
	Top         int    `json:"top,omitempty" jsonschema:"Max rows to return, heaviest first (default 20, max 200); rows_total says how many were dropped"`
}

type ioTrendArgs struct {
	Cluster     string `json:"cluster" jsonschema:"Dasha cluster name"`
	Instance    string `json:"instance" jsonschema:"Dasha instance / host name"`
	Since       string `json:"since,omitempty" jsonschema:"Look-back window ending now, e.g. '6h', '24h', '7d' (default '24h'); ignored when from/to are set"`
	From        string `json:"from,omitempty" jsonschema:"Window start, RFC3339; set together with to"`
	To          string `json:"to,omitempty" jsonschema:"Window end, RFC3339; set together with from"`
	Points      int    `json:"points,omitempty" jsonschema:"Buckets per series (default 24 — a day by the hour over the default window, max 200); raise only when the shape matters more than the breakdown"`
	Context     string `json:"context,omitempty" jsonschema:"Keep only this context: 'normal', 'vacuum', 'bulkread', 'bulkwrite' or 'init'"`
	BackendType string `json:"backend_type,omitempty" jsonschema:"Keep only this backend type before grouping by context"`
	Object      string `json:"object,omitempty" jsonschema:"Keep only this object: 'relation', 'temp relation' or 'wal'"`
}

func registerTools(s *mcp.Server, c *DashaClient) {
	addTool(s, &mcp.Tool{
		Name: "list_clusters",
		Description: "List the PostgreSQL clusters Dasha manages, with their hosts. " +
			"Use this first to choose a (cluster, instance) target for the other tools.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, any, error) {
		out, err := c.Clusters(ctx)

		return jsonResult(out, err)
	})

	addTool(s, &mcp.Tool{
		Name: "get_health_score",
		Description: "Get the instance-level health score (0-100) with per-category breakdown and " +
			"its source (snapshot or metrics) for a cluster/instance.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a instanceArgs) (*mcp.CallToolResult, any, error) {
		out, err := c.HealthScore(ctx, a.Cluster, a.Instance)

		return jsonResult(out, err)
	})

	addTool(s, &mcp.Tool{
		Name: "get_health_recommendations",
		Description: "Get prioritized health-score recommendations (rule_id, category, severity, " +
			"metric_value, database) for a cluster/instance. Pass database for the per-database drill-down. " +
			"Each recommendation names the database it belongs to — for the activity rules " +
			"(long_running_transaction, idle_in_transaction, horizon_lag_xids) that is the database of the " +
			"offending session, so query that one, not the selected one; a null means instance-wide. " +
			"In metrics mode the rules fed by datasource aggregates (cache hit, dead ratios, HOT, xid age, " +
			"checksums, disk, regressions) are null as well, and sequence_exhaustion is null at instance " +
			"scope in either mode — that means no single database owns the number, not that no database " +
			"is affected.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a recommendationsArgs) (*mcp.CallToolResult, any, error) {
		var db *string
		if a.Database != "" {
			db = &a.Database
		}

		out, err := c.Recommendations(ctx, a.Cluster, a.Instance, db)

		return jsonResult(out, err)
	})

	addTool(s, &mcp.Tool{
		Name: "health_details",
		Description: "Name the objects behind a health-score finding. get_health_recommendations tells you " +
			"WHICH rule fired and how bad it is; this tells you WHICH tables, databases or sessions caused it — " +
			"call it whenever a recommendation needs to become an actionable target. Pass the recommendation's " +
			"rule_id as detail; the per-table drill-downs (tables_autovacuum_off, low_hot_update_tables, " +
			"high_dead_ratio_tables) also need a database, the instance-wide ones do not. " +
			"What it returns is a target, not yet a cause: follow up with describe_table on the named table to " +
			"confirm the mechanism (fillfactor, which indexed column the UPDATE touches) before advising a fix.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a healthDetailsArgs) (*mcp.CallToolResult, any, error) {
		d, ok := healthDetailByKey[a.Detail]
		if !ok {
			return errResult("unknown detail " + strconv.Quote(a.Detail) +
				" — pass a rule_id from get_health_recommendations, or one of: " + healthDetailNames()), nil, nil
		}

		if d.needsDB && a.Database == "" {
			return errResult("detail " + strconv.Quote(d.name) + " is per-database — pass database"), nil, nil
		}

		return jsonResult(d.fetch(ctx, c, a))
	})

	addTool(s, &mcp.Tool{
		Name:        "get_instance_info",
		Description: "Get the PostgreSQL server version and recovery state (primary vs standby) for a cluster/instance.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a instanceArgs) (*mcp.CallToolResult, any, error) {
		return jsonResult(c.InstanceInfo(ctx, a.Cluster, a.Instance))
	})

	addTool(s, &mcp.Tool{
		Name: "top_queries",
		Description: "List the top queries for a cluster/instance, ranked by total execution time " +
			"(by='time', default) or WAL volume (by='wal'). Pass database to rank inside one database; " +
			"without it the ranking covers every database of the host and each entry names its own " +
			"('datname'). 'QueryTrunc' holds the first 48 characters of the statement (64 for by='wal'), " +
			"never the whole one: to read or quote the statement call query_report with the row's " +
			"'QueryID' and 'Datname'. Requires pg_stat_statements.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a topQueriesArgs) (*mcp.CallToolResult, any, error) {
		switch a.By {
		case "", "time":
			return jsonResult(c.TopQueriesByTime(ctx, a.Cluster, a.Instance, a.Database))
		case "wal":
			return jsonResult(c.TopQueriesByWal(ctx, a.Cluster, a.Instance, a.Database))
		default:
			return errResult("by must be 'time' or 'wal'"), nil, nil
		}
	})

	addTool(s, &mcp.Tool{
		Name: "running_queries",
		Description: "List currently running queries on a database (pid, duration, state, client address, " +
			"wait event, query) — useful to spot long-running or stuck statements. 'waiting' marks a wait that " +
			"blocks progress: every wait_event_type except Client, Timeout and Activity, which are the idle " +
			"background of any healthy instance.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a dbArgs) (*mcp.CallToolResult, any, error) {
		return jsonResult(c.RunningQueries(ctx, a.Cluster, a.Instance, a.Database))
	})

	addTool(s, &mcp.Tool{
		Name: "list_indexes",
		Description: "List index findings for a database: kind='missing' (default) — index candidates, a " +
			"heuristic over pg_stat_user_tables: tables of 10k+ live rows whose share of index scans is below " +
			"95%. It reads no queries and ignores the indexes a table already has, so a hit means 'worth " +
			"inspecting', never 'an index is missing'. 'unused' (never scanned), or 'usage' (scan statistics).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a listIndexesArgs) (*mcp.CallToolResult, any, error) {
		switch a.Kind {
		case "", "missing":
			return jsonResult(c.IndexesMissing(ctx, a.Cluster, a.Instance, a.Database))
		case "unused":
			return jsonResult(c.IndexesUnused(ctx, a.Cluster, a.Instance, a.Database))
		case "usage":
			return jsonResult(c.IndexesUsage(ctx, a.Cluster, a.Instance, a.Database))
		default:
			return errResult("kind must be 'missing', 'unused' or 'usage'"), nil, nil
		}
	})

	addTool(s, &mcp.Tool{
		Name: "unused_index_report",
		Description: "Decide whether an index is safe to DROP. Use this instead of reading raw counters from " +
			"list_indexes(kind='unused'): a scan counter alone cannot answer the question. Cluster-wide by " +
			"design (it takes no instance) because idx_scan is per-instance and is NOT replicated — an index " +
			"idle on the primary may be serving every read on a replica. It also weighs the counter against the " +
			"statistics window behind it: zero scans right after a stats reset prove nothing. Each index comes " +
			"back with a verdict and the reasoning. ONLY 'drop_candidate' justifies recommending a DROP; on " +
			"'used', 'stale_evidence', 'insufficient_data' or 'unknown' say what the reason says instead — " +
			"'unknown' means a host could not be read, so the cluster-wide picture is incomplete. When " +
			"partitioned=true the index named is the top-level parent and its per-partition children are already " +
			"summed into the verdict: never suggest dropping a partition's child index — PostgreSQL refuses, and " +
			"its HINT points at the parent, which would strip the index off EVERY partition.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a unusedIndexReportArgs) (*mcp.CallToolResult, any, error) {
		return jsonResult(c.UnusedIndexReport(ctx, a.Cluster, a.Database, a.Limit))
	})

	addTool(s, &mcp.Tool{
		Name: "index_advisor",
		Description: "Index candidates for one database, derived from the cluster's real workload: the top of " +
			"pg_stat_statements is parsed on EVERY host, the columns each statement filters, joins, sorts and " +
			"groups by are extracted, and btree indexes that no existing index already covers are proposed — " +
			"each with ready DDL and the statements behind it. Use this, NOT list_indexes(kind='missing'), to " +
			"answer \"which index should I create\": that one reads no queries and ignores the indexes a table " +
			"already has. " +
			"No planner was consulted: planner_checked is false, and weight_pct is the SIZE OF THE PROBLEM — the " +
			"share of analyzed execution time the covered statements hold — never a predicted gain. \"This index " +
			"speeds the query up by 31%\" is a claim this data cannot support; say the statements it would serve " +
			"hold 31% of the analyzed time instead. " +
			"Read warnings BEFORE recommending, not after: write_heavy (the table is written far more often than " +
			"the covered statements run — the index may cost more than it saves), similar_index (an existing " +
			"index already holds every column of the candidate in another order; names lists it — the answer may " +
			"be rewriting that index rather than adding one), many_indexes, matview, partition_root (the ddl is " +
			"then a multi-statement script: CREATE INDEX CONCURRENTLY runs in no transaction block and cannot " +
			"build a partitioned table's root index directly — hand over every statement, in order). " +
			"An empty candidate list is NOT a clean bill of health while gaps is non-empty: part of the workload " +
			"was never analyzed, and summary (covered_time_pct, not_parsed_count, hosts_without_stats) says how " +
			"much. Report that instead of \"the database is well indexed\". " +
			"Takes no instance by design: pg_stat_statements is per-host and is not replicated, so a single-host " +
			"answer would call a database well indexed for a load it never saw. Indexes ARE replicated — one " +
			"CREATE INDEX on the primary serves every host. " +
			"covered_queries carries fingerprints, call counts and the per-host queryid, not the statement text: " +
			"set include_queries=true for the text (clipped), or feed query_id_by_host into query_report on that " +
			"host. " +
			"A 404 means the cluster/database is unknown OR the index_advisor feature is disabled in Dasha's " +
			"configuration — never \"nothing to suggest\". The report is built on demand and never cached; on a " +
			"large workload the call takes tens of seconds. " +
			"Dasha NEVER executes DDL. The ddl is a proposal for a human to run, after checking " +
			"unused_index_report on the same database — a new index laid on top of redundant ones nobody removed " +
			"is not an improvement. " +
			"Read dasha://kb/index-advisor before interpreting the numbers.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a indexAdvisorArgs) (*mcp.CallToolResult, any, error) {
		return jsonResult(indexAdvisor(ctx, c, a))
	})

	addTool(s, &mcp.Tool{
		Name:        "top_tables",
		Description: "List the largest tables in a database by total size.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a dbArgs) (*mcp.CallToolResult, any, error) {
		return jsonResult(c.TopTables(ctx, a.Cluster, a.Instance, a.Database))
	})

	addTool(s, &mcp.Tool{
		Name: "schema_lint",
		Description: "Structural defects of one database's schema, from the system catalog only — no user data " +
			"is read, which is what makes this safe to run on production. Answers what is wrong with the " +
			"STRUCTURE, not what is happening now; the result changes on deploys, not by the second. Each " +
			"finding carries a code, a level ('error', 'warning', 'notice'), the object and the numbers in " +
			"params — build the wording yourself, the API ships no prose. " +
			"ALWAYS read `skipped` before concluding anything: a check listed there did NOT run (no privilege, " +
			"unsupported version, timeout), so its absence from `findings` proves nothing — say so instead of " +
			"reporting the schema as clean. `truncated: true` means a check hit its row cap and the counts are " +
			"a lower bound. When params.partitions is set, the finding is already rolled up to the parent table " +
			"and covers that many partitions — address the parent, never a single partition. " +
			"Reading the findings: 'no_primary_key' is not an instruction to add a key on the spot — ask what " +
			"the table is for and whether logical replication or pg_repack is in the picture, and note that a " +
			"unique index over a nullable column (params.unique_nullable) cannot serve as REPLICA IDENTITY. " +
			"'sequence_exhaustion' with params.owned_column_type = 'integer' needs the COLUMN type changed too, " +
			"which rewrites the table and needs a maintenance window — recommending only ALTER SEQUENCE there " +
			"is wrong. 'uuid_in_non_uuid_type' is a heuristic on column name and length, not a fact: say it " +
			"needs verifying. Runs on the primary only; on a standby the endpoint has nothing to fix.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a schemaLintArgs) (*mcp.CallToolResult, any, error) {
		if a.Level != "" && a.Level != "error" && a.Level != "warning" && a.Level != "notice" {
			return errResult("level must be 'error', 'warning' or 'notice'"), nil, nil
		}

		return jsonResult(c.SchemaLint(ctx, a.Cluster, a.Instance, a.Database, a.Level, a.Limit))
	})

	addTool(s, &mcp.Tool{
		Name: "schema_lint_summary",
		Description: "Finding counts per level for every database of an instance — use it to pick which " +
			"database deserves a full schema_lint call, since schema defects live in a database, not in a " +
			"cluster. Zero counts mean a clean database ONLY when skipped=0 and failed=false: skipped counts " +
			"checks that did not run there, and failed=true means the database could not be read at all. " +
			"The sweep is capped in the number of databases, so a very large instance may be reported only in part.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a instanceArgs) (*mcp.CallToolResult, any, error) {
		return jsonResult(c.SchemaLintSummary(ctx, a.Cluster, a.Instance))
	})

	addTool(s, &mcp.Tool{
		Name: "hot_tables",
		Description: "Top HOT tables of a database from the daily delta snapshot: activity per class " +
			"('reads', 'writes' or 'io') summed over every cluster host, with a per-host breakdown per entry. " +
			"Cluster-wide by design (no instance): activity counters are not replicated. Check snapshot.coverage " +
			"— it states what share of total activity the stored top holds; a low coverage means a fat tail of " +
			"warm objects that the entries do not show. snapshot.hosts_missing non-empty means the snapshot is " +
			"partial. Hash-partitioned tables appear as the parent (partitions summed); range/list partitions " +
			"appear individually. Use rate_per_day for comparisons, not raw deltas: it is the class key normalised " +
			"to the snapshot's actual window (snapshot.windows), so a sub-day window is scaled up to a day, not " +
			"measured over one. Requires snapshot storage (501 otherwise). " +
			"Pairs well with maintenance metrics: a table hot on writes usually deserves per-table autovacuum tuning.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a hotArgs) (*mcp.CallToolResult, any, error) {
		if a.Class != "" && a.Class != "reads" && a.Class != "writes" && a.Class != "io" {
			return errResult("class must be 'reads', 'writes' or 'io'"), nil, nil
		}

		return jsonResult(c.HotTables(ctx, a.Cluster, a.Database, a.Class, a.Limit))
	})

	addTool(s, &mcp.Tool{
		Name: "hot_indexes",
		Description: "Top HOT indexes of a database from the daily delta snapshot; classes 'reads' and 'io' only " +
			"(PostgreSQL keeps no per-index write counters). Same semantics as hot_tables: cluster-wide sums, " +
			"per-host breakdown, coverage honesty. The natural complement of unused_index_report: one names the " +
			"indexes to drop, this names the ones doing the actual work.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a hotArgs) (*mcp.CallToolResult, any, error) {
		if a.Class != "" && a.Class != "reads" && a.Class != "io" {
			return errResult("class must be 'reads' or 'io'"), nil, nil
		}

		return jsonResult(c.HotIndexes(ctx, a.Cluster, a.Database, a.Class, a.Limit))
	})

	addTool(s, &mcp.Tool{
		Name: "blocked_queries",
		Description: "List sessions currently blocked on locks (and the sessions blocking them) " +
			"for a database, or for the whole host with scope='instance' — an incident rarely stays " +
			"inside one database. Every row names the database it belongs to; a lock target in another " +
			"database is reported as a relation OID, since names resolve through the catalog of the " +
			"database being read.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a blockedQueriesArgs) (*mcp.CallToolResult, any, error) {
		return jsonResult(c.BlockedQueries(ctx, a.Cluster, a.Instance, a.Database, a.Scope))
	})

	addTool(s, &mcp.Tool{
		Name: "health_trend",
		Description: "Get the health-score time series for a cluster/instance: per-timestamp score, the " +
			"seasonal baseline and detected dips. range='24h' (default), '7d' or '30d'. Metrics-backed mode only.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a healthTrendArgs) (*mcp.CallToolResult, any, error) {
		span, step := trendWindow(a.Range)
		if span == 0 {
			return errResult("range must be '24h', '7d' or '30d'"), nil, nil
		}

		to := time.Now()

		return jsonResult(c.HealthTrend(ctx, a.Cluster, a.Instance, to.Add(-span), to, step))
	})

	addTool(s, &mcp.Tool{
		Name:        "health_databases",
		Description: "Get per-database health scores for a cluster/instance, including the worst-scoring database.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a instanceArgs) (*mcp.CallToolResult, any, error) {
		return jsonResult(c.HealthDatabases(ctx, a.Cluster, a.Instance))
	})

	addTool(s, &mcp.Tool{
		Name: "get_replication",
		Description: "Get replication status (standbys and lag), slots (WAL retention), and config " +
			"(synchronous settings) for a cluster/instance.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a instanceArgs) (*mcp.CallToolResult, any, error) {
		out := map[string]any{}

		st, err := c.ReplicationStatus(ctx, a.Cluster, a.Instance)
		section(out, "status", st, err)

		sl, err := c.ReplicationSlots(ctx, a.Cluster, a.Instance)
		section(out, "slots", sl, err)

		cf, err := c.ReplicationConfig(ctx, a.Cluster, a.Instance)
		section(out, "config", cf, err)

		return sectionsResult(out)
	})

	addTool(s, &mcp.Tool{
		Name: "settings_analyze",
		Description: "Analyse the PostgreSQL configuration (pg_settings) for a cluster/instance and return " +
			"findings and suggested adjustments.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a instanceArgs) (*mcp.CallToolResult, any, error) {
		return jsonResult(c.SettingsAnalyze(ctx, a.Cluster, a.Instance))
	})

	addTool(s, &mcp.Tool{
		Name: "wait_events",
		Description: "Get the current wait events (grouped by type/event) for a cluster/instance — what " +
			"backends are waiting on right now.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a instanceArgs) (*mcp.CallToolResult, any, error) {
		return jsonResult(c.WaitEvents(ctx, a.Cluster, a.Instance))
	})

	addTool(s, &mcp.Tool{
		Name: "query_report",
		Description: "Get the full pg_stat_statements report for a cluster/instance (per-query calls, time, " +
			"rows, I/O). Every row names its database in 'datname' and is ranked within it, so a queryid " +
			"identifies a statement only together with that name. Pass database to keep only that one — " +
			"percentages are then shares of it, otherwise shares of the whole instance. Pass exclude_users " +
			"to drop noise from monitoring/replication roles. This is the source of statement text: 'Query' " +
			"is the whole statement as pg_stat_statements stored it, clipped only by the server's " +
			"track_activity_query_size (a text cut there ends in '...'), unlike the 48-character 'QueryTrunc' " +
			"of top_queries. Rows are the top of each metric per database, so pass queryid to pull in a " +
			"statement that leads none of them. Requires pg_stat_statements.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a queryReportArgs) (*mcp.CallToolResult, any, error) {
		return jsonResult(c.QueryReport(ctx, a.Cluster, a.Instance, a.Database, a.Queryid, a.ExcludeUsers))
	})

	addTool(s, &mcp.Tool{
		Name: "list_snapshots",
		Description: "List the stored pg_stat_statements snapshots of an instance (id, captured_at, " +
			"which databases each covers). Snapshots are host-wide — 'database' on a listed snapshot is " +
			"only the database it was read through. Use this to obtain the snapshot IDs that query_compare " +
			"needs. Requires snapshot storage.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a listSnapshotsArgs) (*mcp.CallToolResult, any, error) {
		return jsonResult(c.Snapshots(ctx, a.Cluster, a.Instance))
	})

	addTool(s, &mcp.Tool{
		Name: "query_compare",
		Description: "Compare two pg_stat_statements snapshots (snapshot_a vs snapshot_b) for a database to " +
			"surface query regressions; omit snapshot_b to compare snapshot_a against live stats. Sides are " +
			"matched by queryid within a database. A snapshot stored before per-database attribution can only " +
			"be compared against another one of its own generation — pairing it with a newer snapshot, or " +
			"with live stats, is refused rather than answered with invented matches; list_snapshots reports " +
			"the generation as json_version. Get IDs from list_snapshots.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a queryCompareArgs) (*mcp.CallToolResult, any, error) {
		var b *string
		if a.SnapshotB != "" {
			b = &a.SnapshotB
		}

		return jsonResult(c.QueryCompare(ctx, a.Cluster, a.Instance, a.Database, a.Scope, a.SnapshotA, b, a.ExcludeUsers))
	})

	addTool(s, &mcp.Tool{
		Name: "vacuum_danger",
		Description: "Assess transaction-id wraparound risk for a database: per-table xid age vs. the freeze " +
			"horizon (transaction_id_danger) plus the instance autovacuum freeze settings (autovacuum_freeze_max_age).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a dbArgs) (*mcp.CallToolResult, any, error) {
		out := map[string]any{}

		td, err := c.TransactionIdDanger(ctx, a.Cluster, a.Instance, a.Database)
		section(out, "transaction_id_danger", td, err)

		fz, err := c.AutovacuumFreezeMaxAge(ctx, a.Cluster, a.Instance)
		section(out, "autovacuum_freeze_max_age", fz, err)

		return sectionsResult(out)
	})

	addTool(s, &mcp.Tool{
		Name: "connections",
		Description: "Diagnose connection usage for a cluster/instance: counts by backend state and by client " +
			"source, plus a capped pg_stat_activity sample of who holds the connections.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a connectionsArgs) (*mcp.CallToolResult, any, error) {
		limit := a.Limit
		if limit <= 0 {
			limit = connectionSampleLimit
		}

		out := map[string]any{}

		st, err := c.ConnectionStates(ctx, a.Cluster, a.Instance)
		section(out, "states", st, err)

		sr, err := c.ConnectionSources(ctx, a.Cluster, a.Instance)
		section(out, "sources", sr, err)

		act, err := c.ConnectionStatActivity(ctx, a.Cluster, a.Instance, limit)
		section(out, "activity", act, err)

		return sectionsResult(out)
	})

	addTool(s, &mcp.Tool{
		Name: "describe_table",
		Description: "Describe one table in depth: layout, estimated bloat, partitions, row-count estimate and " +
			"autovacuum/analyze stats. schema defaults to 'public'.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a describeTableArgs) (*mcp.CallToolResult, any, error) {
		schema := a.Schema
		if schema == "" {
			schema = "public"
		}

		partitionLimit := a.Limit
		if partitionLimit <= 0 {
			partitionLimit = defaultPartitionLimit
		}

		out := map[string]any{}

		d, err := c.TableDescribe(ctx, a.Cluster, a.Instance, a.Database, schema, a.Table)
		section(out, "table", d, err)

		bl, err := c.TableDescribeBloat(ctx, a.Cluster, a.Instance, a.Database, schema, a.Table)
		section(out, "bloat", bl, err)

		pt, err := c.TableDescribePartitions(ctx, a.Cluster, a.Instance, a.Database, schema, a.Table, partitionLimit)
		section(out, "partitions", pt, err)

		re, err := c.TableDescribeRowEstimate(ctx, a.Cluster, a.Instance, a.Database, schema, a.Table)
		section(out, "row_estimate", re, err)

		vs, err := c.TableDescribeVacuumStats(ctx, a.Cluster, a.Instance, a.Database, schema, a.Table)
		section(out, "vacuum_stats", vs, err)

		return sectionsResult(out)
	})

	addTool(s, &mcp.Tool{
		Name: "fleet_health",
		Description: "Scan every cluster/instance Dasha manages and return the worst-scoring instances " +
			"(health score, ascending). One call instead of looping list_clusters + get_health_score.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a fleetHealthArgs) (*mcp.CallToolResult, any, error) {
		return jsonResult(fleetHealth(ctx, c, a.Limit))
	})

	addTool(s, &mcp.Tool{
		Name: "search_logs",
		Description: "Search PostgreSQL server or connection-pooler logs of a cluster whose logs Dasha can " +
			"reach (supports_logs=true in list_clusters; log_streams lists the streams it serves). Every call " +
			"reaches the log store and is rate-limited per user (the operator sets the limit per source) — " +
			"make each call count: keep the default dedup=true overview, a narrow window (since='1h') and " +
			"severity/message filters, and refine with one follow-up call instead of paging raw records. " +
			"After a 429 back off before retrying.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a searchLogsArgs) (*mcp.CallToolResult, any, error) {
		params, errMsg := logsParams(a)
		if errMsg != "" {
			return errResult(errMsg), nil, nil
		}

		return jsonResult(c.SearchLogs(ctx, params))
	})

	addTool(s, &mcp.Tool{
		Name: "io_summary",
		Description: "Break instance-wide physical I/O down over a time window: who read, wrote and extended, " +
			"from the stored pg_stat_io snapshots. Answers what wait_events (who waits, not who causes it) and " +
			"query_report (client backends only, pg_stat_statements only) cannot — autovacuum vs client load, " +
			"the bulkread share (sequential scans bypassing the cache), extends (real file growth), fsyncs on a " +
			"regular backend (the checkpointer is falling behind), 'temp relation' (instance-wide work_mem " +
			"spills). group_by=context (default) is the cheapest answer to 'whose I/O'; 'full' breaks it down " +
			"by backend_type x object x context and needs top. Requires PostgreSQL 16 or newer: older hosts have " +
			"no pg_stat_io at all and come back empty with empty_reason='unsupported_version', which does NOT " +
			"mean 'no I/O'. Every empty answer carries an empty_reason, and only 'no_io' means no classifiable " +
			"physical I/O across the whole window (cache hits can still be heavy): 'no_io_in_measured_part' " +
			"means a counter epoch broke inside it and " +
			"the quiet covers the measured part alone, while 'no_snapshots_in_window', " +
			"'no_comparable_snapshots', 'window_after_history' and 'no_io_matching_filter' all mean the " +
			"question went unanswered — check totals and meta before reporting an all-clear. Time counters " +
			"answer to two settings: track_io_timing for relation and temp relation rows, " +
			"track_wal_io_timing for the 'wal' object (PostgreSQL 18+). Under whichever is off every time " +
			"metric is zero by construction — a missing measurement, not a missing load; meta.track_io_timing " +
			"and meta.track_wal_io_timing say which, and avg_read_ms/avg_write_ms are absent rather than 0. " +
			"Both flags report the newest capture: meta.track_io_timing_changed and " +
			"meta.track_wal_io_timing_changed mark a setting toggled inside the window, so earlier captures " +
			"can carry real times under a setting that now reads off and the timing covers part of the " +
			"window only. A window longer than 31 days is cut back to it and flagged " +
			"window_capped. pg_stat_io is instance-wide: there is no database parameter. A counter absent " +
			"from values is zero. Needs snapshot storage (501 otherwise). " +
			"Read dasha://kb/pg-stat-io before interpreting the numbers.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a ioSummaryArgs) (*mcp.CallToolResult, any, error) {
		return jsonResult(ioSummary(ctx, c, a))
	})

	addTool(s, &mcp.Tool{
		Name: "io_trend",
		Description: "Coarse time series of physical I/O per pg_stat_io context — when the load started and " +
			"whether it lines up with the autovacuum window. Defaults to the last 24 hours in 24 buckets, " +
			"grouped by context; use io_summary on the window this narrows down to find out who is behind one. " +
			"A point covering a statistics reset, a restart or a major upgrade carries complete=false and a " +
			"coverage_pct: its counters are real but measure only that share of the bucket's span, so it is not " +
			"comparable with a complete point and a lower number there is not a drop in load (incomplete_points " +
			"counts such buckets). An incomplete point with no values at all measured nothing. In a complete " +
			"point an absent metric is zero. This series carries reads, read_bytes, writes, write_bytes and " +
			"extends, plus read_time and write_time where timing was measured, so its empty_reason='no_io' " +
			"does not rule out fsyncs, evictions, reuses or writebacks — confirm with io_summary over the same " +
			"window. Same preconditions as io_summary, " +
			"including empty_reason on an empty answer: PostgreSQL 16+, instance-wide (no database), snapshot " +
			"storage required (501 otherwise). Time metrics are carried when track_io_timing or " +
			"track_wal_io_timing is on; grouping is by context, which merges WAL and relation rows, so with " +
			"only track_wal_io_timing on a bucket's times are WAL's alone — read meta.track_io_timing and " +
			"meta.track_wal_io_timing before attributing them, and split them with io_summary group_by=full. " +
			"Read dasha://kb/pg-stat-io before interpreting the numbers.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a ioTrendArgs) (*mcp.CallToolResult, any, error) {
		return jsonResult(ioTrend(ctx, c, a))
	})
}

// closedWorld marks the tools as not interacting with an open world of external
// entities (Dasha queries a fixed, configured fleet — not the internet), so
// clients can reason about them as a closed, safe domain.
var closedWorld = false

// addTool registers a read-only Dasha tool, defaulting the annotations so MCP
// clients can present it as safe (and, where supported, auto-approve it): it does
// not modify anything (ReadOnlyHint) and its domain is closed (OpenWorldHint).
func addTool[In, Out any](
	s *mcp.Server,
	t *mcp.Tool,
	h func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error),
) {
	if t.Annotations == nil {
		t.Annotations = &mcp.ToolAnnotations{ //nolint:exhaustruct
			ReadOnlyHint:  true,
			OpenWorldHint: &closedWorld,
		}
	}

	mcp.AddTool(s, t, h)
}

// connectionSampleLimit caps the pg_stat_activity rows the connections tool
// returns, keeping the result readable while still showing who is connected.
const connectionSampleLimit = 100

// defaultPartitionLimit caps the partitions describe_table lists, so a heavily
// partitioned table does not blow the response size limit.
const defaultPartitionLimit = 50

// trendWindow maps a range keyword to a span and sampling step (seconds),
// matching the UI's ranges. Returns span=0 for an unknown keyword.
func trendWindow(rng string) (span time.Duration, step int) {
	switch rng {
	case "", "24h":
		return 24 * time.Hour, 300
	case "7d":
		return 7 * 24 * time.Hour, 1800
	case "30d":
		return 30 * 24 * time.Hour, 3600
	default:
		return 0, 0
	}
}

// resolveWindow maps the since / from+to argument pair every windowed tool
// accepts onto an absolute window, defaulting to the last def. Returns a
// non-empty message instead of a window when the arguments are invalid.
func resolveWindow(since, from, to string, def time.Duration) (time.Time, time.Time, string) {
	end := time.Now()
	start := end.Add(-def)

	switch {
	case from != "" || to != "":
		if from == "" || to == "" {
			return time.Time{}, time.Time{}, "from and to must be set together (RFC3339)"
		}

		var err error
		if start, err = time.Parse(time.RFC3339, from); err != nil {
			return time.Time{}, time.Time{}, "from must be RFC3339 (e.g. 2026-07-10T12:00:00Z)"
		}

		if end, err = time.Parse(time.RFC3339, to); err != nil {
			return time.Time{}, time.Time{}, "to must be RFC3339 (e.g. 2026-07-10T13:00:00Z)"
		}

		if !start.Before(end) {
			return time.Time{}, time.Time{}, "from must be before to"
		}
	case since != "":
		d, err := parseSince(since)
		if err != nil || d <= 0 {
			return time.Time{}, time.Time{}, "since must be a positive duration like '15m', '1h', '24h' or '7d'"
		}

		start = end.Add(-d)
	}

	return start, end, ""
}

const maxSinceDays = int64(math.MaxInt64 / (24 * time.Hour))

// time.ParseDuration has no day unit; models write '7d'.
func parseSince(since string) (time.Duration, error) {
	days, ok := strings.CutSuffix(since, "d")
	if !ok {
		return time.ParseDuration(since)
	}

	n, err := strconv.ParseInt(days, 10, 64)
	if err != nil {
		return 0, err
	}

	if n > maxSinceDays || n < -maxSinceDays {
		return 0, errors.New("day count out of range")
	}

	return time.Duration(n) * 24 * time.Hour, nil
}

// logsDefaultSince is the default look-back window for search_logs; a short
// window keeps the upstream scan (and the result) small.
const logsDefaultSince = time.Hour

// logsDefaultPageSize caps raw (dedup=false) records per page, keeping one
// page readable and cheap to fetch.
const logsDefaultPageSize = 100

// logsParams validates search_logs arguments locally and maps them onto the
// API params. Local validation matters more than usual here: the endpoint is
// rate-limited per user (it fronts the log store), so a request that would just
// 400 upstream must not burn a rate-limit slot.
func logsParams(a searchLogsArgs) (*apiclient.GetLogsParams, string) {
	const (
		typePostgresql = apiclient.GetLogsParamsServiceTypePostgresql
		typePooler     = apiclient.GetLogsParamsServiceTypePooler
	)

	serviceType := apiclient.GetLogsParamsServiceType(cmp.Or(a.ServiceType, string(typePostgresql)))
	if serviceType != typePostgresql && serviceType != typePooler {
		return nil, "service_type must be 'postgresql' or 'pooler'"
	}

	from, to, msg := resolveWindow(a.Since, a.From, a.To, logsDefaultSince)
	if msg != "" {
		return nil, msg
	}

	// Dedup defaults to on: grouped results are far smaller and usually enough.
	// Raw pagination is opt-in and mutually exclusive with dedup upstream.
	dedup := a.Dedup == nil || *a.Dedup
	if a.PageToken != "" {
		if dedup && a.Dedup != nil {
			return nil, "page_token cannot be combined with dedup=true"
		}

		dedup = false
	}

	pageSize := a.PageSize
	if pageSize <= 0 {
		pageSize = logsDefaultPageSize
	}

	return &apiclient.GetLogsParams{
		ClusterName: a.Cluster,
		ServiceType: serviceType,
		From:        from,
		To:          to,
		Severity:    optStrings(a.Severity),
		Host:        opt(a.Host),
		Message:     optStrings(a.Message),
		Exclude:     optStrings(a.Exclude),
		Database:    opt(a.Database),
		User:        opt(a.User),
		Dedup:       &dedup,
		PageSize:    &pageSize,
		PageToken:   opt(a.PageToken),
	}, ""
}
