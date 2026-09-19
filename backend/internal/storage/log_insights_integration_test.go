//go:build integration

package storage

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/explain"
	"github.com/dbulashev/dasha/internal/health"
	"github.com/dbulashev/dasha/internal/logs"
	"github.com/dbulashev/dasha/internal/logs/insights"
	"github.com/dbulashev/dasha/internal/testinfra"
)

func newInsightsTestStorage(t *testing.T) *Storage {
	t.Helper()

	pool := testinfra.IsolateEmptyPool(t)
	ctx := t.Context()

	for _, ddl := range []string{createLogInsightsScansSQL, createLogInsightsGroupsSQL, createLogInsightsGroupsIdxSQL} {
		_, err := pool.Exec(ctx, ddl)
		require.NoError(t, err, "log insights DDL")
	}

	return &Storage{pool: pool, ddlPool: pool, logger: zap.NewNop()}
}

func insightsTestGroup(ord int, queryID int64, hasQueryID bool, sum, maxMs float64) insights.PlanGroup {
	at := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
	duration := maxMs

	return insights.PlanGroup{
		Ord:        ord,
		QueryID:    queryID,
		HasQueryID: hasQueryID,
		Hash:       "shape" + string(rune('a'+ord)),
		Count:      2,
		Durations:  insights.DurationStats{Min: 1, P50: sum / 2, P95: maxMs, Max: maxMs, Sum: sum},
		First:      at,
		Last:       at.Add(time.Minute),
		Sample: explain.Plan{
			Source:     explain.SourceLog,
			Format:     explain.FormatText,
			QueryText:  "SELECT count(*) FROM orders",
			QueryID:    queryID,
			HasQueryID: hasQueryID,
			Duration:   &duration,
			Root: explain.Node{
				Type:     "Aggregate",
				Children: []explain.Node{{Type: "Seq Scan", Relation: "orders"}},
			},
		},
		Findings: []explain.Finding{{
			Code:     explain.RuleSeqScanLarge,
			Severity: health.SeverityMedium,
			Path:     []int{0},
			NodeType: "Seq Scan",
			Relation: "orders",
			Params:   map[string]any{explain.ParamTableRows: 1e6},
		}},
	}
}

func insightsTestScan(id uuid.UUID) logs.Scan {
	from := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)

	return logs.Scan{
		ID:        id,
		Kind:      logs.ScanInsights,
		Cluster:   "prod",
		Stream:    "postgresql",
		Host:      "db-1",
		From:      from,
		To:        from.Add(15 * time.Minute),
		CreatedAt: from.Add(15 * time.Minute),
		Result: logs.ScanResult{
			ScanID:  id,
			Scanned: 4200,
			Plans:   insights.PlansSummary{Records: 12, Parsed: 12, TotalGroups: 3},
		},
	}
}

func TestInsightsScanRoundTrip(t *testing.T) {
	s := newInsightsTestStorage(t)
	ctx := t.Context()

	id := uuid.New()
	groups := []insights.PlanGroup{
		insightsTestGroup(0, 42, true, 300, 30),
		insightsTestGroup(1, 42, true, 200, 150),
		insightsTestGroup(2, 0, false, 100, 10),
	}

	require.NoError(t, s.SaveInsightsScan(ctx, insightsTestScan(id), groups))

	scan, err := s.GetInsightsScan(ctx, id, 2)
	require.NoError(t, err)

	assert.Equal(t, logs.ScanInsights, scan.Kind)
	assert.Equal(t, "prod", scan.Cluster)
	assert.Equal(t, "db-1", scan.Host)
	assert.Equal(t, 4200, scan.Result.Scanned)
	assert.Equal(t, 3, scan.Result.Plans.TotalGroups)

	// The top by total time plus the top by slowest run — the two the scan
	// itself answered with.
	require.Len(t, scan.Result.Plans.Groups, 2)
	assert.Equal(t, 0, scan.Result.Plans.Groups[0].Ord)
	assert.Equal(t, 1, scan.Result.Plans.Groups[1].Ord)
	assert.Equal(t, "Seq Scan", scan.Result.Plans.Groups[0].Sample.Root.Children[0].Type)

	one, err := s.GetInsightsScan(ctx, id, 1)
	require.NoError(t, err)
	require.Len(t, one.Result.Plans.Groups, 1)
	assert.Equal(t, 0, one.Result.Plans.Groups[0].Ord)
}

func TestInsightsGroupsPageAndFilter(t *testing.T) {
	s := newInsightsTestStorage(t)
	ctx := t.Context()

	id := uuid.New()
	groups := []insights.PlanGroup{
		insightsTestGroup(0, 42, true, 300, 30),
		insightsTestGroup(1, 77, true, 200, 150),
		insightsTestGroup(2, 0, false, 100, 10),
	}

	require.NoError(t, s.SaveInsightsScan(ctx, insightsTestScan(id), groups))

	page, err := s.ListInsightsGroups(ctx, logs.GroupsQuery{ScanID: id, Order: logs.GroupOrderSum, Limit: 2})
	require.NoError(t, err)
	assert.Equal(t, 3, page.Total)
	require.Len(t, page.Rows, 2)
	assert.Equal(t, "SELECT count(*) FROM orders", page.Rows[0].QueryText)
	assert.Len(t, page.Rows[0].Findings, 1)

	byMax, err := s.ListInsightsGroups(ctx, logs.GroupsQuery{ScanID: id, Order: logs.GroupOrderMax, Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, 1, byMax.Rows[0].Ord)

	queryID := int64(42)

	filtered, err := s.ListInsightsGroups(ctx,
		logs.GroupsQuery{ScanID: id, QueryID: &queryID, Order: logs.GroupOrderSum, Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, 1, filtered.Total)
	require.Len(t, filtered.Rows, 1)
	assert.True(t, filtered.Rows[0].HasQueryID)

	group, err := s.GetInsightsGroup(ctx, id, 2)
	require.NoError(t, err)
	assert.False(t, group.HasQueryID)
	assert.Equal(t, "Aggregate", group.Sample.Root.Type)
}

func TestInsightsAllGroupsCarryTheirIndexes(t *testing.T) {
	s := newInsightsTestStorage(t)
	ctx := t.Context()

	id := uuid.New()
	indexed := insightsTestGroup(1, 77, true, 200, 150)
	indexed.Sample.Root.Children = []explain.Node{{ //nolint:exhaustruct
		Type:      "Index Scan",
		Relation:  "orders",
		IndexName: "orders_status_idx",
	}}

	groups := []insights.PlanGroup{insightsTestGroup(0, 42, true, 300, 30), indexed}

	require.NoError(t, s.SaveInsightsScan(ctx, insightsTestScan(id), groups))

	all, err := s.AllInsightsGroups(ctx, id)
	require.NoError(t, err)
	require.Len(t, all, 2)
	assert.Equal(t, 0, all[0].Ord)
	assert.Empty(t, all[0].Indexes)

	// A comparison reads the rows alone and has no tree to look the index up in.
	assert.Equal(t, []string{"orders_status_idx"}, all[1].Indexes)

	none, err := s.AllInsightsGroups(ctx, uuid.New())
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestInsightsScanNotFound(t *testing.T) {
	s := newInsightsTestStorage(t)
	ctx := t.Context()

	_, err := s.GetInsightsScan(ctx, uuid.New(), 10)
	assert.True(t, errors.Is(err, logs.ErrNotFound))

	_, err = s.ListInsightsGroups(ctx, logs.GroupsQuery{ScanID: uuid.New(), Order: logs.GroupOrderSum, Limit: 10})
	assert.True(t, errors.Is(err, logs.ErrNotFound))

	id := uuid.New()
	require.NoError(t, s.SaveInsightsScan(ctx, insightsTestScan(id), nil))

	_, err = s.GetInsightsGroup(ctx, id, 7)
	assert.True(t, errors.Is(err, logs.ErrNotFound))
}

func TestTruncateInsightsScans(t *testing.T) {
	s := newInsightsTestStorage(t)
	ctx := t.Context()

	id := uuid.New()
	require.NoError(t, s.SaveInsightsScan(ctx, insightsTestScan(id), []insights.PlanGroup{
		insightsTestGroup(0, 42, true, 300, 30),
	}))

	require.NoError(t, s.TruncateInsightsScans(ctx))

	_, err := s.GetInsightsScan(ctx, id, 10)
	assert.True(t, errors.Is(err, logs.ErrNotFound))

	var groups int

	require.NoError(t, s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM log_insights_groups`).Scan(&groups))
	assert.Zero(t, groups)
}
