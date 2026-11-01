// Package discovery implements service discovery for PostgreSQL clusters.
package discovery

import (
	"context"
	"fmt"
	"io"
	"time"

	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/discovery/patroni"
	"github.com/dbulashev/dasha/internal/discovery/postgres"
	"github.com/dbulashev/dasha/internal/discovery/yandex"
)

const (
	discoveryTypeYandexMDB = config.SourceYandexMDB
	discoveryTypePostgres  = config.SourcePostgres
	discoveryTypePatroni   = config.SourcePatroni
)

// Provider discovers clusters from a single configured source. A provider that
// also implements io.Closer is closed when its loop stops.
type Provider interface {
	// Discover returns the current set of clusters for this source. A non-nil
	// error means the cycle failed: the engine then keeps whatever the source
	// published last instead of replacing it.
	Discover(ctx context.Context) ([]config.Cluster, error)
	// Interval is how often Discover is called.
	Interval() time.Duration
}

// Engine runs periodic discovery and updates the Clusters provider.
type Engine struct {
	cfg      map[string]config.DiscoveryEntry
	clusters config.Clusters
	registry *yandex.Registry
	logger   *zap.Logger
}

// NewEngine creates a discovery engine from the config's discovery section.
// The registry is populated with each folder's SDK so that other components
// (e.g. the logs service) can reuse the same credentials.
func NewEngine(
	cfg map[string]config.DiscoveryEntry,
	clusters config.Clusters,
	registry *yandex.Registry,
	logger *zap.Logger,
) *Engine {
	return &Engine{
		cfg:      cfg,
		clusters: clusters,
		registry: registry,
		logger:   logger,
	}
}

// Start launches background goroutines for each discovery entry.
// It returns immediately. Cancel the context to stop all discovery loops.
func (e *Engine) Start(ctx context.Context) error {
	if len(e.cfg) == 0 {
		return nil
	}

	prefixNames := prefixByType(e.cfg)

	for name, entry := range e.cfg {
		provider, err := e.newProvider(name, entry, prefixNames[entry.Type])
		if err != nil {
			e.logger.Warn("failed to start discovery",
				zap.String("entry", name),
				zap.String("type", entry.Type),
				zap.Error(err),
			)

			continue
		}

		go e.runLoop(ctx, name, provider)

		e.logger.Info("discovery started",
			zap.String("entry", name),
			zap.String("type", entry.Type),
			zap.Duration("interval", provider.Interval()),
		)
	}

	return nil
}

// countEntries returns how many discovery entries have the given type.
func countEntries(cfg map[string]config.DiscoveryEntry, entryType string) int {
	count := 0

	for _, entry := range cfg {
		if entry.Type == entryType {
			count++
		}
	}

	return count
}

// prefixByType reports, per discovery type, whether its cluster names are
// qualified with the entry name. Only entries of the same type are counted:
// adding an entry of another type must not rename monitored clusters.
func prefixByType(cfg map[string]config.DiscoveryEntry) map[string]bool {
	return map[string]bool{
		discoveryTypeYandexMDB: countEntries(cfg, discoveryTypeYandexMDB) > 1,
		discoveryTypePatroni:   countEntries(cfg, discoveryTypePatroni) > 1,
	}
}

// newProvider builds the provider for one discovery entry.
func (e *Engine) newProvider(name string, entry config.DiscoveryEntry, prefixName bool) (Provider, error) {
	switch entry.Type {
	case discoveryTypeYandexMDB:
		return yandex.NewProvider(name, entry.Config, e.registry, prefixName, e.logger)
	case discoveryTypePostgres:
		// The entry name is the cluster name, so it needs no qualifying.
		return postgres.NewProvider(name, entry.Config, e.logger)
	case discoveryTypePatroni:
		return patroni.NewProvider(name, entry.Config, prefixName, e.logger)
	default:
		return nil, fmt.Errorf("unknown discovery type %q", entry.Type)
	}
}

func (e *Engine) runLoop(ctx context.Context, name string, provider Provider) {
	defer closeProvider(name, provider, e.logger)

	// Run first discovery immediately.
	e.discover(ctx, name, provider)

	ticker := time.NewTicker(provider.Interval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			e.logger.Info("discovery stopped", zap.String("entry", name))

			return
		case <-ticker.C:
			e.discover(ctx, name, provider)
		}
	}
}

func closeProvider(name string, provider Provider, logger *zap.Logger) {
	c, ok := provider.(io.Closer)
	if !ok {
		return
	}

	if err := c.Close(); err != nil {
		logger.Warn("failed to close discovery provider", zap.String("entry", name), zap.Error(err))
	}
}

func (e *Engine) discover(ctx context.Context, name string, provider Provider) {
	discovered, err := provider.Discover(ctx)
	if err != nil {
		e.logger.Warn("discovery cycle failed", zap.String("entry", name), zap.Error(err))

		return
	}

	if rejected := e.clusters.UpdateSource(name, discovered); len(rejected) > 0 {
		e.logger.Warn("discovered clusters skipped, name already in use",
			zap.String("entry", name),
			zap.Strings("clusters", rejected),
		)
	}

	e.logger.Debug("discovery cycle completed",
		zap.String("entry", name),
		zap.Int("clusters", len(discovered)),
	)
}
