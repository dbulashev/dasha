package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dbulashev/dasha/internal/explain"
	"github.com/dbulashev/dasha/internal/logs"
	"github.com/dbulashev/dasha/internal/logs/insights"
)

var insightsGroupColumns = []string{
	"scan_id", "ord", "query_id", "hash", "count", "sum_ms", "max_ms", "group_row", "plan",
}

// SaveInsightsScan stores one scan with every group it found: the response
// carries the top of the ranking, the snapshot carries the tail as well.
func (s *Storage) SaveInsightsScan(ctx context.Context, scan logs.Scan, groups []insights.PlanGroup) error {
	summary, err := json.Marshal(scan.Result)
	if err != nil {
		return fmt.Errorf("storage: marshal insights summary: %w", err)
	}

	rows := make([][]any, 0, len(groups))

	for _, g := range groups {
		row, err := json.Marshal(g.Row())
		if err != nil {
			return fmt.Errorf("storage: marshal plan group: %w", err)
		}

		plan, err := json.Marshal(g.Sample)
		if err != nil {
			return fmt.Errorf("storage: marshal plan: %w", err)
		}

		rows = append(rows, []any{
			scan.ID, g.Ord, insightsQueryID(g), g.Hash, g.Count,
			g.Durations.Sum, g.Durations.Max, string(row), string(plan),
		})
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin insights scan: %w", err)
	}

	defer tx.Rollback(ctx) //nolint:errcheck

	_, err = tx.Exec(ctx, `
		INSERT INTO log_insights_scans
		    (scan_id, created_at, kind, cluster_name, stream, host, window_from, window_to, summary)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)`,
		scan.ID, scan.CreatedAt, scan.Kind, scan.Cluster, scan.Stream,
		nullIfEmpty(scan.Host), scan.From, scan.To, jsonbArg(summary))
	if err != nil {
		return fmt.Errorf("storage: insert insights scan: %w", err)
	}

	_, err = tx.CopyFrom(ctx, pgx.Identifier{"log_insights_groups"}, insightsGroupColumns, pgx.CopyFromRows(rows))
	if err != nil {
		return fmt.Errorf("storage: copy insights groups: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit insights scan: %w", err)
	}

	return nil
}

// GetInsightsScan returns the stored summary with the same groups the scan
// answered with: at most top of them, taken by total time and by slowest run in
// turn.
func (s *Storage) GetInsightsScan(ctx context.Context, id uuid.UUID, top int) (logs.Scan, error) {
	scan := logs.Scan{ID: id} //nolint:exhaustruct

	var summary []byte

	err := s.pool.QueryRow(ctx, `
		SELECT kind, cluster_name, stream, COALESCE(host, ''), window_from, window_to, created_at, summary
		FROM log_insights_scans
		WHERE scan_id = $1`, id).
		Scan(&scan.Kind, &scan.Cluster, &scan.Stream, &scan.Host,
			&scan.From, &scan.To, &scan.CreatedAt, &summary)

	if errors.Is(err, pgx.ErrNoRows) {
		return logs.Scan{}, logs.ErrNotFound
	}

	if err != nil {
		return logs.Scan{}, fmt.Errorf("storage: get insights scan: %w", err)
	}

	if err := json.Unmarshal(summary, &scan.Result); err != nil {
		return logs.Scan{}, fmt.Errorf("storage: unmarshal insights summary: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
		WITH ranked AS (
		    SELECT ord,
		           row_number() OVER (ORDER BY ord) AS by_sum,
		           row_number() OVER (ORDER BY max_ms DESC, ord) AS by_max
		    FROM log_insights_groups
		    WHERE scan_id = $1
		),
		picked AS (
		    SELECT ord FROM ranked
		    ORDER BY least(by_sum, by_max), by_sum
		    LIMIT $2
		)
		SELECT group_row, plan
		FROM log_insights_groups
		WHERE scan_id = $1 AND ord IN (SELECT ord FROM picked)
		ORDER BY ord`, id, top)
	if err != nil {
		return logs.Scan{}, fmt.Errorf("storage: get insights scan groups: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var row, plan []byte

		if err := rows.Scan(&row, &plan); err != nil {
			return logs.Scan{}, fmt.Errorf("storage: scan insights group: %w", err)
		}

		g, err := insightsGroup(row, plan)
		if err != nil {
			return logs.Scan{}, err
		}

		scan.Result.Plans.Groups = append(scan.Result.Plans.Groups, g)
	}

	if err := rows.Err(); err != nil {
		return logs.Scan{}, fmt.Errorf("storage: read insights scan groups: %w", err)
	}

	return scan, nil
}

// ListInsightsGroups pages the groups of a scan without their plan trees.
func (s *Storage) ListInsightsGroups(ctx context.Context, q logs.GroupsQuery) (logs.GroupPage, error) {
	var (
		page   logs.GroupPage
		exists bool
	)

	err := s.pool.QueryRow(ctx, `
		SELECT
		    (SELECT COUNT(*) FROM log_insights_groups
		     WHERE scan_id = $1 AND ($2::bigint IS NULL OR query_id = $2)
		       AND `+insightsFindingsFilter+`),
		    EXISTS (SELECT 1 FROM log_insights_scans WHERE scan_id = $1)`,
		q.ScanID, q.QueryID, q.WithFindings).Scan(&page.Total, &exists)
	if err != nil {
		return logs.GroupPage{}, fmt.Errorf("storage: count insights groups: %w", err)
	}

	if !exists {
		return logs.GroupPage{}, logs.ErrNotFound
	}

	rows, err := s.pool.Query(ctx, `
		SELECT group_row
		FROM log_insights_groups
		WHERE scan_id = $1 AND ($2::bigint IS NULL OR query_id = $2)
		  AND `+insightsFindingsFilter+`
		ORDER BY `+insightsGroupOrder(q.Order)+`
		LIMIT $4 OFFSET $5`,
		q.ScanID, q.QueryID, q.WithFindings, q.Limit, q.Offset)
	if err != nil {
		return logs.GroupPage{}, fmt.Errorf("storage: list insights groups: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var row []byte

		if err := rows.Scan(&row); err != nil {
			return logs.GroupPage{}, fmt.Errorf("storage: scan insights group row: %w", err)
		}

		var gr insights.GroupRow
		if err := json.Unmarshal(row, &gr); err != nil {
			return logs.GroupPage{}, fmt.Errorf("storage: unmarshal plan group: %w", err)
		}

		page.Rows = append(page.Rows, gr)
	}

	if err := rows.Err(); err != nil {
		return logs.GroupPage{}, fmt.Errorf("storage: read insights groups: %w", err)
	}

	return page, nil
}

// AllInsightsGroups returns every group of a scan without its plan tree, ranked
// by total time.
func (s *Storage) AllInsightsGroups(ctx context.Context, id uuid.UUID) ([]insights.GroupRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT group_row
		FROM log_insights_groups
		WHERE scan_id = $1
		ORDER BY ord`, id)
	if err != nil {
		return nil, fmt.Errorf("storage: list all insights groups: %w", err)
	}
	defer rows.Close()

	var out []insights.GroupRow

	for rows.Next() {
		var row []byte

		if err := rows.Scan(&row); err != nil {
			return nil, fmt.Errorf("storage: scan insights group row: %w", err)
		}

		var gr insights.GroupRow
		if err := json.Unmarshal(row, &gr); err != nil {
			return nil, fmt.Errorf("storage: unmarshal plan group: %w", err)
		}

		out = append(out, gr)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: read insights groups: %w", err)
	}

	return out, nil
}

// GetInsightsGroup returns one group of a scan with its plan tree.
func (s *Storage) GetInsightsGroup(ctx context.Context, id uuid.UUID, ord int) (insights.PlanGroup, error) {
	var row, plan []byte

	err := s.pool.QueryRow(ctx, `
		SELECT group_row, plan
		FROM log_insights_groups
		WHERE scan_id = $1 AND ord = $2`, id, ord).Scan(&row, &plan)

	if errors.Is(err, pgx.ErrNoRows) {
		return insights.PlanGroup{}, logs.ErrNotFound
	}

	if err != nil {
		return insights.PlanGroup{}, fmt.Errorf("storage: get insights group: %w", err)
	}

	return insightsGroup(row, plan)
}

// TruncateInsightsScans drops every stored scan. Runs as the DDL role: TRUNCATE
// needs a privilege the read-write role of a hardened install does not hold.
// Table order follows SaveInsightsScan: the reverse deadlocks against a
// concurrent save.
func (s *Storage) TruncateInsightsScans(ctx context.Context) error {
	if _, err := s.ddlPool.Exec(ctx, `TRUNCATE log_insights_scans, log_insights_groups`); err != nil {
		return fmt.Errorf("storage: truncate insights scans: %w", err)
	}

	return nil
}

func insightsGroup(row, plan []byte) (insights.PlanGroup, error) {
	var gr insights.GroupRow
	if err := json.Unmarshal(row, &gr); err != nil {
		return insights.PlanGroup{}, fmt.Errorf("storage: unmarshal plan group: %w", err)
	}

	var p explain.Plan
	if err := json.Unmarshal(plan, &p); err != nil {
		return insights.PlanGroup{}, fmt.Errorf("storage: unmarshal plan: %w", err)
	}

	return gr.WithPlan(p), nil
}

func insightsQueryID(g insights.PlanGroup) any {
	if !g.HasQueryID {
		return nil
	}

	return g.QueryID
}

// insightsFindingsFilter keeps every group unless the request asked for the ones
// a rule fired on. A group without findings stores them as json null, and the
// length is taken inside a CASE: the order of AND is not guaranteed, so a guard
// beside the call would not stop it from running on a scalar.
const insightsFindingsFilter = `(NOT $3::bool
    OR jsonb_array_length(
        CASE jsonb_typeof(group_row->'Findings')
            WHEN 'array' THEN group_row->'Findings'
            ELSE '[]'::jsonb
        END) > 0)`

// insightsGroupOrder maps the validated order of a request to SQL; ord is the
// rank by total time, so sum needs no second column.
func insightsGroupOrder(order string) string {
	switch order {
	case logs.GroupOrderMax:
		return "max_ms DESC, ord"
	case logs.GroupOrderCount:
		return "count DESC, ord"
	default:
		return "ord"
	}
}
