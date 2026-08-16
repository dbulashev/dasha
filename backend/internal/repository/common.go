package repository

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"maps"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/hotobjects"
	"github.com/dbulashev/dasha/internal/indexadvisor"
	"github.com/dbulashev/dasha/internal/pkg/mapstruct"
	"github.com/dbulashev/dasha/internal/schemalint"
	"github.com/dbulashev/dasha/internal/sqlparse"
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
	GetConnectionWaitEvents(ctx context.Context, clusterName, instanceName string) ([]dto.WaitEvent, error)
	GetDatabaseUsers(ctx context.Context, clusterName, instanceName string) ([]string, error)
	GetHealthScoreMetrics(ctx context.Context, clusterName, instanceName, databaseName string) (*dto.HealthScoreMetrics, error)
	GetHealthScorePerDatabase(ctx context.Context, clusterName, instanceName string) ([]dto.HealthScoreDatabaseMetrics, error)
	GetHealthScoreXidWraparoundDatabases(ctx context.Context, clusterName, instanceName string, limit, offset int) ([]dto.HealthScoreXidWraparoundDatabase, error)
	GetHealthScoreTablesAutovacuumOff(ctx context.Context, clusterName, instanceName, databaseName string, limit, offset int) ([]dto.HealthScoreTableReloption, error)
	GetHealthScoreLowHotUpdateTables(ctx context.Context, clusterName, instanceName, databaseName string, limit, offset int) ([]dto.HealthScoreLowHotUpdateTable, error)
	GetHealthScoreHighDeadRatioTables(ctx context.Context, clusterName, instanceName, databaseName string, limit, offset int) ([]dto.HealthScoreHighDeadRatioTable, error)
	GetHealthScoreHorizonBlockingSessions(ctx context.Context, clusterName, instanceName string, limit, offset int) ([]dto.HealthScoreHorizonBlockingSession, error)
	GetInvalidConstraints(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.InvalidConstraint, error)
	GetDatabaseHealth(ctx context.Context, clusterName, instanceName, databaseName string) (*dto.DatabaseHealth, error)
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
	GetIndexesUnused(
		ctx context.Context, clusterName,
		instanceName,
		databaseName string,
		threshold,
		limit,
		offset int,
	) ([]dto.IndexUnused, error)
	GetIndexesUnusedAllHosts(ctx context.Context, clusterName, databaseName string, threshold, limit, offset int) ([]dto.IndexUnused, error)
	GetIndexUnusedReport(ctx context.Context, clusterName, databaseName string) (dto.IndexClusterScans, error)
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
	GetMaintenanceInfo(
		ctx context.Context,
		clusterName,
		instanceName,
		databaseName string,
		tableName *string,
		limit,
		offset int,
	) ([]dto.MaintenanceInfo, error)
	GetMaintenanceTransactionIdDanger(
		ctx context.Context,
		clusterName,
		instanceName,
		databaseName string,
	) ([]dto.MaintenanceTransactionIdDanger, error)
	GetMaintenanceVacuumProgress(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.MaintenanceVacuumProgress, error)
	GetMaintenanceAutovacuumSummary(ctx context.Context, clusterName, instanceName, databaseName string) (*dto.MaintenanceAutovacuumSummary, error)
	GetHotSampleTables(ctx context.Context, clusterName, instanceName, databaseName string, schema, object *string) ([]hotobjects.AnchorRow, *time.Time, bool, error)
	GetHotSampleIndexes(ctx context.Context, clusterName, instanceName, databaseName string, schema, object *string) ([]hotobjects.AnchorRow, *time.Time, bool, error)
	GetIndexAdvisorReport(
		ctx context.Context,
		clusterName, instanceName, databaseName string,
		excludeUsers []string,
	) (indexadvisor.Report, error)
	GetSchemaLintReport(ctx context.Context, clusterName, instanceName, databaseName string) (schemalint.Report, error)
	GetSchemaLintSummary(ctx context.Context, clusterName, instanceName string) ([]schemalint.DatabaseSummary, error)
	GetSequenceHeadroom(ctx context.Context, clusterName, instanceName, databaseName string) (float64, bool, error)
	GetQueriesBlocked(ctx context.Context, clusterName, instanceName, databaseName, scope string) ([]dto.QueryBlocked, error)
	GetQueriesRunning(ctx context.Context, clusterName, instanceName, databaseName string, minDuration int, queryFilter *string, queryFilterMode string, username *string) ([]dto.QueryRunning, error)
	GetQueriesTop10ByTime(ctx context.Context, clusterName, instanceName, databaseName, scope string) ([]dto.QueryTop10ByTime, error)
	GetQueriesTop10ByWal(ctx context.Context, clusterName, instanceName, databaseName, scope string) ([]dto.QueryTop10ByWal, error)
	GetQueriesReport(ctx context.Context, clusterName, instanceName, databaseName string, excludeUsers []string, queryID *int64) ([]dto.QueryReport, error)
	GetQueriesTop10Chart(ctx context.Context, clusterName, instanceName, databaseName, scope string) ([]dto.QueryTop10ChartItem, error)
	PgssDatabase(ctx context.Context, clusterName, instanceName, databaseName string) (string, error)
	GetQueryStatsStatus(ctx context.Context, clusterName, instanceName, databaseName string) (dto.QueryStatsStatus, error)
	ResetQueryStats(ctx context.Context, clusterName, instanceName, databaseName string) error
	GetActiveConnectionCount(ctx context.Context, clusterName, instanceName string) (int, error)
	GetBlockedSessionCount(ctx context.Context, clusterName, instanceName, databaseName string) (int, error)
	GetProgressAnalyze(ctx context.Context, clusterName, instanceName string) ([]dto.ProgressAnalyze, error)
	GetProgressBaseBackup(ctx context.Context, clusterName, instanceName string) ([]dto.ProgressBaseBackup, error)
	GetProgressCluster(ctx context.Context, clusterName, instanceName string) ([]dto.ProgressCluster, error)
	GetProgressIndex(ctx context.Context, clusterName, instanceName string) ([]dto.ProgressIndex, error)
	GetProgressVacuum(ctx context.Context, clusterName, instanceName string) ([]dto.ProgressVacuum, error)
	GetTablesDescribe(ctx context.Context, clusterName, instanceName, databaseName, schemaName, tableName string) (*dto.TableDescribe, error)
	GetTablesDescribeBloat(ctx context.Context, clusterName, instanceName, databaseName, schemaName, tableName string) (*dto.TableDescribeBloat, error)
	GetTablesDescribePartitions(ctx context.Context, clusterName, instanceName, databaseName, schemaName, tableName string, limit, offset int) ([]dto.TableDescribePartition, error)
	GetTablesDescribeVacuumStats(ctx context.Context, clusterName, instanceName, databaseName, schemaName, tableName string) (*dto.VacuumStats, error)
	GetTablesDescribeRowEstimate(ctx context.Context, clusterName, instanceName, databaseName, schemaName, tableName string) (*dto.RowEstimate, error)
	GetPgstattupleAvailable(ctx context.Context, clusterName, instanceName, databaseName string) (bool, error)
	GetReplicationStatus(ctx context.Context, clusterName, instanceName string) ([]dto.ReplicationStatus, error)
	GetReplicationSlots(ctx context.Context, clusterName, instanceName string) ([]dto.ReplicationSlot, error)
	GetReplicationConfig(ctx context.Context, clusterName, instanceName string) (*dto.ReplicationConfig, error)
	GetTablesSchemas(ctx context.Context, clusterName, instanceName, databaseName string) ([]string, error)
	GetTablesSearch(ctx context.Context, clusterName, instanceName, databaseName, schemaName, q string, limit int) ([]string, error)
	GetTablesTopKBySize(ctx context.Context, clusterName, instanceName, databaseName string, limit int) ([]dto.TableTopKBySize, error)
	GetTablesCaching(ctx context.Context, clusterName, instanceName, databaseName string, limit, offset int) ([]dto.TableCaching, error)
	GetTablesHitRate(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.TableHitRate, error)
	GetTablesPartitions(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.TablePartition, error)
}

const defaultPgStatsView = "pg_catalog.pg_stats"

const defaultPgssResetFunc = "pg_stat_statements_reset"

// validPgIdentifier matches schema-qualified or plain SQL identifiers (no injection risk).
var validPgIdentifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*(\.[a-zA-Z_][a-zA-Z0-9_]*)?$`)

type pgxPoolItem struct {
	Host     config.Host
	Database config.Database
	pool     *pgxpool.Pool
}

type PgxPools map[config.ClusterName][]pgxPoolItem

type PgxPool struct {
	mu                    sync.RWMutex
	clusters              config.Clusters
	pools                 PgxPools
	logger                *zap.Logger
	pgStatsViewConfig     string   // configured pg_stats_view from global config
	resolvedPgStatsView   sync.Map // *pgxpool.Pool → string (resolved view name)
	resolvedExtSchemas    sync.Map // extSchemaKey → string (quoted extension schema)
	pgssResetFuncConfig   string   // configured pgss_reset_function from global config
	poolConfig            config.PoolConfig
	schemaLintConfig      schemalint.Config
	sequenceHeadroomCache sync.Map // cluster/instance/database → sequenceHeadroomEntry
	indexAdvisorConfig    indexadvisor.Config
	sqlParserOnce         sync.Once
	sqlParser             sqlparse.Parser // built on first use, see indexAdvisorParser
}

func NewRepositoryPgxPool(
	clusters config.Clusters,
	pgStatsView, pgssResetFunc string,
	poolCfg config.PoolConfig,
	schemaLintCfg schemalint.Config,
	indexAdvisorCfg indexadvisor.Config,
	logger *zap.Logger,
) Repository {
	return &PgxPool{
		clusters:            clusters,
		pools:               PgxPools{},
		mu:                  sync.RWMutex{},
		logger:              logger,
		pgStatsViewConfig:   pgStatsView,
		pgssResetFuncConfig: pgssResetFunc,
		poolConfig:          poolCfg,
		schemaLintConfig:    schemaLintCfg,
		indexAdvisorConfig:  indexAdvisorCfg,
	}
}

// pgssResetFunction returns the configured pgss reset function, or the extension's
// own pg_stat_statements_reset when unset/invalid — qualified with the schema the
// extension lives in, since that need not be on the search_path.
// A configured name is validated against validPgIdentifier (no injection risk) and
// used as written: it may carry its own schema (monitoring.reset_pgss), and without
// one it is resolved through the search_path, as its author intended.
func (p *PgxPool) pgssResetFunction(ctx context.Context, pool *pgxpool.Pool) string {
	fallback := func() string {
		return qualify(p.extensionSchema(ctx, pool, extPgss), defaultPgssResetFunc)
	}

	f := strings.TrimSpace(p.pgssResetFuncConfig)
	if f == "" {
		return fallback()
	}

	if !validPgIdentifier.MatchString(f) {
		p.logger.Warn("invalid pgss_reset_function, using default",
			zap.String("configured", f), zap.String("default", defaultPgssResetFunc))

		return fallback()
	}

	return f
}

func (p *PgxPool) Clusters(ctx context.Context) ([]dto.ClusterInfo, error) {
	err := p.ensurePool(ctx)
	if err != nil {
		return nil, fmt.Errorf("ensure pool: %w", err)
	}

	// Build cluster name -> source/capability lookups from the live cluster
	// provider so the API can expose where each cluster came from and whether
	// its logs are searchable.
	sources := make(map[config.ClusterName]string)
	supportsLogs := make(map[config.ClusterName]bool)

	if cls, cfgErr := p.clusters.Get(ctx); cfgErr == nil {
		for _, c := range cls {
			sources[c.Name] = c.Source
			supportsLogs[c.Name] = c.SupportsLogs()
		}
	} else {
		p.logger.Warn("clusters metadata lookup failed; source/supports_logs will be empty",
			zap.Error(cfgErr))
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
			Name:         clusterName,
			Source:       sources[clusterName],
			SupportsLogs: supportsLogs[clusterName],
			Instances:    instances,
			Databases:    databases,
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

		seen := make(map[hostDbKey]bool, len(items))

		for _, item := range items {
			key := hostDbKey{item.Host, item.Database}

			switch {
			case !desiredSet[key]:
				toClose = append(toClose, item.pool)
				p.logger.Debug("pool removed",
					zap.String("cluster", string(clName)),
					zap.String("host", string(item.Host)),
					zap.String("database", string(item.Database)),
				)
			case seen[key]:
				// Left over from two ensurePool calls that planned the same
				// connection before either had registered it. Nothing looks a
				// second pool up, so it would idle here forever.
				toClose = append(toClose, item.pool)
				p.logger.Debug("duplicate pool dropped",
					zap.String("cluster", string(clName)),
					zap.String("host", string(item.Host)),
					zap.String("database", string(item.Database)),
				)
			default:
				seen[key] = true

				kept = append(kept, item)
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
		p.forgetPool(pool)

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

			// url.URL escapes credentials; a hand-built string breaks on a
			// password containing @ : / or ?.
			dsn := url.URL{ //nolint:exhaustruct
				Scheme: "postgres",
				User:   url.UserPassword(t.cluster.UserName, t.cluster.Password),
				Host:   net.JoinHostPort(string(t.host), port),
				Path:   "/" + string(t.db),
			}

			pool, err := p.getPool(ctx, dsn.String())
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

	// Collect successful connections under lock. Planning happens without one, so
	// a concurrent ensurePool — six lookup paths call it, and a cold start has
	// them all racing — may have connected the same host+db and got here first.
	// The late pool is closed rather than appended: no lookup would ever reach it.
	var duplicates []*pgxpool.Pool

	p.mu.Lock()

	for r := range resultsCh {
		if p.hasPoolLocked(r.clusterName, r.item) {
			duplicates = append(duplicates, r.item.pool)

			continue
		}

		p.pools[r.clusterName] = append(p.pools[r.clusterName], r.item)
	}

	p.mu.Unlock()

	for _, pool := range duplicates {
		p.logger.Debug("duplicate pool closed after a concurrent connect")

		go pool.Close()
	}

	return nil
}

// hasPoolLocked reports whether a pool for this host+db is already registered.
// Callers must hold p.mu.
func (p *PgxPool) hasPoolLocked(clusterName config.ClusterName, item pgxPoolItem) bool {
	for _, existing := range p.pools[clusterName] {
		if existing.Host == item.Host && existing.Database == item.Database {
			return true
		}
	}

	return false
}

// Dasha opens one pool per (host, database) and connects as a single monitoring
// role, so pgx's default of MaxConns = max(4, NumCPU) multiplies badly behind a
// per-user connection pooler (e.g. Odyssey/PgBouncer pool_size). Default to a
// small pool with a short idle time so the footprint stays low and idle
// connections are returned to the pooler between dashboard refreshes;
// db_pool / autosnapshot_db_pool override per field.
const (
	defaultPoolMaxConns        = 4
	defaultPoolMaxConnIdleTime = 2 * time.Minute
)

func (p *PgxPool) getPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	databaseConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.ParseConfig | %w", err)
	}

	databaseConfig.ConnConfig.ConnectTimeout = poolConnectTimeout
	databaseConfig.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	// Conservative defaults (above), overridden per field by db_pool /
	// autosnapshot_db_pool when set (> 0).
	databaseConfig.MaxConns = defaultPoolMaxConns
	if p.poolConfig.MaxConns > 0 {
		databaseConfig.MaxConns = p.poolConfig.MaxConns
	}

	databaseConfig.MaxConnIdleTime = defaultPoolMaxConnIdleTime
	if p.poolConfig.MaxConnIdleTime > 0 {
		databaseConfig.MaxConnIdleTime = p.poolConfig.MaxConnIdleTime
	}

	if p.poolConfig.MaxConnLifetime > 0 {
		databaseConfig.MaxConnLifetime = p.poolConfig.MaxConnLifetime
	}

	if databaseConfig.ConnConfig.RuntimeParams == nil {
		databaseConfig.ConnConfig.RuntimeParams = make(map[string]string)
	}

	maps.Copy(databaseConfig.ConnConfig.RuntimeParams, runtimeParams)

	connectCtx, cancel := context.WithTimeout(ctx, poolConnectTimeout)
	defer cancel()

	ret, err := pgxpool.NewWithConfig(connectCtx, databaseConfig)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.NewWithConfig | %w", err)
	}

	return ret, nil
}

// Extensions whose objects the SQL templates address by name. Each is commonly
// installed into a dedicated schema (CREATE EXTENSION … SCHEMA ext), which is
// not on the default search_path.
const (
	extPgss        = "pg_stat_statements"
	extPgstattuple = "pgstattuple"
)

// extensionSchemaQuery returns the schema an extension is installed into.
const extensionSchemaQuery = `
SELECT n.nspname
FROM pg_catalog.pg_extension e
JOIN pg_catalog.pg_namespace n ON n.oid = e.extnamespace
WHERE e.extname::text = $1`

// extSchemaKey caches a resolved schema per pool, not per host: pg_extension is
// a per-database catalog, so the same instance can hold the extension in a
// different schema in every database.
type extSchemaKey struct {
	pool *pgxpool.Pool
	ext  string
}

// extSchemaEntry caches one lookup. A miss is held briefly, so CREATE EXTENSION
// does not need a Dasha restart to take effect; a hit is held long, since
// ALTER EXTENSION … SET SCHEMA is rare but must not require one either.
type extSchemaEntry struct {
	schema  string
	expires time.Time
}

const (
	extSchemaMissTTL = time.Minute
	extSchemaHitTTL  = time.Hour
)

// extensionSchema returns the quoted schema of an installed extension, or "" when
// it is absent (or the catalog is unreadable — the caller's query then fails on
// its own terms rather than here).
func (p *PgxPool) extensionSchema(ctx context.Context, pool *pgxpool.Pool, ext string) string {
	key := extSchemaKey{pool: pool, ext: ext}
	if v, ok := p.resolvedExtSchemas.Load(key); ok {
		entry := v.(extSchemaEntry)
		if time.Now().Before(entry.expires) {
			return entry.schema
		}
	}

	queryCtx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	var schema string

	err := pool.QueryRow(queryCtx, extensionSchemaQuery, ext).Scan(&schema)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			p.logger.Warn("extension schema lookup failed",
				zap.String("extension", ext), zap.Error(err))
		}

		p.resolvedExtSchemas.Store(key, extSchemaEntry{schema: "", expires: time.Now().Add(extSchemaMissTTL)})

		return ""
	}

	quoted := pgx.Identifier{schema}.Sanitize()
	p.resolvedExtSchemas.Store(key, extSchemaEntry{schema: quoted, expires: time.Now().Add(extSchemaHitTTL)})

	p.logger.Debug("extension schema resolved",
		zap.String("extension", ext), zap.String("schema", quoted))

	return quoted
}

// forgetPool drops the per-pool caches of a pool that is being closed. Both are
// keyed by the pool pointer, so without this a cluster whose hosts or databases
// churn — service discovery, a config reload — accumulates entries no lookup can
// ever reach again.
func (p *PgxPool) forgetPool(pool *pgxpool.Pool) {
	p.resolvedPgStatsView.Delete(pool)

	p.resolvedExtSchemas.Range(func(k, _ any) bool {
		if key, ok := k.(extSchemaKey); ok && key.pool == pool {
			p.resolvedExtSchemas.Delete(k)
		}

		return true
	})
}

// lockTimeout bounds how long a query may wait for a lock on a user object.
const lockTimeout = "1s"

// rollbackTimeout bounds the deferred rollback that runs after the request
// context is gone.
const rollbackTimeout = 5 * time.Second

// querier is the part of pgx.Tx that the lock-timeout callers use, so the same
// callback works when the query has to run outside a transaction.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// withLockTimeout runs fn inside a read-only transaction that gives up on locks
// quickly, so a table held by DDL (ALTER TABLE, VACUUM FULL, REINDEX) fails with
// SQLSTATE 55P03 — reported as 423 with the reason — instead of stalling until
// the query deadline and surfacing as an opaque timeout.
//
// SET LOCAL, not a session SET, and not a startup parameter: PgBouncer refuses a
// connection carrying an untracked startup parameter, and a transaction-pooling
// pooler discards session state between transactions. A transaction, though,
// always stays on one backend.
//
// A pooler in statement mode rejects BEGIN outright; there the query still runs,
// just without a lock timeout — as it did before this existed.
func (p *PgxPool) withLockTimeout(ctx context.Context, pool *pgxpool.Pool, fn func(querier) error) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly}) //nolint:exhaustruct
	if err != nil {
		p.logger.Debug("no transaction available, running without lock_timeout", zap.Error(err))

		return fn(pool)
	}

	// Read-only work, so always roll back: the transaction must not outlive the
	// request as an idle-in-transaction session holding locks of its own. The
	// rollback outlives a cancelled request context on purpose — otherwise pgx
	// destroys the connection instead of returning it to the pool — but on a
	// deadline of its own, so an unresponsive backend cannot hold the caller here.
	defer func() {
		rbCtx, rbCancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
		defer rbCancel()

		_ = tx.Rollback(rbCtx)
	}()

	if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout = '"+lockTimeout+"'"); err != nil {
		return fmt.Errorf("set lock_timeout | %w", err)
	}

	return fn(tx)
}

// qualify prefixes an object with its schema, leaving it bare when the schema is
// unknown — then the query behaves exactly as before schema resolution existed.
func qualify(schema, object string) string {
	if schema == "" {
		return object
	}

	return schema + "." + object
}

// pgssTemplateData carries the schema-qualified pg_stat_statements relations into
// the SQL templates. Qualifying in the SQL — rather than extending search_path on
// the connection — is what keeps this working behind a transaction-pooling pooler,
// which discards session state between transactions (Odyssey's pool_discard
// issues RESET ALL; PgBouncer hands the next query to another backend).
type pgssTemplateData struct {
	Pgss     string
	PgssInfo string
}

func (p *PgxPool) pgssTemplateData(ctx context.Context, pool *pgxpool.Pool) pgssTemplateData {
	schema := p.extensionSchema(ctx, pool, extPgss)

	return pgssTemplateData{
		Pgss:     qualify(schema, "pg_stat_statements"),
		PgssInfo: qualify(schema, "pg_stat_statements_info"),
	}
}

// pgstattupleTemplateData carries the schema-qualified pgstattuple functions.
type pgstattupleTemplateData struct {
	PgstattupleApprox string
}

func (p *PgxPool) pgstattupleTemplateData(ctx context.Context, pool *pgxpool.Pool) pgstattupleTemplateData {
	return pgstattupleTemplateData{
		PgstattupleApprox: qualify(p.extensionSchema(ctx, pool, extPgstattuple), "pgstattuple_approx"),
	}
}

// resolvePgStatsView checks whether the globally configured pg_stats view is accessible
// on the given pool and returns the view name to use in SQL templates.
// Results are cached per pool.
func (p *PgxPool) resolvePgStatsView(ctx context.Context, pool *pgxpool.Pool) string {
	if v, ok := p.resolvedPgStatsView.Load(pool); ok {
		return v.(string)
	}

	configured := p.pgStatsViewConfig
	if configured == "" || !validPgIdentifier.MatchString(configured) {
		if configured != "" {
			p.logger.Warn("pg_stats_view has invalid identifier format, using default",
				zap.String("pg_stats_view", configured))
		}

		p.resolvedPgStatsView.Store(pool, defaultPgStatsView)

		return defaultPgStatsView
	}

	// Check if the view is accessible
	checkCtx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	_, err := pool.Exec(checkCtx, "SELECT 1 FROM "+configured+" LIMIT 0")

	var result string
	if err != nil {
		p.logger.Warn("pg_stats_view not accessible, falling back to pg_catalog.pg_stats",
			zap.String("pg_stats_view", configured),
			zap.Error(err))

		result = defaultPgStatsView
	} else {
		p.logger.Debug("using custom pg_stats_view",
			zap.String("pg_stats_view", configured))

		result = configured
	}

	p.resolvedPgStatsView.Store(pool, result)

	return result
}
