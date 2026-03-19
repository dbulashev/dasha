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

func (p *PgxPool) GetMaintenanceAutovacuumFreezeMaxAge(
	ctx context.Context,
	clusterName,
	instanceName string,
) ([]dto.MaintenanceAutovacuumFreezeMaxAge, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return nil, fmt.Errorf("GetMaintenanceAutovacuumFreezeMaxAge | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getMaintenanceAutovacuumFreezeMaxAge(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getMaintenanceAutovacuumFreezeMaxAge | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetMaintenanceInfo(ctx context.Context, clusterName, instanceName, databaseName string, tableName *string, limit, offset int) ([]dto.MaintenanceInfo, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetMaintenanceInfo | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getMaintenanceInfo(ctx, vNum, pool, tableName, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("getMaintenanceInfo | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetMaintenanceTransactionIdDanger(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
) ([]dto.MaintenanceTransactionIdDanger, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetMaintenanceTransactionIdDanger | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getMaintenanceTransactionIdDanger(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getMaintenanceTransactionIdDanger | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetMaintenanceVacuumProgress(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
) ([]dto.MaintenanceVacuumProgress, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetMaintenanceVacuumProgress | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getMaintenanceVacuumProgress(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getMaintenanceVacuumProgress | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getMaintenanceAutovacuumFreezeMaxAge(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
) ([]dto.MaintenanceAutovacuumFreezeMaxAge, error) {
	qStr, err := query.Get(serverVersion, enums.QueryMaintenanceAutovacuumFreezeMaxAge, nil)
	if err != nil {
		return nil, fmt.Errorf("getMaintenanceAutovacuumFreezeMaxAge | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getMaintenanceAutovacuumFreezeMaxAge | %w", err)
	}

	ret := make([]dto.MaintenanceAutovacuumFreezeMaxAge, 0, 1)

	for rows.Next() {
		var autovacuumFreezeMaxAge int64

		err = rows.Scan(&autovacuumFreezeMaxAge)
		if err != nil {
			return nil, fmt.Errorf("getMaintenanceAutovacuumFreezeMaxAge | %w", err)
		}

		ret = append(ret, dto.MaintenanceAutovacuumFreezeMaxAge{
			AutovacuumFreezeMaxAge: autovacuumFreezeMaxAge,
		})
	}

	return ret, nil
}

func (p *PgxPool) getMaintenanceInfo(ctx context.Context, serverVersion int, pool *pgxpool.Pool, tableName *string, limit, offset int) ([]dto.MaintenanceInfo, error) {
	qStr, err := query.Get(serverVersion, enums.QueryMaintenanceInfo, nil)
	if err != nil {
		return nil, fmt.Errorf("getMaintenanceInfo | %w", err)
	}

	rows, err := pool.Query(ctx, qStr, tableName, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("getMaintenanceInfo | %w", err)
	}

	ret := make([]dto.MaintenanceInfo, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			schema, table                string
			lastVacuum, lastAutovacuum   pgtype.Timestamp
			lastAnalyze, lastAutoanalyze pgtype.Timestamp
			deadRows, liveRows           int64
		)

		err = rows.Scan(&schema, &table, &lastVacuum, &lastAutovacuum, &lastAnalyze, &lastAutoanalyze, &deadRows, &liveRows)
		if err != nil {
			return nil, fmt.Errorf("getMaintenanceInfo | %w", err)
		}

		ret = append(ret, dto.MaintenanceInfo{
			Schema:          schema,
			Table:           table,
			LastVacuum:      convertPgTimestampToTime(lastVacuum),
			LastAutovacuum:  convertPgTimestampToTime(lastAutovacuum),
			LastAnalyze:     convertPgTimestampToTime(lastAnalyze),
			LastAutoanalyze: convertPgTimestampToTime(lastAutoanalyze),
			DeadRows:        deadRows,
			LiveRows:        liveRows,
		})
	}

	return ret, nil
}

func (p *PgxPool) getMaintenanceTransactionIdDanger(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
) ([]dto.MaintenanceTransactionIdDanger, error) {
	maxAgeRows, err := p.getMaintenanceAutovacuumFreezeMaxAge(ctx, serverVersion, pool)
	if err != nil {
		return nil, fmt.Errorf("get autovacuum_freeze_max_age | %w", err)
	}

	var freezeMaxAge int64 = 200000000 // default PG value
	if len(maxAgeRows) > 0 {
		freezeMaxAge = maxAgeRows[0].AutovacuumFreezeMaxAge
	}

	// Danger threshold: tables with less than 5% of max age remaining
	dangerThreshold := freezeMaxAge / 20 //nolint:mnd

	qStr, err := query.Get(serverVersion, enums.QueryMaintenanceTransactionIdDanger, nil)
	if err != nil {
		return nil, fmt.Errorf("getMaintenanceTransactionIdDanger | %w", err)
	}

	rows, err := pool.Query(ctx, qStr, freezeMaxAge, dangerThreshold)
	if err != nil {
		return nil, fmt.Errorf("getMaintenanceTransactionIdDanger | %w", err)
	}

	ret := make([]dto.MaintenanceTransactionIdDanger, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			schema, table    string
			transactionsLeft int64
		)

		err = rows.Scan(&schema, &table, &transactionsLeft)
		if err != nil {
			return nil, fmt.Errorf("getMaintenanceTransactionIdDanger | %w", err)
		}

		ret = append(ret, dto.MaintenanceTransactionIdDanger{
			Schema:           schema,
			Table:            table,
			TransactionsLeft: transactionsLeft,
		})
	}

	return ret, nil
}

func (p *PgxPool) getMaintenanceVacuumProgress(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
) ([]dto.MaintenanceVacuumProgress, error) {
	qStr, err := query.Get(serverVersion, enums.QueryMaintenanceVacuumProgress, nil)
	if err != nil {
		return nil, fmt.Errorf("getMaintenanceVacuumProgress | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getMaintenanceVacuumProgress | %w", err)
	}

	ret := make([]dto.MaintenanceVacuumProgress, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			pid   int32
			phase string
		)

		err = rows.Scan(&pid, &phase)
		if err != nil {
			return nil, fmt.Errorf("getMaintenanceVacuumProgress | %w", err)
		}

		ret = append(ret, dto.MaintenanceVacuumProgress{
			Pid:   pid,
			Phase: phase,
		})
	}

	return ret, nil
}

func convertPgTimestampToTime(ts pgtype.Timestamp) *time.Time {
	if !ts.Valid {
		return nil
	}

	return &ts.Time
}
