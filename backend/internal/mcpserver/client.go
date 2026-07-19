package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/dbulashev/dasha/gen/apiclient"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// DashaClient is a thin, identity-passthrough wrapper over the generated Dasha
// API client: every call forwards the caller's token as the X-API-Key header.
type DashaClient struct {
	api    *apiclient.ClientWithResponses
	token  string // the identity bound to this client (stdio default, or per-request via withToken)
	logger *zap.Logger
}

// NewDashaClient builds a client against the configured Dasha API.
func NewDashaClient(cfg Config) (*DashaClient, error) {
	cfg = cfg.withDefaults()

	hc := &http.Client{Timeout: cfg.Timeout} //nolint:exhaustruct

	api, err := apiclient.NewClientWithResponses(cfg.DashaURL, apiclient.WithHTTPClient(hc))
	if err != nil {
		return nil, fmt.Errorf("mcp: build dasha client: %w", err)
	}

	return &DashaClient{api: api, token: cfg.Token, logger: cfg.Logger}, nil
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

// editor injects this client's token as X-API-Key: the stdio single identity, or
// the per-request identity bound by withToken in HTTP mode.
func (d *DashaClient) editor(_ context.Context) apiclient.RequestEditorFn {
	return func(_ context.Context, req *http.Request) error {
		if d.token != "" {
			req.Header.Set("X-API-Key", d.token)
		}

		return nil
	}
}

// Clusters lists the configured/discovered clusters of the fleet — the entry
// point for an LLM to pick a (cluster, instance) target.
func (d *DashaClient) Clusters(ctx context.Context) ([]apiclient.Cluster, error) {
	resp, err := d.api.GetClustersWithResponse(ctx, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("clusters", err)
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
		return nil, wrapErr("health_score", err)
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
		return nil, wrapErr("recommendations", err)
	}

	if resp.JSON200 == nil {
		return nil, statusError("recommendations", resp.HTTPResponse)
	}

	return resp.JSON200, nil
}

// Health-score drill-downs. Recommendations names the rule that fired and how bad
// it is; these name the objects behind it, one method per rule_id, so a caller can
// go straight from a recommendation to its evidence. A zero limit lets the API
// apply its own default (15).

// HealthTablesAutovacuumOff lists tables with autovacuum_enabled=false in their
// reloptions — the evidence for the tables_with_autovacuum_off rule.
func (d *DashaClient) HealthTablesAutovacuumOff(
	ctx context.Context,
	cluster, instance, database string,
	limit int,
) (any, error) {
	r, err := d.api.GetHealthScoreTablesAutovacuumOffWithResponse(ctx, &apiclient.GetHealthScoreTablesAutovacuumOffParams{
		ClusterName: cluster, Instance: instance, Database: database, Limit: opt(limit), Offset: nil,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("tables_autovacuum_off", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "tables_autovacuum_off")
}

// HealthLowHotUpdateTables lists the tables whose UPDATEs least often take the HOT
// path (every non-HOT update rewrites every index) — evidence for the
// low_hot_update_ratio and high_newpage_update_ratio rules.
func (d *DashaClient) HealthLowHotUpdateTables(
	ctx context.Context,
	cluster, instance, database string,
	limit int,
) (any, error) {
	r, err := d.api.GetHealthScoreLowHotUpdateTablesWithResponse(ctx, &apiclient.GetHealthScoreLowHotUpdateTablesParams{
		ClusterName: cluster, Instance: instance, Database: database, Limit: opt(limit), Offset: nil,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("low_hot_update_tables", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "low_hot_update_tables")
}

// HealthHighDeadRatioTables lists the tables with the most dead tuples relative to
// live ones — evidence for the high_dead_ratio rule.
func (d *DashaClient) HealthHighDeadRatioTables(
	ctx context.Context,
	cluster, instance, database string,
	limit int,
) (any, error) {
	r, err := d.api.GetHealthScoreHighDeadRatioTablesWithResponse(ctx, &apiclient.GetHealthScoreHighDeadRatioTablesParams{
		ClusterName: cluster, Instance: instance, Database: database, Limit: opt(limit), Offset: nil,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("high_dead_ratio_tables", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "high_dead_ratio_tables")
}

// HealthXidWraparoundDatabases lists databases by transaction-id age — evidence for
// the transaction-id wraparound rules. Instance-wide: it takes no database.
func (d *DashaClient) HealthXidWraparoundDatabases(
	ctx context.Context,
	cluster, instance string,
	limit int,
) (any, error) {
	r, err := d.api.GetHealthScoreXidWraparoundDatabasesWithResponse(ctx, &apiclient.GetHealthScoreXidWraparoundDatabasesParams{
		ClusterName: cluster, Instance: instance, Limit: opt(limit), Offset: nil,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("xid_wraparound_databases", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "xid_wraparound_databases")
}

// HealthHorizonBlockingSessions lists the sessions holding back the xmin horizon
// (so vacuum cannot reclaim dead tuples) — evidence for the horizon rules.
// Instance-wide: it takes no database.
func (d *DashaClient) HealthHorizonBlockingSessions(
	ctx context.Context,
	cluster, instance string,
	limit int,
) (any, error) {
	r, err := d.api.GetHealthScoreHorizonBlockingSessionsWithResponse(ctx, &apiclient.GetHealthScoreHorizonBlockingSessionsParams{
		ClusterName: cluster, Instance: instance, Limit: opt(limit), Offset: nil,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("horizon_blocking_sessions", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "horizon_blocking_sessions")
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
// AllHosts is forced on: idx_scan is per-instance and is NOT replicated, so an
// index idle on the primary can be serving the whole read workload on a replica.
// The cluster-wide aggregation takes max(idx_scan) across every host, so only an
// index unused EVERYWHERE is reported — recommending a DROP from the single-host
// view would break replica reads.
func (d *DashaClient) IndexesUnused(ctx context.Context, cluster, instance, database string) (any, error) {
	r, err := d.api.GetIndexesUnusedWithResponse(ctx, &apiclient.GetIndexesUnusedParams{
		ClusterName: cluster, Instance: instance, Database: database, AllHosts: opt(true),
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("list_indexes", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "list_indexes")
}

// UnusedIndexReport returns the cluster-wide verdict on every index: whether it can
// be dropped, and why. Takes no instance — the whole point is that one host cannot
// prove an index unused (idx_scan is not replicated), so the verdict weighs every
// host of the cluster and the statistics window behind each counter.
func (d *DashaClient) UnusedIndexReport(ctx context.Context, cluster, database string, limit int) (any, error) {
	r, err := d.api.GetIndexesUnusedReportWithResponse(ctx, &apiclient.GetIndexesUnusedReportParams{
		ClusterName: cluster, Database: database, Limit: opt(limit), Offset: nil,
	}, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("unused_index_report", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "unused_index_report")
}

// HotTables returns the stored hot-tables top for one metric class: daily
// per-host deltas summed cluster-wide, with the tail histogram and the
// coverage ratio that says how representative the top is.
func (d *DashaClient) HotTables(ctx context.Context, cluster, database, class string, limit int) (any, error) {
	params := &apiclient.GetTablesHotParams{ //nolint:exhaustruct
		ClusterName: cluster, Database: database, Limit: opt(limit),
	}

	if class != "" {
		c := apiclient.GetTablesHotParamsClass(class)
		params.Class = &c
	}

	r, err := d.api.GetTablesHotWithResponse(ctx, params, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("hot_tables", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "hot_tables")
}

// HotIndexes is HotTables for indexes (classes: reads, io — PostgreSQL keeps
// no per-index write counters).
func (d *DashaClient) HotIndexes(ctx context.Context, cluster, database, class string, limit int) (any, error) {
	params := &apiclient.GetIndexesHotParams{ //nolint:exhaustruct
		ClusterName: cluster, Database: database, Limit: opt(limit),
	}

	if class != "" {
		c := apiclient.GetIndexesHotParamsClass(class)
		params.Class = &c
	}

	r, err := d.api.GetIndexesHotWithResponse(ctx, params, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("hot_indexes", err)
	}

	return pick(r.JSON200, r.HTTPResponse, "hot_indexes")
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

// SearchLogs searches PostgreSQL / connection-pooler logs of a Yandex-MDB
// cluster over a bounded time window. Dasha proxies every call to the Yandex
// Cloud logs API and rate-limits the route per user, so each non-200 is mapped
// to guidance the model can act on (wait on 429, narrow on 504) instead of a
// bare status code.
func (d *DashaClient) SearchLogs(ctx context.Context, params *apiclient.GetLogsParams) (any, error) {
	r, err := d.api.GetLogsWithResponse(ctx, params, d.editor(ctx))
	if err != nil {
		return nil, wrapErr("search_logs", err)
	}

	if r.JSON200 == nil && r.HTTPResponse != nil {
		switch r.HTTPResponse.StatusCode {
		case http.StatusTooManyRequests:
			return nil, errors.New("dasha: log search is rate-limited per user to protect the Yandex Cloud API " +
				"(default ~1 request per 30s with a small burst) — wait at least 30 seconds, then retry with " +
				"every needed filter combined into that one call")
		case http.StatusNotImplemented:
			return nil, errors.New("dasha: this cluster does not support log search (501) — only clusters " +
				"discovered via Yandex MDB do; check supports_logs in list_clusters")
		case http.StatusBadRequest:
			return nil, errors.New("dasha: invalid log search parameters (400) — e.g. page_token combined " +
				"with dedup, or an unknown severity value")
		case http.StatusBadGateway:
			return nil, errors.New("dasha: the Yandex Cloud logs API failed upstream (502) — retry later")
		case http.StatusGatewayTimeout:
			return nil, errors.New("dasha: log search timed out before collecting anything (504) — narrow " +
				"the time window or add severity/message filters")
		}
	}

	if r.JSON200 == nil {
		return nil, statusError("search_logs", r.HTTPResponse)
	}

	truncateLogEntries(r.JSON200)

	return r.JSON200, nil
}

// maxLogFieldBytes caps any single log field (message text, query, …) in a
// search_logs result: one multi-megabyte statement would otherwise flood the
// model's context — or trip the whole-result size cap — while its head is
// enough to identify the query.
const maxLogFieldBytes = 2000

// truncateLogEntries clips oversized field values in place, appending a marker
// with the original length so the model knows content was cut.
func truncateLogEntries(res *apiclient.LogSearchResult) {
	for i := range res.Items {
		e := &res.Items[i]
		if e.Text != nil {
			*e.Text = clip(*e.Text)
		}

		if e.Fields != nil {
			for k, v := range *e.Fields {
				(*e.Fields)[k] = clip(v)
			}
		}
	}
}

// clip cuts s at maxLogFieldBytes on a rune boundary.
func clip(s string) string {
	if len(s) <= maxLogFieldBytes {
		return s
	}

	cut := maxLogFieldBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}

	return s[:cut] + fmt.Sprintf("… [truncated, %d bytes total]", len(s))
}

// optStrings returns a pointer to the slice for optional query params, or nil
// when empty so the parameter is omitted entirely.
func optStrings(v []string) *[]string {
	if len(v) == 0 {
		return nil
	}

	return &v
}

// deref returns the pointed-to value, or T's zero value for nil — for optional
// response fields.
func deref[T any](p *T) T {
	if p == nil {
		var zero T

		return zero
	}

	return *p
}

// opt returns a pointer to v for optional query params, or nil when v is the
// zero value so the parameter is omitted entirely.
func opt[T comparable](v T) *T {
	var zero T
	if v == zero {
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
	case http.StatusTooManyRequests:
		return fmt.Errorf("dasha: rate limited (429) on %s — pause before retrying instead of calling again immediately", op)
	default:
		return fmt.Errorf("dasha: %s returned status %d", op, code)
	}
}
