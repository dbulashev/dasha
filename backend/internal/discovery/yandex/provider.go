package yandex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/discovery/filter"
)

const defaultRefreshInterval = 5 * time.Minute

var (
	errAuthorizedKeyRequired = errors.New("authorized_key is required")
	errFolderIDRequired      = errors.New("folder_id is required")
)

// Provider discovers PostgreSQL clusters in one Yandex Cloud folder.
type Provider struct {
	folderName string
	folderID   string
	username   string
	password   string
	interval   time.Duration
	// prefixName qualifies cluster names with the folder name, which only makes
	// sense when more than one folder is configured.
	prefixName bool
	sdk        *SDK
	filters    []filter.Filter
	logger     *zap.Logger
}

// NewProvider decodes a yandex-mdb discovery block and prepares the folder's SDK.
// The SDK is also stored in registry (when non-nil) so that other components —
// e.g. the logs service — reuse the same credentials.
func NewProvider(
	folderName string,
	raw map[string]any,
	registry *Registry,
	prefixName bool,
	logger *zap.Logger,
) (*Provider, error) {
	cfg, unused, err := config.DecodeDiscoveryConfig[config.YandexMDBConfig](raw)
	if err != nil {
		return nil, err
	}

	if len(unused) > 0 {
		logger.Warn("unknown keys in discovery config, ignored",
			zap.String("entry", folderName),
			zap.Strings("keys", unused),
		)
	}

	if cfg.AuthorizedKey == "" {
		return nil, errAuthorizedKeyRequired
	}

	if cfg.FolderID == "" {
		return nil, errFolderIDRequired
	}

	filters := make([]filter.Filter, 0, len(cfg.Clusters))

	for _, c := range cfg.Clusters {
		f, err := filter.New(c.Name, c.Db, c.ExcludeName, c.ExcludeDb)
		if err != nil {
			return nil, fmt.Errorf("compile filter: %w", err)
		}

		filters = append(filters, *f)
	}

	password := cfg.Password
	if cfg.PasswordFromEnv != "" {
		password = os.Getenv(cfg.PasswordFromEnv)

		if password == "" {
			logger.Warn("password_from_env is set but the variable is empty",
				zap.String("entry", folderName),
				zap.String("variable", cfg.PasswordFromEnv),
			)
		}
	}

	sdk, err := NewSDK(cfg.AuthorizedKey)
	if err != nil {
		return nil, fmt.Errorf("init yandex sdk: %w", err)
	}

	if registry != nil {
		registry.Register(cfg.FolderID, sdk)
	}

	interval := time.Duration(cfg.RefreshInterval) * time.Minute
	if cfg.RefreshInterval <= 0 {
		interval = defaultRefreshInterval
	}

	// The entry's own settings; the engine logs name, type and interval.
	logger.Info("yandex-mdb discovery configured",
		zap.String("entry", folderName),
		zap.String("folder_id", cfg.FolderID),
	)

	return &Provider{
		folderName: folderName,
		folderID:   cfg.FolderID,
		username:   cfg.User,
		password:   password,
		interval:   interval,
		prefixName: prefixName,
		sdk:        sdk,
		filters:    filters,
		logger:     logger,
	}, nil
}

// Interval returns the configured refresh interval.
func (p *Provider) Interval() time.Duration {
	return p.interval
}

// Discover lists the folder's clusters through the Yandex Cloud API.
func (p *Provider) Discover(ctx context.Context) ([]config.Cluster, error) {
	ycClusters, err := p.sdk.GetPostgreSQLClusters(ctx, p.folderID, p.filters, p.logger)
	if err != nil {
		return nil, fmt.Errorf("list clusters in folder %q: %w", p.folderName, err)
	}

	discovered := make([]config.Cluster, 0, len(ycClusters))

	for _, yc := range ycClusters {
		clusterName := yc.Name
		if p.prefixName {
			clusterName = p.folderName + "_" + clusterName
		}

		hosts := make([]config.Host, 0, len(yc.Hosts))
		hostNames := make([]string, 0, len(yc.Hosts))

		for _, h := range yc.Hosts {
			hosts = append(hosts, config.Host(h.Name))
			hostNames = append(hostNames, h.Name)
		}

		databases := make([]config.Database, 0, len(yc.Databases))
		dbNames := make([]string, 0, len(yc.Databases))

		for _, db := range yc.Databases {
			databases = append(databases, config.Database(db.Name))
			dbNames = append(dbNames, db.Name)
		}

		p.logger.Debug("discovered cluster",
			zap.String("folder", p.folderName),
			zap.String("cluster", clusterName),
			zap.Strings("hosts", hostNames),
			zap.Strings("databases", dbNames),
		)

		discovered = append(discovered, config.Cluster{
			Name:       config.ClusterName(clusterName),
			UserName:   p.username,
			Password:   p.password,
			Port:       YandexMDBPort,
			Databases:  databases,
			Hosts:      hosts,
			Source:     config.SourceYandexMDB,
			ProviderID: yc.ID,
			Labels: map[string]string{
				"folder_id": p.folderID,
				"folder":    p.folderName,
			},
		})
	}

	return discovered, nil
}
