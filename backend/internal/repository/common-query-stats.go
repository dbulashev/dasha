package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/enums"
	"github.com/dbulashev/dasha/internal/query"
)

func (p *PgxPool) GetQueryStatsStatus(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
) (dto.QueryStatsStatus, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return dto.QueryStatsStatus{}, fmt.Errorf("GetQueryStatsStatus | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return dto.QueryStatsStatus{}, fmt.Errorf("get server version | %w", err)
	}

	available, _ := p.getQueryStatsAvailable(ctx, vNum, pool)
	enabled, _ := p.getQueryStatsEnabled(ctx, vNum, pool)
	readable, _ := p.getQueryStatsReadable(ctx, vNum, pool)

	return dto.QueryStatsStatus{
		Available: available,
		Enabled:   enabled,
		Readable:  readable,
	}, nil
}

func (p *PgxPool) getQueryStatsAvailable(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
) (bool, error) {
	qStr, err := query.Get(serverVersion, enums.QueryCommonQueryStatsAvailable, nil)
	if err != nil {
		return false, fmt.Errorf("getQueryStatsAvailable | %w", err)
	}

	var b bool

	err = pool.QueryRow(ctx, qStr).Scan(&b)
	if err != nil {
		return false, fmt.Errorf("getQueryStatsAvailable | %w", err)
	}

	return b, nil
}

func (p *PgxPool) getQueryStatsEnabled(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
) (bool, error) {
	qStr, err := query.Get(serverVersion, enums.QueryCommonQueryStatsEnabled, nil)
	if err != nil {
		return false, fmt.Errorf("getQueryStatsEnabled | %w", err)
	}

	var b bool

	err = pool.QueryRow(ctx, qStr).Scan(&b)
	if err != nil {
		return false, fmt.Errorf("getQueryStatsEnabled | %w", err)
	}

	return b, nil
}

func (p *PgxPool) getQueryStatsReadable(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
) (bool, error) {
	qStr, err := query.Get(serverVersion, enums.QueryCommonQueryStatsReadable, nil)
	if err != nil {
		return false, fmt.Errorf("getQueryStatsReadable | %w", err)
	}

	_, err = pool.Exec(ctx, qStr)

	return err == nil, nil
}
