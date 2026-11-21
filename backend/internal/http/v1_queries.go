package http

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/google/uuid"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/pkg/mapstruct"
	"github.com/dbulashev/dasha/internal/pkg/sanitize"
	"github.com/dbulashev/dasha/internal/pkg/shortcut"
	"github.com/dbulashev/dasha/internal/querystats"
	"github.com/dbulashev/dasha/internal/repository"
)

const msgIncomparableSnapshots = "these two cannot be compared: one names the database of every " +
	"statement and the other was stored before per-database attribution existed, so a queryid " +
	"means something different on each side. Compare snapshots of the same generation — " +
	"live statistics always count as the newer one"

func (s *Handlers) GetPgssStatsResetTime(
	ctx context.Context,
	req serverhttp.GetPgssStatsResetTimeRequestObject,
) (serverhttp.GetPgssStatsResetTimeResponseObject, error) {
	t, err := s.repo.GetPgssStatsResetTime(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetPgssStatsResetTime404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetPgssStatsResetTime | %w", err)
	}

	if t == nil {
		return serverhttp.GetPgssStatsResetTime404Response{}, nil
	}

	return serverhttp.GetPgssStatsResetTime200JSONResponse{Time: t.Time}, nil
}

func (s *Handlers) GetQueriesBlocked(
	ctx context.Context,
	req serverhttp.GetQueriesBlockedRequestObject,
) (serverhttp.GetQueriesBlockedResponseObject, error) {
	queries, err := s.repo.GetQueriesBlocked(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database,
		querystats.ResolveScope(req.Params.Scope, req.Params.Database))
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetQueriesBlocked404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetQueriesBlocked | %w", err)
	}

	var ret serverhttp.GetQueriesBlocked200JSONResponse = mapstruct.SliceMap(
		queries,
		func(t dto.QueryBlocked) serverhttp.QueryBlocked {
			return serverhttp.QueryBlocked{
				LockedItem:                            t.LockedItem,
				BlockedPid:                            t.BlockedPid,
				BlockedDatabase:                       t.BlockedDatabase,
				BlockedUser:                           t.BlockedUser,
				BlockedQuery:                          sanitize.SQL(t.BlockedQuery),
				BlockedDuration:                       t.BlockedDuration,
				BlockedDurationMs:                     t.BlockedDurationMs,
				BlockedMode:                           t.BlockedMode,
				BlockingPid:                           t.BlockingPid,
				BlockingUser:                          t.BlockingUser,
				StateOfBlockingProcess:                t.StateOfBlockingProcess,
				CurrentOrRecentQueryInBlockingProcess: sanitize.SQL(t.CurrentOrRecentQueryInBlockingProcess),
				BlockingDuration:                      t.BlockingDuration,
				BlockingDurationMs:                    t.BlockingDurationMs,
				BlockingMode:                          t.BlockingMode,
			}
		})

	return ret, nil
}

func (s *Handlers) GetQueriesRunning(
	ctx context.Context,
	req serverhttp.GetQueriesRunningRequestObject,
) (serverhttp.GetQueriesRunningResponseObject, error) {
	minDuration := 10
	if req.Params.MinDuration != nil {
		minDuration = *req.Params.MinDuration
	}

	// Filter by query text (ILIKE / NOT ILIKE) and by exact usename. Empty values disable the filter.
	var queryFilter *string
	if req.Params.QueryFilter != nil && *req.Params.QueryFilter != "" {
		queryFilter = req.Params.QueryFilter
	}

	queryFilterMode := "like"
	if req.Params.QueryFilterMode != nil && *req.Params.QueryFilterMode == serverhttp.NotLike {
		queryFilterMode = "not_like"
	}

	var username *string
	if req.Params.Username != nil && *req.Params.Username != "" {
		username = req.Params.Username
	}

	queries, err := s.repo.GetQueriesRunning(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database, minDuration, queryFilter, queryFilterMode, username)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetQueriesRunning404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetQueriesRunning | %w", err)
	}

	var ret serverhttp.GetQueriesRunning200JSONResponse = mapstruct.SliceMap(
		queries,
		func(t dto.QueryRunning) serverhttp.QueryRunning {
			return serverhttp.QueryRunning{
				Pid:           t.Pid,
				State:         t.State,
				Source:        t.Source,
				Duration:      t.Duration,
				Waiting:       t.Waiting,
				WaitEventType: t.WaitEventType,
				WaitEvent:     t.WaitEvent,
				ClientAddr:    t.ClientAddr,
				Query:         sanitize.SQL(t.Query),
				StartedAt:     t.StartedAt,
				DurationMs:    t.DurationMs,
				User:          t.User,
				BackendType:   t.BackendType,
			}
		})

	return ret, nil
}

func (s *Handlers) GetQueriesTop10ByTime(
	ctx context.Context,
	req serverhttp.GetQueriesTop10ByTimeRequestObject,
) (serverhttp.GetQueriesTop10ByTimeResponseObject, error) {
	database := deref(req.Params.Database)

	queries, err := s.repo.GetQueriesTop10ByTime(ctx, req.Params.ClusterName, req.Params.Instance, database,
		querystats.ResolveScope(req.Params.Scope, database))
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetQueriesTop10ByTime404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetQueriesTop10ByTime | %w", err)
	}

	var ret serverhttp.GetQueriesTop10ByTime200JSONResponse = mapstruct.SliceMap(
		queries,
		func(t dto.QueryTop10ByTime) serverhttp.QueryTop10ByTime {
			return serverhttp.QueryTop10ByTime{
				QueryID:    strconv.FormatInt(t.QueryID, 10),
				Datname:    t.Datname,
				ExecTime:   t.ExecTime,
				ExecTimeMs: t.ExecTimeMs,
				IoCpuPct:   t.IoCpuPct,
				IoPct:      t.IoPct,
				CpuPct:     t.CpuPct,
				QueryTrunc: sanitize.SQL(t.QueryTrunc),
			}
		})

	return ret, nil
}

func (s *Handlers) GetQueriesTop10ByWal(
	ctx context.Context,
	req serverhttp.GetQueriesTop10ByWalRequestObject,
) (serverhttp.GetQueriesTop10ByWalResponseObject, error) {
	database := deref(req.Params.Database)

	queries, err := s.repo.GetQueriesTop10ByWal(ctx, req.Params.ClusterName, req.Params.Instance, database,
		querystats.ResolveScope(req.Params.Scope, database))
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetQueriesTop10ByWal404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetQueriesTop10ByWal | %w", err)
	}

	var ret serverhttp.GetQueriesTop10ByWal200JSONResponse = mapstruct.SliceMap(
		queries,
		func(t dto.QueryTop10ByWal) serverhttp.QueryTop10ByWal {
			return serverhttp.QueryTop10ByWal{
				QueryID:    strconv.FormatInt(t.QueryID, 10),
				Datname:    t.Datname,
				WalVolume:  t.WalVolume,
				WalBytes:   t.WalBytes,
				QueryTrunc: sanitize.SQL(t.QueryTrunc),
			}
		})

	return ret, nil
}

func (s *Handlers) GetQueriesTop10Chart(
	ctx context.Context,
	req serverhttp.GetQueriesTop10ChartRequestObject,
) (serverhttp.GetQueriesTop10ChartResponseObject, error) {
	database := deref(req.Params.Database)

	items, err := s.repo.GetQueriesTop10Chart(ctx, req.Params.ClusterName, req.Params.Instance, database,
		querystats.ResolveScope(req.Params.Scope, database))
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetQueriesTop10Chart404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetQueriesTop10Chart | %w", err)
	}

	ret := serverhttp.GetQueriesTop10Chart200JSONResponse{} //nolint:exhaustruct

	for _, item := range items {
		entry := serverhttp.QueryTop10ChartItem{
			QueryID: strconv.FormatInt(item.QueryID, 10),
			Datname: item.Datname,
			Pct:     item.Pct,
		}

		switch item.Metric {
		case "calls":
			ret.Calls = append(ret.Calls, entry)
		case "total_exec_time":
			ret.TotalExecTime = append(ret.TotalExecTime, entry)
		case "rows":
			ret.Rows = append(ret.Rows, entry)
		case "shared_blks_hit":
			ret.SharedBlksHit = append(ret.SharedBlksHit, entry)
		case "shared_blks_read":
			ret.SharedBlksRead = append(ret.SharedBlksRead, entry)
		case "shared_blks_dirtied":
			ret.SharedBlksDirtied = append(ret.SharedBlksDirtied, entry)
		case "temp_blks_read":
			ret.TempBlksRead = append(ret.TempBlksRead, entry)
		case "temp_blks_written":
			ret.TempBlksWritten = append(ret.TempBlksWritten, entry)
		case "wal_records":
			ret.WalRecords = append(ret.WalRecords, entry)
		}
	}

	return ret, nil
}

// parseQueryID: an unparseable id is treated as absent, since it only widens the
// report and a 400 would break a link for no gain.
func parseQueryID(raw *string) *int64 {
	if raw == nil || *raw == "" {
		return nil
	}

	id, err := strconv.ParseInt(*raw, 10, 64)
	if err != nil {
		return nil
	}

	return &id
}

func (s *Handlers) GetQueriesReport(
	ctx context.Context,
	req serverhttp.GetQueriesReportRequestObject,
) (serverhttp.GetQueriesReportResponseObject, error) {
	var excludeUsers []string
	if req.Params.ExcludeUsers != nil {
		excludeUsers = *req.Params.ExcludeUsers
	}

	database := deref(req.Params.Database)

	queries, err := s.repo.GetQueriesReport(ctx, req.Params.ClusterName, req.Params.Instance,
		database, excludeUsers, parseQueryID(req.Params.Queryid))
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetQueriesReport404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetQueriesReport | %w", err)
	}

	var ret serverhttp.GetQueriesReport200JSONResponse = mapstruct.SliceMap(
		querystats.ScopeReport(queries, querystats.ResolveScope(req.Params.Scope, database), database),
		func(t dto.QueryReport) serverhttp.QueryReport {
			t.Query = sanitize.SQL(t.Query)
			return mapQueryReport(t)
		})

	return ret, nil
}

func (s *Handlers) GetQueriesCompare(
	ctx context.Context,
	req serverhttp.GetQueriesCompareRequestObject,
) (serverhttp.GetQueriesCompareResponseObject, error) {
	if s.queryStats == nil {
		return serverhttp.GetQueriesCompare501Response{}, nil
	}

	creq := querystats.CompareRequest{
		SnapshotA: uuid.UUID(req.Params.SnapshotA),
		Cluster:   req.Params.ClusterName,
		Instance:  req.Params.Instance,
		Database:  req.Params.Database,
		Scope:     req.Params.Scope,
	}

	if req.Params.SnapshotB != nil {
		b := uuid.UUID(*req.Params.SnapshotB)
		creq.SnapshotB = &b
	}

	if req.Params.ExcludeUsers != nil {
		creq.ExcludeUsers = *req.Params.ExcludeUsers
	}

	pairs, err := s.queryStats.Compare(ctx, creq)
	switch {
	case errors.Is(err, querystats.ErrSnapshotNotFound), errors.Is(err, repository.ErrNotFound):
		return serverhttp.GetQueriesCompare404Response{}, nil
	case errors.Is(err, querystats.ErrIncomparable):
		return serverhttp.GetQueriesCompare409JSONResponse{Message: msgIncomparableSnapshots}, nil
	case err != nil:
		return nil, fmt.Errorf("GetQueriesCompare | %w", err)
	}

	items := make([]serverhttp.QueryCompareItem, 0, len(pairs))

	for _, p := range pairs {
		items = append(items, serverhttp.QueryCompareItem{
			QueryID: strconv.FormatInt(p.Key.QueryID, 10),
			Datname: shortcut.Ptr(p.Key.Datname),
			Query:   sanitize.SQL(p.Query),
			Left:    compareSide(p.Left),
			Right:   compareSide(p.Right),
		})
	}

	return serverhttp.GetQueriesCompare200JSONResponse(items), nil
}

func compareSide(r *dto.QueryReport) *serverhttp.QueryReportMetrics {
	if r == nil {
		return nil
	}

	m := mapQueryReportMetrics(*r)

	return &m
}

func (s *Handlers) GetQueryStatsStatus(
	ctx context.Context,
	req serverhttp.GetQueryStatsStatusRequestObject,
) (serverhttp.GetQueryStatsStatusResponseObject, error) {
	status, err := s.repo.GetQueryStatsStatus(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetQueryStatsStatus404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetQueryStatsStatus | %w", err)
	}

	ret := serverhttp.GetQueryStatsStatus200JSONResponse{
		Available:  status.Available,
		Enabled:    status.Enabled,
		Readable:   status.Readable,
		Restricted: status.Restricted,
		Source:     status.Source,
	}

	return ret, nil
}

func (s *Handlers) PostQueriesResetStats(
	ctx context.Context,
	req serverhttp.PostQueriesResetStatsRequestObject,
) (serverhttp.PostQueriesResetStatsResponseObject, error) {
	if !s.cfg.EnableQueryStatsReset {
		return serverhttp.PostQueriesResetStats403Response{}, nil
	}

	err := s.repo.ResetQueryStats(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.PostQueriesResetStats404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("PostQueriesResetStats | %w", err)
	}

	return serverhttp.PostQueriesResetStats204Response{}, nil
}
