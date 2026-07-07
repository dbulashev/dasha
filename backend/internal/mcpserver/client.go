package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/dbulashev/dasha/gen/apiclient"
	"github.com/google/uuid"
)

// tokenKey carries the per-request Dasha API token so the HTTP transport can pass
// each MCP client's identity through to Dasha — there is no shared server token
// in HTTP mode, which keeps users isolated and RBAC intact.
type tokenKey struct{}

// WithToken returns a context carrying the Dasha API token (a static token or a
// personal access token) used for outbound calls made within it.
func WithToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, tokenKey{}, token)
}

func tokenFromContext(ctx context.Context) string {
	t, _ := ctx.Value(tokenKey{}).(string)

	return t
}

// DashaClient is a thin, identity-passthrough wrapper over the generated Dasha
// API client: every call forwards the caller's token as the X-API-Key header.
type DashaClient struct {
	api   *apiclient.ClientWithResponses
	token string // default token (stdio single-identity); a per-request ctx token wins
}

// NewDashaClient builds a client against the configured Dasha API.
func NewDashaClient(cfg Config) (*DashaClient, error) {
	cfg = cfg.withDefaults()

	hc := &http.Client{Timeout: cfg.Timeout} //nolint:exhaustruct

	api, err := apiclient.NewClientWithResponses(cfg.DashaURL, apiclient.WithHTTPClient(hc))
	if err != nil {
		return nil, fmt.Errorf("mcp: build dasha client: %w", err)
	}

	return &DashaClient{api: api, token: cfg.Token}, nil
}

// withToken returns a shallow copy bound to a specific token, sharing the
// underlying HTTP client. Used in HTTP mode to give each request its own
// identity (per-user passthrough) without rebuilding the API client. An empty
// token keeps the configured default (the DASHA_MCP_TOKEN fallback) rather than
// clearing it, so header-less clients fall back to that identity as documented.
func (d *DashaClient) withToken(token string) *DashaClient {
	if token == "" {
		return d
	}

	c := *d
	c.token = token

	return &c
}

// editor injects the effective token as X-API-Key: the per-request token from
// the context (HTTP passthrough) when present, otherwise the configured default
// (stdio single identity).
func (d *DashaClient) editor(ctx context.Context) apiclient.RequestEditorFn {
	token := tokenFromContext(ctx)
	if token == "" {
		token = d.token
	}

	return func(_ context.Context, req *http.Request) error {
		if token != "" {
			req.Header.Set("X-API-Key", token)
		}

		return nil
	}
}

// Clusters lists the configured/discovered clusters of the fleet — the entry
// point for an LLM to pick a (cluster, instance) target.
func (d *DashaClient) Clusters(ctx context.Context) ([]apiclient.Cluster, error) {
	resp, err := d.api.GetClustersWithResponse(ctx, d.editor(ctx))
	if err != nil {
		return nil, fmt.Errorf("mcp: clusters: %w", err)
	}

	if resp.JSON200 == nil {
		return nil, statusError("clusters", resp.HTTPResponse)
	}

	return *resp.JSON200, nil
}

// HealthScore returns the instance-level composite health score.
func (d *DashaClient) HealthScore(ctx context.Context, cluster, instance string) (*apiclient.HealthScore, error) {
	resp, err := d.api.GetHealthScoreWithResponse(ctx, &apiclient.GetHealthScoreParams{
		ClusterName: cluster,
		Instance:    instance,
	}, d.editor(ctx))
	if err != nil {
		return nil, fmt.Errorf("mcp: health_score: %w", err)
	}

	if resp.JSON200 == nil {
		return nil, statusError("health_score", resp.HTTPResponse)
	}

	return resp.JSON200, nil
}

// Recommendations returns the health-score recommendations; pass a non-nil
// database for the per-database drill-down.
func (d *DashaClient) Recommendations(
	ctx context.Context,
	cluster, instance string,
	database *string,
) (*apiclient.HealthScoreRecommendations, error) {
	resp, err := d.api.GetHealthScoreRecommendationsWithResponse(ctx, &apiclient.GetHealthScoreRecommendationsParams{
		ClusterName: cluster,
		Instance:    instance,
		Database:    database,
	}, d.editor(ctx))
	if err != nil {
		return nil, fmt.Errorf("mcp: recommendations: %w", err)
	}

	if resp.JSON200 == nil {
		return nil, statusError("recommendations", resp.HTTPResponse)
	}

	return resp.JSON200, nil
}

// InstanceInfo returns server version and recovery state for an instance.
func (d *DashaClient) InstanceInfo(ctx context.Context, cluster, instance string) (any, error) {
	r, err := d.api.GetInstanceInfoWithResponse(ctx, &apiclient.GetInstanceInfoParams{
		ClusterName: cluster, Instance: instance,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("instance_info", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "instance_info")
}

// TopQueriesByTime lists the top queries by total execution time.
func (d *DashaClient) TopQueriesByTime(ctx context.Context, cluster, instance string) (any, error) {
	r, err := d.api.GetQueriesTop10ByTimeWithResponse(ctx, &apiclient.GetQueriesTop10ByTimeParams{
		ClusterName: cluster, Instance: instance,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("top_queries", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "top_queries")
}

// TopQueriesByWal lists the top queries by WAL volume.
func (d *DashaClient) TopQueriesByWal(ctx context.Context, cluster, instance string) (any, error) {
	r, err := d.api.GetQueriesTop10ByWalWithResponse(ctx, &apiclient.GetQueriesTop10ByWalParams{
		ClusterName: cluster, Instance: instance,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("top_queries", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "top_queries")
}

// RunningQueries lists currently-running queries on a database.
func (d *DashaClient) RunningQueries(ctx context.Context, cluster, instance, database string) (any, error) {
	r, err := d.api.GetQueriesRunningWithResponse(ctx, &apiclient.GetQueriesRunningParams{
		ClusterName: cluster, Instance: instance, Database: database,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("running_queries", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "running_queries")
}

// IndexesMissing lists suggested missing indexes for a database.
func (d *DashaClient) IndexesMissing(ctx context.Context, cluster, instance, database string) (any, error) {
	r, err := d.api.GetIndexesMissingWithResponse(ctx, &apiclient.GetIndexesMissingParams{
		ClusterName: cluster, Instance: instance, Database: database,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("list_indexes", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "list_indexes")
}

// IndexesUnused lists never-scanned (unused) indexes for a database.
func (d *DashaClient) IndexesUnused(ctx context.Context, cluster, instance, database string) (any, error) {
	r, err := d.api.GetIndexesUnusedWithResponse(ctx, &apiclient.GetIndexesUnusedParams{
		ClusterName: cluster, Instance: instance, Database: database,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("list_indexes", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "list_indexes")
}

// IndexesUsage lists index scan statistics for a database.
func (d *DashaClient) IndexesUsage(ctx context.Context, cluster, instance, database string) (any, error) {
	r, err := d.api.GetIndexesUsageWithResponse(ctx, &apiclient.GetIndexesUsageParams{
		ClusterName: cluster, Instance: instance, Database: database,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("list_indexes", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "list_indexes")
}

// TopTables lists the largest tables in a database.
func (d *DashaClient) TopTables(ctx context.Context, cluster, instance, database string) (any, error) {
	r, err := d.api.GetTablesTopKBySizeWithResponse(ctx, &apiclient.GetTablesTopKBySizeParams{
		ClusterName: cluster, Instance: instance, Database: database,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("top_tables", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "top_tables")
}

// BlockedQueries lists sessions blocked on locks (and their blockers).
func (d *DashaClient) BlockedQueries(ctx context.Context, cluster, instance, database string) (any, error) {
	r, err := d.api.GetQueriesBlockedWithResponse(ctx, &apiclient.GetQueriesBlockedParams{
		ClusterName: cluster, Instance: instance, Database: database,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("blocked_queries", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "blocked_queries")
}

// HealthTrend returns the health-score time series (points, seasonal baseline,
// dips) over [from, to] at the given step (seconds). Metrics-backed mode only.
func (d *DashaClient) HealthTrend(ctx context.Context, cluster, instance string, from, to time.Time, step int) (any, error) {
	r, err := d.api.GetHealthScoreHistoryWithResponse(ctx, &apiclient.GetHealthScoreHistoryParams{
		ClusterName: cluster, Instance: instance, From: from, To: to, StepSeconds: &step,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("health_trend", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "health_trend")
}

// HealthDatabases returns per-database health scores (incl. the worst).
func (d *DashaClient) HealthDatabases(ctx context.Context, cluster, instance string) (any, error) {
	r, err := d.api.GetHealthScoreDatabasesWithResponse(ctx, &apiclient.GetHealthScoreDatabasesParams{
		ClusterName: cluster, Instance: instance,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("health_databases", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "health_databases")
}

// ReplicationStatus returns standby state and lag.
func (d *DashaClient) ReplicationStatus(ctx context.Context, cluster, instance string) (any, error) {
	r, err := d.api.GetReplicationStatusWithResponse(ctx, &apiclient.GetReplicationStatusParams{
		ClusterName: cluster, Instance: instance,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("replication_status", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "replication_status")
}

// ReplicationSlots returns replication slots and their WAL retention.
func (d *DashaClient) ReplicationSlots(ctx context.Context, cluster, instance string) (any, error) {
	r, err := d.api.GetReplicationSlotsWithResponse(ctx, &apiclient.GetReplicationSlotsParams{
		ClusterName: cluster, Instance: instance,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("replication_slots", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "replication_slots")
}

// ReplicationConfig returns synchronous replication settings.
func (d *DashaClient) ReplicationConfig(ctx context.Context, cluster, instance string) (any, error) {
	r, err := d.api.GetReplicationConfigWithResponse(ctx, &apiclient.GetReplicationConfigParams{
		ClusterName: cluster, Instance: instance,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("replication_config", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "replication_config")
}

// SettingsAnalyze returns configuration findings/suggestions from pg_settings.
func (d *DashaClient) SettingsAnalyze(ctx context.Context, cluster, instance string) (any, error) {
	r, err := d.api.GetSettingsAnalyzeWithResponse(ctx, &apiclient.GetSettingsAnalyzeParams{
		ClusterName: cluster, Instance: instance,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("settings_analyze", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "settings_analyze")
}

// WaitEvents returns current wait events grouped by type/event.
func (d *DashaClient) WaitEvents(ctx context.Context, cluster, instance string) (any, error) {
	r, err := d.api.GetConnectionWaitEventsWithResponse(ctx, &apiclient.GetConnectionWaitEventsParams{
		ClusterName: cluster, Instance: instance,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("wait_events", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "wait_events")
}

// QueryReport returns the full pg_stat_statements report for an instance,
// optionally excluding the given usernames (e.g. monitoring/replication roles).
func (d *DashaClient) QueryReport(ctx context.Context, cluster, instance string, excludeUsers []string) (any, error) {
	r, err := d.api.GetQueriesReportWithResponse(ctx, &apiclient.GetQueriesReportParams{
		ClusterName: cluster, Instance: instance, ExcludeUsers: optStrings(excludeUsers),
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("query_report", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "query_report")
}

// Snapshots lists the stored pg_stat_statements snapshots for a database — the
// source of the snapshot IDs that query_compare consumes.
func (d *DashaClient) Snapshots(ctx context.Context, cluster, instance, database string) (any, error) {
	r, err := d.api.GetSnapshotsWithResponse(ctx, &apiclient.GetSnapshotsParams{
		ClusterName: cluster, Instance: instance, Database: database,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("list_snapshots", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "list_snapshots")
}

// QueryCompare diffs two pg_stat_statements snapshots (or snapshotA vs. live
// when snapshotB is empty), so regressions between two points in time surface.
func (d *DashaClient) QueryCompare(
	ctx context.Context,
	cluster, instance, database, snapshotA string,
	snapshotB *string,
	excludeUsers []string,
) (any, error) {
	a, err := uuid.Parse(snapshotA)
	if err != nil {
		return nil, fmt.Errorf("mcp: query_compare: invalid snapshot_a %q: %w", snapshotA, err)
	}

	var bPtr *uuid.UUID
	if snapshotB != nil && *snapshotB != "" {
		b, perr := uuid.Parse(*snapshotB)
		if perr != nil {
			return nil, fmt.Errorf("mcp: query_compare: invalid snapshot_b %q: %w", *snapshotB, perr)
		}
		bPtr = &b
	}

	r, err := d.api.GetQueriesCompareWithResponse(ctx, &apiclient.GetQueriesCompareParams{
		ClusterName: cluster, Instance: instance, Database: database,
		SnapshotA: a, SnapshotB: bPtr, ExcludeUsers: optStrings(excludeUsers),
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("query_compare", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "query_compare")
}

// TransactionIdDanger reports per-table transaction-id age vs. the wraparound
// horizon for a database (the urgent vacuum-freeze targets).
func (d *DashaClient) TransactionIdDanger(ctx context.Context, cluster, instance, database string) (any, error) {
	r, err := d.api.GetMaintenanceTransactionIdDangerWithResponse(ctx, &apiclient.GetMaintenanceTransactionIdDangerParams{
		ClusterName: cluster, Instance: instance, Database: database,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("transaction_id_danger", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "transaction_id_danger")
}

// AutovacuumFreezeMaxAge returns the instance-level autovacuum freeze settings
// and how close relations sit to the freeze threshold.
func (d *DashaClient) AutovacuumFreezeMaxAge(ctx context.Context, cluster, instance string) (any, error) {
	r, err := d.api.GetMaintenanceAutovacuumFreezeMaxAgeWithResponse(ctx, &apiclient.GetMaintenanceAutovacuumFreezeMaxAgeParams{
		ClusterName: cluster, Instance: instance,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("autovacuum_freeze_max_age", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "autovacuum_freeze_max_age")
}

// ConnectionStates returns connection counts grouped by backend state.
func (d *DashaClient) ConnectionStates(ctx context.Context, cluster, instance string) (any, error) {
	r, err := d.api.GetConnectionStatesWithResponse(ctx, &apiclient.GetConnectionStatesParams{
		ClusterName: cluster, Instance: instance,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("connection_states", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "connection_states")
}

// ConnectionSources returns connection counts grouped by client source.
func (d *DashaClient) ConnectionSources(ctx context.Context, cluster, instance string) (any, error) {
	r, err := d.api.GetConnectionSourcesWithResponse(ctx, &apiclient.GetConnectionSourcesParams{
		ClusterName: cluster, Instance: instance,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("connection_sources", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "connection_sources")
}

// ConnectionStatActivity returns a capped slice of live pg_stat_activity rows
// (who holds the connections), to diagnose connection saturation.
func (d *DashaClient) ConnectionStatActivity(ctx context.Context, cluster, instance string, limit int) (any, error) {
	r, err := d.api.GetConnectionStatActivityWithResponse(ctx, &apiclient.GetConnectionStatActivityParams{
		ClusterName: cluster, Instance: instance, Limit: &limit,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("connection_stat_activity", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "connection_stat_activity")
}

// TableDescribe returns the column/index/constraint layout of one table.
func (d *DashaClient) TableDescribe(ctx context.Context, cluster, instance, database, schema, table string) (any, error) {
	r, err := d.api.GetTablesDescribeWithResponse(ctx, &apiclient.GetTablesDescribeParams{
		ClusterName: cluster, Instance: instance, Database: database, Schema: schema, Table: table,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("describe_table", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "describe_table")
}

// TableDescribeBloat returns the estimated bloat of one table.
func (d *DashaClient) TableDescribeBloat(ctx context.Context, cluster, instance, database, schema, table string) (any, error) {
	r, err := d.api.GetTablesDescribeBloatWithResponse(ctx, &apiclient.GetTablesDescribeBloatParams{
		ClusterName: cluster, Instance: instance, Database: database, Schema: schema, Table: table,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("describe_table", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "describe_table")
}

// TableDescribePartitions returns up to limit partitions of one partitioned
// table (heavily partitioned tables can have thousands — the cap keeps the
// result within the response size limit).
func (d *DashaClient) TableDescribePartitions(ctx context.Context, cluster, instance, database, schema, table string, limit int) (any, error) {
	r, err := d.api.GetTablesDescribePartitionsWithResponse(ctx, &apiclient.GetTablesDescribePartitionsParams{
		ClusterName: cluster, Instance: instance, Database: database, Schema: schema, Table: table, Limit: &limit,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("describe_table", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "describe_table")
}

// TableDescribeRowEstimate returns the estimated vs. exact row count of one table.
func (d *DashaClient) TableDescribeRowEstimate(ctx context.Context, cluster, instance, database, schema, table string) (any, error) {
	r, err := d.api.GetTablesDescribeRowEstimateWithResponse(ctx, &apiclient.GetTablesDescribeRowEstimateParams{
		ClusterName: cluster, Instance: instance, Database: database, Schema: schema, Table: table,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("describe_table", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "describe_table")
}

// TableDescribeVacuumStats returns autovacuum/analyze stats for one table.
func (d *DashaClient) TableDescribeVacuumStats(ctx context.Context, cluster, instance, database, schema, table string) (any, error) {
	r, err := d.api.GetTablesDescribeVacuumStatsWithResponse(ctx, &apiclient.GetTablesDescribeVacuumStatsParams{
		ClusterName: cluster, Instance: instance, Database: database, Schema: schema, Table: table,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("describe_table", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "describe_table")
}

// optStrings returns a pointer to the slice for optional query params, or nil
// when empty so the parameter is omitted entirely.
func optStrings(v []string) *[]string {
	if len(v) == 0 {
		return nil
	}

	return &v
}

func wrapErr(op string, err error) error {
	return fmt.Errorf("mcp: %s: %w", op, err)
}

// pick returns the 200 payload, or a mapped error when it is absent. Call only
// after the transport error has been checked (so the response is non-nil).
func pick[T any](payload *T, resp *http.Response, op string) (any, error) {
	if payload == nil {
		return nil, statusError(op, resp)
	}

	return payload, nil
}

// statusError maps a non-200 Dasha response to a message an LLM can act on.
func statusError(op string, resp *http.Response) error {
	code := 0
	if resp != nil {
		code = resp.StatusCode
	}

	switch code {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("dasha: access denied (%d) — the token's role is insufficient for %s", code, op)
	case http.StatusNotFound:
		return fmt.Errorf("dasha: not found (404) — unknown cluster/instance/database")
	default:
		return fmt.Errorf("dasha: %s returned status %d", op, code)
	}
}
