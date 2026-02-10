package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/enums"
	"github.com/dbulashev/dasha/internal/query"
)

func (p *PgxPool) GetFksPossibleNulls(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.FksPossibleNulls, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetFksPossibleNulls | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getFksPossibleNulls(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getFksPossibleNulls | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetFksPossibleSimilar(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
) ([]dto.FksPossibleSimilar, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetFksPossibleSimilar | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getFksPossibleSimilar(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getFksPossibleSimilar | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetFkTypeMismatch(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.FkTypeMismatch, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetFkTypeMismatch | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getFkTypeMismatch(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getFkTypeMismatch | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getFksPossibleNulls(ctx context.Context, serverVersion int, pool *pgxpool.Pool) ([]dto.FksPossibleNulls, error) {
	qStr, err := query.Get(serverVersion, enums.QueryFksPossibleNulls, nil)
	if err != nil {
		return nil, fmt.Errorf("getFksPossibleNulls | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getFksPossibleNulls | %w", err)
	}

	ret := make([]dto.FksPossibleNulls, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			fkName, relName string
			attNames        []string
		)

		err = rows.Scan(&fkName, &relName, &attNames)
		if err != nil {
			return nil, fmt.Errorf("getFksPossibleNulls | %w", err)
		}

		ret = append(ret, dto.FksPossibleNulls{
			FkName:   fkName,
			RelName:  relName,
			AttNames: attNames,
		})
	}

	return ret, nil
}

func (p *PgxPool) getFksPossibleSimilar(ctx context.Context, serverVersion int, pool *pgxpool.Pool) ([]dto.FksPossibleSimilar, error) {
	qStr1, err := query.Get(serverVersion, enums.QueryFksPossibleSimilar1, nil)
	if err != nil {
		return nil, fmt.Errorf("getFksPossibleSimilar | %w", err)
	}

	qStr2, err := query.Get(serverVersion, enums.QueryFksPossibleSimilar2, nil)
	if err != nil {
		return nil, fmt.Errorf("getFksPossibleSimilar | %w", err)
	}

	ret := make([]dto.FksPossibleSimilar, 0, 10) //nolint:mnd

	// Выполняем первый запрос
	rows1, err := pool.Query(ctx, qStr1)
	if err != nil {
		return nil, fmt.Errorf("getFksPossibleSimilar | %w", err)
	}

	for rows1.Next() {
		var table, fkName, fk1Name string

		err = rows1.Scan(&table, &fkName, &fk1Name)
		if err != nil {
			return nil, fmt.Errorf("getFksPossibleSimilar | %w", err)
		}

		ret = append(ret, dto.FksPossibleSimilar{
			Table:   table,
			FkName:  fkName,
			Fk1Name: fk1Name,
		})
	}

	rows1.Close()

	rows2, err := pool.Query(ctx, qStr2)
	if err != nil {
		return nil, fmt.Errorf("getFksPossibleSimilar | %w", err)
	}

	for rows2.Next() {
		var table, fkName, fk1Name string

		err = rows2.Scan(&table, &fkName, &fk1Name)
		if err != nil {
			return nil, fmt.Errorf("getFksPossibleSimilar | %w", err)
		}

		ret = append(ret, dto.FksPossibleSimilar{
			Table:   table,
			FkName:  fkName,
			Fk1Name: fk1Name,
		})
	}

	rows2.Close()

	return ret, nil
}

func (p *PgxPool) getFkTypeMismatch(ctx context.Context, serverVersion int, pool *pgxpool.Pool) ([]dto.FkTypeMismatch, error) {
	qStr, err := query.Get(serverVersion, enums.QueryFksTypeMismatch, nil)
	if err != nil {
		return nil, fmt.Errorf("getFkTypeMismatch | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getFkTypeMismatch | %w", err)
	}

	ret := make([]dto.FkTypeMismatch, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			fkName                     string
			fromRel, toRel             string
			relAttNames, toRelAttNames []string
		)

		err = rows.Scan(&fkName, &fromRel, &relAttNames, &toRel, &toRelAttNames)
		if err != nil {
			return nil, fmt.Errorf("getFkTypeMismatch | %w", err)
		}

		ret = append(ret, dto.FkTypeMismatch{
			FkName:        fkName,
			FromRel:       fromRel,
			RelAttNames:   relAttNames,
			ToRel:         toRel,
			ToRelAttNames: toRelAttNames,
		})
	}

	return ret, nil
}
