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

// GetQueriesBlocked lists blocked/blocking pairs. pg_locks and pg_stat_activity
// are instance-wide, so scope only decides whether the result is narrowed to the
// connected database; object names, however, resolve through the catalog of the
// database the pool connects to — entries of other databases carry a relation
// OID instead of a name.
func (p *PgxPool) GetQueriesBlocked(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName,
	scope string,
) ([]dto.QueryBlocked, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetQueriesBlocked | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getQueriesBlocked(ctx, vNum, pool, databaseFilter(databaseName, scope))
	if err != nil {
		return nil, fmt.Errorf("getQueriesBlocked | %w", err)
	}

	return ret, nil
}

// databaseFilter turns a scope into the nullable SQL parameter the templates
// take: NULL keeps the answer instance-wide.
func databaseFilter(databaseName, scope string) *string {
	if scope == dto.ScopeInstance || databaseName == "" {
		return nil
	}

	return &databaseName
}

func (p *PgxPool) GetQueriesRunning(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
	minDuration int,
	queryFilter *string,
	queryFilterMode string,
	username *string,
) ([]dto.QueryRunning, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetQueriesRunning | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getQueriesRunning(ctx, vNum, pool, minDuration, queryFilter, queryFilterMode, username)
	if err != nil {
		return nil, fmt.Errorf("getQueriesRunning | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetQueriesTop10ByTime(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName,
	scope string,
) ([]dto.QueryTop10ByTime, error) {
	pool, err := p.pgssPool(ctx, clusterName, instanceName, databaseName)
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

	ret, err := p.getQueriesTop10ByTime(ctx, vNum, pool, databaseFilter(databaseName, scope))
	if err != nil {
		return nil, fmt.Errorf("getQueriesTop10ByTime | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetQueriesTop10ByWal(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName,
	scope string,
) ([]dto.QueryTop10ByWal, error) {
	pool, err := p.pgssPool(ctx, clusterName, instanceName, databaseName)
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

	ret, err := p.getQueriesTop10ByWal(ctx, vNum, pool, databaseFilter(databaseName, scope))
	if err != nil {
		return nil, fmt.Errorf("getQueriesTop10ByWal | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetQueriesTop10Chart(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName,
	scope string,
) ([]dto.QueryTop10ChartItem, error) {
	pool, err := p.pgssPool(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetQueriesTop10Chart | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	if readable, _ := p.getQueryStatsReadable(ctx, vNum, pool); !readable {
		return nil, nil
	}

	ret, err := p.getQueriesTop10Chart(ctx, vNum, pool, databaseFilter(databaseName, scope))
	if err != nil {
		return nil, fmt.Errorf("getQueriesTop10Chart | %w", err)
	}

	return ret, nil
}

// GetQueriesReport returns the report of the whole instance, every row tagged
// with its database and ranked within it. Narrowing to one database is the HTTP
// layer's job: the same rows are what a snapshot stores, and a snapshot must
// stay usable after the user switches database.
func (p *PgxPool) GetQueriesReport(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
	excludeUsers []string,
) ([]dto.QueryReport, error) {
	pool, err := p.pgssPool(ctx, clusterName, instanceName, databaseName)
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

	if excludeUsers == nil {
		excludeUsers = []string{}
	}

	ret, err := p.getQueriesReport(ctx, vNum, pool, excludeUsers)
	if err != nil {
		return nil, fmt.Errorf("getQueriesReport | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getQueriesBlocked(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
	database *string,
) ([]dto.QueryBlocked, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryQueriesBlocked, nil)
	if err != nil {
		return nil, fmt.Errorf("getQueriesBlocked | %w", err)
	}

	rows, err := pool.Query(ctx, qStr, database)
	if err != nil {
		return nil, fmt.Errorf("getQueriesBlocked | %w", err)
	}

	ret := make([]dto.QueryBlocked, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			lockedItem, blockedDatabase, blockedUser, blockedQuery, blockedDuration, blockedMode       string
			blockingUser, stateOfBlockingProcess, currentOrRecentQuery, blockingDuration, blockingMode string
			blockedPid, blockingPid                                                                    int32
			blockedDurationMs, blockingDurationMs                                                      pgtype.Float8
		)

		err = rows.Scan(&lockedItem, &blockedPid, &blockedDatabase, &blockedUser, &blockedQuery, &blockedDuration, &blockedDurationMs,
			&blockedMode, &blockingPid, &blockingUser, &stateOfBlockingProcess,
			&currentOrRecentQuery, &blockingDuration, &blockingDurationMs, &blockingMode)
		if err != nil {
			return nil, fmt.Errorf("getQueriesBlocked | %w", err)
		}

		entry := dto.QueryBlocked{
			LockedItem:                            lockedItem,
			BlockedPid:                            blockedPid,
			BlockedDatabase:                       blockedDatabase,
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
		} //nolint: exhaustruct
		if blockedDurationMs.Valid {
			entry.BlockedDurationMs = &blockedDurationMs.Float64
		}

		if blockingDurationMs.Valid {
			entry.BlockingDurationMs = &blockingDurationMs.Float64
		}

		ret = append(ret, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getQueriesBlocked | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getQueriesRunning(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
	minDuration int,
	queryFilter *string,
	queryFilterMode string,
	username *string,
) ([]dto.QueryRunning, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryQueriesRunning, struct{ MinDuration int }{MinDuration: minDuration})
	if err != nil {
		return nil, fmt.Errorf("getQueriesRunning | %w", err)
	}

	rows, err := pool.Query(ctx, qStr, queryFilter, queryFilterMode, username)
	if err != nil {
		return nil, fmt.Errorf("getQueriesRunning | %w", err)
	}

	ret := make([]dto.QueryRunning, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			pid                                                  int32
			state, source, duration, queryStr, user, backendType string
			waitEventType, waitEvent, clientAddr                 string
			waiting                                              bool
			startedAt                                            time.Time
			durationMs                                           float64
		)

		err = rows.Scan(&pid, &state, &source, &duration, &waiting, &waitEventType, &waitEvent,
			&clientAddr, &queryStr, &startedAt, &durationMs, &user, &backendType)
		if err != nil {
			return nil, fmt.Errorf("getQueriesRunning | %w", err)
		}

		ret = append(ret, dto.QueryRunning{
			Pid:           pid,
			State:         state,
			Source:        source,
			Duration:      duration,
			Waiting:       waiting,
			WaitEventType: waitEventType,
			WaitEvent:     waitEvent,
			ClientAddr:    clientAddr,
			Query:         queryStr,
			StartedAt:     startedAt,
			DurationMs:    durationMs,
			User:          user,
			BackendType:   backendType,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getQueriesRunning | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getQueriesTop10ByTime(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
	database *string,
) ([]dto.QueryTop10ByTime, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryQueriesTop10ByTime, p.pgssTemplateData(ctx, pool))
	if err != nil {
		return nil, fmt.Errorf("getQueriesTop10ByTime | %w", err)
	}

	rows, err := pool.Query(ctx, qStr, database)
	if err != nil {
		return nil, fmt.Errorf("getQueriesTop10ByTime | %w", err)
	}

	ret := make([]dto.QueryTop10ByTime, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			queryID                                 int64
			datname, execTime, ioCpuPct, queryTrunc string
			execTimeMs, ioPct, cpuPct               float64
		)

		err = rows.Scan(&queryID, &datname, &execTime, &execTimeMs, &ioCpuPct, &ioPct, &cpuPct, &queryTrunc)
		if err != nil {
			return nil, fmt.Errorf("getQueriesTop10ByTime | %w", err)
		}

		ret = append(ret, dto.QueryTop10ByTime{
			QueryID:    queryID,
			Datname:    datname,
			ExecTime:   execTime,
			ExecTimeMs: execTimeMs,
			IoCpuPct:   ioCpuPct,
			IoPct:      ioPct,
			CpuPct:     cpuPct,
			QueryTrunc: queryTrunc,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getQueriesTop10ByTime | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getQueriesTop10ByWal(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
	database *string,
) ([]dto.QueryTop10ByWal, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryQueriesTop10ByWal, p.pgssTemplateData(ctx, pool))
	if err != nil {
		return nil, fmt.Errorf("getQueriesTop10ByWal | %w", err)
	}

	rows, err := pool.Query(ctx, qStr, database)
	if err != nil {
		return nil, fmt.Errorf("getQueriesTop10ByWal | %w", err)
	}

	ret := make([]dto.QueryTop10ByWal, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			queryID                        int64
			datname, walVolume, queryTrunc string
			walBytes                       int64
		)

		err = rows.Scan(&queryID, &datname, &walVolume, &walBytes, &queryTrunc)
		if err != nil {
			return nil, fmt.Errorf("getQueriesTop10ByWal | %w", err)
		}

		ret = append(ret, dto.QueryTop10ByWal{
			QueryID:    queryID,
			Datname:    datname,
			WalVolume:  walVolume,
			WalBytes:   walBytes,
			QueryTrunc: queryTrunc,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getQueriesTop10ByWal | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getQueriesTop10Chart(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
	database *string,
) ([]dto.QueryTop10ChartItem, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryQueriesTop10Chart, p.pgssTemplateData(ctx, pool))
	if err != nil {
		return nil, fmt.Errorf("getQueriesTop10Chart | %w", err)
	}

	rows, err := pool.Query(ctx, qStr, database)
	if err != nil {
		return nil, fmt.Errorf("getQueriesTop10Chart | %w", err)
	}

	ret := make([]dto.QueryTop10ChartItem, 0, 90) //nolint:mnd

	for rows.Next() {
		var (
			metric, datname string
			queryID         int64
			pct             float64
		)

		err = rows.Scan(&metric, &queryID, &datname, &pct)
		if err != nil {
			return nil, fmt.Errorf("getQueriesTop10Chart | %w", err)
		}

		ret = append(ret, dto.QueryTop10ChartItem{
			Metric:  metric,
			QueryID: queryID,
			Datname: datname,
			Pct:     pct,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getQueriesTop10Chart | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getQueriesReport(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
	excludeUsers []string,
) ([]dto.QueryReport, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryQueriesReport, p.pgssTemplateData(ctx, pool))
	if err != nil {
		return nil, fmt.Errorf("getQueriesReport | %w", err)
	}

	rows, err := pool.Query(ctx, qStr, excludeUsers)
	if err != nil {
		return nil, fmt.Errorf("getQueriesReport | %w", err)
	}

	ret := make([]dto.QueryReport, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			queryID                                                                    int64
			queryText                                                                  pgtype.Text
			usernames                                                                  []string
			datname                                                                    string
			rowsVal, calls                                                             pgtype.Int8
			rowsPct, rowsPctInst, callsPct, callsPctInst                               pgtype.Float8
			totalTimeMs, totalTimePct, totalTimePctInst                                pgtype.Float8
			execTimeMs, minExecTimeMs, maxExecTimeMs, meanExecTimeMs, stddevExecTimeMs pgtype.Float8
			planTimeMs, minPlanTimeMs, maxPlanTimeMs, meanPlanTimeMs, stddevPlanTimeMs pgtype.Float8
			ioTimeMs, ioTimePct, ioTimePctInst                                         pgtype.Float8
			cpuTimeMs, cpuTimePct, cpuTimePctInst                                      pgtype.Float8
			cacheHitRatio                                                              pgtype.Float8
			sharedBlksDirtiedPct, sharedBlksDirtiedPctInst                             pgtype.Float8
			sharedBlksWrittenPct, sharedBlksWrittenPctInst                             pgtype.Float8
			walBytes                                                                   pgtype.Int8
			walBytesPct, walBytesPctInst                                               pgtype.Float8
			walRecords, walFpi                                                         pgtype.Int8
			tempBlks                                                                   pgtype.Int8
			tempBlksPct, tempBlksPctInst                                               pgtype.Float8
		)

		err = rows.Scan(
			&queryID, &queryText, &usernames, &datname,
			&rowsVal, &rowsPct, &rowsPctInst,
			&calls, &callsPct, &callsPctInst,
			&totalTimeMs, &totalTimePct, &totalTimePctInst,
			&execTimeMs, &minExecTimeMs, &maxExecTimeMs, &meanExecTimeMs, &stddevExecTimeMs,
			&planTimeMs, &minPlanTimeMs, &maxPlanTimeMs, &meanPlanTimeMs, &stddevPlanTimeMs,
			&ioTimeMs, &ioTimePct, &ioTimePctInst,
			&cpuTimeMs, &cpuTimePct, &cpuTimePctInst,
			&cacheHitRatio,
			&sharedBlksDirtiedPct, &sharedBlksDirtiedPctInst,
			&sharedBlksWrittenPct, &sharedBlksWrittenPctInst,
			&walBytes, &walBytesPct, &walBytesPctInst, &walRecords, &walFpi,
			&tempBlks, &tempBlksPct, &tempBlksPctInst,
		)
		if err != nil {
			return nil, fmt.Errorf("getQueriesReport | %w", err)
		}

		ret = append(ret, dto.QueryReport{
			QueryID:                      queryID,
			Query:                        queryText.String,
			Usernames:                    usernames,
			Datname:                      datname,
			StddevExecTimeMs:             nullFloat(stddevExecTimeMs),
			StddevPlanTimeMs:             nullFloat(stddevPlanTimeMs),
			Rows:                         nullInt(rowsVal),
			RowsPct:                      nullFloat(rowsPct),
			RowsPctInstance:              nullFloat(rowsPctInst),
			Calls:                        nullInt(calls),
			CallsPct:                     nullFloat(callsPct),
			CallsPctInstance:             nullFloat(callsPctInst),
			TotalTimeMs:                  nullFloat(totalTimeMs),
			TotalTimePct:                 nullFloat(totalTimePct),
			TotalTimePctInstance:         nullFloat(totalTimePctInst),
			ExecTimeMs:                   nullFloat(execTimeMs),
			MinExecTimeMs:                nullFloat(minExecTimeMs),
			MaxExecTimeMs:                nullFloat(maxExecTimeMs),
			MeanExecTimeMs:               nullFloat(meanExecTimeMs),
			PlanTimeMs:                   nullFloat(planTimeMs),
			MinPlanTimeMs:                nullFloat(minPlanTimeMs),
			MaxPlanTimeMs:                nullFloat(maxPlanTimeMs),
			MeanPlanTimeMs:               nullFloat(meanPlanTimeMs),
			IoTimeMs:                     nullFloat(ioTimeMs),
			IoTimePct:                    nullFloat(ioTimePct),
			IoTimePctInstance:            nullFloat(ioTimePctInst),
			CpuTimeMs:                    nullFloat(cpuTimeMs),
			CpuTimePct:                   nullFloat(cpuTimePct),
			CpuTimePctInstance:           nullFloat(cpuTimePctInst),
			CacheHitRatio:                nullFloat(cacheHitRatio),
			SharedBlksDirtiedPct:         nullFloat(sharedBlksDirtiedPct),
			SharedBlksDirtiedPctInstance: nullFloat(sharedBlksDirtiedPctInst),
			SharedBlksWrittenPct:         nullFloat(sharedBlksWrittenPct),
			SharedBlksWrittenPctInstance: nullFloat(sharedBlksWrittenPctInst),
			WalBytes:                     nullInt(walBytes),
			WalBytesPct:                  nullFloat(walBytesPct),
			WalBytesPctInstance:          nullFloat(walBytesPctInst),
			WalRecords:                   nullInt(walRecords),
			WalFpi:                       nullInt(walFpi),
			TempBlks:                     nullInt(tempBlks),
			TempBlksPct:                  nullFloat(tempBlksPct),
			TempBlksPctInstance:          nullFloat(tempBlksPctInst),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getQueriesReport | %w", err)
	}

	return ret, nil
}

// nullFloat and nullInt keep a NULL distinguishable from a real zero: the DTO
// carries pointers, and pgtype's zero value alone would silently become 0.
func nullFloat(v pgtype.Float8) *float64 {
	if !v.Valid {
		return nil
	}

	return &v.Float64
}

func nullInt(v pgtype.Int8) *int64 {
	if !v.Valid {
		return nil
	}

	return &v.Int64
}

func (p *PgxPool) ResetQueryStats(ctx context.Context, clusterName, instanceName, databaseName string) error {
	// The reset function drops the instance-wide statistics wherever it is
	// called from, so it has to be called where the extension exists — the same
	// database the statistics are read through.
	pool, err := p.pgssPool(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return fmt.Errorf("ResetQueryStats | %w", err)
	}

	queryCtx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	fn := p.pgssResetFunction(queryCtx, pool)

	_, err = pool.Exec(queryCtx, "SELECT "+fn+"()")
	if err != nil {
		return fmt.Errorf("%s | %w", fn, err)
	}

	return nil
}

// GetActiveConnectionCount returns the number of backends in state='active'
// on the given instance (excluding the caller's own backend).
func (p *PgxPool) GetActiveConnectionCount(ctx context.Context, clusterName, instanceName string) (int, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return 0, fmt.Errorf("GetActiveConnectionCount | %w", err)
	}

	queryCtx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	var n int

	err = pool.QueryRow(queryCtx,
		`SELECT count(*) FROM pg_stat_activity WHERE state = 'active' AND pid <> pg_backend_pid()`,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("active count | %w", err)
	}

	return n, nil
}

// GetBlockedSessionCount returns how many backends are currently blocked on a
// lock — a cheap, instance-wide probe used for background lock-spike tracking.
func (p *PgxPool) GetBlockedSessionCount(ctx context.Context, clusterName, instanceName, databaseName string) (int, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return 0, fmt.Errorf("GetBlockedSessionCount | %w", err)
	}

	queryCtx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	return p.getBlockedSessionCount(queryCtx, pool)
}

func (p *PgxPool) getBlockedSessionCount(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	var n int

	// Instance-wide, like the detailed capture it precedes: an activity spike is
	// detected across the whole host, so contention in any database of it counts.
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM pg_stat_activity a
		 WHERE cardinality(pg_blocking_pids(a.pid)) > 0`,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("blocked count | %w", err)
	}

	return n, nil
}
