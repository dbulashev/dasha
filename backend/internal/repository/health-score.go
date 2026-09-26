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

	m, err := p.healthScoreMetrics(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("GetHealthScoreMetrics | %w", err)
	}

	return m, nil
}

func (p *PgxPool) healthScoreMetrics(ctx context.Context, pool *pgxpool.Pool) (*dto.HealthScoreMetrics, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	qStr, err := query.Get(vNum, enums.QueryCommonHealthScore, nil)
	if err != nil {
		return nil, err
	}

	conn, err := p.acquireConn(ctx, pool)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	var m dto.HealthScoreMetrics

	err = conn.QueryRow(ctx, qStr).Scan(
		&m.InRecovery,
		&m.Database,
		&m.TotalConnections,
		&m.ActiveConnections,
		&m.IdleInTransaction,
		&m.IdleInTransactionDatabase,
		&m.LongestTransactionSeconds,
		&m.LongestTransactionDatabase,
		&m.MaxConnections,
		&m.CacheHitRatio,
		&m.CacheSampleBlocks,
		&m.TrackIoTimingEnabled,
		&m.MaxDeadRatio,
		&m.AvgDeadRatio,
		&m.TablesHighBloat,
		&m.ReplicaCount,
		&m.MaxReplayLagSeconds,
		&m.MaxLagBytes,
		&m.DisconnectedReplicas,
		&m.MaxXidAge,
		&m.VacuumBacklogTables,
		&m.MaxOverdueVacuumAgeHours,
		&m.TablesNeverVacuumed,
		&m.AutovacuumEnabled,
		&m.TrackCountsEnabled,
		&m.TablesWithAutovacuumOff,
		&m.MaxRelfrozenxidAge,
		&m.HorizonLagXids,
		&m.HorizonDatabase,
		&m.TimedCheckpoints,
		&m.RequestedCheckpoints,
		&m.ActiveLockWaiters,
		&m.LongestLockWaitSeconds,
		&m.UngrantedLocks,
		&m.DeadlocksTotal,
		&m.HeavyweightLocksTotal,
		&m.MaxLocksPerTransaction,
		&m.HotUpdateRatio,
		&m.NewpageUpdateRatio,
		&m.StalePlannerStatsTables,
		&m.WalLevel,
		&m.LogicalSlotsActive,
	)
	if err != nil {
		return nil, err
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

	type collected struct {
		m   dto.HealthScoreDatabaseMetrics
		err error
	}

	got := make([]collected, len(pools))

	forEachLimited(p.healthDatabaseConcurrency(), pools, func(i int, dbp dbPool) {
		got[i].m, got[i].err = p.collectHealthScorePerDatabase(ctx, dbp.pool, dbp.database)
	})

	results := make([]dto.HealthScoreDatabaseMetrics, 0, len(pools))

	for i, c := range got {
		if c.err != nil {
			p.logger.Warn("GetHealthScorePerDatabase: skip database",
				zap.String("cluster", clusterName),
				zap.String("database", pools[i].database),
				zap.Error(c.err))

			continue
		}

		results = append(results, c.m)
	}

	slices.SortFunc(results, func(a, b dto.HealthScoreDatabaseMetrics) int {
		return strings.Compare(a.Database, b.Database)
	})

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
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return dto.HealthScoreDatabaseMetrics{}, fmt.Errorf("get server version | %w", err)
	}

	qStr, err := query.Get(vNum, enums.QueryCommonHealthScorePerDatabase, nil)
	if err != nil {
		return dto.HealthScoreDatabaseMetrics{}, fmt.Errorf("query.Get | %w", err)
	}

	conn, err := p.acquireConn(ctx, pool)
	if err != nil {
		return dto.HealthScoreDatabaseMetrics{}, err
	}
	defer conn.Release()

	var m dto.HealthScoreDatabaseMetrics

	err = conn.QueryRow(ctx, qStr).Scan(
		&m.Database,
		&m.SizeBytes,
		&m.CacheHitRatio,
		&m.CacheSampleBlocks,
		&m.MaxDeadRatio,
		&m.AvgDeadRatio,
		&m.TablesHighBloat,
		&m.MaxXidAge,
		&m.VacuumBacklogTables,
		&m.MaxOverdueVacuumAgeHours,
		&m.TablesNeverVacuumed,
		&m.HotUpdateRatio,
		&m.NewpageUpdateRatio,
	)
	if err != nil {
		return dto.HealthScoreDatabaseMetrics{}, fmt.Errorf("scan | %w", err)
	}

	if m.Database == "" {
		m.Database = databaseName
	}

	return m, nil
}
