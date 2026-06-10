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
	"github.com/dbulashev/dasha/internal/repository"
)

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
	queries, err := s.repo.GetQueriesBlocked(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
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
				Pid:         t.Pid,
				State:       t.State,
				Source:      t.Source,
				Duration:    t.Duration,
				Waiting:     t.Waiting,
				Query:       sanitize.SQL(t.Query),
				StartedAt:   t.StartedAt,
				DurationMs:  t.DurationMs,
				User:        t.User,
				BackendType: t.BackendType,
			}
		})

	return ret, nil
}

func (s *Handlers) GetQueriesTop10ByTime(
	ctx context.Context,
	req serverhttp.GetQueriesTop10ByTimeRequestObject,
) (serverhttp.GetQueriesTop10ByTimeResponseObject, error) {
	queries, err := s.repo.GetQueriesTop10ByTime(ctx, req.Params.ClusterName, req.Params.Instance)
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
	queries, err := s.repo.GetQueriesTop10ByWal(ctx, req.Params.ClusterName, req.Params.Instance)
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
	items, err := s.repo.GetQueriesTop10Chart(ctx, req.Params.ClusterName, req.Params.Instance)
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

func (s *Handlers) GetQueriesReport(
	ctx context.Context,
	req serverhttp.GetQueriesReportRequestObject,
) (serverhttp.GetQueriesReportResponseObject, error) {
	var excludeUsers []string
	if req.Params.ExcludeUsers != nil {
		excludeUsers = *req.Params.ExcludeUsers
	}

	queries, err := s.repo.GetQueriesReport(ctx, req.Params.ClusterName, req.Params.Instance, excludeUsers)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetQueriesReport404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetQueriesReport | %w", err)
	}

	var ret serverhttp.GetQueriesReport200JSONResponse = mapstruct.SliceMap(
		queries,
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
	if s.storage == nil {
		return serverhttp.GetQueriesCompare501Response{}, nil
	}

	// Load source A (always a snapshot).
	reportsA, err := s.storage.GetSnapshot(ctx, uuid.UUID(req.Params.SnapshotA))
	if err != nil {
		return nil, fmt.Errorf("GetQueriesCompare | snapshot A: %w", err)
	}

	if reportsA == nil {
		return serverhttp.GetQueriesCompare404Response{}, nil
	}

	var reportsB []dto.QueryReport

	if req.Params.SnapshotB != nil {
		reportsB, err = s.storage.GetSnapshot(ctx, uuid.UUID(*req.Params.SnapshotB))
		if err != nil {
			return nil, fmt.Errorf("GetQueriesCompare | snapshot B: %w", err)
		}

		if reportsB == nil {
			return serverhttp.GetQueriesCompare404Response{}, nil
		}
	} else {
		var excludeUsers []string
		if req.Params.ExcludeUsers != nil {
			excludeUsers = *req.Params.ExcludeUsers
		}

		reportsB, err = s.repo.GetQueriesReport(ctx, req.Params.ClusterName, req.Params.Instance, excludeUsers)
		if errors.Is(err, repository.ErrNotFound) {
			return serverhttp.GetQueriesCompare404Response{}, nil
		}

		if err != nil {
			return nil, fmt.Errorf("GetQueriesCompare | live report: %w", err)
		}
	}

	joined := mapstruct.SliceFullJoin(
		reportsA, reportsB,
		func(r dto.QueryReport) int64 { return r.QueryID },
		func(r dto.QueryReport) int64 { return r.QueryID },
	)

	items := make([]serverhttp.QueryCompareItem, 0, len(joined))

	for _, pair := range joined {
		var queryID int64

		var query string

		var left, right *serverhttp.QueryReportMetrics

		if pair.Left != nil {
			pair.Left.Query = sanitize.SQL(pair.Left.Query)
			queryID = pair.Left.QueryID
			query = pair.Left.Query
			m := mapQueryReportMetrics(*pair.Left)
			left = &m
		}

		if pair.Right != nil {
			pair.Right.Query = sanitize.SQL(pair.Right.Query)
			if queryID == 0 {
				queryID = pair.Right.QueryID
			}

			if query == "" {
				query = pair.Right.Query
			}

			m := mapQueryReportMetrics(*pair.Right)
			right = &m
		}

		items = append(items, serverhttp.QueryCompareItem{
			QueryID: strconv.FormatInt(queryID, 10),
			Query:   query,
			Left:    left,
			Right:   right,
		})
	}

	return serverhttp.GetQueriesCompare200JSONResponse(items), nil
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
		Available: status.Available,
		Enabled:   status.Enabled,
		Readable:  status.Readable,
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
