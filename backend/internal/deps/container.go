package deps

import (
	"fmt"

	"github.com/samber/do"
	"github.com/spf13/viper"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/discovery"
	"github.com/dbulashev/dasha/internal/repository"
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
		clusters := do.MustInvoke[config.Clusters](i)
		logger := do.MustInvoke[*zap.Logger](i)

		return provideRepository(clusters, logger), nil
	})

	do.Provide(i, func(i *do.Injector) (*discovery.Engine, error) {
		cfg := do.MustInvoke[*config.Config](i)
		clusters := do.MustInvoke[config.Clusters](i)
		logger := do.MustInvoke[*zap.Logger](i)

		return provideDiscovery(cfg, clusters, logger), nil
	})

	return &Container{i: i}
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

	return &c, nil
}

func provideClusters(cfg config.Config) config.Clusters {
	return config.NewClustersFromConfig(cfg)
}

func provideRepository(clusters config.Clusters, logger *zap.Logger) repository.Repository {
	return repository.NewRepositoryPgxPool(clusters, logger)
}

func provideDiscovery(cfg *config.Config, clusters config.Clusters, logger *zap.Logger) *discovery.Engine {
	if len(cfg.Discovery) == 0 {
		return nil
	}

	return discovery.NewEngine(cfg.Discovery, clusters, logger)
}
