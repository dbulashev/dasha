// Package discovery implements service discovery for PostgreSQL clusters.
package discovery

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/discovery/filter"
	"github.com/dbulashev/dasha/internal/discovery/yandex"
)

const (
	defaultRefreshInterval = 5 // minutes
	discoveryTypeYandexMDB = "yandex-mdb"
)

// Engine runs periodic discovery and updates the Clusters provider.
type Engine struct {
	cfg      map[string]config.DiscoveryEntry
	clusters config.Clusters
	logger   *zap.Logger
}

// NewEngine creates a discovery engine from the config's discovery section.
func NewEngine(cfg map[string]config.DiscoveryEntry, clusters config.Clusters, logger *zap.Logger) *Engine {
	return &Engine{
		cfg:      cfg,
		clusters: clusters,
		logger:   logger,
	}
}

// Start launches background goroutines for each discovery entry.
// It returns immediately. Cancel the context to stop all discovery loops.
func (e *Engine) Start(ctx context.Context) error {
	if len(e.cfg) == 0 {
		return nil
	}

	multipleFolders := len(e.cfg) > 1

	for folderName, entry := range e.cfg {
		switch entry.Type {
		case discoveryTypeYandexMDB:
			if err := e.startYandexMDB(ctx, folderName, entry.Config, multipleFolders); err != nil {
				e.logger.Warn("failed to start yandex-mdb discovery",
					zap.String("folder", folderName), zap.Error(err))
			}
		default:
			e.logger.Warn("unknown discovery type, skipped",
				zap.String("folder", folderName), zap.String("type", entry.Type))
		}
	}

	return nil
}

func (e *Engine) startYandexMDB(ctx context.Context, folderName string, cfg config.YandexMDBConfig, prefixWithFolder bool) error {
	password := cfg.Password
	if cfg.PasswordFromEnv != "" {
		password = os.Getenv(cfg.PasswordFromEnv)
	}

	if cfg.AuthorizedKey == "" {
		return fmt.Errorf("authorized_key is required for folder %q", folderName)
	}

	if cfg.FolderID == "" {
		return fmt.Errorf("folder_id is required for folder %q", folderName)
	}

	sdk, err := yandex.NewSDK(cfg.AuthorizedKey)
	if err != nil {
		return fmt.Errorf("init yandex sdk for folder %q: %w", folderName, err)
	}

	filters := make([]filter.Filter, 0, len(cfg.Clusters))
	for _, c := range cfg.Clusters {
		filters = append(filters, *filter.New(c.Name, c.Db, c.ExcludeName, c.ExcludeDb))
	}

	interval := time.Duration(cfg.RefreshInterval) * time.Minute
	if cfg.RefreshInterval <= 0 {
		interval = time.Duration(defaultRefreshInterval) * time.Minute
	}

	go e.runLoop(ctx, sdk, folderName, cfg.FolderID, cfg.User, password, filters, interval, prefixWithFolder)

	e.logger.Info("yandex-mdb discovery started",
		zap.String("folder", folderName),
		zap.String("folder_id", cfg.FolderID),
		zap.Duration("interval", interval),
	)

	return nil
}

func (e *Engine) runLoop(
	ctx context.Context,
	sdk *yandex.SDK,
	folderName, folderID, username, password string,
	filters []filter.Filter,
	interval time.Duration,
	prefixWithFolder bool,
) {
	// Run first discovery immediately.
	e.discover(ctx, sdk, folderName, folderID, username, password, filters, prefixWithFolder)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			e.logger.Info("discovery engine stopped", zap.String("folder", folderName))

			return
		case <-ticker.C:
			e.discover(ctx, sdk, folderName, folderID, username, password, filters, prefixWithFolder)
		}
	}
}

func (e *Engine) discover(
	ctx context.Context,
	sdk *yandex.SDK,
	folderName, folderID, username, password string,
	filters []filter.Filter,
	prefixWithFolder bool,
) {
	ycClusters, err := sdk.GetPostgreSQLClusters(ctx, folderID, filters, e.logger)
	if err != nil {
		e.logger.Warn("discovery cycle failed", zap.String("folder", folderName), zap.Error(err))

		return
	}

	discovered := make([]config.Cluster, 0, len(ycClusters))
	for _, yc := range ycClusters {
		clusterName := yc.Name
		if prefixWithFolder {
			clusterName = folderName + "_" + clusterName
		}

		hosts := make([]config.Host, 0, len(yc.Hosts))
		for _, h := range yc.Hosts {
			hosts = append(hosts, config.Host(h.Name))
		}

		databases := make([]config.Database, 0, len(yc.Databases))
		for _, db := range yc.Databases {
			databases = append(databases, config.Database(db.Name))
		}

		hostNames := make([]string, 0, len(hosts))
		for _, h := range hosts {
			hostNames = append(hostNames, string(h))
		}

		dbNames := make([]string, 0, len(databases))
		for _, db := range databases {
			dbNames = append(dbNames, string(db))
		}

		e.logger.Debug("discovered cluster",
			zap.String("folder", folderName),
			zap.String("cluster", clusterName),
			zap.Strings("hosts", hostNames),
			zap.Strings("databases", dbNames),
		)

		discovered = append(discovered, config.Cluster{
			Name:       config.ClusterName(clusterName),
			UserName:   username,
			Password:   password,
			Port:       yandex.YandexMDBPort,
			Databases:  databases,
			Hosts:      hosts,
			Source:     discoveryTypeYandexMDB,
			ProviderID: yc.ID,
			Labels: map[string]string{
				"folder_id": folderID,
			},
		})
	}

	e.clusters.Update(discovered)
	e.logger.Debug("discovery cycle completed",
		zap.String("folder", folderName),
		zap.Int("clusters", len(discovered)),
	)
}
