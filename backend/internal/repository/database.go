package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/enums"
	"github.com/dbulashev/dasha/internal/query"
)

func (p *PgxPool) GetStatsResetTime(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.StatsResetTime, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetStatsResetTime | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getStatsResetTime(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getStatsResetTime | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getStatsResetTime(ctx context.Context, serverVersion int, pool *pgxpool.Pool) ([]dto.StatsResetTime, error) {
	qStr, err := query.Get(serverVersion, enums.QueryDatabaseStatsResetTime, nil)
	if err != nil {
		return nil, fmt.Errorf("getStatsResetTime | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getStatsResetTime | %w", err)
	}

	ret := make([]dto.StatsResetTime, 0, 10) //nolint:mnd

	for rows.Next() {
		var resetTime pgtype.Timestamptz

		err = rows.Scan(&resetTime)
		if err != nil {
			return nil, fmt.Errorf("getStatsResetTime | %w", err)
		}

		ret = append(ret, dto.StatsResetTime{
			Time: resetTime.Time,
		})
	}

	return ret, nil
}

func (p *PgxPool) GetPgssStatsResetTime(ctx context.Context, clusterName, instanceName, databaseName string) (*dto.StatsResetTime, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetPgssStatsResetTime | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	// pg_stat_statements_info available since PostgreSQL 14
	if vNum < 140000 { //nolint:mnd
		return nil, nil
	}

	return p.getPgssStatsResetTime(ctx, vNum, pool)
}

func (p *PgxPool) GetDatabaseSize(ctx context.Context, clusterName, instanceName, databaseName string) (*dto.DatabaseSize, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetDatabaseSize | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	return p.getDatabaseSize(ctx, vNum, pool)
}

func (p *PgxPool) getDatabaseSize(ctx context.Context, serverVersion int, pool *pgxpool.Pool) (*dto.DatabaseSize, error) {
	qStr, err := query.Get(serverVersion, enums.QueryDatabaseSize, nil)
	if err != nil {
		return nil, fmt.Errorf("getDatabaseSize | %w", err)
	}

	var (
		sizeBytes  int64
		sizePretty string
	)

	err = pool.QueryRow(ctx, qStr).Scan(&sizeBytes, &sizePretty)
	if err != nil {
		return nil, fmt.Errorf("getDatabaseSize | %w", err)
	}

	return &dto.DatabaseSize{SizeBytes: sizeBytes, SizePretty: sizePretty}, nil
}

func (p *PgxPool) getPgssStatsResetTime(ctx context.Context, serverVersion int, pool *pgxpool.Pool) (*dto.StatsResetTime, error) {
	qStr, err := query.Get(serverVersion, enums.QueryDatabasePgssStatsResetTime, nil)
	if err != nil {
		return nil, fmt.Errorf("getPgssStatsResetTime | %w", err)
	}

	var resetTime pgtype.Timestamptz

	err = pool.QueryRow(ctx, qStr).Scan(&resetTime)
	if err != nil {
		return nil, fmt.Errorf("getPgssStatsResetTime | %w", err)
	}

	if !resetTime.Valid {
		return nil, nil
	}

	return &dto.StatsResetTime{Time: resetTime.Time}, nil
}
