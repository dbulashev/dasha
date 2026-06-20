package http

import (
	"context"
	"errors"
	"fmt"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/health"
	"github.com/dbulashev/dasha/internal/pkg/sanitize"
	"github.com/dbulashev/dasha/internal/repository"
)

func (s *Handlers) GetHealthScoreDatabases(
	ctx context.Context,
	req serverhttp.GetHealthScoreDatabasesRequestObject,
) (serverhttp.GetHealthScoreDatabasesResponseObject, error) {
	metrics, err := s.repo.GetHealthScorePerDatabase(ctx, req.Params.ClusterName, req.Params.Instance)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetHealthScoreDatabases404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetHealthScoreDatabases | %w", err)
	}

	weights, err := s.loadHealthWeights(ctx, req.Params.ClusterName)
	if err != nil {
		return nil, fmt.Errorf("GetHealthScoreDatabases | loadHealthWeights | %w", err)
	}

	// Mirror the instance-level behaviour: drop maintenance per-DB on
	// standbys, where autovacuum / ANALYZE do not run.
	info, err := s.repo.GetInstanceInfo(ctx, req.Params.ClusterName, req.Params.Instance)
	if err != nil {
		return nil, fmt.Errorf("GetHealthScoreDatabases | GetInstanceInfo | %w", err)
	}

	per := make([]health.PerDBMetrics, 0, len(metrics))
	for _, m := range metrics {
		per = append(per, health.PerDBMetrics{
			Database:                 m.Database,
			SizeBytes:                m.SizeBytes,
			CacheHitRatio:            m.CacheHitRatio,
			MaxDeadRatio:             m.MaxDeadRatio,
			AvgDeadRatio:             m.AvgDeadRatio,
			TablesHighBloat:          m.TablesHighBloat,
			MaxXidAge:                m.MaxXidAge,
			VacuumBacklogTables:      m.VacuumBacklogTables,
			MaxOverdueVacuumAgeHours: m.MaxOverdueVacuumAgeHours,
			TablesNeverVacuumed:      m.TablesNeverVacuumed,
		})
	}

	scores := health.ComputePerDB(per, weights, info.InRecovery)

	databases := make([]serverhttp.HealthScoreDatabase, 0, len(scores))
	for _, ds := range scores {
		cats := make([]serverhttp.HealthScoreCategory, 0, len(ds.Categories))
		for _, c := range ds.Categories {
			cats = append(cats, serverhttp.HealthScoreCategory{
				Name:    string(c.Name),
				Score:   c.Score,
				Weight:  c.Weight,
				Penalty: c.Penalty,
				Details: c.Details,
			})
		}

		databases = append(databases, serverhttp.HealthScoreDatabase{
			Database:   ds.Database,
			SizeBytes:  ds.SizeBytes,
			Score:      ds.Score,
			Categories: cats,
		})
	}

	var worst *string
	if w := health.WorstDatabase(scores); w != nil {
		name := w.Database
		worst = &name
	}

	return serverhttp.GetHealthScoreDatabases200JSONResponse{
		Databases:            databases,
		ApplicableCategories: health.CategoryStrings(health.PerDBApplicableCategories),
		WorstDatabase:        worst,
	}, nil
}

// defaultHealthScoreDetailLimit is the page size for the inline recommendation
// detail tables; it matches the frontend DEFAULT_PAGE_SIZE so has-more paging
// over the full result set works (the lists can be long, e.g. every table with
// a high dead-tuple ratio).
const defaultHealthScoreDetailLimit = 15

func (s *Handlers) GetHealthScoreXidWraparoundDatabases(
	ctx context.Context,
	req serverhttp.GetHealthScoreXidWraparoundDatabasesRequestObject,
) (serverhttp.GetHealthScoreXidWraparoundDatabasesResponseObject, error) {
	limit, offset := paginationDefaults(req.Params.Limit, req.Params.Offset, defaultHealthScoreDetailLimit)

	rows, err := s.repo.GetHealthScoreXidWraparoundDatabases(ctx, req.Params.ClusterName, req.Params.Instance, limit, offset)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetHealthScoreXidWraparoundDatabases404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetHealthScoreXidWraparoundDatabases | %w", err)
	}

	out := make(serverhttp.GetHealthScoreXidWraparoundDatabases200JSONResponse, 0, len(rows))
	for _, r := range rows {
		out = append(out, serverhttp.HealthScoreXidWraparoundDatabase{
			Database: r.Database,
			XidAge:   r.XidAge,
		})
	}

	return out, nil
}

func (s *Handlers) GetHealthScoreTablesAutovacuumOff(
	ctx context.Context,
	req serverhttp.GetHealthScoreTablesAutovacuumOffRequestObject,
) (serverhttp.GetHealthScoreTablesAutovacuumOffResponseObject, error) {
	limit, offset := paginationDefaults(req.Params.Limit, req.Params.Offset, defaultHealthScoreDetailLimit)

	rows, err := s.repo.GetHealthScoreTablesAutovacuumOff(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database, limit, offset)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetHealthScoreTablesAutovacuumOff404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetHealthScoreTablesAutovacuumOff | %w", err)
	}

	out := make(serverhttp.GetHealthScoreTablesAutovacuumOff200JSONResponse, 0, len(rows))
	for _, r := range rows {
		out = append(out, serverhttp.HealthScoreTableReloption{
			Schema:     r.Schema,
			Table:      r.Table,
			RelOptions: r.RelOptions,
		})
	}

	return out, nil
}

func (s *Handlers) GetHealthScoreLowHotUpdateTables(
	ctx context.Context,
	req serverhttp.GetHealthScoreLowHotUpdateTablesRequestObject,
) (serverhttp.GetHealthScoreLowHotUpdateTablesResponseObject, error) {
	limit, offset := paginationDefaults(req.Params.Limit, req.Params.Offset, defaultHealthScoreDetailLimit)

	rows, err := s.repo.GetHealthScoreLowHotUpdateTables(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database, limit, offset)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetHealthScoreLowHotUpdateTables404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetHealthScoreLowHotUpdateTables | %w", err)
	}

	out := make(serverhttp.GetHealthScoreLowHotUpdateTables200JSONResponse, 0, len(rows))
	for _, r := range rows {
		out = append(out, serverhttp.HealthScoreLowHotUpdateTable{
			Schema:     r.Schema,
			Table:      r.Table,
			Updates:    r.Updates,
			HotUpdates: r.HotUpdates,
			HotRatio:   r.HotRatio,
		})
	}

	return out, nil
}

func (s *Handlers) GetHealthScoreHighDeadRatioTables(
	ctx context.Context,
	req serverhttp.GetHealthScoreHighDeadRatioTablesRequestObject,
) (serverhttp.GetHealthScoreHighDeadRatioTablesResponseObject, error) {
	limit, offset := paginationDefaults(req.Params.Limit, req.Params.Offset, defaultHealthScoreDetailLimit)

	rows, err := s.repo.GetHealthScoreHighDeadRatioTables(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database, limit, offset)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetHealthScoreHighDeadRatioTables404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetHealthScoreHighDeadRatioTables | %w", err)
	}

	out := make(serverhttp.GetHealthScoreHighDeadRatioTables200JSONResponse, 0, len(rows))
	for _, r := range rows {
		out = append(out, serverhttp.HealthScoreHighDeadRatioTable{
			Schema:     r.Schema,
			Table:      r.Table,
			LiveTuples: r.LiveTuples,
			DeadTuples: r.DeadTuples,
			DeadRatio:  r.DeadRatio,
		})
	}

	return out, nil
}

func (s *Handlers) GetHealthScoreHorizonBlockingSessions(
	ctx context.Context,
	req serverhttp.GetHealthScoreHorizonBlockingSessionsRequestObject,
) (serverhttp.GetHealthScoreHorizonBlockingSessionsResponseObject, error) {
	limit, offset := paginationDefaults(req.Params.Limit, req.Params.Offset, defaultHealthScoreDetailLimit)

	rows, err := s.repo.GetHealthScoreHorizonBlockingSessions(ctx, req.Params.ClusterName, req.Params.Instance, limit, offset)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetHealthScoreHorizonBlockingSessions404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetHealthScoreHorizonBlockingSessions | %w", err)
	}

	out := make(serverhttp.GetHealthScoreHorizonBlockingSessions200JSONResponse, 0, len(rows))
	for _, r := range rows {
		out = append(out, serverhttp.HealthScoreHorizonBlockingSession{
			PID:                 r.PID,
			Username:            r.Username,
			State:               r.State,
			WaitEventType:       r.WaitEventType,
			WaitEvent:           r.WaitEvent,
			XactDurationSeconds: r.XactDurationSeconds,
			BackendXmin:         r.BackendXmin,
			Query:               sanitize.SQL(r.Query),
		})
	}

	return out, nil
}
