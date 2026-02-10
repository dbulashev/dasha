package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/enums"
	"github.com/dbulashev/dasha/internal/query"
)

func (p *PgxPool) GetTablesDescribe(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName,
	schemaName,
	tableName string,
) (*dto.TableDescribe, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetTablesDescribe | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	metadata, err := p.getTablesDescribeMetadata(ctx, vNum, pool, schemaName, tableName)
	if err != nil {
		return nil, fmt.Errorf("getTablesDescribeMetadata | %w", err)
	}

	if metadata == nil {
		return nil, fmt.Errorf("table %s.%s | %w", schemaName, tableName, ErrNotFound)
	}

	pgStatsView := p.resolvePgStatsView(ctx, pool)

	columns, err := p.getTablesDescribeColumns(ctx, vNum, pool, schemaName, tableName, pgStatsView)
	if err != nil {
		return nil, fmt.Errorf("getTablesDescribeColumns | %w", err)
	}

	indexes, err := p.getTablesDescribeIndexes(ctx, vNum, pool, schemaName, tableName)
	if err != nil {
		return nil, fmt.Errorf("getTablesDescribeIndexes | %w", err)
	}

	checkConstraints, err := p.getTablesDescribeCheckConstraints(ctx, vNum, pool, schemaName, tableName)
	if err != nil {
		return nil, fmt.Errorf("getTablesDescribeCheckConstraints | %w", err)
	}

	fkConstraints, err := p.getTablesDescribeFkConstraints(ctx, vNum, pool, schemaName, tableName)
	if err != nil {
		return nil, fmt.Errorf("getTablesDescribeFkConstraints | %w", err)
	}

	referencedBy, err := p.getTablesDescribeReferencedBy(ctx, vNum, pool, schemaName, tableName)
	if err != nil {
		return nil, fmt.Errorf("getTablesDescribeReferencedBy | %w", err)
	}

	metadata.Columns = columns
	metadata.Indexes = indexes
	metadata.CheckConstraints = checkConstraints
	metadata.FkConstraints = fkConstraints
	metadata.ReferencedBy = referencedBy

	return metadata, nil
}

func (p *PgxPool) getTablesDescribeMetadata(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
	schemaName, tableName string,
) (*dto.TableDescribe, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryTablesDescribeMetadata, nil)
	if err != nil {
		return nil, fmt.Errorf("query.Get | %w", err)
	}

	row := pool.QueryRow(ctx, qStr, schemaName, tableName)

	var ret dto.TableDescribe

	err = row.Scan(
		&ret.Schema,
		&ret.TableName,
		&ret.TableType,
		&ret.AccessMethod,
		&ret.Tablespace,
		&ret.Options,
		&ret.SizeTotal,
		&ret.SizeTable,
		&ret.SizeToast,
		&ret.SizeIndexes,
		&ret.EstimatedRows,
		&ret.StatInfo,
		&ret.PartitionOf,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil //nolint:nilnil
		}

		return nil, fmt.Errorf("scan | %w", err)
	}

	return &ret, nil
}

func (p *PgxPool) getTablesDescribeColumns(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
	schemaName, tableName, pgStatsView string,
) ([]dto.TableDescribeColumn, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryTablesDescribeColumns,
		struct{ PgStatsView string }{PgStatsView: pgStatsView})
	if err != nil {
		return nil, fmt.Errorf("query.Get | %w", err)
	}

	rows, err := pool.Query(ctx, qStr, schemaName, tableName)
	if err != nil {
		return nil, fmt.Errorf("query | %w", err)
	}

	ret := make([]dto.TableDescribeColumn, 0, 20) //nolint:mnd

	for rows.Next() {
		var col dto.TableDescribeColumn

		err = rows.Scan(
			&col.Name,
			&col.Type,
			&col.Collation,
			&col.Nullable,
			&col.Default,
			&col.Storage,
			&col.Description,
			&col.NullFrac,
			&col.NDistinct,
			&col.AvgWidth,
		)
		if err != nil {
			return nil, fmt.Errorf("scan | %w", err)
		}

		ret = append(ret, col)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getTablesDescribeIndexes(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
	schemaName, tableName string,
) ([]dto.TableDescribeIndex, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryTablesDescribeIndexes, nil)
	if err != nil {
		return nil, fmt.Errorf("query.Get | %w", err)
	}

	rows, err := pool.Query(ctx, qStr, schemaName, tableName)
	if err != nil {
		return nil, fmt.Errorf("query | %w", err)
	}

	ret := make([]dto.TableDescribeIndex, 0, 10) //nolint:mnd

	for rows.Next() {
		var idx dto.TableDescribeIndex

		err = rows.Scan(
			&idx.Name,
			&idx.Definition,
			&idx.IsPrimary,
			&idx.IsUnique,
			&idx.IsValid,
			&idx.SizeBytes,
			&idx.Size,
		)
		if err != nil {
			return nil, fmt.Errorf("scan | %w", err)
		}

		ret = append(ret, idx)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getTablesDescribeCheckConstraints(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
	schemaName, tableName string,
) ([]dto.TableDescribeConstraint, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryTablesDescribeCheckConstraints, nil)
	if err != nil {
		return nil, fmt.Errorf("query.Get | %w", err)
	}

	rows, err := pool.Query(ctx, qStr, schemaName, tableName)
	if err != nil {
		return nil, fmt.Errorf("query | %w", err)
	}

	ret := make([]dto.TableDescribeConstraint, 0, 10) //nolint:mnd

	for rows.Next() {
		var c dto.TableDescribeConstraint

		err = rows.Scan(&c.Name, &c.Definition)
		if err != nil {
			return nil, fmt.Errorf("scan | %w", err)
		}

		ret = append(ret, c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getTablesDescribeFkConstraints(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
	schemaName, tableName string,
) ([]dto.TableDescribeConstraint, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryTablesDescribeFkConstraints, nil)
	if err != nil {
		return nil, fmt.Errorf("query.Get | %w", err)
	}

	rows, err := pool.Query(ctx, qStr, schemaName, tableName)
	if err != nil {
		return nil, fmt.Errorf("query | %w", err)
	}

	ret := make([]dto.TableDescribeConstraint, 0, 10) //nolint:mnd

	for rows.Next() {
		var c dto.TableDescribeConstraint

		err = rows.Scan(&c.Name, &c.Definition)
		if err != nil {
			return nil, fmt.Errorf("scan | %w", err)
		}

		ret = append(ret, c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getTablesDescribeReferencedBy(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
	schemaName, tableName string,
) ([]dto.TableDescribeReferencedBy, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryTablesDescribeReferencedBy, nil)
	if err != nil {
		return nil, fmt.Errorf("query.Get | %w", err)
	}

	rows, err := pool.Query(ctx, qStr, schemaName, tableName)
	if err != nil {
		return nil, fmt.Errorf("query | %w", err)
	}

	ret := make([]dto.TableDescribeReferencedBy, 0, 10) //nolint:mnd

	for rows.Next() {
		var r dto.TableDescribeReferencedBy

		err = rows.Scan(&r.Name, &r.SourceTable, &r.Definition)
		if err != nil {
			return nil, fmt.Errorf("scan | %w", err)
		}

		ret = append(ret, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetPgstattupleAvailable(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
) (bool, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return false, fmt.Errorf("GetPgstattupleAvailable | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return false, fmt.Errorf("get server version | %w", err)
	}

	return p.getPgstattupleAvailable(ctx, vNum, pool)
}

func (p *PgxPool) getPgstattupleAvailable(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryTablesPgstattupleAvailable, nil)
	if err != nil {
		return false, fmt.Errorf("query.Get | %w", err)
	}

	var available bool

	err = pool.QueryRow(ctx, qStr).Scan(&available)
	if err != nil {
		return false, fmt.Errorf("scan | %w", err)
	}

	return available, nil
}

func (p *PgxPool) GetTablesDescribeBloat(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName,
	schemaName,
	tableName string,
) (*dto.TableDescribeBloat, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetTablesDescribeBloat | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	return p.getTablesDescribeBloat(ctx, vNum, pool, schemaName, tableName)
}

func (p *PgxPool) getTablesDescribeBloat(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
	schemaName, tableName string,
) (*dto.TableDescribeBloat, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryTablesDescribeBloat, nil)
	if err != nil {
		return nil, fmt.Errorf("query.Get | %w", err)
	}

	var ret dto.TableDescribeBloat

	err = pool.QueryRow(ctx, qStr, schemaName, tableName).Scan(
		&ret.TableLen,
		&ret.TableLenPretty,
		&ret.ApproxTupleCount,
		&ret.ApproxTupleLen,
		&ret.ApproxTupleLenPretty,
		&ret.ApproxTuplePercent,
		&ret.DeadTupleCount,
		&ret.DeadTupleLen,
		&ret.DeadTupleLenPretty,
		&ret.DeadTuplePercent,
		&ret.ApproxFreeSpace,
		&ret.ApproxFreeSpacePretty,
		&ret.ApproxFreePercent,
	)
	if err != nil {
		return nil, fmt.Errorf("scan | %w", err)
	}

	return &ret, nil
}

func (p *PgxPool) GetTablesDescribePartitions(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName,
	schemaName,
	tableName string,
	limit, offset int,
) ([]dto.TableDescribePartition, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetTablesDescribePartitions | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	return p.getTablesDescribePartitions(ctx, vNum, pool, schemaName, tableName, limit, offset)
}

func (p *PgxPool) getTablesDescribePartitions(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
	schemaName, tableName string,
	limit, offset int,
) ([]dto.TableDescribePartition, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryTablesDescribePartitions, nil)
	if err != nil {
		return nil, fmt.Errorf("query.Get | %w", err)
	}

	rows, err := pool.Query(ctx, qStr, schemaName, tableName, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query | %w", err)
	}

	ret := make([]dto.TableDescribePartition, 0, limit)

	for rows.Next() {
		var p dto.TableDescribePartition

		err = rows.Scan(
			&p.Schema,
			&p.Name,
			&p.PartitionExpression,
			&p.SizeBytes,
			&p.Size,
		)
		if err != nil {
			return nil, fmt.Errorf("scan | %w", err)
		}

		ret = append(ret, p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetTablesSchemas(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
) ([]string, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetTablesSchemas | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	return p.getTablesSchemas(ctx, vNum, pool)
}

func (p *PgxPool) getTablesSchemas(ctx context.Context, serverVersion int, pool *pgxpool.Pool) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryTablesDescribeSchemas, nil)
	if err != nil {
		return nil, fmt.Errorf("query.Get | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("query | %w", err)
	}

	ret := make([]string, 0, 10) //nolint:mnd

	for rows.Next() {
		var name string

		err = rows.Scan(&name)
		if err != nil {
			return nil, fmt.Errorf("scan | %w", err)
		}

		ret = append(ret, name)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetTablesSearch(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName,
	schemaName,
	q string,
	limit int,
) ([]string, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetTablesSearch | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	if limit <= 0 || limit > 200 {
		limit = 50
	}

	return p.getTablesSearch(ctx, vNum, pool, schemaName, q, limit)
}

func (p *PgxPool) getTablesSearch(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
	schemaName, q string,
	limit int,
) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryTablesDescribeSearch, nil)
	if err != nil {
		return nil, fmt.Errorf("query.Get | %w", err)
	}

	rows, err := pool.Query(ctx, qStr, schemaName, q, limit)
	if err != nil {
		return nil, fmt.Errorf("query | %w", err)
	}

	ret := make([]string, 0, 50) //nolint:mnd

	for rows.Next() {
		var name string

		err = rows.Scan(&name)
		if err != nil {
			return nil, fmt.Errorf("scan | %w", err)
		}

		ret = append(ret, name)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows | %w", err)
	}

	return ret, nil
}
