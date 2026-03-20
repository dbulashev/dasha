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

func (p *PgxPool) GetTablesTopKBySize(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
	limit int,
) ([]dto.TableTopKBySize, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetTablesTopKBySize | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	if limit <= 0 || limit > 100 {
		limit = 10
	}

	ret, err := p.getTablesTopKBySize(ctx, vNum, pool, limit)
	if err != nil {
		return nil, fmt.Errorf("getTablesTopKBySize | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetTablesCaching(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
	limit,
	offset int,
) ([]dto.TableCaching, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetTablesCaching | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getTablesCaching(ctx, vNum, pool, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("getTablesCaching | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetTablesHitRate(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.TableHitRate, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetTablesHitRate | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getTablesHitRate(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getTablesHitRate | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetTablesPartitions(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.TablePartition, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetTablesPartitions | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getTablesPartitions(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getTablesPartitions | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getTablesTopKBySize(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
	limit int,
) ([]dto.TableTopKBySize, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryTablesTopKBySize, nil)
	if err != nil {
		return nil, fmt.Errorf("getTablesTopKBySize | %w", err)
	}

	rows, err := pool.Query(ctx, qStr, limit)
	if err != nil {
		return nil, fmt.Errorf("getTablesTopKBySize | %w", err)
	}

	ret := make([]dto.TableTopKBySize, 0, 20) //nolint:mnd

	for rows.Next() {
		var (
			table, total, indexes, main, fsm, vm string
			nIdx, totalBytes                     int64
			toast, statInfo, bloat, options      pgtype.Text
		)

		err = rows.Scan(
			&table,
			&nIdx,
			&totalBytes,
			&total,
			&toast,
			&indexes,
			&main,
			&fsm,
			&vm,
			&statInfo,
			&bloat,
			&options,
		)
		if err != nil {
			return nil, fmt.Errorf("getTablesTopKBySize | %w", err)
		}

		ret = append(ret, dto.TableTopKBySize{
			Table:      table,
			NIdx:       nIdx,
			TotalBytes: totalBytes,
			Total:      total,
			Toast:      toast.String,
			Indexes:    indexes,
			Main:       main,
			Fsm:        fsm,
			Vm:         vm,
			StatInfo:   statInfo.String,
			Bloat:      bloat.String,
			Options:    options.String,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getTablesTopKBySize | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getTablesCaching(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
	limit,
	offset int,
) ([]dto.TableCaching, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryTablesCaching, nil)
	if err != nil {
		return nil, fmt.Errorf("getTablesCaching | %w", err)
	}

	rows, err := pool.Query(ctx, qStr, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("getTablesCaching | %w", err)
	}

	ret := make([]dto.TableCaching, 0, 50) //nolint:mnd

	for rows.Next() {
		var (
			schema, table                                  string
			hitRate, idxHitRate, toastHitRate, tidxHitRate pgtype.Float8
		)

		err = rows.Scan(&schema, &table, &hitRate, &idxHitRate, &toastHitRate, &tidxHitRate)
		if err != nil {
			return nil, fmt.Errorf("getTablesCaching | %w", err)
		}

		item := dto.TableCaching{ //nolint:exhaustruct
			Schema: schema,
			Table:  table,
		}
		if hitRate.Valid {
			item.HitRate = &hitRate.Float64
		}

		if idxHitRate.Valid {
			item.IdxHitRate = &idxHitRate.Float64
		}

		if toastHitRate.Valid {
			item.ToastHitRate = &toastHitRate.Float64
		}

		if tidxHitRate.Valid {
			item.ToastIdxHitRate = &tidxHitRate.Float64
		}

		ret = append(ret, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getTablesCaching | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getTablesHitRate(ctx context.Context, serverVersion int, pool *pgxpool.Pool) ([]dto.TableHitRate, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryTablesHitRate, nil)
	if err != nil {
		return nil, fmt.Errorf("getTablesHitRate | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getTablesHitRate | %w", err)
	}

	ret := make([]dto.TableHitRate, 0, 1)

	for rows.Next() {
		var rate pgtype.Float8

		err = rows.Scan(&rate)
		if err != nil {
			return nil, fmt.Errorf("getTablesHitRate | %w", err)
		}

		if rate.Valid {
			ret = append(ret, dto.TableHitRate{
				Rate: rate.Float64,
			})
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getTablesHitRate | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getTablesPartitions(ctx context.Context, serverVersion int, pool *pgxpool.Pool) ([]dto.TablePartition, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryTablesPartitions, nil)
	if err != nil {
		return nil, fmt.Errorf("getTablesPartitions | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getTablesPartitions | %w", err)
	}

	ret := make([]dto.TablePartition, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			parentSchema, parent string
			childsCount          int64
			childsSizeBytes      int64
			childsSize           pgtype.Text
			childsAvgSizeBytes   int64
			childsAvgSize        pgtype.Text
		)

		err = rows.Scan(&parentSchema, &parent, &childsCount, &childsSizeBytes, &childsSize, &childsAvgSizeBytes, &childsAvgSize)
		if err != nil {
			return nil, fmt.Errorf("getTablesPartitions | %w", err)
		}

		ret = append(ret, dto.TablePartition{
			ParentSchema:       parentSchema,
			Parent:             parent,
			ChildsCount:        childsCount,
			ChildsSizeBytes:    childsSizeBytes,
			ChildsSize:         childsSize.String,
			ChildsAvgSizeBytes: childsAvgSizeBytes,
			ChildsAvgSize:      childsAvgSize.String,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getTablesPartitions | %w", err)
	}

	return ret, nil
}
