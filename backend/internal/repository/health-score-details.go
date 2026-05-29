package repository

import (
	"context"
	"fmt"

	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/enums"
	"github.com/dbulashev/dasha/internal/query"
)

// GetHealthScoreXidWraparoundDatabases returns per-database transaction-ID
// age, used as inline detail for the xid_wraparound_risk recommendation.
func (p *PgxPool) GetHealthScoreXidWraparoundDatabases(
	ctx context.Context,
	clusterName, instanceName string,
) ([]dto.HealthScoreXidWraparoundDatabase, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return nil, fmt.Errorf("GetHealthScoreXidWraparoundDatabases | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(vNum, enums.QueryCommonHealthScoreXidWraparoundDatabases, nil)
	if err != nil {
		return nil, fmt.Errorf("query.Get | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("pool.Query | %w", err)
	}
	defer rows.Close()

	out := make([]dto.HealthScoreXidWraparoundDatabase, 0, 10)

	for rows.Next() {
		var r dto.HealthScoreXidWraparoundDatabase
		if err := rows.Scan(&r.Database, &r.XidAge); err != nil {
			return nil, fmt.Errorf("scan | %w", err)
		}

		out = append(out, r)
	}

	return out, rows.Err()
}

// GetHealthScoreTablesAutovacuumOff lists tables with
// autovacuum_enabled=false in reloptions.
func (p *PgxPool) GetHealthScoreTablesAutovacuumOff(
	ctx context.Context,
	clusterName, instanceName, databaseName string,
) ([]dto.HealthScoreTableReloption, error) {
	return p.scanTableReloptions(ctx, clusterName, instanceName, databaseName,
		enums.QueryCommonHealthScoreTablesAutovacuumOff)
}

// GetHealthScoreLowHotUpdateTables lists tables with the lowest HOT-update
// ratio (most UPDATEs that rewrite every index).
func (p *PgxPool) GetHealthScoreLowHotUpdateTables(
	ctx context.Context,
	clusterName, instanceName, databaseName string,
) ([]dto.HealthScoreLowHotUpdateTable, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetHealthScoreLowHotUpdateTables | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(vNum, enums.QueryCommonHealthScoreLowHotUpdateTables, nil)
	if err != nil {
		return nil, fmt.Errorf("query.Get | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("pool.Query | %w", err)
	}
	defer rows.Close()

	out := make([]dto.HealthScoreLowHotUpdateTable, 0, 20)

	for rows.Next() {
		var r dto.HealthScoreLowHotUpdateTable
		if err := rows.Scan(&r.Schema, &r.Table, &r.Updates, &r.HotUpdates, &r.HotRatio); err != nil {
			return nil, fmt.Errorf("scan | %w", err)
		}

		out = append(out, r)
	}

	return out, rows.Err()
}

// GetHealthScoreHighDeadRatioTables lists tables with the highest dead-tuple
// ratio — direct VACUUM ANALYZE targets for the high_max_dead_ratio
// recommendation.
func (p *PgxPool) GetHealthScoreHighDeadRatioTables(
	ctx context.Context,
	clusterName, instanceName, databaseName string,
) ([]dto.HealthScoreHighDeadRatioTable, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetHealthScoreHighDeadRatioTables | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(vNum, enums.QueryCommonHealthScoreHighDeadRatioTables, nil)
	if err != nil {
		return nil, fmt.Errorf("query.Get | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("pool.Query | %w", err)
	}
	defer rows.Close()

	out := make([]dto.HealthScoreHighDeadRatioTable, 0, 20)

	for rows.Next() {
		var r dto.HealthScoreHighDeadRatioTable
		if err := rows.Scan(&r.Schema, &r.Table, &r.LiveTuples, &r.DeadTuples, &r.DeadRatio); err != nil {
			return nil, fmt.Errorf("scan | %w", err)
		}

		out = append(out, r)
	}

	return out, rows.Err()
}

// GetHealthScoreHorizonBlockingSessions lists sessions whose backend_xmin
// is oldest, pinning the MVCC horizon.
func (p *PgxPool) GetHealthScoreHorizonBlockingSessions(
	ctx context.Context,
	clusterName, instanceName string,
) ([]dto.HealthScoreHorizonBlockingSession, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return nil, fmt.Errorf("GetHealthScoreHorizonBlockingSessions | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(vNum, enums.QueryCommonHealthScoreHorizonBlockingSessions, nil)
	if err != nil {
		return nil, fmt.Errorf("query.Get | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("pool.Query | %w", err)
	}
	defer rows.Close()

	out := make([]dto.HealthScoreHorizonBlockingSession, 0, 10)

	for rows.Next() {
		var r dto.HealthScoreHorizonBlockingSession
		if err := rows.Scan(
			&r.PID,
			&r.Username,
			&r.State,
			&r.WaitEventType,
			&r.WaitEvent,
			&r.XactDurationSeconds,
			&r.BackendXmin,
			&r.Query,
		); err != nil {
			return nil, fmt.Errorf("scan | %w", err)
		}

		out = append(out, r)
	}

	return out, rows.Err()
}

// scanTableReloptions runs a per-database detail query that returns
// (schema, table, reloptions string) and collects the rows.
func (p *PgxPool) scanTableReloptions(
	ctx context.Context,
	clusterName, instanceName, databaseName string,
	q enums.Query,
) ([]dto.HealthScoreTableReloption, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("scanTableReloptions | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(vNum, q, nil)
	if err != nil {
		return nil, fmt.Errorf("query.Get | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("pool.Query | %w", err)
	}
	defer rows.Close()

	out := make([]dto.HealthScoreTableReloption, 0, 16)

	for rows.Next() {
		var r dto.HealthScoreTableReloption
		if err := rows.Scan(&r.Schema, &r.Table, &r.RelOptions); err != nil {
			return nil, fmt.Errorf("scan | %w", err)
		}

		out = append(out, r)
	}

	return out, rows.Err()
}
