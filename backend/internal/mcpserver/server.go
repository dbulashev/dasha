package mcpserver

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/dbulashev/dasha/gen/apiclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

// NewMCPServer builds the MCP server with the read-only Dasha tools registered.
// Output type 'any' is used for every tool so the handler fully controls the
// (compact) text content; the input schema is still auto-derived from the typed
// args via json/jsonschema struct tags. lang selects the knowledge-base
// language ("en"/"ru"; unknown values fall back to "en").
func NewMCPServer(client *DashaClient, version, lang string) *mcp.Server {
	return newServer(client, version, lang, nil)
}

// newServer builds the server with shared options. cache is nil for stdio (one
// server for the process) or a shared *mcp.SchemaCache for HTTP (a server per
// token), where it avoids re-deriving every tool's schema by reflection. The
// instructions, prompt playbooks and knowledge-base resources come in lang
// ("en"/"ru"; unknown falls back to "en").
func newServer(client *DashaClient, version, lang string, cache *mcp.SchemaCache) *mcp.Server {
	if !validLang(lang) {
		lang = kbDefaultLang
	}

	t := textsFor(lang)

	s := mcp.NewServer(&mcp.Implementation{ //nolint:exhaustruct
		Name:    "dasha-mcp",
		Title:   "Dasha PostgreSQL diagnostics",
		Version: version,
	}, &mcp.ServerOptions{ //nolint:exhaustruct
		Instructions: t.instructions,
		SchemaCache:  cache,
	})
	registerTools(s, client)
	registerPrompts(s, t)
	registerResources(s, lang)

	if client.logger != nil {
		s.AddReceivingMiddleware(loggingMiddleware(client.logger))
	}

	return s
}

// loggingMiddleware logs every incoming MCP call — method, target (tool,
// prompt or resource), duration, outcome. Arguments are never logged: they may
// embed user-written filters or query text, and tokens must stay out of logs.
func loggingMiddleware(logger *zap.Logger) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			start := time.Now()
			res, err := next(ctx, method, req)

			fields := []zap.Field{
				zap.String("method", method),
				zap.Duration("duration", time.Since(start)),
			}

			switch p := req.GetParams().(type) {
			case *mcp.CallToolParamsRaw:
				fields = append(fields, zap.String("tool", p.Name))
			case *mcp.GetPromptParams:
				fields = append(fields, zap.String("prompt", p.Name))
			case *mcp.ReadResourceParams:
				fields = append(fields, zap.String("resource", p.URI))
			}

			switch {
			case err != nil:
				logger.Warn("mcp call failed", append(fields, zap.Error(err))...)
			default:
				if r, ok := res.(*mcp.CallToolResult); ok && r.IsError {
					fields = append(fields, zap.Bool("is_error", true))
				}

				logger.Info("mcp call", fields...)
			}

			return res, err
		}
	}
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
		Description: "Search PostgreSQL server or connection-pooler (Odyssey) logs of a Yandex-MDB-discovered " +
			"cluster (supports_logs=true in list_clusters). Every call reaches the Yandex Cloud API and is " +
			"rate-limited per user (default ~1 request per 30s with a small burst) — make each call count: " +
			"keep the default dedup=true overview, a narrow window (since='1h') and severity/message filters, " +
			"and refine with one follow-up call instead of paging raw records. After a 429 wait ~30 seconds.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a searchLogsArgs) (*mcp.CallToolResult, any, error) {
		params, errMsg := logsParams(a)
		if errMsg != "" {
			return errResult(errMsg), nil, nil
		}

		return jsonResult(c.SearchLogs(ctx, params))
	})
}

// defaultFleetLimit caps how many worst instances fleet_health returns by default.
const defaultFleetLimit = 5

// fleetHealthConcurrency bounds the parallel per-instance health-score fetches so
// a large fleet is scored quickly without flooding Dasha with requests at once.
const fleetHealthConcurrency = 12

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

	// Collect every target first, then score them concurrently: a large fleet
	// otherwise serializes one HTTP round-trip per instance.
	rows := make([]fleetEntry, 0)

	for _, cl := range clusters {
		name := derefStr(cl.Name)
		if cl.Instances == nil {
			continue
		}

		for _, inst := range *cl.Instances {
			rows = append(rows, fleetEntry{Cluster: name, Instance: derefStr(inst.HostName)}) //nolint:exhaustruct
		}
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(fleetHealthConcurrency)

	for i := range rows {
		g.Go(func() error {
			hs, herr := c.HealthScore(gctx, rows[i].Cluster, rows[i].Instance)
			if herr != nil {
				rows[i].Error = herr.Error()

				return nil // tolerate per-instance failures
			}

			rows[i].Score = hs.Score
			rows[i].Source = derefStr(hs.Source)
			rows[i].InRecovery = hs.InRecovery

			return nil
		})
	}

	_ = g.Wait() // no goroutine returns a non-nil error; failures are per-row

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

// defaultPartitionLimit caps the partitions describe_table lists, so a heavily
// partitioned table does not blow the response size limit.
const defaultPartitionLimit = 50

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

// logsDefaultSince is the default look-back window for search_logs; a short
// window keeps the upstream Yandex API scan (and the result) small.
const logsDefaultSince = time.Hour

// logsDefaultPageSize caps raw (dedup=false) records per page, keeping one
// page readable and cheap to fetch.
const logsDefaultPageSize = 100

// logsParams validates search_logs arguments locally and maps them onto the
// API params. Local validation matters more than usual here: the endpoint is
// rate-limited per user (it fronts the Yandex Cloud API), so a request that
// would just 400 upstream must not burn a rate-limit slot.
func logsParams(a searchLogsArgs) (*apiclient.GetLogsParams, string) {
	serviceType := apiclient.GetLogsParamsServiceType(cmp.Or(a.ServiceType, string(apiclient.Postgresql)))
	if serviceType != apiclient.Postgresql && serviceType != apiclient.Pooler {
		return nil, "service_type must be 'postgresql' or 'pooler'"
	}

	to := time.Now()
	from := to.Add(-logsDefaultSince)

	switch {
	case a.From != "" || a.To != "":
		if a.From == "" || a.To == "" {
			return nil, "from and to must be set together (RFC3339)"
		}

		var err error
		if from, err = time.Parse(time.RFC3339, a.From); err != nil {
			return nil, "from must be RFC3339 (e.g. 2026-07-10T12:00:00Z)"
		}

		if to, err = time.Parse(time.RFC3339, a.To); err != nil {
			return nil, "to must be RFC3339 (e.g. 2026-07-10T13:00:00Z)"
		}

		if !from.Before(to) {
			return nil, "from must be before to"
		}
	case a.Since != "":
		d, err := time.ParseDuration(a.Since)
		if err != nil || d <= 0 {
			return nil, "since must be a positive duration like '15m', '1h' or '24h'"
		}

		from = to.Add(-d)
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

// section folds one part of a composite result in, recording a per-part error
// rather than failing the whole call (e.g. one replication sub-query failing).
func section(out map[string]any, key string, v any, err error) {
	if err != nil {
		out[key+"_error"] = err.Error()
	} else {
		out[key] = v
	}
}

// sectionsResult renders a composite result, but marks it IsError when EVERY
// section failed (e.g. a permission error on every sub-request) so the model
// does not treat an all-errors payload as usable data.
func sectionsResult(out map[string]any) (*mcp.CallToolResult, any, error) {
	allFailed := len(out) > 0
	for k := range out {
		if !strings.HasSuffix(k, "_error") {
			allFailed = false

			break
		}
	}

	if allFailed {
		b, err := json.Marshal(out)
		if err != nil {
			return errResult(fmt.Sprintf("mcp: encode result: %v", err)), nil, nil
		}

		return errResult(string(b)), nil, nil
	}

	return jsonResult(out, nil)
}
