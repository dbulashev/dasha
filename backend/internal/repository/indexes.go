package repository

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/enums"
	"github.com/dbulashev/dasha/internal/query"
)

func (p *PgxPool) GetIndexesBloat(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
	limit,
	offset int,
) ([]dto.IndexBloat, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetIndexesBloat | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	pgStatsView := p.resolvePgStatsView(ctx, pool)

	ret, err := p.getIndexesBloat(ctx, vNum, pool, limit, offset, pgStatsView)
	if err != nil {
		return nil, fmt.Errorf("getIndexesBloat | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetIndexesBtreeOnArray(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
) ([]dto.IndexBtreeOnArray, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetIndexesBtreeOnArray | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getIndexesBtreeOnArray(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getIndexesBtreeOnArray | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetIndexesCaching(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
	limit,
	offset int,
) ([]dto.IndexCaching, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetIndexesCaching | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getIndexesCaching(ctx, vNum, pool, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("getIndexesCaching | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetIndexesHitRate(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
) ([]dto.IndexHitRate, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetIndexesHitRate | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getIndexesHitRate(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getIndexesHitRate | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetIndexesInvalidOrNotReady(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
) ([]dto.IndexInvalidOrNotReady, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetIndexesInvalidOrNotReady | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getIndexesInvalidOrNotReady(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getIndexesInvalidOrNotReady | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetIndexesMissing(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
) ([]dto.IndexMissing, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetIndexesMissing | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getIndexesMissing(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getIndexesMissing | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetIndexesSimilar1(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
) ([]dto.IndexSimilar1, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetIndexesSimilar1 | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getIndexesSimilar1(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getIndexesSimilar1 | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetIndexesSimilar2(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
) ([]dto.IndexSimilar2, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetIndexesSimilar2 | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getIndexesSimilar2(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getIndexesSimilar2 | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetIndexesSimilar3(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
) ([]dto.IndexSimilar3, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetIndexesSimilar3 | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getIndexesSimilar3(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getIndexesSimilar3 | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetIndexesTopKBySize(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
) ([]dto.IndexTopKBySize, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetIndexesTopKBySize | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getIndexesTopKBySize(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getIndexesTopKBySize | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetIndexesUnused(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
	threshold,
	limit,
	offset int,
) ([]dto.IndexUnused, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetIndexesUnused | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getIndexesUnused(ctx, vNum, pool, threshold, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("getIndexesUnused | %w", err)
	}

	return ret, nil
}

// GetIndexesUnusedAllHosts queries all hosts in the cluster and returns indexes
// that are unused (idx_scan <= threshold) across ALL hosts.
func (p *PgxPool) GetIndexesUnusedAllHosts(
	ctx context.Context,
	clusterName,
	databaseName string,
	threshold,
	limit,
	offset int,
) ([]dto.IndexUnused, error) {
	allPools, err := p.getPoolsByClusterAndDatabase(ctx, clusterName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetIndexesUnusedAllHosts | %w", err)
	}

	// Query all hosts in parallel — get ALL indexes with scan counts (no threshold filter)
	type hostResult struct {
		items []dto.IndexUnused
		err   error
	}

	resultsCh := make(chan hostResult, len(allPools))

	var wg sync.WaitGroup

	for _, pool := range allPools {
		wg.Add(1)

		go func(pool *pgxpool.Pool) {
			defer wg.Done()

			vNum, err := p.getServerVersionNum(ctx, pool)
			if err != nil {
				resultsCh <- hostResult{err: err} //nolint:exhaustruct

				return
			}

			items, err := p.getIndexesAllScans(ctx, vNum, pool)
			resultsCh <- hostResult{items: items, err: err}
		}(pool)
	}

	wg.Wait()
	close(resultsCh)

	// Aggregate: take max(idx_scan) across hosts for each index
	type indexKey struct{ Schema, Table, Index string }

	type indexAgg struct {
		MaxScans int64
		MaxSize  int64
	}

	agg := make(map[indexKey]*indexAgg)

	for r := range resultsCh {
		if r.err != nil {
			p.logger.Warn("getIndexesAllScans on host", zap.Error(r.err))

			continue
		}

		for _, item := range r.items {
			k := indexKey{item.Schema, item.Table, item.Index}
			if a, ok := agg[k]; ok {
				if item.IndexScans > a.MaxScans {
					a.MaxScans = item.IndexScans
				}

				if item.SizeBytes > a.MaxSize {
					a.MaxSize = item.SizeBytes
				}
			} else {
				agg[k] = &indexAgg{MaxScans: item.IndexScans, MaxSize: item.SizeBytes}
			}
		}
	}

	// Filter: keep only indexes where max scans across all hosts <= threshold
	var all []dto.IndexUnused

	for k, a := range agg {
		if a.MaxScans <= int64(threshold) {
			all = append(all, dto.IndexUnused{
				Schema:     k.Schema,
				Table:      k.Table,
				Index:      k.Index,
				SizeBytes:  a.MaxSize,
				IndexScans: a.MaxScans,
			})
		}
	}

	// Sort by size descending
	sortIndexesUnusedBySize(all)

	// Apply pagination
	start := min(offset, len(all))
	end := min(start+limit, len(all))

	return all[start:end], nil
}

func (p *PgxPool) GetIndexesUsage(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
	limit,
	offset int,
) ([]dto.IndexUsage, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetIndexesUsage | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getIndexesUsage(ctx, vNum, pool, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("getIndexesUsage | %w", err)
	}

	return ret, nil
}

const defaultWastedBytesThreshold = 8192

func (p *PgxPool) getIndexesBloat(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
	limit,
	offset int,
	pgStatsView string,
) ([]dto.IndexBloat, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryIndexesBloat,
		struct{ PgStatsView string }{PgStatsView: pgStatsView})
	if err != nil {
		return nil, fmt.Errorf("getIndexesBloat | %w", err)
	}

	rows, err := pool.Query(ctx, qStr, defaultWastedBytesThreshold, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("getIndexesBloat | %w", err)
	}

	ret := make([]dto.IndexBloat, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			schema, table, index, definition string
			bloatBytes, indexBytes           int64
			primary                          bool
		)

		err = rows.Scan(&schema, &table, &index, &bloatBytes, &indexBytes, &definition, &primary)
		if err != nil {
			return nil, fmt.Errorf("getIndexesBloat | %w", err)
		}

		ret = append(ret, dto.IndexBloat{
			Schema:     schema,
			Table:      table,
			Index:      index,
			BloatBytes: bloatBytes,
			IndexBytes: indexBytes,
			Definition: definition,
			Primary:    primary,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getIndexesBloat | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getIndexesBtreeOnArray(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
) ([]dto.IndexBtreeOnArray, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryIndexesBtreeOnArray, nil)
	if err != nil {
		return nil, fmt.Errorf("getIndexesBtreeOnArray | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getIndexesBtreeOnArray | %w", err)
	}

	ret := make([]dto.IndexBtreeOnArray, 0, 10) //nolint:mnd

	for rows.Next() {
		var table, index string

		err = rows.Scan(&table, &index)
		if err != nil {
			return nil, fmt.Errorf("getIndexesBtreeOnArray | %w", err)
		}

		ret = append(ret, dto.IndexBtreeOnArray{
			Table: table,
			Index: index,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getIndexesBtreeOnArray | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getIndexesCaching(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
	limit,
	offset int,
) ([]dto.IndexCaching, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryIndexesCaching, nil)
	if err != nil {
		return nil, fmt.Errorf("getIndexesCaching | %w", err)
	}

	rows, err := pool.Query(ctx, qStr, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("getIndexesCaching | %w", err)
	}

	ret := make([]dto.IndexCaching, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			schema, table, index string
			hitRate              pgtype.Float8
		)

		err = rows.Scan(&schema, &table, &index, &hitRate)
		if err != nil {
			return nil, fmt.Errorf("getIndexesCaching | %w", err)
		}

		ret = append(ret, dto.IndexCaching{
			Schema:  schema,
			Table:   table,
			Index:   index,
			HitRate: hitRate.Float64,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getIndexesCaching | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getIndexesHitRate(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
) ([]dto.IndexHitRate, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryIndexesHitRate, nil)
	if err != nil {
		return nil, fmt.Errorf("getIndexesHitRate | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getIndexesHitRate | %w", err)
	}

	ret := make([]dto.IndexHitRate, 0, 1)

	for rows.Next() {
		var rate pgtype.Float8

		err = rows.Scan(&rate)
		if err != nil {
			return nil, fmt.Errorf("getIndexesHitRate | %w", err)
		}

		if rate.Valid {
			ret = append(ret, dto.IndexHitRate{
				Rate: rate.Float64,
			})
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getIndexesHitRate | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getIndexesInvalidOrNotReady(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
) ([]dto.IndexInvalidOrNotReady, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryIndexesInvalidOrNotReady, nil)
	if err != nil {
		return nil, fmt.Errorf("getIndexesInvalidOrNotReady | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getIndexesInvalidOrNotReady | %w", err)
	}

	ret := make([]dto.IndexInvalidOrNotReady, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			table, indexName, constraint pgtype.Text
			isValid, isReady             bool
		)

		err = rows.Scan(&table, &indexName, &isValid, &isReady, &constraint)
		if err != nil {
			return nil, fmt.Errorf("getIndexesInvalidOrNotReady | %w", err)
		}

		ret = append(ret, dto.IndexInvalidOrNotReady{
			Table:      table.String,
			IndexName:  indexName.String,
			IsValid:    isValid,
			IsReady:    isReady,
			Constraint: constraint.String,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getIndexesInvalidOrNotReady | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getIndexesMissing(ctx context.Context, serverVersion int, pool *pgxpool.Pool) ([]dto.IndexMissing, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryIndexesMissing, nil)
	if err != nil {
		return nil, fmt.Errorf("getIndexesMissing | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getIndexesMissing | %w", err)
	}

	ret := make([]dto.IndexMissing, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			schema, table           pgtype.Text
			percentOfTimesIndexUsed pgtype.Float8
			estimatedRows           int64
		)

		err = rows.Scan(&schema, &table, &percentOfTimesIndexUsed, &estimatedRows)
		if err != nil {
			return nil, fmt.Errorf("getIndexesMissing | %w", err)
		}

		var pct *float64
		if percentOfTimesIndexUsed.Valid {
			pct = &percentOfTimesIndexUsed.Float64
		}

		ret = append(ret, dto.IndexMissing{
			Schema:                  schema.String,
			Table:                   table.String,
			PercentOfTimesIndexUsed: pct,
			EstimatedRows:           estimatedRows,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getIndexesMissing | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getIndexesSimilar1(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
) ([]dto.IndexSimilar1, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryIndexesSimilar1, nil)
	if err != nil {
		return nil, fmt.Errorf("getIndexesSimilar1 | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getIndexesSimilar1 | %w", err)
	}

	ret := make([]dto.IndexSimilar1, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			table,
			i1UniqueIndexName,
			i2IndexName,
			i1UniqueIndexDefinition,
			i2IndexDefinition string
			i1UsedInConstraint,
			i2UsedInConstraint pgtype.Text
		)

		err = rows.Scan(&table,
			&i1UniqueIndexName,
			&i2IndexName,
			&i1UniqueIndexDefinition,
			&i2IndexDefinition,
			&i1UsedInConstraint,
			&i2UsedInConstraint,
		)
		if err != nil {
			return nil, fmt.Errorf("getIndexesSimilar1 | %w", err)
		}

		ret = append(ret, dto.IndexSimilar1{
			Table:                   table,
			I1UniqueIndexName:       i1UniqueIndexName,
			I2IndexName:             i2IndexName,
			I1UniqueIndexDefinition: i1UniqueIndexDefinition,
			I2IndexDefinition:       i2IndexDefinition,
			I1UsedInConstraint:      i1UsedInConstraint.String,
			I2UsedInConstraint:      i2UsedInConstraint.String,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getIndexesSimilar1 | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getIndexesSimilar2(ctx context.Context, serverVersion int, pool *pgxpool.Pool) ([]dto.IndexSimilar2, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryIndexesSimilar2, nil)
	if err != nil {
		return nil, fmt.Errorf("getIndexesSimilar2 | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getIndexesSimilar2 | %w", err)
	}

	ret := make([]dto.IndexSimilar2, 0, 10) //nolint: mnd

	for rows.Next() {
		var table, fkName, fkName2 string

		err = rows.Scan(&table, &fkName, &fkName2)
		if err != nil {
			return nil, fmt.Errorf("getIndexesSimilar2 | %w", err)
		}

		ret = append(ret, dto.IndexSimilar2{
			Table:   table,
			FkName:  fkName,
			FkName2: fkName2,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getIndexesSimilar2 | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getIndexesSimilar3(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
) ([]dto.IndexSimilar3, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryIndexesSimilar3, nil)
	if err != nil {
		return nil, fmt.Errorf("getIndexesSimilar3 | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getIndexesSimilar3 | %w", err)
	}

	ret := make([]dto.IndexSimilar3, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			table,
			i1IndexName,
			i2IndexName,
			simplifiedIndexDefinition,
			i1IndexDefinition,
			i2IndexDefinition string
			i1UsedInConstraint,
			i2UsedInConstraint pgtype.Text
		)

		err = rows.Scan(
			&table,
			&i1IndexName,
			&i2IndexName,
			&simplifiedIndexDefinition,
			&i1IndexDefinition,
			&i2IndexDefinition,
			&i1UsedInConstraint,
			&i2UsedInConstraint,
		)
		if err != nil {
			return nil, fmt.Errorf("getIndexesSimilar3 | %w", err)
		}

		ret = append(ret, dto.IndexSimilar3{
			Table:                     table,
			I1IndexName:               i1IndexName,
			I2IndexName:               i2IndexName,
			SimplifiedIndexDefinition: simplifiedIndexDefinition,
			I1IndexDefinition:         i1IndexDefinition,
			I2IndexDefinition:         i2IndexDefinition,
			I1UsedInConstraint:        i1UsedInConstraint.String,
			I2UsedInConstraint:        i2UsedInConstraint.String,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getIndexesSimilar3 | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getIndexesTopKBySize(ctx context.Context, serverVersion int, pool *pgxpool.Pool) ([]dto.IndexTopKBySize, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryIndexesTopKBySize, nil)
	if err != nil {
		return nil, fmt.Errorf("getIndexesTopKBySize | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getIndexesTopKBySize | %w", err)
	}

	ret := make([]dto.IndexTopKBySize, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			tablespace, table, index, size string
			sizeBytes                      int64
		)

		err = rows.Scan(&tablespace, &table, &index, &size, &sizeBytes)
		if err != nil {
			return nil, fmt.Errorf("getIndexesTopKBySize | %w", err)
		}

		ret = append(ret, dto.IndexTopKBySize{
			Tablespace: tablespace,
			Table:      table,
			Index:      index,
			Size:       size,
			SizeBytes:  sizeBytes,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getIndexesTopKBySize | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getIndexesUnused(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
	threshold,
	limit,
	offset int,
) ([]dto.IndexUnused, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryIndexesUnused, nil)
	if err != nil {
		return nil, fmt.Errorf("getIndexesUnused | %w", err)
	}

	rows, err := pool.Query(ctx, qStr, threshold, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("getIndexesUnused | %w", err)
	}

	ret := make([]dto.IndexUnused, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			schema, table, index  string
			sizeBytes, indexScans int64
		)

		err = rows.Scan(&schema, &table, &index, &sizeBytes, &indexScans)
		if err != nil {
			return nil, fmt.Errorf("getIndexesUnused | %w", err)
		}

		ret = append(ret, dto.IndexUnused{
			Schema:     schema,
			Table:      table,
			Index:      index,
			SizeBytes:  sizeBytes,
			IndexScans: indexScans,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getIndexesUnused | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getIndexesUsage(ctx context.Context, serverVersion int, pool *pgxpool.Pool, limit, offset int) ([]dto.IndexUsage, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryIndexesUsage, nil)
	if err != nil {
		return nil, fmt.Errorf("getIndexesUsage | %w", err)
	}

	rows, err := pool.Query(ctx, qStr, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("getIndexesUsage | %w", err)
	}

	ret := make([]dto.IndexUsage, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			schema, table           pgtype.Text
			percentOfTimesIndexUsed pgtype.Float8
			estimatedRows           int64
		)

		err = rows.Scan(&schema, &table, &percentOfTimesIndexUsed, &estimatedRows)
		if err != nil {
			return nil, fmt.Errorf("getIndexesUsage | %w", err)
		}

		var pct *float64
		if percentOfTimesIndexUsed.Valid {
			pct = &percentOfTimesIndexUsed.Float64
		}

		ret = append(ret, dto.IndexUsage{
			Schema:                  schema.String,
			Table:                   table.String,
			PercentOfTimesIndexUsed: pct,
			EstimatedRows:           estimatedRows,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getIndexesUsage | %w", err)
	}

	return ret, nil
}

// getIndexesAllScans returns all non-unique indexes with their scan counts (no threshold filter).
func (p *PgxPool) getIndexesAllScans(ctx context.Context, serverVersion int, pool *pgxpool.Pool) ([]dto.IndexUnused, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryIndexesAllScans, nil)
	if err != nil {
		return nil, fmt.Errorf("getIndexesAllScans | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getIndexesAllScans | %w", err)
	}

	ret := make([]dto.IndexUnused, 0, 100) //nolint:mnd

	for rows.Next() {
		var (
			schema, table, index  string
			sizeBytes, indexScans int64
		)

		err = rows.Scan(&schema, &table, &index, &sizeBytes, &indexScans)
		if err != nil {
			return nil, fmt.Errorf("getIndexesAllScans | %w", err)
		}

		ret = append(ret, dto.IndexUnused{
			Schema:     schema,
			Table:      table,
			Index:      index,
			SizeBytes:  sizeBytes,
			IndexScans: indexScans,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getIndexesAllScans | %w", err)
	}

	return ret, nil
}

func sortIndexesUnusedBySize(items []dto.IndexUnused) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].SizeBytes != items[j].SizeBytes {
			return items[i].SizeBytes > items[j].SizeBytes
		}

		return items[i].Index < items[j].Index
	})
}
