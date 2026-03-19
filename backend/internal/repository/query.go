package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/enums"
	"github.com/dbulashev/dasha/internal/query"
)

func (p *PgxPool) GetQueriesBlocked(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.QueryBlocked, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetQueriesBlocked | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getQueriesBlocked(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getQueriesBlocked | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetQueriesRunning(ctx context.Context, clusterName, instanceName, databaseName string, minDuration int) ([]dto.QueryRunning, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetQueriesRunning | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getQueriesRunning(ctx, vNum, pool, minDuration)
	if err != nil {
		return nil, fmt.Errorf("getQueriesRunning | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetQueriesTop10ByTime(ctx context.Context, clusterName, instanceName string) ([]dto.QueryTop10ByTime, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return nil, fmt.Errorf("GetQueriesTop10ByTime | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	if readable, _ := p.getQueryStatsReadable(ctx, vNum, pool); !readable {
		return nil, nil
	}

	ret, err := p.getQueriesTop10ByTime(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getQueriesTop10ByTime | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetQueriesTop10ByWal(ctx context.Context, clusterName, instanceName string) ([]dto.QueryTop10ByWal, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return nil, fmt.Errorf("GetQueriesTop10ByWal | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	if readable, _ := p.getQueryStatsReadable(ctx, vNum, pool); !readable {
		return nil, nil
	}

	ret, err := p.getQueriesTop10ByWal(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getQueriesTop10ByWal | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetQueriesReport(ctx context.Context, clusterName, instanceName string) ([]dto.QueryReport, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return nil, fmt.Errorf("GetQueriesReport | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	if readable, _ := p.getQueryStatsReadable(ctx, vNum, pool); !readable {
		return nil, nil
	}

	ret, err := p.getQueriesReport(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getQueriesReport | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getQueriesBlocked(ctx context.Context, serverVersion int, pool *pgxpool.Pool) ([]dto.QueryBlocked, error) {
	qStr, err := query.Get(serverVersion, enums.QueryQueriesBlocked, nil)
	if err != nil {
		return nil, fmt.Errorf("getQueriesBlocked | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getQueriesBlocked | %w", err)
	}

	ret := make([]dto.QueryBlocked, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			lockedItem, blockedUser, blockedQuery, blockedDuration, blockedMode                        string
			blockingUser, stateOfBlockingProcess, currentOrRecentQuery, blockingDuration, blockingMode string
			blockedPid, blockingPid                                                                    int32
		)

		err = rows.Scan(&lockedItem, &blockedPid, &blockedUser, &blockedQuery, &blockedDuration,
			&blockedMode, &blockingPid, &blockingUser, &stateOfBlockingProcess,
			&currentOrRecentQuery, &blockingDuration, &blockingMode)
		if err != nil {
			return nil, fmt.Errorf("getQueriesBlocked | %w", err)
		}

		ret = append(ret, dto.QueryBlocked{
			LockedItem:                            lockedItem,
			BlockedPid:                            blockedPid,
			BlockedUser:                           blockedUser,
			BlockedQuery:                          blockedQuery,
			BlockedDuration:                       blockedDuration,
			BlockedMode:                           blockedMode,
			BlockingPid:                           blockingPid,
			BlockingUser:                          blockingUser,
			StateOfBlockingProcess:                stateOfBlockingProcess,
			CurrentOrRecentQueryInBlockingProcess: currentOrRecentQuery,
			BlockingDuration:                      blockingDuration,
			BlockingMode:                          blockingMode,
		})
	}

	return ret, nil
}

func (p *PgxPool) getQueriesRunning(ctx context.Context, serverVersion int, pool *pgxpool.Pool, minDuration int) ([]dto.QueryRunning, error) {
	qStr, err := query.Get(serverVersion, enums.QueryQueriesRunning, struct{ MinDuration int }{MinDuration: minDuration})
	if err != nil {
		return nil, fmt.Errorf("getQueriesRunning | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getQueriesRunning | %w", err)
	}

	ret := make([]dto.QueryRunning, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			pid                                                  int32
			state, source, duration, queryStr, user, backendType string
			waiting                                              bool
			startedAt                                            time.Time
			durationMs                                           float64
		)

		err = rows.Scan(&pid, &state, &source, &duration, &waiting, &queryStr,
			&startedAt, &durationMs, &user, &backendType)
		if err != nil {
			return nil, fmt.Errorf("getQueriesRunning | %w", err)
		}

		ret = append(ret, dto.QueryRunning{
			Pid:         pid,
			State:       state,
			Source:      source,
			Duration:    duration,
			Waiting:     waiting,
			Query:       queryStr,
			StartedAt:   startedAt,
			DurationMs:  durationMs,
			User:        user,
			BackendType: backendType,
		})
	}

	return ret, nil
}

func (p *PgxPool) getQueriesTop10ByTime(ctx context.Context, serverVersion int, pool *pgxpool.Pool) ([]dto.QueryTop10ByTime, error) {
	qStr, err := query.Get(serverVersion, enums.QueryQueriesTop10ByTime, nil)
	if err != nil {
		return nil, fmt.Errorf("getQueriesTop10ByTime | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getQueriesTop10ByTime | %w", err)
	}

	ret := make([]dto.QueryTop10ByTime, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			queryID                        int64
			execTime, ioCpuPct, queryTrunc string
			execTimeMs, ioPct, cpuPct      float64
		)

		err = rows.Scan(&queryID, &execTime, &execTimeMs, &ioCpuPct, &ioPct, &cpuPct, &queryTrunc)
		if err != nil {
			return nil, fmt.Errorf("getQueriesTop10ByTime | %w", err)
		}

		ret = append(ret, dto.QueryTop10ByTime{
			QueryID:    queryID,
			ExecTime:   execTime,
			ExecTimeMs: execTimeMs,
			IoCpuPct:   ioCpuPct,
			IoPct:      ioPct,
			CpuPct:     cpuPct,
			QueryTrunc: queryTrunc,
		})
	}

	return ret, nil
}

func (p *PgxPool) getQueriesTop10ByWal(ctx context.Context, serverVersion int, pool *pgxpool.Pool) ([]dto.QueryTop10ByWal, error) {
	qStr, err := query.Get(serverVersion, enums.QueryQueriesTop10ByWal, nil)
	if err != nil {
		return nil, fmt.Errorf("getQueriesTop10ByWal | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getQueriesTop10ByWal | %w", err)
	}

	ret := make([]dto.QueryTop10ByWal, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			queryID               int64
			walVolume, queryTrunc string
			walBytes              int64
		)

		err = rows.Scan(&queryID, &walVolume, &walBytes, &queryTrunc)
		if err != nil {
			return nil, fmt.Errorf("getQueriesTop10ByWal | %w", err)
		}

		ret = append(ret, dto.QueryTop10ByWal{
			QueryID:    queryID,
			WalVolume:  walVolume,
			WalBytes:   walBytes,
			QueryTrunc: queryTrunc,
		})
	}

	return ret, nil
}

func (p *PgxPool) getQueriesReport( //nolint:gocyclo
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
) ([]dto.QueryReport, error) {
	qStr, err := query.Get(serverVersion, enums.QueryQueriesReport, nil)
	if err != nil {
		return nil, fmt.Errorf("getQueriesReport | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getQueriesReport | %w", err)
	}

	ret := make([]dto.QueryReport, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			queryID                                                  int64
			queryText                                                pgtype.Text
			rowsVal, calls                                           pgtype.Int8
			rowsPct, callsPct                                        pgtype.Float8
			totalTimeMs, totalTimePct                                pgtype.Float8
			execTimeMs, minExecTimeMs, maxExecTimeMs, meanExecTimeMs pgtype.Float8
			planTimeMs, minPlanTimeMs, maxPlanTimeMs, meanPlanTimeMs pgtype.Float8
			ioTimeMs, ioTimePct                                      pgtype.Float8
			cpuTimeMs, cpuTimePct                                    pgtype.Float8
			cacheHitRatio                                            pgtype.Float8
			sharedBlksDirtiedPct, sharedBlksWrittenPct               pgtype.Float8
			walBytes                                                 pgtype.Int8
			walBytesPct                                              pgtype.Float8
			walRecords, walFpi                                       pgtype.Int8
			tempBlks                                                 pgtype.Int8
			tempBlksPct                                              pgtype.Float8
		)

		err = rows.Scan(
			&queryID, &queryText,
			&rowsVal, &rowsPct,
			&calls, &callsPct,
			&totalTimeMs, &totalTimePct,
			&execTimeMs, &minExecTimeMs, &maxExecTimeMs, &meanExecTimeMs,
			&planTimeMs, &minPlanTimeMs, &maxPlanTimeMs, &meanPlanTimeMs,
			&ioTimeMs, &ioTimePct,
			&cpuTimeMs, &cpuTimePct,
			&cacheHitRatio,
			&sharedBlksDirtiedPct, &sharedBlksWrittenPct,
			&walBytes, &walBytesPct, &walRecords, &walFpi,
			&tempBlks, &tempBlksPct,
		)
		if err != nil {
			return nil, fmt.Errorf("getQueriesReport | %w", err)
		}

		r := dto.QueryReport{QueryID: queryID, Query: queryText.String} //nolint: exhaustruct
		if rowsVal.Valid {
			r.Rows = &rowsVal.Int64
		}

		if rowsPct.Valid {
			r.RowsPct = &rowsPct.Float64
		}

		if calls.Valid {
			r.Calls = &calls.Int64
		}

		if callsPct.Valid {
			r.CallsPct = &callsPct.Float64
		}

		if totalTimeMs.Valid {
			r.TotalTimeMs = &totalTimeMs.Float64
		}

		if totalTimePct.Valid {
			r.TotalTimePct = &totalTimePct.Float64
		}

		if execTimeMs.Valid {
			r.ExecTimeMs = &execTimeMs.Float64
		}

		if minExecTimeMs.Valid {
			r.MinExecTimeMs = &minExecTimeMs.Float64
		}

		if maxExecTimeMs.Valid {
			r.MaxExecTimeMs = &maxExecTimeMs.Float64
		}

		if meanExecTimeMs.Valid {
			r.MeanExecTimeMs = &meanExecTimeMs.Float64
		}

		if planTimeMs.Valid {
			r.PlanTimeMs = &planTimeMs.Float64
		}

		if minPlanTimeMs.Valid {
			r.MinPlanTimeMs = &minPlanTimeMs.Float64
		}

		if maxPlanTimeMs.Valid {
			r.MaxPlanTimeMs = &maxPlanTimeMs.Float64
		}

		if meanPlanTimeMs.Valid {
			r.MeanPlanTimeMs = &meanPlanTimeMs.Float64
		}

		if ioTimeMs.Valid {
			r.IoTimeMs = &ioTimeMs.Float64
		}

		if ioTimePct.Valid {
			r.IoTimePct = &ioTimePct.Float64
		}

		if cpuTimeMs.Valid {
			r.CpuTimeMs = &cpuTimeMs.Float64
		}

		if cpuTimePct.Valid {
			r.CpuTimePct = &cpuTimePct.Float64
		}

		if cacheHitRatio.Valid {
			r.CacheHitRatio = &cacheHitRatio.Float64
		}

		if sharedBlksDirtiedPct.Valid {
			r.SharedBlksDirtiedPct = &sharedBlksDirtiedPct.Float64
		}

		if sharedBlksWrittenPct.Valid {
			r.SharedBlksWrittenPct = &sharedBlksWrittenPct.Float64
		}

		if walBytes.Valid {
			r.WalBytes = &walBytes.Int64
		}

		if walBytesPct.Valid {
			r.WalBytesPct = &walBytesPct.Float64
		}

		if walRecords.Valid {
			r.WalRecords = &walRecords.Int64
		}

		if walFpi.Valid {
			r.WalFpi = &walFpi.Int64
		}

		if tempBlks.Valid {
			r.TempBlks = &tempBlks.Int64
		}

		if tempBlksPct.Valid {
			r.TempBlksPct = &tempBlksPct.Float64
		}

		ret = append(ret, r)
	}

	return ret, nil
}
