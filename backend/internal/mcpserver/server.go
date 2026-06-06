package mcpserver

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// serverInstructions primes the model on how to drive the read-only Dasha tools
// — sent to the client at initialization so it picks targets and tools correctly
// without trial and error.
const serverInstructions = `Dasha exposes read-only PostgreSQL fleet diagnostics. All tools are safe to call.

Getting oriented:
- Call list_clusters first to get cluster and instance (host) names, or fleet_health for a worst-first overview of the whole fleet.
- Most tools require cluster + instance; query/index/table/lock tools also require database.

Investigating:
- For a guided workflow prefer the prompts: diagnose_cluster, explain_health_score, investigate_slow_queries, find_index_opportunities, fleet_overview.
- Typical chain: get_health_score -> get_health_recommendations -> (top_queries, blocked_queries, list_indexes, describe_table) to drill into the worst findings.
- health_trend needs metrics-backed mode (a configured datasource); it returns an error otherwise.
- query_compare needs snapshot IDs from list_snapshots.

If a result is refused as too large, narrow it (one database, a smaller range, or a more specific tool).`

// NewMCPServer builds the MCP server with the read-only Dasha tools registered.
// Output type 'any' is used for every tool so the handler fully controls the
// (compact) text content; the input schema is still auto-derived from the typed
// args via json/jsonschema struct tags.
func NewMCPServer(client *DashaClient, version string) *mcp.Server {
	return newServer(client, version, nil)
}

// newServer builds the server with shared options. cache is nil for stdio (one
// server for the process) or a shared *mcp.SchemaCache for HTTP (a server per
// token), where it avoids re-deriving every tool's schema by reflection.
func newServer(client *DashaClient, version string, cache *mcp.SchemaCache) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{ //nolint:exhaustruct
		Name:    "dasha-mcp",
		Title:   "Dasha PostgreSQL diagnostics",
		Version: version,
	}, &mcp.ServerOptions{ //nolint:exhaustruct
		Instructions: serverInstructions,
		SchemaCache:  cache,
	})
	registerTools(s, client)
	registerPrompts(s)

	return s
}

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

type topQueriesArgs struct {
	Cluster  string `json:"cluster" jsonschema:"Dasha cluster name"`
	Instance string `json:"instance" jsonschema:"Dasha instance / host name"`
	By       string `json:"by,omitempty" jsonschema:"Ranking metric: 'time' (total execution time, default) or 'wal' (WAL volume)"`
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
	ExcludeUsers []string `json:"exclude_users,omitempty" jsonschema:"Optional: usernames to exclude (e.g. monitoring/replication roles)"`
}

type queryCompareArgs struct {
	Cluster      string   `json:"cluster" jsonschema:"Dasha cluster name"`
	Instance     string   `json:"instance" jsonschema:"Dasha instance / host name"`
	Database     string   `json:"database" jsonschema:"Database name"`
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
}

type fleetHealthArgs struct {
	Limit int `json:"limit,omitempty" jsonschema:"How many worst-scoring instances to return (default 5)"`
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
			"metric_value) for a cluster/instance. Pass database for the per-database drill-down.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a recommendationsArgs) (*mcp.CallToolResult, any, error) {
		var db *string
		if a.Database != "" {
			db = &a.Database
		}

		out, err := c.Recommendations(ctx, a.Cluster, a.Instance, db)

		return jsonResult(out, err)
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
			"(by='time', default) or WAL volume (by='wal'). Requires pg_stat_statements.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a topQueriesArgs) (*mcp.CallToolResult, any, error) {
		switch a.By {
		case "", "time":
			return jsonResult(c.TopQueriesByTime(ctx, a.Cluster, a.Instance))
		case "wal":
			return jsonResult(c.TopQueriesByWal(ctx, a.Cluster, a.Instance))
		default:
			return errResult("by must be 'time' or 'wal'"), nil, nil
		}
	})

	addTool(s, &mcp.Tool{
		Name: "running_queries",
		Description: "List currently running queries on a database (pid, duration, state, query) — " +
			"useful to spot long-running or stuck statements.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a dbArgs) (*mcp.CallToolResult, any, error) {
		return jsonResult(c.RunningQueries(ctx, a.Cluster, a.Instance, a.Database))
	})

	addTool(s, &mcp.Tool{
		Name: "list_indexes",
		Description: "List index findings for a database: kind='missing' (suggested new indexes, " +
			"default), 'unused' (never scanned), or 'usage' (scan statistics).",
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
		Name:        "top_tables",
		Description: "List the largest tables in a database by total size.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a dbArgs) (*mcp.CallToolResult, any, error) {
		return jsonResult(c.TopTables(ctx, a.Cluster, a.Instance, a.Database))
	})

	addTool(s, &mcp.Tool{
		Name: "blocked_queries",
		Description: "List sessions currently blocked on locks (and the sessions blocking them) " +
			"for a database.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a dbArgs) (*mcp.CallToolResult, any, error) {
		return jsonResult(c.BlockedQueries(ctx, a.Cluster, a.Instance, a.Database))
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

		return jsonResult(out, nil)
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
			"rows, I/O). Pass exclude_users to drop noise from monitoring/replication roles. Requires pg_stat_statements.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a queryReportArgs) (*mcp.CallToolResult, any, error) {
		return jsonResult(c.QueryReport(ctx, a.Cluster, a.Instance, a.ExcludeUsers))
	})

	addTool(s, &mcp.Tool{
		Name: "list_snapshots",
		Description: "List the stored pg_stat_statements snapshots for a database (id, captured_at). " +
			"Use this to obtain the snapshot IDs that query_compare needs. Requires snapshot storage.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a dbArgs) (*mcp.CallToolResult, any, error) {
		return jsonResult(c.Snapshots(ctx, a.Cluster, a.Instance, a.Database))
	})

	addTool(s, &mcp.Tool{
		Name: "query_compare",
		Description: "Compare two pg_stat_statements snapshots (snapshot_a vs snapshot_b) for a database to " +
			"surface query regressions; omit snapshot_b to compare snapshot_a against live stats. Get IDs from list_snapshots.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a queryCompareArgs) (*mcp.CallToolResult, any, error) {
		var b *string
		if a.SnapshotB != "" {
			b = &a.SnapshotB
		}

		return jsonResult(c.QueryCompare(ctx, a.Cluster, a.Instance, a.Database, a.SnapshotA, b, a.ExcludeUsers))
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

		return jsonResult(out, nil)
	})

	addTool(s, &mcp.Tool{
		Name: "connections",
		Description: "Diagnose connection usage for a cluster/instance: counts by backend state and by client " +
			"source, plus a capped pg_stat_activity sample of who holds the connections.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a instanceArgs) (*mcp.CallToolResult, any, error) {
		out := map[string]any{}

		st, err := c.ConnectionStates(ctx, a.Cluster, a.Instance)
		section(out, "states", st, err)

		sr, err := c.ConnectionSources(ctx, a.Cluster, a.Instance)
		section(out, "sources", sr, err)

		act, err := c.ConnectionStatActivity(ctx, a.Cluster, a.Instance, connectionSampleLimit)
		section(out, "activity", act, err)

		return jsonResult(out, nil)
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

		out := map[string]any{}

		d, err := c.TableDescribe(ctx, a.Cluster, a.Instance, a.Database, schema, a.Table)
		section(out, "table", d, err)

		bl, err := c.TableDescribeBloat(ctx, a.Cluster, a.Instance, a.Database, schema, a.Table)
		section(out, "bloat", bl, err)

		pt, err := c.TableDescribePartitions(ctx, a.Cluster, a.Instance, a.Database, schema, a.Table)
		section(out, "partitions", pt, err)

		re, err := c.TableDescribeRowEstimate(ctx, a.Cluster, a.Instance, a.Database, schema, a.Table)
		section(out, "row_estimate", re, err)

		vs, err := c.TableDescribeVacuumStats(ctx, a.Cluster, a.Instance, a.Database, schema, a.Table)
		section(out, "vacuum_stats", vs, err)

		return jsonResult(out, nil)
	})

	addTool(s, &mcp.Tool{
		Name: "fleet_health",
		Description: "Scan every cluster/instance Dasha manages and return the worst-scoring instances " +
			"(health score, ascending). One call instead of looping list_clusters + get_health_score.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a fleetHealthArgs) (*mcp.CallToolResult, any, error) {
		return jsonResult(fleetHealth(ctx, c, a.Limit))
	})
}

// defaultFleetLimit caps how many worst instances fleet_health returns by default.
const defaultFleetLimit = 5

// fleetEntry is one instance's health in the fleet ranking.
type fleetEntry struct {
	Cluster    string  `json:"cluster"`
	Instance   string  `json:"instance"`
	Score      float64 `json:"score,omitempty"`
	Source     string  `json:"source,omitempty"`
	InRecovery bool    `json:"in_recovery,omitempty"`
	Error      string  `json:"error,omitempty"` // set when this instance's score could not be read
}

// fleetHealth ranks every instance by health score ascending (worst first),
// tolerating per-instance failures so one bad host does not sink the whole scan.
func fleetHealth(ctx context.Context, c *DashaClient, limit int) (any, error) {
	if limit <= 0 {
		limit = defaultFleetLimit
	}

	clusters, err := c.Clusters(ctx)
	if err != nil {
		return nil, err
	}

	var rows []fleetEntry

	for _, cl := range clusters {
		name := derefStr(cl.Name)
		if cl.Instances == nil {
			continue
		}

		for _, inst := range *cl.Instances {
			host := derefStr(inst.HostName)
			entry := fleetEntry{Cluster: name, Instance: host}

			hs, herr := c.HealthScore(ctx, name, host)
			if herr != nil {
				entry.Error = herr.Error()
			} else {
				entry.Score = hs.Score
				entry.Source = derefStr(hs.Source)
				entry.InRecovery = hs.InRecovery
			}

			rows = append(rows, entry)
		}
	}

	// Scored instances first (ascending), unreadable ones last.
	slices.SortStableFunc(rows, func(a, b fleetEntry) int {
		if (a.Error == "") != (b.Error == "") {
			if a.Error == "" {
				return -1
			}

			return 1
		}

		return cmp.Compare(a.Score, b.Score)
	})

	if len(rows) > limit {
		rows = rows[:limit]
	}

	return map[string]any{"limit": limit, "worst": rows}, nil
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}

	return *p
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

// maxResultBytes caps a single tool's JSON result so one call cannot flood the
// model's context window (and the transport). An oversized result is refused
// with a hint to narrow the request, rather than returning truncated — and thus
// invalid — JSON the model cannot parse.
const maxResultBytes = 256 * 1024

// jsonResult renders a payload as compact JSON text, or maps an error to an
// isError tool result the model can read and react to (rather than a protocol
// error it cannot see).
func jsonResult(payload any, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return errResult(err.Error()), nil, nil
	}

	b, mErr := json.Marshal(payload)
	if mErr != nil {
		return errResult(fmt.Sprintf("mcp: encode result: %v", mErr)), nil, nil
	}

	if len(b) > maxResultBytes {
		return oversizedResult(len(b)), nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(b)}},
	}, nil, nil
}

// oversizedResult tells the model the result was too large to return and how to
// get a smaller one, as a structured isError payload it can act on.
func oversizedResult(size int) *mcp.CallToolResult {
	msg := fmt.Sprintf(
		`{"error":"result too large","bytes":%d,"limit":%d,`+
			`"suggestion":"narrow the request — target a single database, use a more specific tool, `+
			`or a smaller range — the full result exceeds the response size limit"}`,
		size, maxResultBytes)

	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}
}

func errResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}
}

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

// section folds one part of a composite result in, recording a per-part error
// rather than failing the whole call (e.g. one replication sub-query failing).
func section(out map[string]any, key string, v any, err error) {
	if err != nil {
		out[key+"_error"] = err.Error()
	} else {
		out[key] = v
	}
}

// reqArg ("required") and optArg helpers read prompt arguments.
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
// that tells the model which tools to call and in what order.
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

func registerPrompts(s *mcp.Server) {
	s.AddPrompt(&mcp.Prompt{
		Name:        "diagnose_cluster",
		Description: "Diagnose why a PostgreSQL instance is unhealthy and propose fixes.",
		Arguments:   clusterInstanceArgs(),
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		cluster, instance := target(req)

		return userPrompt("Cluster diagnosis", fmt.Sprintf(
			"Diagnose the health of cluster %q instance %q. Steps: (1) call get_health_score; "+
				"(2) call get_health_recommendations; (3) if performance or locks look bad, also call "+
				"top_queries (by=time) and blocked_queries. Then summarise the root cause(s) and concrete "+
				"fixes, prioritising HIGH-severity findings.", cluster, instance))
	})

	s.AddPrompt(&mcp.Prompt{
		Name:        "explain_health_score",
		Description: "Explain an instance's health score and its recommendations.",
		Arguments: append(clusterInstanceArgs(),
			&mcp.PromptArgument{Name: "database", Description: "Optional: per-database scope"}),
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		cluster, instance := target(req)

		return userPrompt("Health score explanation", fmt.Sprintf(
			"Explain the health score of cluster %q instance %q%s. Call get_health_score and "+
				"get_health_recommendations, then explain the overall number, which categories drag it "+
				"down, any critical-floor conditions, and what each recommendation means.",
			cluster, instance, dbSuffix(arg(req, "database"))))
	})

	s.AddPrompt(&mcp.Prompt{
		Name:        "find_index_opportunities",
		Description: "Find missing/unused indexes in a database and tie them to slow queries.",
		Arguments: append(clusterInstanceArgs(),
			&mcp.PromptArgument{Name: "database", Description: "Database to inspect", Required: true}),
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		cluster, instance := target(req)

		return userPrompt("Index opportunities", fmt.Sprintf(
			"Find indexing opportunities in database %q of cluster %q instance %q. Call list_indexes with "+
				"kind=missing and kind=unused, and top_queries (by=time). Recommend indexes to add and "+
				"unused indexes to drop, tying them to the slow queries.",
			arg(req, "database"), cluster, instance))
	})

	s.AddPrompt(&mcp.Prompt{
		Name:        "investigate_slow_queries",
		Description: "Investigate slow / stuck / blocked queries on an instance.",
		Arguments: append(clusterInstanceArgs(),
			&mcp.PromptArgument{Name: "database", Description: "Database for running/blocked queries", Required: true}),
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		cluster, instance := target(req)

		return userPrompt("Slow query investigation", fmt.Sprintf(
			"Investigate slow queries on cluster %q instance %q (database %q). Call top_queries (by=time), "+
				"running_queries, and blocked_queries. Identify the heaviest statements, anything stuck or "+
				"blocked, and suggest next steps.", cluster, instance, arg(req, "database")))
	})

	s.AddPrompt(&mcp.Prompt{
		Name:        "fleet_overview",
		Description: "Summarise health across the whole fleet and surface the worst instances.",
	}, func(_ context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return userPrompt("Fleet overview",
			"Give a fleet health overview. Call list_clusters, then get_health_score for each cluster's "+
				"hosts, and report the worst-scoring clusters/instances with their main issues.")
	})
}
