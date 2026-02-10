package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dbulashev/dasha/internal/enums"
	"github.com/dbulashev/dasha/internal/query"
)

func (p *PgxPool) GetDatabaseUsers(ctx context.Context, clusterName, instanceName string) ([]string, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return nil, fmt.Errorf("GetDatabaseUsers | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getDatabaseUsers(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getDatabaseUsers | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getDatabaseUsers(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryCommonDatabaseUsers, nil)
	if err != nil {
		return nil, fmt.Errorf("getDatabaseUsers | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getDatabaseUsers | %w", err)
	}

	ret := make([]string, 0, 16) //nolint:mnd

	for rows.Next() {
		var rolname string

		err = rows.Scan(&rolname)
		if err != nil {
			return nil, fmt.Errorf("getDatabaseUsers | %w", err)
		}

		ret = append(ret, rolname)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getDatabaseUsers | %w", err)
	}

	return ret, nil
}
