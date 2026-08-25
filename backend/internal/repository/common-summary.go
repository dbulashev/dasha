package repository

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

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

// pgssPool picks the pool to read pg_stat_statements through. The view is
// per-database — the extension may be created in one database only, and in a
// schema of its own there — while its contents are instance-wide. A named
// database is therefore a preference, not a constraint: reading the same
// instance-wide numbers through a database that lacks the extension would only
// fail, so any database that has it serves just as well. The named pool is kept
// as the last resort, to let the query report the real error.
func (p *PgxPool) pgssPool(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
) (*pgxpool.Pool, error) {
	item, err := p.pgssPoolItem(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, err
	}

	return item.pool, nil
}

// PgssDatabase reports which database the statistics are actually read through —
// the provenance an auto-snapshot has to record, since the requested database is
// only a preference (see pgssPool).
func (p *PgxPool) PgssDatabase(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
) (string, error) {
	item, err := p.pgssPoolItem(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return "", fmt.Errorf("PgssDatabase | %w", err)
	}

	return string(item.Database), nil
}

// PgssSource reports the extension the statistics are read through. Stored as
// snapshot provenance: the two extensions compute queryid differently. Empty
// when no source is resolved, so the snapshot records nothing rather than a guess.
func (p *PgxPool) PgssSource(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
) (string, error) {
	item, err := p.pgssPoolItem(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return "", fmt.Errorf("PgssSource | %w", err)
	}

	src := p.statsSource(ctx, item.pool)
	if !src.Present() {
		return "", nil
	}

	return src.Name(), nil
}

// pgssPoolItem implements the choice behind pgssPool: the first database
// carrying either query-statistics extension. Neither the extension version nor
// its readability is weighed in — an instance whose databases disagree is the
// DBA's to fix, and probing every database would cost a query each.
func (p *PgxPool) pgssPoolItem(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
) (pgxPoolItem, error) {
	items, err := p.getPoolItemsByClusterAndInstance(ctx, clusterName, instanceName)
	if err != nil {
		return pgxPoolItem{}, err
	}

	var named *pgxPoolItem

	if databaseName != "" {
		for i := range items {
			if string(items[i].Database) == databaseName {
				named = &items[i]

				break
			}
		}

		// A database that was asked for but does not exist stays an error, as it
		// was before the choice moved here — silently answering from another one
		// would hide a typo in the request.
		if named == nil {
			return pgxPoolItem{}, fmt.Errorf("%w | %s/%s/%s", ErrNotFound, clusterName, instanceName, databaseName)
		}

		if p.statsSource(ctx, named.pool).Present() {
			return *named, nil
		}
	}

	for i := range items {
		src := p.statsSource(ctx, items[i].pool)
		if !src.Present() {
			continue
		}

		if named != nil {
			p.logger.Debug("query statistics extension missing in the requested database, reading through another one",
				zap.String("cluster", clusterName),
				zap.String("instance", instanceName),
				zap.String("requested_database", databaseName),
				zap.String("database", string(items[i].Database)),
				zap.String("extension", src.Name()))
		}

		return items[i], nil
	}

	// Nowhere to be found: keep the named pool so the query reports the real
	// error, and fall back to a stable pick when no database was named.
	if named != nil {
		return *named, nil
	}

	return items[0], nil
}

// getPoolItemsByClusterAndInstance returns every per-database pool of one
// instance, ordered by database name so that a fallback pick — and the error a
// broken instance reports — stays the same between calls.
func (p *PgxPool) getPoolItemsByClusterAndInstance(
	ctx context.Context,
	clusterName,
	instanceName string,
) ([]pgxPoolItem, error) {
	err := p.ensurePool(ctx)
	if err != nil {
		return nil, fmt.Errorf("ensure pool | %w", err)
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	var items []pgxPoolItem

	for cluster, clusterItems := range p.pools {
		if cluster.String() != clusterName {
			continue
		}

		for _, item := range clusterItems {
			if item.Host.String() != instanceName {
				continue
			}

			items = append(items, item)
		}
	}

	if len(items) == 0 {
		return nil, fmt.Errorf("%w | %s/%s", ErrNotFound, clusterName, instanceName)
	}

	slices.SortFunc(items, func(a, b pgxPoolItem) int {
		return strings.Compare(a.Database.String(), b.Database.String())
	})

	return items, nil
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
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

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
			approxSizeBytes, amount     int64
		)

		err = rows.Scan(&namespace, &kind, &approxSize, &approxSizeBytes, &amount)
		if err != nil {
			return nil, fmt.Errorf("getCommonSummary | %w", err)
		}

		ret = append(ret, dto.CommonSummary{
			Namespace:       namespace,
			Kind:            kind,
			ApproxSize:      approxSize,
			ApproxSizeBytes: approxSizeBytes,
			Amount:          amount,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getCommonSummary | %w", err)
	}

	return ret, nil
}
