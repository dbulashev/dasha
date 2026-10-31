package postgres

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/discovery/filter"
	"github.com/dbulashev/dasha/internal/discovery/pgdb"
	"github.com/dbulashev/dasha/internal/pkg/mapstruct"
)

const (
	// matchAllClusters satisfies filter.New: this provider serves a single
	// cluster, so only the database patterns matter.
	matchAllClusters = ".*"
)

// Provider discovers the databases of one PostgreSQL cluster.
type Provider struct {
	name     string
	cfg      Config
	filter   *filter.Filter
	list     pgdb.Lister
	logger   *zap.Logger
	interval time.Duration
	// hostIdx is the host that answered last, so a dead first host is not
	// retried first every cycle. Only the discovery loop touches it, one cycle
	// at a time.
	hostIdx int
	// published is the database list of the previous cycle, kept to log what
	// changed. Same single-goroutine ownership as hostIdx.
	published []string
}

// NewProvider decodes a postgres discovery block. The entry name becomes the
// cluster name.
func NewProvider(name string, raw map[string]any, logger *zap.Logger) (*Provider, error) {
	cfg, unused, err := config.DecodeDiscoveryConfig[Config](raw)
	if err != nil {
		return nil, err
	}

	if len(unused) > 0 {
		logger.Warn("unknown keys in discovery config, ignored",
			zap.String("entry", name),
			zap.Strings("keys", unused),
		)
	}

	cfg = cfg.withDefaults()

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	if cfg.PasswordFromEnv != "" && cfg.Password == "" {
		logger.Warn("password_from_env is set but the variable is empty",
			zap.String("entry", name),
			zap.String("variable", cfg.PasswordFromEnv),
		)
	}

	interval, raised := config.DiscoveryInterval(cfg.RefreshInterval)
	if raised {
		logger.Warn("refresh_interval is below the minimum, raised",
			zap.String("entry", name),
			zap.Duration("configured", cfg.RefreshInterval),
			zap.Duration("interval", interval),
		)
	}

	f, err := filter.New(matchAllClusters, cfg.Db, nil, cfg.ExcludeDb)
	if err != nil {
		return nil, fmt.Errorf("compile filter: %w", err)
	}

	// The entry's own settings; the engine logs name, type and interval.
	logger.Info("postgres discovery configured",
		zap.String("entry", name),
		zap.Strings("hosts", cfg.Hosts),
		zap.String("port", cfg.Port),
		zap.String("bootstrap_db", cfg.BootstrapDB),
	)

	return &Provider{
		name:      name,
		cfg:       cfg,
		filter:    f,
		list:      pgdb.ListDatabases,
		logger:    logger,
		interval:  interval,
		hostIdx:   0,
		published: nil,
	}, nil
}

// Interval returns the configured refresh interval.
func (p *Provider) Interval() time.Duration {
	return p.interval
}

// Discover lists the cluster's databases and returns it as a single cluster.
func (p *Provider) Discover(ctx context.Context) ([]config.Cluster, error) {
	names, err := p.listDatabases(ctx)
	if err != nil {
		return nil, err
	}

	return p.clusters(names), nil
}

// clusters applies the db filters and wraps the result into one cluster.
func (p *Provider) clusters(names []string) []config.Cluster {
	databases := make([]config.Database, 0, len(names))
	kept := make([]string, 0, len(names))

	for _, name := range names {
		if p.filter.MatchDb(name) {
			databases = append(databases, config.Database(name))
			kept = append(kept, name)
		}
	}

	p.logChanges(kept)

	if len(databases) == 0 {
		p.logger.Warn("no databases left after filtering",
			zap.String("entry", p.name),
			zap.Int("candidates", len(names)),
		)
	}

	hosts := make([]config.Host, 0, len(p.cfg.Hosts))
	for _, h := range p.cfg.Hosts {
		hosts = append(hosts, config.Host(h))
	}

	return []config.Cluster{{
		Name:       config.ClusterName(p.name),
		UserName:   p.cfg.User,
		Password:   p.cfg.Password,
		Port:       p.cfg.Port,
		Databases:  databases,
		Hosts:      hosts,
		Source:     config.SourcePostgres,
		ProviderID: p.name,
		Labels:     map[string]string{"bootstrap_db": p.cfg.BootstrapDB},
	}}
}

// logChanges reports what this cycle added or dropped, so a CREATE/DROP
// DATABASE leaves a trace in the log and not only in the UI.
func (p *Provider) logChanges(current []string) {
	if p.published == nil {
		p.logger.Info("databases discovered",
			zap.String("entry", p.name),
			zap.Strings("databases", current),
		)

		p.published = current

		return
	}

	added := mapstruct.SliceMissing(current, p.published)
	removed := mapstruct.SliceMissing(p.published, current)

	if len(added) > 0 || len(removed) > 0 {
		p.logger.Info("database list changed",
			zap.String("entry", p.name),
			zap.Strings("added", added),
			zap.Strings("removed", removed),
		)
	}

	p.published = current
}

// listDatabases queries the first host that answers, starting from the one that
// answered last cycle.
func (p *Provider) listDatabases(ctx context.Context) ([]string, error) {
	names, idx, err := pgdb.ListFirst(ctx, p.list, p.targets(), p.hostIdx)
	if err != nil {
		return nil, fmt.Errorf("no host answered for entry %q: %w", p.name, err)
	}

	p.hostIdx = idx

	p.logger.Debug("databases listed",
		zap.String("entry", p.name),
		zap.String("host", p.cfg.Hosts[idx]),
		zap.Int("databases", len(names)),
	)

	return names, nil
}

func (p *Provider) targets() []pgdb.Target {
	out := make([]pgdb.Target, 0, len(p.cfg.Hosts))
	for _, h := range p.cfg.Hosts {
		out = append(out, pgdb.Target{
			Host:     h,
			Port:     p.cfg.Port,
			User:     p.cfg.User,
			Password: p.cfg.Password,
			Database: p.cfg.BootstrapDB,
		})
	}

	return out
}
