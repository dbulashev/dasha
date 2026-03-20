package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/enums"
	"github.com/dbulashev/dasha/internal/query"
)

func (p *PgxPool) GetInvalidConstraints(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
) ([]dto.InvalidConstraint, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetInvalidConstraints | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getInvalidConstraints(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getInvalidConstraints | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getInvalidConstraints(ctx context.Context, serverVersion int, pool *pgxpool.Pool) ([]dto.InvalidConstraint, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryConstraintsInvalidConstraints, nil)
	if err != nil {
		return nil, fmt.Errorf("getInvalidConstraints | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getInvalidConstraints | %w", err)
	}

	ret := make([]dto.InvalidConstraint, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			schema, table, name, referencedSchema, referencedTable string
		)

		err = rows.Scan(&schema, &table, &name, &referencedSchema, &referencedTable)
		if err != nil {
			return nil, fmt.Errorf("getInvalidConstraints | %w", err)
		}

		ret = append(ret, dto.InvalidConstraint{
			Schema:           schema,
			Table:            table,
			Name:             name,
			ReferencedSchema: referencedSchema,
			ReferencedTable:  referencedTable,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getInvalidConstraints | %w", err)
	}

	return ret, nil
}
