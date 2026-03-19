package repository

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"maps"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/pkg/mapstruct"
)

const (
	poolConnectTimeout = 5 * time.Second
	queryTimeout       = 10 * time.Second
)

var ErrNotFound = errors.New("not found")

type Repository interface {
	Clusters(ctx context.Context) ([]dto.ClusterInfo, error)
	GetCommonSummary(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.CommonSummary, error)
	GetConnectionSources(ctx context.Context, clusterName, instanceName string, limit, offset int) ([]dto.ConnectionSources, error)
	GetConnectionStates(ctx context.Context, clusterName, instanceName string) ([]dto.ConnectionStates, error)
	GetConnectionStatActivity(
		ctx context.Context,
		clusterName,
		instanceName string,
		limit,
		offset int,
		username,
		state string,
	) ([]dto.ConnectionStatActivity, error)
	GetInvalidConstraints(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.InvalidConstraint, error)
	GetDatabaseSize(ctx context.Context, clusterName, instanceName, databaseName string) (*dto.DatabaseSize, error)
	GetStatsResetTime(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.StatsResetTime, error)
	GetPgssStatsResetTime(ctx context.Context, clusterName, instanceName, databaseName string) (*dto.StatsResetTime, error)
	GetFksPossibleNulls(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.FksPossibleNulls, error)
	GetFksPossibleSimilar(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.FksPossibleSimilar, error)
	GetFkTypeMismatch(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.FkTypeMismatch, error)
	GetIndexesBloat(ctx context.Context, clusterName, instanceName, databaseName string, limit, offset int) ([]dto.IndexBloat, error)
	GetIndexesBtreeOnArray(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.IndexBtreeOnArray, error)
	GetIndexesCaching(ctx context.Context, clusterName, instanceName, databaseName string, limit, offset int) ([]dto.IndexCaching, error)
	GetIndexesHitRate(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.IndexHitRate, error)
	GetIndexesInvalidOrNotReady(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.IndexInvalidOrNotReady, error)
	GetIndexesMissing(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.IndexMissing, error)
	GetIndexesSimilar1(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.IndexSimilar1, error)
	GetIndexesSimilar2(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.IndexSimilar2, error)
	GetIndexesSimilar3(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.IndexSimilar3, error)
	GetIndexesTopKBySize(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.IndexTopKBySize, error)
	GetIndexesUnused(ctx context.Context, clusterName, instanceName, databaseName string, threshold, limit, offset int) ([]dto.IndexUnused, error)
	GetIndexesUnusedAllHosts(ctx context.Context, clusterName, databaseName string, threshold, limit, offset int) ([]dto.IndexUnused, error)
	GetIndexesUsage(ctx context.Context, clusterName, instanceName, databaseName string, limit, offset int) ([]dto.IndexUsage, error)
	GetInstanceInfo(ctx context.Context, clusterName, instanceName string) (dto.InstanceInfo, error)
	GetPgSettings(ctx context.Context, clusterName, instanceName string, limit, offset int) ([]dto.PgSetting, error)
	GetAutovacuumSettings(ctx context.Context, clusterName, instanceName string) ([]dto.PgSetting, error)
	GetSettingsAnalyze(ctx context.Context, clusterName, instanceName string) ([]dto.SettingsNotification, error)
	GetMaintenanceAutovacuumFreezeMaxAge(
		ctx context.Context,
		clusterName,
		instanceName string,
	) ([]dto.MaintenanceAutovacuumFreezeMaxAge, error)
	GetMaintenanceInfo(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.MaintenanceInfo, error)
	GetMaintenanceTransactionIdDanger(
		ctx context.Context,
		clusterName,
		instanceName,
		databaseName string,
	) ([]dto.MaintenanceTransactionIdDanger, error)
	GetMaintenanceVacuumProgress(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.MaintenanceVacuumProgress, error)
	GetQueriesBlocked(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.QueryBlocked, error)
	GetQueriesRunning(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.QueryRunning, error)
	GetQueriesTop10ByTime(ctx context.Context, clusterName, instanceName string) ([]dto.QueryTop10ByTime, error)
	GetQueriesTop10ByWal(ctx context.Context, clusterName, instanceName string) ([]dto.QueryTop10ByWal, error)
	GetQueriesReport(ctx context.Context, clusterName, instanceName string) ([]dto.QueryReport, error)
	GetQueryStatsStatus(ctx context.Context, clusterName, instanceName, databaseName string) (dto.QueryStatsStatus, error)
	GetProgressAnalyze(ctx context.Context, clusterName, instanceName string) ([]dto.ProgressAnalyze, error)
	GetProgressBaseBackup(ctx context.Context, clusterName, instanceName string) ([]dto.ProgressBaseBackup, error)
	GetProgressCluster(ctx context.Context, clusterName, instanceName string) ([]dto.ProgressCluster, error)
	GetProgressIndex(ctx context.Context, clusterName, instanceName string) ([]dto.ProgressIndex, error)
	GetProgressVacuum(ctx context.Context, clusterName, instanceName string) ([]dto.ProgressVacuum, error)
	GetTablesTopKBySize(ctx context.Context, clusterName, instanceName, databaseName string, limit int) ([]dto.TableTopKBySize, error)
	GetTablesCaching(ctx context.Context, clusterName, instanceName, databaseName string, limit, offset int) ([]dto.TableCaching, error)
	GetTablesHitRate(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.TableHitRate, error)
	GetTablesPartitions(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.TablePartition, error)
}

type pgxPoolItem struct {
	Host     config.Host
	Database config.Database
	pool     *pgxpool.Pool
}

type PgxPools map[config.ClusterName][]pgxPoolItem

type PgxPool struct {
	mu       sync.RWMutex
	clusters config.Clusters
	pools    PgxPools
	logger   *zap.Logger
}

func NewRepositoryPgxPool(clusters config.Clusters, logger *zap.Logger) Repository {
	return &PgxPool{clusters: clusters, pools: PgxPools{}, mu: sync.RWMutex{}, logger: logger}
}

func (p *PgxPool) Clusters(ctx context.Context) ([]dto.ClusterInfo, error) {
	err := p.ensurePool(ctx)
	if err != nil {
		return nil, fmt.Errorf("ensure pool: %w", err)
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	var ret []dto.ClusterInfo

	for clusterName, pools := range p.pools {
		hosts := mapstruct.SliceUniqueMember(pools, func(i pgxPoolItem) config.Host {
			return i.Host
		})

		databases := mapstruct.SliceUniqueMember(pools, func(i pgxPoolItem) string {
			return string(i.Database)
		})

		instances := mapstruct.SliceMap(hosts, func(h config.Host) dto.Instance {
			return dto.Instance{HostName: h}
		})

		ret = append(ret, dto.ClusterInfo{
			Name:      clusterName,
			Instances: instances,
			Databases: databases,
		})
	}

	return ret, nil
}

type poolResult struct {
	clusterName config.ClusterName
	item        pgxPoolItem
}

// hostDbKey uniquely identifies a pool within a cluster.
type hostDbKey struct {
	Host config.Host
	Db   config.Database
}

func (p *PgxPool) ensurePool(ctx context.Context) error {
	cls, err := p.clusters.Get(ctx)
	if err != nil {
		return fmt.Errorf("config.Clusters.Get | %w", err)
	}

	// Build desired state: cluster -> set of host+db pairs.
	type connTask struct {
		cluster config.Cluster
		host    config.Host
		db      config.Database
	}

	desired := make(map[config.ClusterName]map[hostDbKey]bool)
	for _, cl := range cls {
		if desired[cl.Name] == nil {
			desired[cl.Name] = make(map[hostDbKey]bool)
		}

		for _, host := range cl.Hosts {
			for _, db := range cl.Databases {
				desired[cl.Name][hostDbKey{host, db}] = true
			}
		}
	}

	p.mu.Lock()

	// Find pools to remove (exist in p.pools but not in desired).
	var toClose []*pgxpool.Pool

	for clName, items := range p.pools {
		desiredSet := desired[clName]
		if desiredSet == nil {
			// Entire cluster removed.
			for _, item := range items {
				toClose = append(toClose, item.pool)
			}

			delete(p.pools, clName)
			p.logger.Debug("cluster removed from pool", zap.String("cluster", string(clName)))

			continue
		}
		// Check individual host+db pairs.
		var kept []pgxPoolItem

		for _, item := range items {
			key := hostDbKey{item.Host, item.Database}
			if desiredSet[key] {
				kept = append(kept, item)
			} else {
				toClose = append(toClose, item.pool)
				p.logger.Debug("pool removed",
					zap.String("cluster", string(clName)),
					zap.String("host", string(item.Host)),
					zap.String("database", string(item.Database)),
				)
			}
		}

		p.pools[clName] = kept
	}

	// Find missing pools to add.
	var tasks []connTask

	for _, cl := range cls {
		if _, ok := p.pools[cl.Name]; !ok {
			p.pools[cl.Name] = make([]pgxPoolItem, 0)
		}

		for _, host := range cl.Hosts {
			for _, db := range cl.Databases {
				found := false

				for _, pp := range p.pools[cl.Name] {
					if pp.Host == host && pp.Database == db {
						found = true

						break
					}
				}

				if !found {
					tasks = append(tasks, connTask{cluster: cl, host: host, db: db})
				}
			}
		}
	}

	p.mu.Unlock()

	// Close removed pools in background (Close waits for active queries).
	for _, pool := range toClose {
		go pool.Close()
	}

	if len(tasks) == 0 {
		return nil
	}

	// Connect to all missing pools in parallel.
	resultsCh := make(chan poolResult, len(tasks))

	var wg sync.WaitGroup
	for _, task := range tasks {
		wg.Add(1)

		go func(t connTask) {
			defer wg.Done()

			port := t.cluster.Port
			if port == "" {
				port = "5432"
			}

			dsn := fmt.Sprintf("postgres://%s:%s@%s/%s",
				t.cluster.UserName,
				t.cluster.Password,
				net.JoinHostPort(string(t.host), port),
				t.db,
			)

			pool, err := p.getPool(ctx, dsn)
			if err != nil {
				p.logger.Warn("failed to connect to database, skipping",
					zap.String("cluster", string(t.cluster.Name)),
					zap.String("host", string(t.host)),
					zap.String("database", string(t.db)),
					zap.Error(err),
				)

				return
			}

			p.logger.Debug("pool connected",
				zap.String("cluster", string(t.cluster.Name)),
				zap.String("host", string(t.host)),
				zap.String("database", string(t.db)),
			)

			resultsCh <- poolResult{
				clusterName: t.cluster.Name,
				item:        pgxPoolItem{Host: t.host, Database: t.db, pool: pool},
			}
		}(task)
	}

	wg.Wait()
	close(resultsCh)

	// Collect successful connections under lock.
	p.mu.Lock()
	for r := range resultsCh {
		p.pools[r.clusterName] = append(p.pools[r.clusterName], r.item)
	}
	p.mu.Unlock()

	return nil
}

func (p *PgxPool) getPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	databaseConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.ParseConfig | %w", err)
	}

	databaseConfig.ConnConfig.ConnectTimeout = poolConnectTimeout
	databaseConfig.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	maps.Copy(databaseConfig.ConnConfig.RuntimeParams, runtimeParams)

	connectCtx, cancel := context.WithTimeout(ctx, poolConnectTimeout)
	defer cancel()

	ret, err := pgxpool.NewWithConfig(connectCtx, databaseConfig)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.NewWithConfig | %w", err)
	}

	return ret, nil
}
