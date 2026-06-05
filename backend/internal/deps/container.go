package deps

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/samber/do"
	"github.com/spf13/viper"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/auth"
	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/discovery"
	"github.com/dbulashev/dasha/internal/metrics"
	"github.com/dbulashev/dasha/internal/pkg/pat"
	"github.com/dbulashev/dasha/internal/repository"
	"github.com/dbulashev/dasha/internal/storage"
)

type Container struct {
	i *do.Injector
}

func NewContainer() *Container {
	i := do.New()

	do.Provide(i, func(i *do.Injector) (*zap.Logger, error) {
		cfg := do.MustInvoke[*config.Config](i)

		return provideLogger(cfg.Debug), nil
	})

	do.Provide(i, func(_ *do.Injector) (*config.Config, error) {
		cfg, err := provideConfig()

		return cfg, err
	})

	do.Provide(i, func(i *do.Injector) (config.Clusters, error) {
		cfg := do.MustInvoke[*config.Config](i)

		return provideClusters(*cfg), nil
	})

	do.Provide(i, func(i *do.Injector) (repository.Repository, error) {
		cfg := do.MustInvoke[*config.Config](i)
		clusters := do.MustInvoke[config.Clusters](i)
		logger := do.MustInvoke[*zap.Logger](i)

		return provideRepository(*cfg, clusters, logger), nil
	})

	do.Provide(i, func(i *do.Injector) (*discovery.Engine, error) {
		cfg := do.MustInvoke[*config.Config](i)
		clusters := do.MustInvoke[config.Clusters](i)
		logger := do.MustInvoke[*zap.Logger](i)

		return provideDiscovery(cfg, clusters, logger), nil
	})

	do.Provide(i, func(i *do.Injector) (*metrics.Service, error) {
		cfg := do.MustInvoke[*config.Config](i)
		clusters := do.MustInvoke[config.Clusters](i)

		return metrics.NewService(cfg.HealthScore.Metrics, clusterMetaProvider{clusters: clusters})
	})

	return &Container{i: i}
}

// clusterMetaProvider adapts the cluster registry to metrics.MetadataProvider so
// the matcher can auto-map service-discovered clusters that the static targets[]
// list does not enumerate. Lives here (not in metrics) to keep the metrics
// package free of a config import (config already imports metrics).
type clusterMetaProvider struct {
	clusters config.Clusters
}

func (p clusterMetaProvider) LookupMeta(cluster, instance string) (metrics.DiscoveryMeta, bool) {
	all, err := p.clusters.Get(context.Background())
	if err != nil {
		return metrics.DiscoveryMeta{}, false
	}

	for i := range all {
		c := &all[i]
		if string(c.Name) != cluster {
			continue
		}

		for _, h := range c.Hosts {
			if string(h) == instance {
				return metrics.DiscoveryMeta{
					Source:     c.Source,
					ProviderID: c.ProviderID,
					Labels:     c.Labels,
				}, true
			}
		}
	}

	return metrics.DiscoveryMeta{}, false
}

func (c *Container) Config() *config.Config {
	return do.MustInvoke[*config.Config](c.i)
}

func (c *Container) Logger() *zap.Logger {
	return do.MustInvoke[*zap.Logger](c.i)
}

func (c *Container) Clusters() config.Clusters {
	return do.MustInvoke[config.Clusters](c.i)
}

func (c *Container) Repository() repository.Repository {
	return do.MustInvoke[repository.Repository](c.i)
}

func (c *Container) Discovery() *discovery.Engine {
	return do.MustInvoke[*discovery.Engine](c.i)
}

// Metrics returns the metrics-backed Health Score service, or nil when the
// feature is disabled (the returned *metrics.Service answers Enabled()==false).
func (c *Container) Metrics() *metrics.Service {
	return do.MustInvoke[*metrics.Service](c.i)
}

func (c *Container) AuthMiddlewares(ctx context.Context, resolver auth.PATResolver) (*auth.Middlewares, error) {
	cfg := c.Config()
	logger := c.Logger()

	return auth.NewMiddlewares(ctx, cfg.Auth, resolver, logger)
}

// patResolver adapts the snapshot storage to auth.PATResolver: it hashes the
// presented secret and looks it up, mapping an active token to its owner's
// identity and role. Lives here (not in auth) to keep auth free of a storage
// dependency.
type patResolver struct {
	storage *storage.Storage
	logger  *zap.Logger
}

func (r patResolver) ResolveToken(ctx context.Context, presented string) (*auth.UserContext, bool) {
	// Fast reject anything that isn't a personal access token before hitting the DB.
	if !strings.HasPrefix(presented, pat.Prefix) {
		return nil, false
	}

	idn, ok, err := r.storage.ResolveAPIToken(ctx, pat.Hash(presented))
	if err != nil {
		r.logger.Warn("personal access token resolve failed", zap.Error(err))

		return nil, false
	}

	if !ok {
		return nil, false
	}

	return &auth.UserContext{
		Name:       idn.Subject,
		Role:       idn.Role,
		AuthMethod: auth.MethodPAT,
	}, true
}

// NewPATResolver returns an auth.PATResolver backed by storage, or nil (PAT auth
// disabled) when storage is not configured.
func NewPATResolver(st *storage.Storage, logger *zap.Logger) auth.PATResolver {
	if st == nil {
		return nil
	}

	return patResolver{storage: st, logger: logger}
}

func provideLogger(debug bool) *zap.Logger {
	var (
		l   *zap.Logger
		err error
	)

	if debug {
		l, err = zap.NewDevelopment()
	} else {
		l, err = zap.NewProduction()
	}

	if err != nil {
		panic(err)
	}

	return l
}

func provideConfig() (*config.Config, error) {
	viper.SetConfigName("dasha")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("$HOME/.dasha")
	viper.AddConfigPath("/etc/dasha/")

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("provideConfig | failed to read config: %w", err)
	}

	var c config.Config

	err := viper.Unmarshal(&c)
	if err != nil {
		return nil, fmt.Errorf("provideConfig | %w", err)
	}

	for i := range c.Clusters {
		if c.Clusters[i].PasswordFromEnv != "" {
			c.Clusters[i].Password = os.Getenv(c.Clusters[i].PasswordFromEnv)
		}
	}

	for i := range c.Auth.Tokens {
		if c.Auth.Tokens[i].TokenFromEnv != "" {
			c.Auth.Tokens[i].Token = os.Getenv(c.Auth.Tokens[i].TokenFromEnv)
		}

		if c.Auth.Tokens[i].Role == "" {
			c.Auth.Tokens[i].Role = "viewer"
		}
	}

	if c.Auth.OIDC != nil && c.Auth.OIDC.ClientSecretFromEnv != "" {
		c.Auth.OIDC.ClientSecret = os.Getenv(c.Auth.OIDC.ClientSecretFromEnv)
	}

	if c.Auth.CookieSecretFromEnv != "" {
		c.Auth.CookieSecret = os.Getenv(c.Auth.CookieSecretFromEnv)
	}

	if c.Storage.DSNFromEnv != "" {
		c.Storage.DSN = os.Getenv(c.Storage.DSNFromEnv)
	}

	if c.Storage.DSNMigrationFromEnv != "" {
		c.Storage.DSNMigration = os.Getenv(c.Storage.DSNMigrationFromEnv)
	}

	if env := c.HealthScore.Metrics.Datasource.Auth.TokenFromEnv; env != "" {
		c.HealthScore.Metrics.Datasource.Auth.Token = os.Getenv(env)
	}

	if env := c.HealthScore.Metrics.Datasource.Auth.PasswordFromEnv; env != "" {
		c.HealthScore.Metrics.Datasource.Auth.Password = os.Getenv(env)
	}

	c.HealthScore.Metrics = c.HealthScore.Metrics.WithDefaults()

	if err := c.HealthScore.Metrics.Validate(); err != nil {
		return nil, fmt.Errorf("provideConfig | health_score.metrics: %w", err)
	}

	if err := c.Auth.Validate(); err != nil {
		return nil, fmt.Errorf("provideConfig | auth config: %w", err)
	}

	return &c, nil
}

func provideClusters(cfg config.Config) config.Clusters {
	return config.NewClustersFromConfig(cfg)
}

func provideRepository(cfg config.Config, clusters config.Clusters, logger *zap.Logger) repository.Repository {
	return repository.NewRepositoryPgxPool(clusters, cfg.PgStatsView, logger)
}

func provideDiscovery(cfg *config.Config, clusters config.Clusters, logger *zap.Logger) *discovery.Engine {
	if len(cfg.Discovery) == 0 {
		return nil
	}

	return discovery.NewEngine(cfg.Discovery, clusters, logger)
}
