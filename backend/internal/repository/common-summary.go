package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/enums"
	"github.com/dbulashev/dasha/internal/query"
)

func (p *PgxPool) getPoolByClusterNameAndInstance(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
) (*pgxpool.Pool, error) {
	err := p.ensurePool(ctx)
	if err != nil {
		return nil, fmt.Errorf("ensure pool | %w", err)
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	for cluster, pools := range p.pools {
		if cluster.String() != clusterName {
			continue
		}

		for _, pool := range pools {
			if pool.Host.String() != instanceName {
				continue
			}

			if databaseName != "" && pool.Database.String() != databaseName {
				continue
			}

			return pool.pool, nil
		}
	}

	return nil, fmt.Errorf("%w | %s/%s", ErrNotFound, clusterName, instanceName)
}

func (p *PgxPool) getPoolsByClusterAndDatabase(
	ctx context.Context,
	clusterName,
	databaseName string,
) ([]*pgxpool.Pool, error) {
	err := p.ensurePool(ctx)
	if err != nil {
		return nil, fmt.Errorf("ensure pool | %w", err)
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	var pools []*pgxpool.Pool
	for cluster, items := range p.pools {
		if cluster.String() != clusterName {
			continue
		}

		for _, item := range items {
			if databaseName != "" && item.Database.String() != databaseName {
				continue
			}

			pools = append(pools, item.pool)
		}
	}

	if len(pools) == 0 {
		return nil, fmt.Errorf("%w | %s/%s", ErrNotFound, clusterName, databaseName)
	}

	return pools, nil
}

func (p *PgxPool) GetCommonSummary(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
) ([]dto.CommonSummary, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetCommonSummary | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getCommonSummary(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getCommonSummary | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getCommonSummary(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
) ([]dto.CommonSummary, error) {
	qStr, err := query.Get(serverVersion, enums.QueryCommonSummary, nil)
	if err != nil {
		return nil, fmt.Errorf("getCommonSummary | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getCommonSummary | %w", err)
	}

	ret := make([]dto.CommonSummary, 0, 8) //nolint:mnd

	for rows.Next() {
		var (
			namespace, kind, approxSize string
			amount                      int64
		)

		err = rows.Scan(&namespace, &kind, &approxSize, &amount)
		if err != nil {
			return nil, fmt.Errorf("getCommonSummary | %w", err)
		}

		ret = append(ret, dto.CommonSummary{Namespace: namespace, Kind: kind, ApproxSize: approxSize, Amount: amount})
	}

	return ret, nil
}
