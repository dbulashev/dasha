package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/enums"
	"github.com/dbulashev/dasha/internal/query"
)

// GetHealthScoreMetrics returns instance-level health metrics. When databaseName
// is non-empty, the query runs against that database's pool so that per-DB
// fields (cache_hit_ratio, dead_tuples, vacuum age) reflect the selected
// database; instance-wide fields (pg_stat_activity, replication, GUCs,
// pg_database) are unaffected. Pass "" to fall back to the first pool found
// for the (cluster, instance) pair — the previous behaviour.
func (p *PgxPool) GetHealthScoreMetrics(ctx context.Context, clusterName, instanceName, databaseName string) (*dto.HealthScoreMetrics, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetHealthScoreMetrics | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(vNum, enums.QueryCommonHealthScore, nil)
	if err != nil {
		return nil, fmt.Errorf("GetHealthScoreMetrics | %w", err)
	}

	var m dto.HealthScoreMetrics

	err = pool.QueryRow(ctx, qStr).Scan(
		&m.TotalConnections,
		&m.ActiveConnections,
		&m.IdleInTransaction,
		&m.LongestTransactionSeconds,
		&m.MaxConnections,
		&m.CacheHitRatio,
		&m.TrackIoTimingEnabled,
		&m.MaxDeadRatio,
		&m.AvgDeadRatio,
		&m.TablesHighBloat,
		&m.ReplicaCount,
		&m.MaxReplayLagSeconds,
		&m.MaxLagBytes,
		&m.DisconnectedReplicas,
		&m.MaxXidAge,
		&m.MaxVacuumAgeHours,
		&m.TablesNeverVacuumed,
		&m.AutovacuumEnabled,
		&m.TrackCountsEnabled,
		&m.TablesWithAutovacuumOff,
		&m.MaxRelfrozenxidAge,
		&m.HorizonLagXids,
		&m.TimedCheckpoints,
		&m.RequestedCheckpoints,
	)
	if err != nil {
		return nil, fmt.Errorf("GetHealthScoreMetrics | %w", err)
	}

	return &m, nil
}

// GetHealthScorePerDatabase iterates over all per-database pools configured for
// the given instance and collects per-DB metrics (Performance / Storage /
// Maintenance). Each query runs against its own database pool because
// pg_stat_user_tables / pg_statio_user_tables are scoped to the current database.
func (p *PgxPool) GetHealthScorePerDatabase(
	ctx context.Context,
	clusterName, instanceName string,
) ([]dto.HealthScoreDatabaseMetrics, error) {
	if err := p.ensurePool(ctx); err != nil {
		return nil, fmt.Errorf("GetHealthScorePerDatabase | %w", err)
	}

	type dbPool struct {
		database string
		pool     *pgxpool.Pool
	}

	p.mu.RLock()

	var pools []dbPool

	seen := make(map[string]bool)

	for cluster, items := range p.pools {
		if cluster.String() != clusterName {
			continue
		}

		for _, it := range items {
			if it.Host.String() != instanceName {
				continue
			}

			db := string(it.Database)
			if seen[db] {
				continue
			}

			seen[db] = true

			pools = append(pools, dbPool{database: db, pool: it.pool})
		}
	}

	p.mu.RUnlock()

	if len(pools) == 0 {
		return nil, fmt.Errorf("%w | %s/%s", ErrNotFound, clusterName, instanceName)
	}

	results := make([]dto.HealthScoreDatabaseMetrics, 0, len(pools))

	for _, dbp := range pools {
		m, err := p.collectHealthScorePerDatabase(ctx, dbp.pool, dbp.database)
		if err != nil {
			p.logger.Warn("GetHealthScorePerDatabase: skip database",
				zap.String("cluster", clusterName),
				zap.String("database", dbp.database),
				zap.Error(err))

			continue
		}

		results = append(results, m)
	}

	// All per-DB collections failed — surface as error so the handler returns
	// 5xx instead of an empty list that looks like a valid "no databases" state.
	if len(results) == 0 {
		return nil, fmt.Errorf("GetHealthScorePerDatabase | %s/%s: all %d database collections failed",
			clusterName, instanceName, len(pools))
	}

	return results, nil
}

func (p *PgxPool) collectHealthScorePerDatabase(
	ctx context.Context,
	pool *pgxpool.Pool,
	databaseName string,
) (dto.HealthScoreDatabaseMetrics, error) {
	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return dto.HealthScoreDatabaseMetrics{}, fmt.Errorf("get server version | %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(vNum, enums.QueryCommonHealthScorePerDatabase, nil)
	if err != nil {
		return dto.HealthScoreDatabaseMetrics{}, fmt.Errorf("query.Get | %w", err)
	}

	var m dto.HealthScoreDatabaseMetrics

	err = pool.QueryRow(ctx, qStr).Scan(
		&m.Database,
		&m.SizeBytes,
		&m.CacheHitRatio,
		&m.MaxDeadRatio,
		&m.AvgDeadRatio,
		&m.TablesHighBloat,
		&m.MaxXidAge,
		&m.MaxVacuumAgeHours,
		&m.TablesNeverVacuumed,
	)
	if err != nil {
		return dto.HealthScoreDatabaseMetrics{}, fmt.Errorf("scan | %w", err)
	}

	if m.Database == "" {
		m.Database = databaseName
	}

	return m, nil
}

